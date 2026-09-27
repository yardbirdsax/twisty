package quality

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

// tileCachePrecision is the coordinate-formatting precision used by every
// TileCache the build server constructs. Stated once here rather than
// repeated at each call site.
const tileCachePrecision = 3

// TileSetConfig configures a TileSet.
type TileSetConfig struct {
	CacheDir    string        // raw-tile-cache directory
	TileSize    float64       // tile edge length in degrees
	OverpassURL string        // Overpass API endpoint
	FetchDelay  time.Duration // delay between consecutive tile fetches; 0 means the 1s TileFetchConfig default
	TileReady   chan Tile     // signaled when a fetch lands a tile in cache; may be nil to disable signaling
}

// TileStatus is the outcome of partitioning a set of tiles by cache/fetch
// state, as returned by TileSet.Ensure.
type TileStatus struct {
	Cached  []Tile // already in the raw tile cache
	Pending []Tile // missing from the cache; a fetch has been kicked off
	Failed  []Tile // previously attempted and recorded as failed
}

// TileSet owns the bookkeeping around fetching and caching a working set of
// map tiles for the build server: the raw TileCache handle (constructed once,
// so Precision is stated in exactly one place), which tiles are currently
// being fetched (for stale-fetch cancellation), and which tiles have failed.
//
// TileSet does not own scoring — see TileScorer for that — only the
// cached/missing/failed bookkeeping that every tile-serving handler in
// route_build.go previously repeated inline.
type TileSet struct {
	Cache       *TileCache
	overpassURL string
	tileSize    float64
	fetchDelay  time.Duration
	tileReady   chan Tile

	failedTiles sync.Map // key: Tile, value: struct{}
	inFlight    sync.Map // key: Tile, value: context.CancelFunc; cancels an in-flight fetch for that tile
}

// NewTileSet builds a TileSet backed by a single TileCache for cfg.CacheDir,
// using tileCachePrecision.
func NewTileSet(cfg TileSetConfig) *TileSet {
	return &TileSet{
		Cache:       &TileCache{Dir: cfg.CacheDir, Precision: tileCachePrecision},
		overpassURL: cfg.OverpassURL,
		tileSize:    cfg.TileSize,
		fetchDelay:  cfg.FetchDelay,
		tileReady:   cfg.TileReady,
	}
}

// Ensure partitions tiles into cached, pending, and failed, kicking off an
// asynchronous fetch for any tile missing from the cache. cancelStale, when
// true, cancels any in-flight fetch for a tile not present in this call's
// tiles (used when the caller's viewport has moved on and stale fetches
// should be abandoned).
func (ts *TileSet) Ensure(tiles []Tile, cancelStale bool) TileStatus {
	var status TileStatus
	var missing []Tile

	for _, t := range tiles {
		if ts.Cache.Has(t) {
			status.Cached = append(status.Cached, t)
		} else {
			missing = append(missing, t)
			status.Pending = append(status.Pending, t)
		}
		if _, failed := ts.failedTiles.Load(t); failed {
			status.Failed = append(status.Failed, t)
		}
	}

	if len(missing) > 0 {
		go ts.FetchMissing(missing, cancelStale)
	}

	return status
}

// FetchMissing fetches tiles from Overpass, writing successes to the cache
// and recording failures in failedTiles. It tracks in-flight tiles so a
// concurrent call for the same tile is a no-op, and — when cancelStale is
// true — cancels any in-flight fetch for a tile not present in tiles.
//
// Each tile is fetched under its own cancellable context so a stale-sweep
// cancellation of one tile never affects sibling tiles claimed in the same
// call.
func (ts *TileSet) FetchMissing(tiles []Tile, cancelStale bool) {
	if cancelStale {
		incoming := make(map[Tile]struct{}, len(tiles))
		for _, t := range tiles {
			incoming[t] = struct{}{}
		}
		ts.inFlight.Range(func(key, val any) bool {
			t := key.(Tile)
			if _, keep := incoming[t]; !keep {
				fn := val.(context.CancelFunc)
				slog.Debug("cancelling stale tile fetch", "south", t.South, "west", t.West)
				fn()
				ts.inFlight.Delete(t)
			}
			return true
		})
	}

	type claimedTile struct {
		tile   Tile
		ctx    context.Context
		cancel context.CancelFunc
	}

	// Phase 1: claim all tiles upfront so inFlight is populated before any HTTP
	// request starts. Each tile gets its own independent context so that the stale
	// sweep can cancel individual tiles without affecting siblings.
	var claimed []claimedTile
	for _, t := range tiles {
		tileCtx, tileCancel := context.WithCancel(context.Background())
		if _, loaded := ts.inFlight.LoadOrStore(t, tileCancel); loaded {
			tileCancel() // immediately release, won't be used
			slog.Debug("FetchMissing: tile already in-flight, skipping", "south", t.South, "west", t.West)
		} else {
			claimed = append(claimed, claimedTile{tile: t, ctx: tileCtx, cancel: tileCancel})
			slog.Debug("FetchMissing: goroutine enqueuing tile", "south", t.South, "west", t.West)
		}
	}
	if len(claimed) == 0 {
		return
	}
	defer func() {
		for _, c := range claimed {
			if fn, ok := ts.inFlight.LoadAndDelete(c.tile); ok {
				fn.(context.CancelFunc)()
			}
		}
	}()

	fetchDelay := ts.fetchDelay
	if fetchDelay == 0 {
		fetchDelay = time.Second
	}

	if err := ts.Cache.EnsureDir(); err != nil {
		slog.Warn("failed to ensure tile cache dir", "dir", ts.Cache.Dir, "error", err)
	}

	cfg := TileFetchConfig{
		Endpoint:       ts.overpassURL,
		TileSize:       ts.tileSize,
		Cache:          ts.Cache,
		RateLimitDelay: fetchDelay,
	}

	// Phase 2: fetch each claimed tile using its own context so a stale-sweep
	// cancel on one tile does not affect the others.
	for _, c := range claimed {
		FetchTiledWaysForTiles(c.ctx, []Tile{c.tile}, cfg)
		if ts.Cache.Has(c.tile) {
			ts.failedTiles.Delete(c.tile)
			if ts.tileReady != nil {
				select {
				case ts.tileReady <- c.tile:
				default:
					slog.Warn("tileReady channel full, SSE push dropped", "south", c.tile.South, "west", c.tile.West)
				}
			}
		} else if c.ctx.Err() == nil {
			ts.failedTiles.Store(c.tile, struct{}{})
		}
	}
}

// IsFailed reports whether tile is currently recorded as failed.
func (ts *TileSet) IsFailed(t Tile) bool {
	_, failed := ts.failedTiles.Load(t)
	return failed
}

// MarkFailed records tile as failed, as if a fetch for it had failed. It
// exists mainly so tests (and callers restoring persisted state) can seed
// failedTiles without reaching into TileSet's internals.
func (ts *TileSet) MarkFailed(t Tile) {
	ts.failedTiles.Store(t, struct{}{})
}

// ClearCacheAndFailed removes tile's cache file (if any) and its failedTiles
// entry (if any), reporting whether either was cleared.
func (ts *TileSet) ClearCacheAndFailed(t Tile) bool {
	cleared := false
	if ts.Cache.Has(t) {
		if err := os.Remove(ts.Cache.Path(t)); err == nil {
			cleared = true
		}
	}
	if _, wasFailed := ts.failedTiles.LoadAndDelete(t); wasFailed {
		cleared = true
	}
	return cleared
}

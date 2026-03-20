package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Tile represents a rectangular geographic tile identified by its (South, West) corner.
type Tile struct {
	South float64
	West  float64
	North float64
	East  float64
}

// snapToGrid snaps a coordinate down to the nearest tile boundary.
func snapToGrid(coord, tileSize float64) float64 {
	return math.Floor(coord/tileSize) * tileSize
}

// ComputeTiles converts a center point and radius into a deterministic set of tile
// coordinates aligned to a global grid. tileSizeDeg is the size of each tile in degrees.
func ComputeTiles(centerLat, centerLon, radiusKm, tileSizeDeg float64) []Tile {
	latOffset := radiusKm / 111.32
	lonOffset := radiusKm / (111.32 * math.Cos(centerLat*math.Pi/180.0))

	south := centerLat - latOffset
	north := centerLat + latOffset
	west := centerLon - lonOffset
	east := centerLon + lonOffset

	gridSouth := snapToGrid(south, tileSizeDeg)
	gridWest := snapToGrid(west, tileSizeDeg)

	// Use index-based iteration to avoid floating-point accumulation errors.
	southIdx := int(math.Round(gridSouth / tileSizeDeg))
	westIdx := int(math.Round(gridWest / tileSizeDeg))

	// Count how many steps are needed in each direction.
	latSteps := 0
	for float64(southIdx+latSteps)*tileSizeDeg < north {
		latSteps++
	}
	lonSteps := 0
	for float64(westIdx+lonSteps)*tileSizeDeg < east {
		lonSteps++
	}

	var tiles []Tile
	for latI := 0; latI < latSteps; latI++ {
		s := float64(southIdx+latI) * tileSizeDeg
		for lonI := 0; lonI < lonSteps; lonI++ {
			w := float64(westIdx+lonI) * tileSizeDeg
			tiles = append(tiles, Tile{
				South: s,
				West:  w,
				North: s + tileSizeDeg,
				East:  w + tileSizeDeg,
			})
		}
	}
	return tiles
}

// TileCache is a file-based cache for raw Overpass JSON responses, keyed by tile coordinates.
type TileCache struct {
	Dir       string
	Precision int // decimal places for coordinate formatting
}

// EnsureDir creates the cache directory if it does not already exist.
func (c *TileCache) EnsureDir() error {
	return os.MkdirAll(c.Dir, 0o755)
}

// Path returns the full file path for the cache entry of the given tile.
func (c *TileCache) Path(t Tile) string {
	return filepath.Join(c.Dir, TileCacheKey(t, c.Precision))
}

// Has returns true if a cache file exists for the given tile.
func (c *TileCache) Has(t Tile) bool {
	_, err := os.Stat(c.Path(t))
	return err == nil
}

// Read reads the cached bytes for the given tile and updates the file's mtime.
func (c *TileCache) Read(t Tile) ([]byte, error) {
	data, err := os.ReadFile(c.Path(t))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	_ = os.Chtimes(c.Path(t), now, now)
	return data, nil
}

// Write atomically writes data to the cache file for the given tile.
func (c *TileCache) Write(t Tile, data []byte) error {
	target := c.Path(t)
	tmp, err := os.CreateTemp(c.Dir, "tile-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	renamed = true
	return nil
}

// ClearAll removes and recreates the cache directory.
func (c *TileCache) ClearAll() error {
	if err := os.RemoveAll(c.Dir); err != nil {
		return err
	}
	return os.MkdirAll(c.Dir, 0o755)
}

// PurgeOlderThan removes cache files whose mtime is older than the given age.
// It returns the number of files removed.
func (c *TileCache) PurgeOlderThan(age time.Duration) (int, error) {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return count, err
		}
		if time.Since(info.ModTime()) > age {
			if err := os.Remove(filepath.Join(c.Dir, entry.Name())); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

// TileCacheKey formats a tile's (South, West) corner into a deterministic string
// suitable for use as a file name. precision controls decimal places (default 3).
func TileCacheKey(t Tile, precision int) string {
	format := fmt.Sprintf("%%.%df", precision)
	south := fmt.Sprintf(format, t.South)
	west := fmt.Sprintf(format, t.West)
	return fmt.Sprintf("tile_%s_%s.json", south, west)
}

// fetchTileRaw performs a stateless per-tile Overpass fetch and returns the raw response bytes.
// Transient HTTP errors (429, 5xx) are returned as plain errors (retryable).
// Non-transient client errors (4xx other than 429) are wrapped in NonRetryable.
func fetchTileRaw(ctx context.Context, endpoint string, t Tile) ([]byte, error) {
	query := fmt.Sprintf(
		`[out:json][timeout:25];way`+HighwayFilter+`(%.6f,%.6f,%.6f,%.6f);out geom;`,
		t.South, t.West, t.North, t.East,
	)

	body := url.Values{}
	body.Set("data", query)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := overpassHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("overpass request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading overpass response: %w", err)
		}
		return raw, nil
	}

	// 429 and 5xx are transient and retryable.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, fmt.Errorf("overpass transient HTTP status: %d", resp.StatusCode)
	}

	// Other 4xx errors are non-retryable.
	return nil, NonRetryable(fmt.Errorf("overpass non-retryable HTTP status: %d", resp.StatusCode))
}

// TileFetchConfig holds configuration for the FetchTiledWays orchestrator.
type TileFetchConfig struct {
	Endpoint       string           // Overpass API base URL (default: overpassBaseURL)
	TileSize       float64          // tile edge length in degrees (default: 0.05)
	Cache          *TileCache
	NoCache        bool             // skip cache reads, still write
	Logger         *slog.Logger
	RateLimitDelay time.Duration    // delay between consecutive HTTP fetches (default: 1s)
	RetryDelay     time.Duration    // initial backoff delay for retries (default: 2s)
	Progress       ProgressReporter // nil → NoopProgressReporter
}

// FetchTiledWays fetches OSM highway ways for the area defined by centerLat, centerLon, and
// radiusKm. It divides the area into tiles, checks the cache for each, fetches missing tiles
// with retry/backoff, and returns the merged, deduplicated set of Ways.
func FetchTiledWays(ctx context.Context, centerLat, centerLon, radiusKm float64, cfg TileFetchConfig) ([]Way, error) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = overpassBaseURL
	}
	if cfg.TileSize == 0 {
		cfg.TileSize = 0.05
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.RateLimitDelay == 0 {
		cfg.RateLimitDelay = 1 * time.Second
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 2 * time.Second
	}
	if cfg.Progress == nil {
		cfg.Progress = NoopProgressReporter{}
	}

	defer cfg.Progress.Done()

	tiles := ComputeTiles(centerLat, centerLon, radiusKm, cfg.TileSize)
	cfg.Logger.Info("starting tiled fetch", "total_tiles", len(tiles))
	cfg.Progress.SetTotal(len(tiles))

	if err := cfg.Cache.EnsureDir(); err != nil {
		return nil, fmt.Errorf("ensuring cache dir: %w", err)
	}

	cacheHits := 0
	fetched := 0
	failed := 0

	var allTileData [][]byte

	for _, tile := range tiles {
		var raw []byte
		cached := false

		if !cfg.NoCache && cfg.Cache.Has(tile) {
			cfg.Logger.Debug("cache hit", "south", tile.South, "west", tile.West)
			var err error
			raw, err = cfg.Cache.Read(tile)
			if err != nil {
				cfg.Logger.Warn("cache read error, will refetch", "south", tile.South, "west", tile.West, "error", err)
				raw = nil
			} else {
				cacheHits++
				cached = true
			}
		}

		if raw == nil {
			attempt := 0
			err := retryWithBackoff(ctx, 3, cfg.RetryDelay, func() error {
				if attempt > 0 {
					cfg.Progress.Retry()
				}
				attempt++
				start := time.Now()
				var e error
				raw, e = fetchTileRaw(ctx, cfg.Endpoint, tile)
				cfg.Progress.FetchDuration(time.Since(start))
				return e
			})
			if err != nil {
				cfg.Logger.Warn("tile fetch failed", "south", tile.South, "west", tile.West, "error", err)
				failed++
				raw = nil
			} else {
				if writeErr := cfg.Cache.Write(tile, raw); writeErr != nil {
					cfg.Logger.Warn("cache write error", "south", tile.South, "west", tile.West, "error", writeErr)
				}

				// Rate-limit: wait 1 second between consecutive fetches.
				// Increment fetched only after the sleep succeeds so the summary
				// log is accurate when context is cancelled during sleep.
				if sleepErr := sleepWithContext(ctx, cfg.RateLimitDelay); sleepErr != nil {
					// The tile was successfully fetched and written; count it before
					// returning so progress accounting includes this tile.
					cfg.Progress.Tick(cached)
					allTileData = append(allTileData, raw)
					// Merge whatever we have so far before returning the context error.
					ways, mergeErr := mergeAndDeduplicate(allTileData, cfg.Logger)
					if mergeErr != nil {
						return nil, mergeErr
					}
					return ways, sleepErr
				}
				fetched++
			}
		}

		if raw == nil {
			continue
		}

		cfg.Progress.Tick(cached)
		allTileData = append(allTileData, raw)
	}

	cfg.Logger.Info("tiled fetch complete",
		"total_tiles", len(tiles),
		"cache_hits", cacheHits,
		"fetched", fetched,
		"failed", failed,
	)

	return mergeAndDeduplicate(allTileData, cfg.Logger)
}

// ParseTileData parses raw Overpass JSON bytes into a slice of Ways.
func ParseTileData(data []byte) ([]Way, error) {
	return parseTileData(data)
}

// parseTileData parses raw Overpass JSON bytes into a slice of Ways.
func parseTileData(data []byte) ([]Way, error) {
	var oResp overpassResponse
	if err := json.Unmarshal(data, &oResp); err != nil {
		return nil, fmt.Errorf("parsing overpass response: %w", err)
	}
	return elementsToWays(oResp.Elements), nil
}

// mergeAndDeduplicate parses raw tile data from multiple tiles and returns a deduplicated
// slice of Ways. Ways with the same OSM ID are deduplicated (first-encountered wins).
// Ways with ID 0 are never deduplicated against each other.
func mergeAndDeduplicate(tileData [][]byte, logger *slog.Logger) ([]Way, error) {
	seen := make(map[int64]struct{})
	var result []Way
	totalWays := 0
	zeroIDCount := 0

	for _, raw := range tileData {
		ways, err := parseTileData(raw)
		if err != nil {
			return nil, err
		}
		for _, w := range ways {
			totalWays++
			if w.ID == 0 {
				zeroIDCount++
				result = append(result, w)
				continue
			}
			if _, exists := seen[w.ID]; !exists {
				seen[w.ID] = struct{}{}
				result = append(result, w)
			}
		}
	}

	duplicates := totalWays - len(result)
	logger.Info(fmt.Sprintf("merged %d ways from %d tiles (%d duplicates removed)", len(result), len(tileData), duplicates))
	if zeroIDCount > 0 {
		logger.Warn("encountered ways with ID 0", "count", zeroIDCount)
	}

	if result == nil {
		result = []Way{}
	}

	return result, nil
}

// sleepWithContext sleeps for duration d or until ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

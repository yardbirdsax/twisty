package quality

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/geo"
)

func TestSnapToGrid(t *testing.T) {
	tileSize := 0.05

	tests := []struct {
		name     string
		coord    float64
		expected float64
	}{
		{"positive value snaps down", 42.37, 42.35},
		{"negative value snaps down", -72.58, -72.60},
		{"exact boundary stays", 42.35, 42.35},
		{"exact negative boundary stays", -72.60, -72.60},
		{"zero stays", 0.0, 0.0},
		{"just above boundary snaps to boundary", 42.350001, 42.35},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := snapToGrid(tc.coord, tileSize)
			if math.Abs(got-tc.expected) > 1e-9 {
				t.Errorf("snapToGrid(%v, %v) = %v, want %v", tc.coord, tileSize, got, tc.expected)
			}
		})
	}
}

func TestComputeTilesSingleTile(t *testing.T) {
	// A very small radius (0.01 km) centered well inside a tile should produce exactly one tile.
	// Center at 42.36, -72.58; 0.01 km radius ~ 0.00009 degrees, fits inside a 0.05 tile.
	tiles := ComputeTiles(42.36, -72.58, 0.01, 0.05)
	if len(tiles) != 1 {
		t.Errorf("expected 1 tile, got %d", len(tiles))
	}
}

func TestComputeTilesMultipleTiles(t *testing.T) {
	// Center at 42.36, -72.58, 5 km radius, 0.05 degree tiles.
	// latOffset = 5/111.32 ≈ 0.0449 degrees
	// lonOffset = 5/(111.32 * cos(42.36°)) ≈ 0.0607 degrees
	// bounding box spans ~0.09 lat x ~0.12 lon, so we expect multiple tiles.
	tiles := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	if len(tiles) != 9 {
		t.Errorf("expected 9 tiles for 5km radius, got %d", len(tiles))
	}

	// Verify all tiles are properly sized.
	for i, tile := range tiles {
		if math.Abs((tile.North-tile.South)-0.05) > 1e-9 {
			t.Errorf("tile %d: height = %v, want 0.05", i, tile.North-tile.South)
		}
		if math.Abs((tile.East-tile.West)-0.05) > 1e-9 {
			t.Errorf("tile %d: width = %v, want 0.05", i, tile.East-tile.West)
		}
	}

	// Verify tiles are grid-aligned (South and West should be multiples of 0.05).
	// snapToGrid ensures the value is math.Floor(coord/tileSize)*tileSize; the inverse
	// check is: coord mod tileSize should be ~0 (or ~tileSize for negative modulo artifacts).
	for i, tile := range tiles {
		southMod := math.Abs(tile.South - snapToGrid(tile.South, 0.05))
		if southMod > 1e-9 {
			t.Errorf("tile %d: South=%v is not aligned to 0.05 grid (mod=%v)", i, tile.South, southMod)
		}
		westMod := math.Abs(tile.West - snapToGrid(tile.West, 0.05))
		if westMod > 1e-9 {
			t.Errorf("tile %d: West=%v is not aligned to 0.05 grid (mod=%v)", i, tile.West, westMod)
		}
	}
}

func TestComputeTilesDeterministic(t *testing.T) {
	// Same inputs must always produce the same tiles.
	tiles1 := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	tiles2 := ComputeTiles(42.36, -72.58, 5.0, 0.05)

	if len(tiles1) != len(tiles2) {
		t.Fatalf("non-deterministic: call 1 returned %d tiles, call 2 returned %d", len(tiles1), len(tiles2))
	}
	for i := range tiles1 {
		if tiles1[i] != tiles2[i] {
			t.Errorf("tile %d differs between calls: %v vs %v", i, tiles1[i], tiles2[i])
		}
	}
}

func TestComputeTilesShiftedCenter(t *testing.T) {
	// Two calls with slightly shifted centers but overlapping areas should share tiles.
	tiles1 := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	tiles2 := ComputeTiles(42.362, -72.578, 5.0, 0.05)

	set1 := make(map[Tile]bool)
	for _, tile := range tiles1 {
		set1[tile] = true
	}

	shared := 0
	for _, tile := range tiles2 {
		if set1[tile] {
			shared++
		}
	}

	if shared == 0 {
		t.Error("expected overlapping tile sets for nearby centers, but found no shared tiles")
	}
}

var testTile = Tile{South: 42.350, West: -72.600, North: 42.400, East: -72.550}

func newTestCache(t *testing.T) *TileCache {
	t.Helper()
	return &TileCache{Dir: t.TempDir(), Precision: 3}
}

func TestTileCacheWriteAndRead(t *testing.T) {
	c := newTestCache(t)
	data := []byte(`{"elements":[]}`)
	if err := c.Write(testTile, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := c.Read(testTile)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Read returned %q, want %q", got, data)
	}
}

func TestTileCacheHas(t *testing.T) {
	c := newTestCache(t)
	if c.Has(testTile) {
		t.Error("Has returned true before Write")
	}
	if err := c.Write(testTile, []byte(`{}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !c.Has(testTile) {
		t.Error("Has returned false after Write")
	}
}

func TestTileCacheReadUpdatesMtime(t *testing.T) {
	c := newTestCache(t)
	if err := c.Write(testTile, []byte(`{}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	past := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(c.Path(testTile), past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if _, err := c.Read(testTile); err != nil {
		t.Fatalf("Read: %v", err)
	}
	info, err := os.Stat(c.Path(testTile))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if time.Since(info.ModTime()) > 5*time.Second {
		t.Errorf("mtime not updated after Read: mtime=%v", info.ModTime())
	}
}

func TestTileCacheAtomicWrite(t *testing.T) {
	c := newTestCache(t)
	want := []byte(`{"elements":[]}`)
	if err := c.Write(testTile, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file found: %s", e.Name())
		}
	}
	if !c.Has(testTile) {
		t.Error("Has returned false after Write")
	}
	got, err := c.Read(testTile)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("Read returned %q, want %q", got, want)
	}
}

func TestTileCacheClearAll(t *testing.T) {
	c := newTestCache(t)
	if err := c.Write(testTile, []byte(`{}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := c.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatalf("ReadDir after ClearAll: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty dir after ClearAll, got %d entries", len(entries))
	}
}

func TestTileCachePurgeOlderThan(t *testing.T) {
	c := newTestCache(t)
	oldTile := Tile{South: 42.300, West: -72.600, North: 42.350, East: -72.550}
	newTile := Tile{South: 42.350, West: -72.600, North: 42.400, East: -72.550}

	for _, tile := range []Tile{oldTile, newTile} {
		if err := c.Write(tile, []byte(`{}`)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(c.Path(oldTile), past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	n, err := c.PurgeOlderThan(1 * time.Hour)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 file purged, got %d", n)
	}
	if c.Has(oldTile) {
		t.Error("old tile still exists after purge")
	}
	if !c.Has(newTile) {
		t.Error("new tile was incorrectly purged")
	}
}

func TestTileCacheReadNonExistent(t *testing.T) {
	c := newTestCache(t)
	_, err := c.Read(testTile)
	if err == nil {
		t.Error("expected error reading non-existent tile, got nil")
	}
}

func TestTileCacheKey(t *testing.T) {
	tests := []struct {
		name      string
		tile      Tile
		precision int
		expected  string
	}{
		{
			name:      "positive coordinates",
			tile:      Tile{South: 42.350, West: 10.050, North: 42.400, East: 10.100},
			precision: 3,
			expected:  "tile_42.350_10.050.json",
		},
		{
			name:      "negative west coordinate",
			tile:      Tile{South: 42.350, West: -72.600, North: 42.400, East: -72.550},
			precision: 3,
			expected:  "tile_42.350_-72.600.json",
		},
		{
			name:      "negative south and west",
			tile:      Tile{South: -33.900, West: -70.650, North: -33.850, East: -70.600},
			precision: 3,
			expected:  "tile_-33.900_-70.650.json",
		},
		{
			name:      "higher precision",
			tile:      Tile{South: 42.3500, West: -72.6000, North: 42.4000, East: -72.5500},
			precision: 4,
			expected:  "tile_42.3500_-72.6000.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TileCacheKey(tc.tile, tc.precision)
			if got != tc.expected {
				t.Errorf("TileCacheKey(%v, %d) = %q, want %q", tc.tile, tc.precision, got, tc.expected)
			}
		})
	}
}

// --- fetchTileRaw tests ---

func validOverpassJSON(t *testing.T) []byte {
	t.Helper()
	resp := overpassResponse{
		Elements: []overpassElement{
			{
				ID:   42,
				Tags: map[string]string{"highway": "primary"},
				Geometry: []overpassGeomPoint{
					{Lat: 42.35, Lon: -72.60},
					{Lat: 42.36, Lon: -72.59},
				},
			},
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestFetchTileRawSuccess(t *testing.T) {
	want := validOverpassJSON(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(want)
	}))
	defer ts.Close()

	tile := Tile{South: 42.35, West: -72.60, North: 42.40, East: -72.55}
	got, err := fetchTileRaw(context.Background(), ts.URL, tile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("raw bytes mismatch: got %q, want %q", got, want)
	}
}

func TestFetchTileRawHTTP429Retryable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	tile := Tile{South: 42.35, West: -72.60, North: 42.40, East: -72.55}
	_, err := fetchTileRaw(context.Background(), ts.URL, tile)
	if err == nil {
		t.Fatal("expected error for HTTP 429, got nil")
	}
	if isNonRetryable(err) {
		t.Errorf("HTTP 429 should be retryable, but got NonRetryable: %v", err)
	}
}

func TestFetchTileRawHTTP400NonRetryable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	tile := Tile{South: 42.35, West: -72.60, North: 42.40, East: -72.55}
	_, err := fetchTileRaw(context.Background(), ts.URL, tile)
	if err == nil {
		t.Fatal("expected error for HTTP 400, got nil")
	}
	if !isNonRetryable(err) {
		t.Errorf("HTTP 400 should be NonRetryable, but it is not: %v", err)
	}
}

// --- FetchTiledWays tests ---

// singleTileCfg returns a TileFetchConfig wired to use a tiny radius that produces one tile.
func singleTileCfg(endpoint string, cache *TileCache) TileFetchConfig {
	return TileFetchConfig{
		Endpoint: endpoint,
		TileSize: 0.05,
		Cache:    cache,
	}
}

func TestFetchTiledWaysCacheHit(t *testing.T) {
	var requestCount int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validOverpassJSON(t))
	}))
	defer ts.Close()

	cache := newTestCache(t)
	// Use the same tile that a tiny radius (0.01 km) at 42.36, -72.58 with 0.05 tiles produces.
	tiles := ComputeTiles(42.36, -72.58, 0.01, 0.05)
	if len(tiles) != 1 {
		t.Fatalf("expected 1 tile for setup, got %d", len(tiles))
	}
	if err := cache.Write(tiles[0], validOverpassJSON(t)); err != nil {
		t.Fatalf("pre-populate cache: %v", err)
	}

	cfg := singleTileCfg(ts.URL, cache)
	ways, err := FetchTiledWays(context.Background(), 42.36, -72.58, 0.01, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ways) == 0 {
		t.Error("expected at least one way from cache")
	}
	if n := atomic.LoadInt64(&requestCount); n != 0 {
		t.Errorf("expected 0 HTTP requests (cache hit), got %d", n)
	}
}

func TestFetchTiledWaysNoCache(t *testing.T) {
	var requestCount int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validOverpassJSON(t))
	}))
	defer ts.Close()

	cache := newTestCache(t)
	// Pre-populate cache so that without NoCache the HTTP call would be skipped.
	tiles := ComputeTiles(42.36, -72.58, 0.01, 0.05)
	if len(tiles) != 1 {
		t.Fatalf("expected 1 tile for setup, got %d", len(tiles))
	}
	if err := cache.Write(tiles[0], validOverpassJSON(t)); err != nil {
		t.Fatalf("pre-populate cache: %v", err)
	}

	cfg := singleTileCfg(ts.URL, cache)
	cfg.NoCache = true

	ways, err := FetchTiledWays(context.Background(), 42.36, -72.58, 0.01, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ways) == 0 {
		t.Error("expected at least one way from fetch")
	}
	if n := atomic.LoadInt64(&requestCount); n == 0 {
		t.Error("expected at least 1 HTTP request when NoCache=true, got 0")
	}
	// Cache should have been written even with NoCache=true.
	if !cache.Has(tiles[0]) {
		t.Error("expected cache to be written even when NoCache=true")
	}
}

func TestFetchTiledWaysPartialFailure(t *testing.T) {
	tiles := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	if len(tiles) < 2 {
		t.Fatalf("need at least 2 tiles for partial failure test, got %d", len(tiles))
	}

	// Fail the first tile, succeed for all others.
	failTile := tiles[0]
	var requestCount int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&requestCount, 1)
		// The very first request corresponds to the failing tile.
		// Use 400 (non-retryable) so the failure is instant with no retry backoff.
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validOverpassJSON(t))
	}))
	defer ts.Close()

	_ = failTile
	cache := newTestCache(t)
	cfg := TileFetchConfig{
		Endpoint: ts.URL,
		TileSize: 0.05,
		Cache:    cache,
		// Use very short retry delay so the test is fast.
	}

	// We can't easily override the retry delay here, so we use retryWithBackoff defaults.
	// To keep the test fast, pre-populate all tiles except the first with valid data.
	for _, tile := range tiles[1:] {
		if err := cache.Write(tile, validOverpassJSON(t)); err != nil {
			t.Fatalf("pre-populate cache: %v", err)
		}
	}

	ways, err := FetchTiledWays(context.Background(), 42.36, -72.58, 5.0, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have results from the non-failed tiles.
	if len(ways) == 0 {
		t.Error("expected ways from partial success, got none")
	}
}

// --- ProgressReporter spy and integration tests ---

// spyProgressReporter records all calls made to it so tests can assert on
// the exact sequence of SetTotal, Tick, and Done invocations.
type spyProgressReporter struct {
	mu       sync.Mutex
	total    int
	ticks    []bool // one entry per Tick call; value = cached argument
	doneCalls int
}

func (s *spyProgressReporter) SetTotal(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total = n
}

func (s *spyProgressReporter) Tick(cached bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ticks = append(s.ticks, cached)
}

func (s *spyProgressReporter) Retry() {}

func (s *spyProgressReporter) FetchDuration(_ time.Duration) {}

func (s *spyProgressReporter) Done() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doneCalls++
}

// TestFetchTiledWaysProgressReporterCacheHit verifies that a fully-cached run
// calls SetTotal with the tile count, Tick once per tile with cached=true, and
// Done exactly once.
func TestFetchTiledWaysProgressReporterCacheHit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP request; all tiles should come from cache")
	}))
	defer ts.Close()

	cache := newTestCache(t)
	tiles := ComputeTiles(42.36, -72.58, 0.01, 0.05)
	if len(tiles) != 1 {
		t.Fatalf("expected 1 tile, got %d", len(tiles))
	}
	if err := cache.Write(tiles[0], validOverpassJSON(t)); err != nil {
		t.Fatalf("pre-populate cache: %v", err)
	}

	spy := &spyProgressReporter{}
	cfg := singleTileCfg(ts.URL, cache)
	cfg.Progress = spy

	if _, err := FetchTiledWays(context.Background(), 42.36, -72.58, 0.01, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spy.total != 1 {
		t.Errorf("SetTotal: got %d, want 1", spy.total)
	}
	if len(spy.ticks) != 1 {
		t.Errorf("Tick call count: got %d, want 1", len(spy.ticks))
	} else if !spy.ticks[0] {
		t.Error("Tick: cached argument should be true for cache hit, got false")
	}
	if spy.doneCalls != 1 {
		t.Errorf("Done call count: got %d, want 1", spy.doneCalls)
	}
}

// TestFetchTiledWaysProgressReporterLiveFetch verifies that a live fetch (no
// cache) calls SetTotal with the tile count, Tick once per tile with
// cached=false, and Done exactly once.
func TestFetchTiledWaysProgressReporterLiveFetch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validOverpassJSON(t))
	}))
	defer ts.Close()

	cache := newTestCache(t)
	tiles := ComputeTiles(42.36, -72.58, 0.01, 0.05)
	if len(tiles) != 1 {
		t.Fatalf("expected 1 tile, got %d", len(tiles))
	}

	spy := &spyProgressReporter{}
	cfg := singleTileCfg(ts.URL, cache)
	cfg.Progress = spy
	cfg.RateLimitDelay = 0 // no sleep in unit test

	if _, err := FetchTiledWays(context.Background(), 42.36, -72.58, 0.01, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spy.total != 1 {
		t.Errorf("SetTotal: got %d, want 1", spy.total)
	}
	if len(spy.ticks) != 1 {
		t.Errorf("Tick call count: got %d, want 1", len(spy.ticks))
	} else if spy.ticks[0] {
		t.Error("Tick: cached argument should be false for live fetch, got true")
	}
	if spy.doneCalls != 1 {
		t.Errorf("Done call count: got %d, want 1", spy.doneCalls)
	}
}

// TestFetchTiledWaysProgressReporterSetTotalMatchesTileCount verifies that the
// value passed to SetTotal equals the number of Tick calls when all tiles
// succeed, across a multi-tile scenario.
func TestFetchTiledWaysProgressReporterSetTotalMatchesTileCount(t *testing.T) {
	tiles := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	if len(tiles) < 2 {
		t.Skipf("need at least 2 tiles, got %d", len(tiles))
	}

	cache := newTestCache(t)
	// Pre-populate all tiles so there are no live fetches (avoids rate-limit delay).
	for _, tile := range tiles {
		if err := cache.Write(tile, validOverpassJSON(t)); err != nil {
			t.Fatalf("pre-populate cache: %v", err)
		}
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP request; all tiles should be cached")
	}))
	defer ts.Close()

	spy := &spyProgressReporter{}
	cfg := TileFetchConfig{
		Endpoint: ts.URL,
		TileSize: 0.05,
		Cache:    cache,
		Progress: spy,
	}

	if _, err := FetchTiledWays(context.Background(), 42.36, -72.58, 5.0, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spy.total != len(tiles) {
		t.Errorf("SetTotal: got %d, want %d", spy.total, len(tiles))
	}
	if len(spy.ticks) != len(tiles) {
		t.Errorf("Tick count: got %d, want %d", len(spy.ticks), len(tiles))
	}
	if spy.doneCalls != 1 {
		t.Errorf("Done call count: got %d, want 1", spy.doneCalls)
	}
}

// --- parseTileData tests ---

func makeOverpassJSON(t *testing.T, elements []overpassElement) []byte {
	t.Helper()
	resp := overpassResponse{Elements: elements}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestParseTileDataBasic(t *testing.T) {
	elements := []overpassElement{
		{
			ID:   101,
			Tags: map[string]string{"highway": "primary", "name": "Main St"},
			Geometry: []overpassGeomPoint{
				{Lat: 42.35, Lon: -72.60},
				{Lat: 42.36, Lon: -72.59},
			},
		},
		{
			ID:   202,
			Tags: map[string]string{"highway": "secondary"},
			Geometry: []overpassGeomPoint{
				{Lat: 42.37, Lon: -72.58},
			},
		},
	}
	data := makeOverpassJSON(t, elements)

	ways, err := parseTileData(data)
	if err != nil {
		t.Fatalf("parseTileData: %v", err)
	}
	if len(ways) != 2 {
		t.Fatalf("expected 2 ways, got %d", len(ways))
	}

	if ways[0].ID != 101 {
		t.Errorf("ways[0].ID = %d, want 101", ways[0].ID)
	}
	if ways[0].Tags["highway"] != "primary" {
		t.Errorf("ways[0].Tags[highway] = %q, want %q", ways[0].Tags["highway"], "primary")
	}
	if len(ways[0].Geometry) != 2 {
		t.Errorf("ways[0].Geometry length = %d, want 2", len(ways[0].Geometry))
	}
	if ways[0].Geometry[0] != (geo.Coord{Lat: 42.35, Lon: -72.60}) {
		t.Errorf("ways[0].Geometry[0] = %v, want {42.35, -72.60}", ways[0].Geometry[0])
	}

	if ways[1].ID != 202 {
		t.Errorf("ways[1].ID = %d, want 202", ways[1].ID)
	}
}

func TestParseTileDataEmpty(t *testing.T) {
	data := makeOverpassJSON(t, []overpassElement{})

	ways, err := parseTileData(data)
	if err != nil {
		t.Fatalf("parseTileData: %v", err)
	}
	if ways == nil {
		t.Error("expected non-nil slice for empty elements")
	}
	if len(ways) != 0 {
		t.Errorf("expected 0 ways, got %d", len(ways))
	}
}

func TestParseTileDataMalformed(t *testing.T) {
	_, err := parseTileData([]byte(`not valid json`))
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

// --- mergeAndDeduplicate tests ---

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 10}))
}

func TestMergeAndDeduplicateNoDuplicates(t *testing.T) {
	tile1 := makeOverpassJSON(t, []overpassElement{
		{ID: 1, Tags: map[string]string{"highway": "primary"}, Geometry: []overpassGeomPoint{{Lat: 1.0, Lon: 2.0}}},
	})
	tile2 := makeOverpassJSON(t, []overpassElement{
		{ID: 2, Tags: map[string]string{"highway": "secondary"}, Geometry: []overpassGeomPoint{{Lat: 3.0, Lon: 4.0}}},
	})

	ways, err := mergeAndDeduplicate([][]byte{tile1, tile2}, discardLogger())
	if err != nil {
		t.Fatalf("mergeAndDeduplicate: %v", err)
	}
	if len(ways) != 2 {
		t.Errorf("expected 2 ways, got %d", len(ways))
	}
}

func TestMergeAndDeduplicateWithDuplicates(t *testing.T) {
	sharedElement := overpassElement{
		ID:       42,
		Tags:     map[string]string{"highway": "primary"},
		Geometry: []overpassGeomPoint{{Lat: 1.0, Lon: 2.0}, {Lat: 3.0, Lon: 4.0}},
	}
	tile1 := makeOverpassJSON(t, []overpassElement{
		sharedElement,
		{ID: 10, Tags: map[string]string{"highway": "residential"}, Geometry: []overpassGeomPoint{{Lat: 5.0, Lon: 6.0}}},
	})
	tile2 := makeOverpassJSON(t, []overpassElement{
		sharedElement,
		{ID: 20, Tags: map[string]string{"highway": "tertiary"}, Geometry: []overpassGeomPoint{{Lat: 7.0, Lon: 8.0}}},
	})

	ways, err := mergeAndDeduplicate([][]byte{tile1, tile2}, discardLogger())
	if err != nil {
		t.Fatalf("mergeAndDeduplicate: %v", err)
	}
	if len(ways) != 3 {
		t.Errorf("expected 3 ways (duplicate removed), got %d", len(ways))
	}

	idCount := make(map[int64]int)
	for _, w := range ways {
		idCount[w.ID]++
	}
	if idCount[42] != 1 {
		t.Errorf("way ID 42 appears %d times, want 1", idCount[42])
	}
}

func TestMergeAndDeduplicateZeroID(t *testing.T) {
	tile1 := makeOverpassJSON(t, []overpassElement{
		{ID: 0, Tags: map[string]string{"highway": "path"}, Geometry: []overpassGeomPoint{{Lat: 1.0, Lon: 2.0}}},
		{ID: 0, Tags: map[string]string{"highway": "footway"}, Geometry: []overpassGeomPoint{{Lat: 3.0, Lon: 4.0}}},
	})

	ways, err := mergeAndDeduplicate([][]byte{tile1}, discardLogger())
	if err != nil {
		t.Fatalf("mergeAndDeduplicate: %v", err)
	}
	if len(ways) != 2 {
		t.Errorf("expected 2 ways with ID 0 (no dedup), got %d", len(ways))
	}
}

func TestMergeAndDeduplicatePreservesGeometry(t *testing.T) {
	geom := []overpassGeomPoint{
		{Lat: 10.1, Lon: 20.2},
		{Lat: 10.3, Lon: 20.4},
		{Lat: 10.5, Lon: 20.6},
	}
	tile1 := makeOverpassJSON(t, []overpassElement{
		{ID: 99, Tags: map[string]string{"highway": "primary"}, Geometry: geom},
	})
	// tile2 has the same way (duplicate)
	tile2 := makeOverpassJSON(t, []overpassElement{
		{ID: 99, Tags: map[string]string{"highway": "primary"}, Geometry: geom},
	})

	ways, err := mergeAndDeduplicate([][]byte{tile1, tile2}, discardLogger())
	if err != nil {
		t.Fatalf("mergeAndDeduplicate: %v", err)
	}
	if len(ways) != 1 {
		t.Fatalf("expected 1 way, got %d", len(ways))
	}
	if len(ways[0].Geometry) != 3 {
		t.Errorf("expected 3 geometry points, got %d", len(ways[0].Geometry))
	}
	expected := []geo.Coord{{Lat: 10.1, Lon: 20.2}, {Lat: 10.3, Lon: 20.4}, {Lat: 10.5, Lon: 20.6}}
	for i, coord := range ways[0].Geometry {
		if coord != expected[i] {
			t.Errorf("geometry[%d] = %v, want %v", i, coord, expected[i])
		}
	}
}

// retrySpyProgress records Retry and FetchDuration calls in addition to the
// standard spy behaviours already covered by spyProgressReporter.
type retrySpyProgress struct {
	NoopProgressReporter
	mu        sync.Mutex
	retries   int
	durations []time.Duration
}

func (s *retrySpyProgress) Retry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retries++
}

func (s *retrySpyProgress) FetchDuration(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.durations = append(s.durations, d)
}

// TestProgressRetryAndDurationSingleSuccess verifies that a single successful
// fetch (no retries) calls FetchDuration once and Retry zero times.
func TestProgressRetryAndDurationSingleSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validOverpassJSON(t))
	}))
	defer ts.Close()

	cache := newTestCache(t)
	spy := &retrySpyProgress{}
	cfg := singleTileCfg(ts.URL, cache)
	cfg.Progress = spy
	cfg.RateLimitDelay = 0

	if _, err := FetchTiledWays(context.Background(), 42.36, -72.58, 0.01, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.retries != 0 {
		t.Errorf("Retry() called %d times, want 0", spy.retries)
	}
	if len(spy.durations) != 1 {
		t.Errorf("FetchDuration() called %d times, want 1", len(spy.durations))
	}
}

// TestProgressRetryAndDurationOneRetry verifies that when the first attempt
// fails and the second succeeds, Retry() is called once and FetchDuration()
// is called twice.
func TestProgressRetryAndDurationOneRetry(t *testing.T) {
	var requestCount int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&requestCount, 1)
		if n == 1 {
			// First attempt: transient error (retryable).
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validOverpassJSON(t))
	}))
	defer ts.Close()

	cache := newTestCache(t)
	spy := &retrySpyProgress{}
	cfg := singleTileCfg(ts.URL, cache)
	cfg.Progress = spy
	cfg.RateLimitDelay = 0
	cfg.RetryDelay = 0

	if _, err := FetchTiledWays(context.Background(), 42.36, -72.58, 0.01, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.retries != 1 {
		t.Errorf("Retry() called %d times, want 1", spy.retries)
	}
	if len(spy.durations) != 2 {
		t.Errorf("FetchDuration() called %d times, want 2", len(spy.durations))
	}
}

// TestProgressRetryAndDurationCacheHit verifies that a cache hit does not
// trigger Retry() or FetchDuration().
func TestProgressRetryAndDurationCacheHit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP request; tile should come from cache")
	}))
	defer ts.Close()

	cache := newTestCache(t)
	tiles := ComputeTiles(42.36, -72.58, 0.01, 0.05)
	if len(tiles) != 1 {
		t.Fatalf("expected 1 tile, got %d", len(tiles))
	}
	if err := cache.Write(tiles[0], validOverpassJSON(t)); err != nil {
		t.Fatalf("pre-populate cache: %v", err)
	}

	spy := &retrySpyProgress{}
	cfg := singleTileCfg(ts.URL, cache)
	cfg.Progress = spy

	if _, err := FetchTiledWays(context.Background(), 42.36, -72.58, 0.01, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.retries != 0 {
		t.Errorf("Retry() called %d times for cache hit, want 0", spy.retries)
	}
	if len(spy.durations) != 0 {
		t.Errorf("FetchDuration() called %d times for cache hit, want 0", len(spy.durations))
	}
}

func TestFetchTiledWaysRateLimit(t *testing.T) {
	// Use a 2-tile scenario so we can measure the delay between fetches.
	// A 3km radius at 42.36,-72.58 with 0.05 degree tiles produces multiple tiles.
	tiles := ComputeTiles(42.36, -72.58, 5.0, 0.05)
	if len(tiles) < 2 {
		t.Skipf("need at least 2 tiles, got %d", len(tiles))
	}

	// Only leave 2 tiles uncached to keep the test fast.
	cache := newTestCache(t)
	for _, tile := range tiles[2:] {
		if err := cache.Write(tile, validOverpassJSON(t)); err != nil {
			t.Fatalf("pre-populate cache: %v", err)
		}
	}

	var (
		requestTimesMu sync.Mutex
		requestTimes   []time.Time
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestTimesMu.Lock()
		requestTimes = append(requestTimes, time.Now())
		requestTimesMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validOverpassJSON(t))
	}))
	defer ts.Close()

	cfg := TileFetchConfig{
		Endpoint: ts.URL,
		TileSize: 0.05,
		Cache:    cache,
	}

	_, err := FetchTiledWays(context.Background(), 42.36, -72.58, 5.0, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(requestTimes) < 2 {
		t.Skipf("only %d fetch requests were made (others served from cache); cannot test rate limiting", len(requestTimes))
	}

	for i := 1; i < len(requestTimes); i++ {
		elapsed := requestTimes[i].Sub(requestTimes[i-1])
		if elapsed < 900*time.Millisecond {
			t.Errorf("delay between fetch %d and %d was %v, want >= 1s", i-1, i, elapsed)
		}
	}
}


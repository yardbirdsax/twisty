package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// parseBBoxFromBody extracts (south, west, north, east) from the POST body of an Overpass
// request. The query looks like: ...way[...](south,west,north,east);...
func parseBBoxFromBody(body string) (south, west, north, east float64, err error) {
	decoded, decErr := url.QueryUnescape(body)
	if decErr != nil {
		// body might already be decoded
		decoded = body
	}

	// Strip "data=" prefix if present.
	if after, ok := strings.CutPrefix(decoded, "data="); ok {
		decoded = after
	}

	// Find the bbox pattern: (...s,w,n,e...)
	// The query format is: way[...](s,w,n,e);
	start := strings.Index(decoded, "](")
	if start == -1 {
		return 0, 0, 0, 0, fmt.Errorf("bbox open paren not found in: %q", decoded)
	}
	start += 2 // skip "]("
	end := strings.Index(decoded[start:], ")")
	if end == -1 {
		return 0, 0, 0, 0, fmt.Errorf("bbox close paren not found in: %q", decoded)
	}
	coords := decoded[start : start+end]
	parts := strings.Split(coords, ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, fmt.Errorf("expected 4 bbox coords, got %d in: %q", len(parts), coords)
	}
	vals := make([]float64, 4)
	for i, p := range parts {
		v, pErr := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if pErr != nil {
			return 0, 0, 0, 0, fmt.Errorf("parsing coord[%d] %q: %w", i, p, pErr)
		}
		vals[i] = v
	}
	return vals[0], vals[1], vals[2], vals[3], nil
}

// bboxKey produces a short string key for a bounding box, suitable for map lookups.
func bboxKey(south, west, north, east float64) string {
	return fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", south, west, north, east)
}

// tileKey returns the key for a Tile.
func tileKey(t Tile) string {
	return bboxKey(t.South, t.West, t.North, t.East)
}

// makeWaysJSON builds an Overpass JSON response containing the given overpassElements.
func makeWaysJSON(t *testing.T, elements []overpassElement) []byte {
	t.Helper()
	resp := overpassResponse{Elements: elements}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal ways JSON: %v", err)
	}
	return b
}

// integrationCfg returns a TileFetchConfig with minimal delays, suitable for integration tests.
func integrationCfg(endpoint string, cache *TileCache) TileFetchConfig {
	return TileFetchConfig{
		Endpoint:       endpoint,
		TileSize:       0.05,
		Cache:          cache,
		RateLimitDelay: 5 * time.Millisecond,
		RetryDelay:     10 * time.Millisecond,
	}
}

// smallTileCenterAndRadius returns a center and radius that produce a predictable small
// number of tiles (typically 4) with 0.05 degree tile size.
const (
	integTestLat    = 42.36
	integTestLon    = -72.58
	integTestRadius = 2.0 // km — produces ~4 tiles
)

// buildTileResponses creates a map from tile bboxKey to Overpass JSON with unique way IDs.
// sharedWayIDs is a list of way IDs that will appear in ALL tile responses (to test dedup).
func buildTileResponses(t *testing.T, tiles []Tile, sharedWayIDs []int64) map[string][]byte {
	t.Helper()
	responses := make(map[string][]byte, len(tiles))
	for i, tile := range tiles {
		elements := make([]overpassElement, 0)
		// Add shared ways first.
		for _, id := range sharedWayIDs {
			elements = append(elements, overpassElement{
				ID:   id,
				Tags: map[string]string{"highway": "primary"},
				Geometry: []overpassGeomPoint{
					{Lat: tile.South + 0.01, Lon: tile.West + 0.01},
					{Lat: tile.South + 0.02, Lon: tile.West + 0.02},
				},
			})
		}
		// Add 2 tile-unique ways.
		baseID := int64((i + 1) * 1000)
		for j := range 2 {
			elements = append(elements, overpassElement{
				ID:   baseID + int64(j),
				Tags: map[string]string{"highway": "residential"},
				Geometry: []overpassGeomPoint{
					{Lat: tile.South + 0.01 + float64(j)*0.005, Lon: tile.West + 0.01},
				},
			})
		}
		responses[tileKey(tile)] = makeWaysJSON(t, elements)
	}
	return responses
}

// newMockOverpassServer returns an httptest.Server that serves pre-built per-tile responses.
// requestCounts maps tile bboxKey -> number of requests received for that tile.
func newMockOverpassServer(t *testing.T, responses map[string][]byte, requestCounts map[string]*int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		south, west, north, east, parseErr := parseBBoxFromBody(string(body))
		if parseErr != nil {
			t.Logf("parseBBoxFromBody error: %v (body=%q)", parseErr, string(body))
			http.Error(w, "bad bbox", http.StatusBadRequest)
			return
		}
		key := bboxKey(south, west, north, east)
		if requestCounts != nil {
			if counter, ok := requestCounts[key]; ok {
				atomic.AddInt64(counter, 1)
			} else {
				// Unknown tile — track it with a new counter (shouldn't happen in most tests).
				var c int64
				requestCounts[key] = &c
				atomic.AddInt64(&c, 1)
			}
		}
		data, ok := responses[key]
		if !ok {
			// Return empty response for unknown tiles.
			data = makeWaysJSON(t, nil)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data) //nolint:errcheck
	}))
}

// --- Test: Full fetch with empty cache ---

func TestTileFetchIntegrationEmptyCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tiles := ComputeTiles(integTestLat, integTestLon, integTestRadius, 0.05)
	if len(tiles) == 0 {
		t.Fatal("expected at least one tile")
	}

	sharedIDs := []int64{9001, 9002}
	responses := buildTileResponses(t, tiles, sharedIDs)

	// Build per-tile request counters.
	requestCounts := make(map[string]*int64, len(tiles))
	for _, tile := range tiles {
		var c int64
		requestCounts[tileKey(tile)] = &c
	}

	ts := newMockOverpassServer(t, responses, requestCounts)
	defer ts.Close()

	cache := &TileCache{Dir: t.TempDir(), Precision: 3}
	cfg := integrationCfg(ts.URL, cache)

	ways, err := FetchTiledWays(context.Background(), integTestLat, integTestLon, integTestRadius, cfg)
	if err != nil {
		t.Fatalf("FetchTiledWays: %v", err)
	}

	// All tiles should have been requested.
	for _, tile := range tiles {
		key := tileKey(tile)
		count := atomic.LoadInt64(requestCounts[key])
		if count != 1 {
			t.Errorf("tile %s: expected 1 request, got %d", key, count)
		}
	}

	// Shared ways should appear exactly once each.
	idCounts := make(map[int64]int)
	for _, w := range ways {
		idCounts[w.ID]++
	}
	for _, id := range sharedIDs {
		if idCounts[id] != 1 {
			t.Errorf("shared way %d appears %d times, want 1", id, idCounts[id])
		}
	}

	// Cache files should exist for each tile.
	for _, tile := range tiles {
		if !cache.Has(tile) {
			t.Errorf("expected cache file for tile %s, but not found", tileKey(tile))
		}
	}

	// Total unique ways: len(sharedIDs) + 2 unique per tile.
	expectedWays := len(sharedIDs) + len(tiles)*2
	if len(ways) != expectedWays {
		t.Errorf("expected %d unique ways, got %d", expectedWays, len(ways))
	}
}

// --- Test: Second run uses cache ---

func TestTileFetchIntegrationCacheHit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tiles := ComputeTiles(integTestLat, integTestLon, integTestRadius, 0.05)
	if len(tiles) == 0 {
		t.Fatal("expected at least one tile")
	}

	responses := buildTileResponses(t, tiles, []int64{9001})
	requestCounts := make(map[string]*int64, len(tiles))
	for _, tile := range tiles {
		var c int64
		requestCounts[tileKey(tile)] = &c
	}

	ts := newMockOverpassServer(t, responses, requestCounts)
	defer ts.Close()

	cache := &TileCache{Dir: t.TempDir(), Precision: 3}
	cfg := integrationCfg(ts.URL, cache)

	// First run: populate cache.
	ways1, err := FetchTiledWays(context.Background(), integTestLat, integTestLon, integTestRadius, cfg)
	if err != nil {
		t.Fatalf("first FetchTiledWays: %v", err)
	}

	// Record mtimes after first run.
	mtimes1 := make(map[string]time.Time, len(tiles))
	for _, tile := range tiles {
		info, statErr := os.Stat(cache.Path(tile))
		if statErr != nil {
			t.Fatalf("stat cache file after first run: %v", statErr)
		}
		mtimes1[tileKey(tile)] = info.ModTime()
	}

	// Sleep briefly so mtime can differ.
	time.Sleep(10 * time.Millisecond)

	// Reset counters.
	for _, tile := range tiles {
		atomic.StoreInt64(requestCounts[tileKey(tile)], 0)
	}

	// Second run: should use cache.
	ways2, err := FetchTiledWays(context.Background(), integTestLat, integTestLon, integTestRadius, cfg)
	if err != nil {
		t.Fatalf("second FetchTiledWays: %v", err)
	}

	// No HTTP requests should have been made.
	for _, tile := range tiles {
		count := atomic.LoadInt64(requestCounts[tileKey(tile)])
		if count != 0 {
			t.Errorf("tile %s: expected 0 HTTP requests on second run, got %d", tileKey(tile), count)
		}
	}

	// Results should be identical.
	if len(ways1) != len(ways2) {
		t.Errorf("way count mismatch: first=%d, second=%d", len(ways1), len(ways2))
	}

	// Cache mtimes should have been updated by the Read call.
	for _, tile := range tiles {
		info, statErr := os.Stat(cache.Path(tile))
		if statErr != nil {
			t.Fatalf("stat cache file after second run: %v", statErr)
		}
		if !info.ModTime().After(mtimes1[tileKey(tile)]) {
			t.Errorf("tile %s: mtime not updated after second run (before=%v, after=%v)",
				tileKey(tile), mtimes1[tileKey(tile)], info.ModTime())
		}
	}
}

// --- Test: Overlapping queries share cache ---

func TestTileFetchIntegrationOverlap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Center A and Center B are shifted enough that at least one tile in set B falls outside set A.
	const (
		latA = 42.36
		lonA = -72.58
		latB = 42.41
		lonB = -72.53
	)

	tilesA := ComputeTiles(latA, lonA, integTestRadius, 0.05)
	tilesB := ComputeTiles(latB, lonB, integTestRadius, 0.05)

	// Compute which tiles overlap (appear in both sets).
	setA := make(map[Tile]struct{}, len(tilesA))
	for _, tile := range tilesA {
		setA[tile] = struct{}{}
	}
	setB := make(map[Tile]struct{}, len(tilesB))
	for _, tile := range tilesB {
		setB[tile] = struct{}{}
	}

	var overlapping []Tile
	var bOnly []Tile
	for _, tile := range tilesB {
		if _, inA := setA[tile]; inA {
			overlapping = append(overlapping, tile)
		} else {
			bOnly = append(bOnly, tile)
		}
	}

	if len(overlapping) == 0 {
		t.Skip("no overlapping tiles between center A and center B; test not meaningful")
	}
	if len(bOnly) == 0 {
		t.Fatal("no B-only tiles found; center B must be shifted further from center A so at least one tile is unique to set B")
	}

	// Build a combined response map covering all tiles from both queries.
	allTiles := make([]Tile, 0, len(tilesA)+len(tilesB))
	seen := make(map[Tile]struct{})
	for _, tile := range append(tilesA, tilesB...) {
		if _, ok := seen[tile]; !ok {
			seen[tile] = struct{}{}
			allTiles = append(allTiles, tile)
		}
	}
	responses := buildTileResponses(t, allTiles, nil)

	// Track request counts per tile.
	requestCounts := make(map[string]*int64, len(allTiles))
	for _, tile := range allTiles {
		var c int64
		requestCounts[tileKey(tile)] = &c
	}

	ts := newMockOverpassServer(t, responses, requestCounts)
	defer ts.Close()

	cache := &TileCache{Dir: t.TempDir(), Precision: 3}

	// First fetch (center A).
	cfgA := integrationCfg(ts.URL, cache)
	_, err := FetchTiledWays(context.Background(), latA, lonA, integTestRadius, cfgA)
	if err != nil {
		t.Fatalf("first FetchTiledWays: %v", err)
	}

	// Reset counters.
	for _, tile := range allTiles {
		atomic.StoreInt64(requestCounts[tileKey(tile)], 0)
	}

	// Second fetch (center B).
	cfgB := integrationCfg(ts.URL, cache)
	_, err = FetchTiledWays(context.Background(), latB, lonB, integTestRadius, cfgB)
	if err != nil {
		t.Fatalf("second FetchTiledWays: %v", err)
	}

	// Overlapping tiles should not have triggered new HTTP requests.
	for _, tile := range overlapping {
		count := atomic.LoadInt64(requestCounts[tileKey(tile)])
		if count != 0 {
			t.Errorf("overlapping tile %s: expected 0 HTTP requests (cache hit), got %d", tileKey(tile), count)
		}
	}

	// B-only tiles should have been fetched.
	for _, tile := range bOnly {
		count := atomic.LoadInt64(requestCounts[tileKey(tile)])
		if count == 0 {
			t.Errorf("B-only tile %s: expected >= 1 HTTP request, got 0", tileKey(tile))
		}
	}
}

// --- Test: --no-cache flag re-fetches ---

func TestTileFetchIntegrationNoCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tiles := ComputeTiles(integTestLat, integTestLon, integTestRadius, 0.05)
	if len(tiles) == 0 {
		t.Fatal("expected at least one tile")
	}

	responses := buildTileResponses(t, tiles, nil)
	requestCounts := make(map[string]*int64, len(tiles))
	for _, tile := range tiles {
		var c int64
		requestCounts[tileKey(tile)] = &c
	}

	ts := newMockOverpassServer(t, responses, requestCounts)
	defer ts.Close()

	cache := &TileCache{Dir: t.TempDir(), Precision: 3}

	// First run: populate cache and record cache file contents.
	cfg := integrationCfg(ts.URL, cache)
	_, err := FetchTiledWays(context.Background(), integTestLat, integTestLon, integTestRadius, cfg)
	if err != nil {
		t.Fatalf("first FetchTiledWays: %v", err)
	}

	// Record mtimes after first run.
	mtimes1 := make(map[string]time.Time, len(tiles))
	for _, tile := range tiles {
		info, statErr := os.Stat(cache.Path(tile))
		if statErr != nil {
			t.Fatalf("stat cache file after first run: %v", statErr)
		}
		mtimes1[tileKey(tile)] = info.ModTime()
	}

	// Sleep briefly so mtime can differ.
	time.Sleep(10 * time.Millisecond)

	// Reset counters.
	for _, tile := range tiles {
		atomic.StoreInt64(requestCounts[tileKey(tile)], 0)
	}

	// Second run: NoCache = true.
	cfgNoCache := integrationCfg(ts.URL, cache)
	cfgNoCache.NoCache = true
	_, err = FetchTiledWays(context.Background(), integTestLat, integTestLon, integTestRadius, cfgNoCache)
	if err != nil {
		t.Fatalf("second FetchTiledWays (NoCache): %v", err)
	}

	// All tiles should have been re-fetched.
	for _, tile := range tiles {
		count := atomic.LoadInt64(requestCounts[tileKey(tile)])
		if count != 1 {
			t.Errorf("tile %s: expected 1 HTTP request with NoCache, got %d", tileKey(tile), count)
		}
	}

	// Cache files should still exist (NoCache still writes).
	for _, tile := range tiles {
		if !cache.Has(tile) {
			t.Errorf("expected cache file for tile %s after NoCache run", tileKey(tile))
		}
	}

	// Cache files should have been updated (new content written) after the NoCache run.
	for _, tile := range tiles {
		info, statErr := os.Stat(cache.Path(tile))
		if statErr != nil {
			t.Fatalf("stat cache file after NoCache run: %v", statErr)
		}
		if !info.ModTime().After(mtimes1[tileKey(tile)]) {
			t.Errorf("tile %s: mtime not updated after NoCache run (before=%v, after=%v)",
				tileKey(tile), mtimes1[tileKey(tile)], info.ModTime())
		}
	}
}

// --- Test: Partial failure resilience ---

func TestTileFetchIntegrationPartialFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tiles := ComputeTiles(integTestLat, integTestLon, integTestRadius, 0.05)
	if len(tiles) < 2 {
		t.Skip("need at least 2 tiles for partial failure test")
	}

	// The first tile will fail; all others succeed.
	failTile := tiles[0]
	successTiles := tiles[1:]

	responses := buildTileResponses(t, successTiles, nil)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		south, west, north, east, parseErr := parseBBoxFromBody(string(body))
		if parseErr != nil {
			http.Error(w, "bad bbox", http.StatusBadRequest)
			return
		}
		key := bboxKey(south, west, north, east)
		failKey := tileKey(failTile)
		if key == failKey {
			// Return 500 (transient, will be retried, but all retries fail).
			// Use 400 (non-retryable) to avoid retry delays.
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, ok := responses[key]
		if !ok {
			data = makeWaysJSON(t, nil)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data) //nolint:errcheck
	}))
	defer ts.Close()

	cache := &TileCache{Dir: t.TempDir(), Precision: 3}
	cfg := integrationCfg(ts.URL, cache)

	ways, err := FetchTiledWays(context.Background(), integTestLat, integTestLon, integTestRadius, cfg)
	if err != nil {
		t.Fatalf("FetchTiledWays should not return error on partial failure, got: %v", err)
	}

	// Should have results from the successful tiles.
	if len(ways) == 0 {
		t.Error("expected ways from successful tiles, got none")
	}

	// No cache file should exist for the failed tile.
	if cache.Has(failTile) {
		t.Errorf("expected no cache file for failed tile %s, but one exists", tileKey(failTile))
	}

	// Cache files should exist for successful tiles.
	for _, tile := range successTiles {
		if !cache.Has(tile) {
			t.Errorf("expected cache file for successful tile %s, but not found", tileKey(tile))
		}
	}
}

// --- Test: Retry behavior on transient error ---

func TestTileFetchIntegrationRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tiles := ComputeTiles(integTestLat, integTestLon, integTestRadius, 0.05)
	if len(tiles) == 0 {
		t.Fatal("expected at least one tile")
	}

	// Use only one tile to keep this test simple and fast.
	// Pre-populate all other tiles in cache.
	targetTile := tiles[0]
	otherTiles := tiles[1:]

	cache := &TileCache{Dir: t.TempDir(), Precision: 3}
	for _, tile := range otherTiles {
		if err := cache.Write(tile, makeWaysJSON(t, []overpassElement{
			{ID: int64(tile.South*1000) + 1, Tags: map[string]string{"highway": "residential"}, Geometry: []overpassGeomPoint{{Lat: tile.South, Lon: tile.West}}},
		})); err != nil {
			t.Fatalf("pre-populate cache: %v", err)
		}
	}

	targetResponse := makeWaysJSON(t, []overpassElement{
		{ID: 77001, Tags: map[string]string{"highway": "primary"}, Geometry: []overpassGeomPoint{{Lat: targetTile.South, Lon: targetTile.West}}},
	})

	var targetRequestCount int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		south, west, north, east, parseErr := parseBBoxFromBody(string(body))
		if parseErr != nil {
			http.Error(w, "bad bbox", http.StatusBadRequest)
			return
		}
		key := bboxKey(south, west, north, east)
		if key == tileKey(targetTile) {
			n := atomic.AddInt64(&targetRequestCount, 1)
			if n <= 2 {
				// First two requests: return 429 (transient, retryable).
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			// Third request: success.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(targetResponse) //nolint:errcheck
			return
		}
		// Other tiles should not be requested (served from cache).
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cfg := integrationCfg(ts.URL, cache)

	ways, err := FetchTiledWays(context.Background(), integTestLat, integTestLon, integTestRadius, cfg)
	if err != nil {
		t.Fatalf("FetchTiledWays: %v", err)
	}

	// The target tile should have been requested exactly 3 times (2 retries + 1 success).
	if n := atomic.LoadInt64(&targetRequestCount); n != 3 {
		t.Errorf("expected 3 requests for target tile (2 retries + 1 success), got %d", n)
	}

	// The target tile should have been eventually fetched.
	if !cache.Has(targetTile) {
		t.Error("expected cache file for successfully retried tile")
	}

	// Should have returned way 77001 from the target tile.
	found := false
	for _, w := range ways {
		if w.ID == 77001 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected way 77001 from target tile in results")
	}
}

// --- Test: Way ID deduplication accuracy ---

func TestTileFetchIntegrationDedup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tiles := ComputeTiles(integTestLat, integTestLon, integTestRadius, 0.05)
	if len(tiles) < 3 {
		t.Skip("need at least 3 tiles for dedup test")
	}

	// A shared way that appears in all tiles with identical geometry.
	const sharedID int64 = 42424242
	sharedGeom := []overpassGeomPoint{
		{Lat: 42.351, Lon: -72.601},
		{Lat: 42.352, Lon: -72.600},
		{Lat: 42.353, Lon: -72.599},
	}
	sharedElement := overpassElement{
		ID:       sharedID,
		Tags:     map[string]string{"highway": "primary"},
		Geometry: sharedGeom,
	}

	// Build responses: shared way + one unique way per tile.
	responses := make(map[string][]byte, len(tiles))
	for i, tile := range tiles {
		elements := []overpassElement{
			sharedElement,
			{
				ID:       int64(i + 1),
				Tags:     map[string]string{"highway": "residential"},
				Geometry: []overpassGeomPoint{{Lat: tile.South + 0.01, Lon: tile.West + 0.01}},
			},
		}
		responses[tileKey(tile)] = makeWaysJSON(t, elements)
	}

	requestCounts := make(map[string]*int64, len(tiles))
	for _, tile := range tiles {
		var c int64
		requestCounts[tileKey(tile)] = &c
	}
	ts := newMockOverpassServer(t, responses, requestCounts)
	defer ts.Close()

	cache := &TileCache{Dir: t.TempDir(), Precision: 3}
	cfg := integrationCfg(ts.URL, cache)

	ways, err := FetchTiledWays(context.Background(), integTestLat, integTestLon, integTestRadius, cfg)
	if err != nil {
		t.Fatalf("FetchTiledWays: %v", err)
	}

	// The shared way should appear exactly once.
	idCounts := make(map[int64]int)
	var sharedWay *Way
	for i := range ways {
		idCounts[ways[i].ID]++
		if ways[i].ID == sharedID {
			sharedWay = &ways[i]
		}
	}
	if idCounts[sharedID] != 1 {
		t.Errorf("shared way %d appears %d times, want 1", sharedID, idCounts[sharedID])
	}

	// The shared way's geometry should be complete (3 points).
	if sharedWay == nil {
		t.Fatal("shared way not found in results")
	}
	if len(sharedWay.Geometry) != 3 {
		t.Errorf("shared way geometry has %d points, want 3", len(sharedWay.Geometry))
	}
	expectedCoords := [][2]float64{{42.351, -72.601}, {42.352, -72.600}, {42.353, -72.599}}
	for i, coord := range sharedWay.Geometry {
		if coord.Lat != expectedCoords[i][0] || coord.Lon != expectedCoords[i][1] {
			t.Errorf("geometry[%d] = {%v, %v}, want {%v, %v}",
				i, coord.Lat, coord.Lon, expectedCoords[i][0], expectedCoords[i][1])
		}
	}

	// Total unique ways: 1 shared + len(tiles) unique.
	expectedWays := 1 + len(tiles)
	if len(ways) != expectedWays {
		t.Errorf("expected %d unique ways, got %d", expectedWays, len(ways))
	}
}

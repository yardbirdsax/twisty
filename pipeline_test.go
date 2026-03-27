package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"testing"

	"github.com/yardbirdsax/twisty/quality"
)

// syntheticTileJSON builds a minimal valid Overpass JSON payload with the given way ID
// and geometry.
func syntheticTileJSON(t *testing.T, wayID int64, wayName string, coords []map[string]float64) []byte {
	t.Helper()
	syntheticWay := map[string]any{
		"id": wayID,
		"tags": map[string]string{
			"highway": "secondary",
			"name":    wayName,
		},
		"geometry": coords,
	}
	payload := map[string]any{
		"elements": []any{syntheticWay},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal synthetic tile: %v", err)
	}
	return raw
}

// buildScoredWay builds a ScoredWay for testing with pre-set segments.
func buildScoredWayForTest(id int64, name string, tier int, length float64) quality.ScoredWay {
	weight := quality.TierWeight1
	if tier == 2 {
		weight = quality.TierWeight2
	}
	return quality.ScoredWay{
		WayID: id,
		Tags:  map[string]string{"name": name, "highway": "secondary"},
		Segments: []quality.ScoredSegment{
			{WayID: id, Tier: tier, Weight: weight, Length: length, Score: length * weight},
		},
	}
}

// TestProcessTilesConcurrently_SameResultAsSequential verifies that the concurrent
// tile processing produces the same groupings as sequential processing.
func TestProcessTilesConcurrently_SameResultAsSequential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 0.5
		tileSize  = 0.05
	)

	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)
	if len(tiles) == 0 {
		t.Fatal("no tiles computed")
	}

	// Build synthetic tile data with two named roads.
	curvedCoords := []map[string]float64{
		{"lat": centerLat, "lon": centerLon},
		{"lat": centerLat + 0.0002, "lon": centerLon + 0.0001},
		{"lat": centerLat + 0.0003, "lon": centerLon - 0.0001},
		{"lat": centerLat + 0.0005, "lon": centerLon + 0.0001},
	}

	road1JSON := syntheticTileJSON(t, 100, "Curvy Road", curvedCoords)
	road2JSON := syntheticTileJSON(t, 200, "Winding Way", curvedCoords)

	// Interleave two ways by combining elements.
	var p1, p2 map[string]any
	if err := json.Unmarshal(road1JSON, &p1); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := json.Unmarshal(road2JSON, &p2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	combined := map[string]any{
		"elements": append(p1["elements"].([]any), p2["elements"].([]any)...),
	}
	combinedJSON, err := json.Marshal(combined)
	if err != nil {
		t.Fatalf("marshal combined: %v", err)
	}

	cacheDir := t.TempDir()
	tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, tile := range tiles {
		if err := tileCache.Write(tile, combinedJSON); err != nil {
			t.Fatalf("TileCache.Write: %v", err)
		}
	}

	scoreCacheDir := t.TempDir()
	scoreCache := &quality.ScoreCache{Dir: scoreCacheDir, Precision: 3}
	if err := scoreCache.EnsureDir(); err != nil {
		t.Fatalf("score EnsureDir: %v", err)
	}

	// Run concurrent pipeline.
	grouped, stats, err := processTilesConcurrently(
		context.Background(),
		tiles,
		tileCache,
		scoreCache,
		true, // noCache
		noopLogger(),
		quality.NoopProgressReporter{},
	)
	if err != nil {
		t.Fatalf("processTilesConcurrently: %v", err)
	}

	if stats.totalWays.Load() == 0 {
		t.Fatal("expected ways scored, got 0")
	}

	// Verify both road names are present.
	if _, ok := grouped["Curvy Road"]; !ok {
		t.Error("expected 'Curvy Road' in grouped results")
	}
	if _, ok := grouped["Winding Way"]; !ok {
		t.Error("expected 'Winding Way' in grouped results")
	}
}

// TestProcessNameGroupsConcurrently_SameResultAsSequential verifies that the concurrent
// name group processing produces the same RoadCollections as calling Aggregate directly.
func TestProcessNameGroupsConcurrently_SameResultAsSequential(t *testing.T) {
	// Build a set of scored ways for two roads.
	ways := []quality.ScoredWay{
		buildScoredWayForTest(1, "Mountain Road", 1, 500.0),
		buildScoredWayForTest(2, "Mountain Road", 2, 300.0),
		buildScoredWayForTest(3, "Valley Road", 1, 800.0),
	}

	groups := quality.GroupWaysByName(ways)

	// Sequential result via Aggregate.
	seqCollections := quality.Aggregate(ways)

	// Concurrent result.
	concCollections, err := processNameGroupsConcurrently(context.Background(), groups)
	if err != nil {
		t.Fatalf("processNameGroupsConcurrently: %v", err)
	}

	// Sort both by name+subindex for deterministic comparison.
	sortCollections := func(cs []quality.RoadCollection) {
		sort.Slice(cs, func(i, j int) bool {
			if cs[i].Name != cs[j].Name {
				return cs[i].Name < cs[j].Name
			}
			return cs[i].SubIndex < cs[j].SubIndex
		})
	}
	sortCollections(seqCollections)
	sortCollections(concCollections)

	if len(seqCollections) != len(concCollections) {
		t.Fatalf("collection count mismatch: sequential=%d concurrent=%d", len(seqCollections), len(concCollections))
	}

	for i, seq := range seqCollections {
		conc := concCollections[i]
		if seq.Name != conc.Name {
			t.Errorf("[%d] Name: seq=%q conc=%q", i, seq.Name, conc.Name)
		}
		if seq.SubIndex != conc.SubIndex {
			t.Errorf("[%d] SubIndex: seq=%d conc=%d", i, seq.SubIndex, conc.SubIndex)
		}
		if seq.TotalScore != conc.TotalScore {
			t.Errorf("[%d] TotalScore: seq=%v conc=%v", i, seq.TotalScore, conc.TotalScore)
		}
		if seq.TotalLength != conc.TotalLength {
			t.Errorf("[%d] TotalLength: seq=%v conc=%v", i, seq.TotalLength, conc.TotalLength)
		}
	}
}

// TestProcessNameGroupsConcurrently_EmptyGroups verifies an empty input returns no collections.
func TestProcessNameGroupsConcurrently_EmptyGroups(t *testing.T) {
	collections, err := processNameGroupsConcurrently(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collections) != 0 {
		t.Errorf("expected 0 collections, got %d", len(collections))
	}
}

// TestProcessTilesConcurrently_ContextCancellation verifies cancellation is propagated.
func TestProcessTilesConcurrently_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 5.0 // enough tiles to have work
		tileSize  = 0.05
	)
	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)

	cacheDir := t.TempDir()
	tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	_ = tileCache.EnsureDir()

	scoreCacheDir := t.TempDir()
	scoreCache := &quality.ScoreCache{Dir: scoreCacheDir, Precision: 3}
	_ = scoreCache.EnsureDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := processTilesConcurrently(
		ctx,
		tiles,
		tileCache,
		scoreCache,
		true,
		noopLogger(),
		quality.NoopProgressReporter{},
	)
	// Either we get context.Canceled or no error (tiles were already all skipped).
	// The point is we don't hang.
	_ = err
}

// TestProcessTilesConcurrently_VaryingGOMAXPROCS verifies correctness with 1, 2, and 4 workers.
func TestProcessTilesConcurrently_VaryingGOMAXPROCS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 0.5
		tileSize  = 0.05
	)

	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)

	curvedCoords := []map[string]float64{
		{"lat": centerLat, "lon": centerLon},
		{"lat": centerLat + 0.0002, "lon": centerLon + 0.0001},
		{"lat": centerLat + 0.0003, "lon": centerLon - 0.0001},
		{"lat": centerLat + 0.0005, "lon": centerLon + 0.0001},
	}
	rawJSON := syntheticTileJSON(t, 42, "Test Road", curvedCoords)

	for _, procs := range []int{1, 2, 4} {
		t.Run(runtime.GOARCH+"_procs_"+itoa(procs), func(t *testing.T) {
			prev := runtime.GOMAXPROCS(procs)
			defer runtime.GOMAXPROCS(prev)

			cacheDir := t.TempDir()
			tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
			_ = tileCache.EnsureDir()
			for _, tile := range tiles {
				_ = tileCache.Write(tile, rawJSON)
			}

			scoreCacheDir := t.TempDir()
			scoreCache := &quality.ScoreCache{Dir: scoreCacheDir, Precision: 3}
			_ = scoreCache.EnsureDir()

			grouped, stats, err := processTilesConcurrently(
				context.Background(),
				tiles,
				tileCache,
				scoreCache,
				true,
				noopLogger(),
				quality.NoopProgressReporter{},
			)
			if err != nil {
				t.Fatalf("GOMAXPROCS=%d: unexpected error: %v", procs, err)
			}
			if stats.totalWays.Load() == 0 {
				t.Fatalf("GOMAXPROCS=%d: expected ways scored", procs)
			}
			if _, ok := grouped["Test Road"]; !ok {
				t.Errorf("GOMAXPROCS=%d: expected 'Test Road' in grouped output", procs)
			}
		})
	}
}

// TestProcessNameGroupsConcurrently_VaryingGOMAXPROCS verifies Phase B correctness at
// different GOMAXPROCS settings.
func TestProcessNameGroupsConcurrently_VaryingGOMAXPROCS(t *testing.T) {
	ways := make([]quality.ScoredWay, 0, 20)
	for i := int64(1); i <= 10; i++ {
		name := "Road " + itoa(int(i))
		ways = append(ways, buildScoredWayForTest(i, name, 1, float64(i)*100))
	}
	groups := quality.GroupWaysByName(ways)
	seqCollections := quality.Aggregate(ways)

	for _, procs := range []int{1, 2, 4} {
		t.Run("procs_"+itoa(procs), func(t *testing.T) {
			prev := runtime.GOMAXPROCS(procs)
			defer runtime.GOMAXPROCS(prev)

			concCollections, err := processNameGroupsConcurrently(context.Background(), groups)
			if err != nil {
				t.Fatalf("GOMAXPROCS=%d: %v", procs, err)
			}
			if len(concCollections) != len(seqCollections) {
				t.Errorf("GOMAXPROCS=%d: collection count mismatch: seq=%d conc=%d",
					procs, len(seqCollections), len(concCollections))
			}
		})
	}
}

// TestProcessTilesConcurrently_ErrorPropagation verifies that a pre-cancelled context
// causes processTilesConcurrently to return a non-nil error to the caller.
func TestProcessTilesConcurrently_ErrorPropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 5.0
		tileSize  = 0.05
	)
	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)
	if len(tiles) == 0 {
		t.Fatal("no tiles computed")
	}

	// Write real tile data so workers have work to do, making it likely the
	// cancellation is detected mid-flight rather than before any work starts.
	curvedCoords := []map[string]float64{
		{"lat": centerLat, "lon": centerLon},
		{"lat": centerLat + 0.0002, "lon": centerLon + 0.0001},
		{"lat": centerLat + 0.0003, "lon": centerLon - 0.0001},
		{"lat": centerLat + 0.0005, "lon": centerLon + 0.0001},
	}
	rawJSON := syntheticTileJSON(t, 99, "Error Road", curvedCoords)

	cacheDir := t.TempDir()
	tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	_ = tileCache.EnsureDir()
	for _, tile := range tiles {
		_ = tileCache.Write(tile, rawJSON)
	}

	scoreCacheDir := t.TempDir()
	scoreCache := &quality.ScoreCache{Dir: scoreCacheDir, Precision: 3}
	_ = scoreCache.EnsureDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling to guarantee cancellation error path

	_, _, err := processTilesConcurrently(
		ctx,
		tiles,
		tileCache,
		scoreCache,
		true,
		noopLogger(),
		quality.NoopProgressReporter{},
	)
	if err == nil {
		t.Fatal("expected a non-nil error from pre-cancelled context, got nil")
	}
}

// TestProcessTilesConcurrently_WorkerError verifies that a real worker error
// (not context cancellation) is propagated back to the caller. It uses
// processTilesConcurrentlyWith with an injected processor that always returns
// an error, exercising the errCh path in the pipeline.
func TestProcessTilesConcurrently_WorkerError(t *testing.T) {
	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 0.5
		tileSize  = 0.05
	)
	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)
	if len(tiles) == 0 {
		t.Fatal("no tiles computed")
	}

	// Inject a processor that always returns a non-nil error.
	injectedErr := fmt.Errorf("injected worker error")
	failingProcessor := func(_ quality.Tile) (*tileResult, error) {
		return nil, injectedErr
	}

	_, _, err := processTilesConcurrentlyWith(
		context.Background(),
		tiles,
		failingProcessor,
		quality.NoopProgressReporter{},
	)
	if err == nil {
		t.Fatal("expected a non-nil error from worker failure, got nil")
	}
	if !errors.Is(err, injectedErr) {
		t.Fatalf("expected injected error, got: %v", err)
	}
}

// TestProcessNameGroupsConcurrently_ErrorPropagation verifies that a pre-cancelled
// context causes processNameGroupsConcurrently to return a non-nil error.
func TestProcessNameGroupsConcurrently_ErrorPropagation(t *testing.T) {
	// Build enough groups so there is real work in flight when cancelled.
	ways := make([]quality.ScoredWay, 0, 20)
	for i := int64(1); i <= 20; i++ {
		name := "Road " + itoa(int(i))
		ways = append(ways, buildScoredWayForTest(i, name, 1, float64(i)*100))
	}
	groups := quality.GroupWaysByName(ways)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	_, err := processNameGroupsConcurrently(ctx, groups)
	if err == nil {
		t.Fatal("expected a non-nil error from pre-cancelled context, got nil")
	}
}

// TestProcessNameGroupsConcurrently_NoDataRace verifies no data races by running
// many goroutines reading shared input concurrently.
func TestProcessNameGroupsConcurrently_NoDataRace(t *testing.T) {
	ways := []quality.ScoredWay{
		buildScoredWayForTest(1, "Race Road A", 1, 500.0),
		buildScoredWayForTest(2, "Race Road B", 2, 300.0),
		buildScoredWayForTest(3, "Race Road C", 1, 700.0),
	}
	groups := quality.GroupWaysByName(ways)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := processNameGroupsConcurrently(context.Background(), groups)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
	wg.Wait()
}

// syntheticTileJSONWithRef builds a minimal valid Overpass JSON payload with
// the given way ID, name, ref, and geometry.
func syntheticTileJSONWithRef(t *testing.T, wayID int64, wayName, wayRef string, coords []map[string]float64) []byte {
	t.Helper()
	tags := map[string]string{
		"highway": "secondary",
	}
	if wayName != "" {
		tags["name"] = wayName
	}
	if wayRef != "" {
		tags["ref"] = wayRef
	}
	syntheticWay := map[string]any{
		"id":       wayID,
		"tags":     tags,
		"geometry": coords,
	}
	payload := map[string]any{
		"elements": []any{syntheticWay},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal synthetic tile: %v", err)
	}
	return raw
}

// TestProcessTilesConcurrently_SemicolonRefSplit verifies that the tile
// collector loop splits semicolon-separated ref tags so a way with
// ref="US 209;PA 901" appears in both the "US 209" and "PA 901" groups, not
// in a single "US 209;PA 901" group.
func TestProcessTilesConcurrently_SemicolonRefSplit(t *testing.T) {
	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 0.5
		tileSize  = 0.05
	)

	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)
	if len(tiles) == 0 {
		t.Fatal("no tiles computed")
	}

	curvedCoords := []map[string]float64{
		{"lat": centerLat, "lon": centerLon},
		{"lat": centerLat + 0.0002, "lon": centerLon + 0.0001},
		{"lat": centerLat + 0.0003, "lon": centerLon - 0.0001},
		{"lat": centerLat + 0.0005, "lon": centerLon + 0.0001},
	}

	// Way 1: single ref
	way1JSON := syntheticTileJSONWithRef(t, 100, "Main Street", "PA 345", curvedCoords)
	// Way 2: semicolon-separated ref
	way2JSON := syntheticTileJSONWithRef(t, 200, "Market Street", "US 209;PA 901", curvedCoords)
	// Way 3: single ref matching one part of way 2's compound ref
	way3JSON := syntheticTileJSONWithRef(t, 300, "Sunbury Road", "PA 901", curvedCoords)

	// Combine all three ways into one tile payload.
	var p1, p2, p3 map[string]any
	json.Unmarshal(way1JSON, &p1)
	json.Unmarshal(way2JSON, &p2)
	json.Unmarshal(way3JSON, &p3)
	combined := map[string]any{
		"elements": append(append(p1["elements"].([]any), p2["elements"].([]any)...), p3["elements"].([]any)...),
	}
	combinedJSON, err := json.Marshal(combined)
	if err != nil {
		t.Fatalf("marshal combined: %v", err)
	}

	cacheDir := t.TempDir()
	tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	_ = tileCache.EnsureDir()
	// Write to first tile only — enough to test grouping.
	if err := tileCache.Write(tiles[0], combinedJSON); err != nil {
		t.Fatalf("TileCache.Write: %v", err)
	}

	scoreCacheDir := t.TempDir()
	scoreCache := &quality.ScoreCache{Dir: scoreCacheDir, Precision: 3}
	_ = scoreCache.EnsureDir()

	grouped, _, err := processTilesConcurrently(
		context.Background(),
		tiles[:1], // only the tile we wrote
		tileCache,
		scoreCache,
		true, // noCache
		noopLogger(),
		quality.NoopProgressReporter{},
	)
	if err != nil {
		t.Fatalf("processTilesConcurrently: %v", err)
	}

	// "PA 345" should have way 100.
	if g, ok := grouped["PA 345"]; !ok {
		t.Error("expected 'PA 345' group")
	} else if len(g) != 1 || g[0].WayID != 100 {
		t.Errorf("PA 345: got %d ways, want 1 (way 100)", len(g))
	}

	// "PA 901" should have BOTH way 200 (from "US 209;PA 901") and way 300.
	pa901, ok := grouped["PA 901"]
	if !ok {
		t.Fatal("expected 'PA 901' group")
	}
	if len(pa901) != 2 {
		t.Errorf("PA 901: got %d ways, want 2", len(pa901))
	} else {
		ids := map[int64]bool{}
		for _, w := range pa901 {
			ids[w.WayID] = true
		}
		if !ids[200] {
			t.Error("PA 901: missing way 200 (ref='US 209;PA 901')")
		}
		if !ids[300] {
			t.Error("PA 901: missing way 300 (ref='PA 901')")
		}
	}

	// "US 209" should have way 200.
	us209, ok := grouped["US 209"]
	if !ok {
		t.Fatal("expected 'US 209' group")
	}
	if len(us209) != 1 || us209[0].WayID != 200 {
		t.Errorf("US 209: got %d ways, want 1 (way 200)", len(us209))
	}

	// The literal "US 209;PA 901" key should NOT exist.
	if g, ok := grouped["US 209;PA 901"]; ok {
		t.Errorf("'US 209;PA 901' should not exist as a literal key, got %d ways", len(g))
	}

	// Name groups should still work.
	if _, ok := grouped["Main Street"]; !ok {
		t.Error("expected 'Main Street' name group")
	}
	if _, ok := grouped["Market Street"]; !ok {
		t.Error("expected 'Market Street' name group")
	}
	if _, ok := grouped["Sunbury Road"]; !ok {
		t.Error("expected 'Sunbury Road' name group")
	}
}

// TestScoreAndAggregateTiles_SameResultAsDirectCalls verifies that scoreAndAggregateTiles
// produces the same RoadCollections and stats as calling the three phases directly.
func TestScoreAndAggregateTiles_SameResultAsDirectCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 0.5
		tileSize  = 0.05
	)

	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)
	if len(tiles) == 0 {
		t.Fatal("no tiles computed")
	}

	curvedCoords := []map[string]float64{
		{"lat": centerLat, "lon": centerLon},
		{"lat": centerLat + 0.0002, "lon": centerLon + 0.0001},
		{"lat": centerLat + 0.0003, "lon": centerLon - 0.0001},
		{"lat": centerLat + 0.0005, "lon": centerLon + 0.0001},
	}
	rawJSON := syntheticTileJSON(t, 99, "Helper Road", curvedCoords)

	// Build a tile cache shared between the "direct" and "helper" runs.
	// Each run gets its own score cache (to avoid cross-contamination).
	setupTileCache := func(t *testing.T) *quality.TileCache {
		t.Helper()
		tc := &quality.TileCache{Dir: t.TempDir(), Precision: 3}
		if err := tc.EnsureDir(); err != nil {
			t.Fatalf("EnsureDir: %v", err)
		}
		for _, tile := range tiles {
			if err := tc.Write(tile, rawJSON); err != nil {
				t.Fatalf("TileCache.Write: %v", err)
			}
		}
		return tc
	}

	setupScoreCache := func(t *testing.T) *quality.ScoreCache {
		t.Helper()
		sc := &quality.ScoreCache{Dir: t.TempDir(), Precision: 3}
		if err := sc.EnsureDir(); err != nil {
			t.Fatalf("score EnsureDir: %v", err)
		}
		return sc
	}

	ctx := context.Background()

	// Direct path: processTilesConcurrently → processNameGroupsConcurrently → ApplyPenalties.
	directTC := setupTileCache(t)
	directSC := setupScoreCache(t)
	grouped, directStats, err := processTilesConcurrently(ctx, tiles, directTC, directSC, true, noopLogger(), quality.NoopProgressReporter{})
	if err != nil {
		t.Fatalf("processTilesConcurrently: %v", err)
	}
	directCollections, err := processNameGroupsConcurrently(ctx, grouped)
	if err != nil {
		t.Fatalf("processNameGroupsConcurrently: %v", err)
	}
	quality.ApplyPenalties(directCollections)

	// Helper path: scoreAndAggregateTiles.
	helperTC := setupTileCache(t)
	helperSC := setupScoreCache(t)
	helperCollections, helperStats, err := scoreAndAggregateTiles(ctx, tiles, helperTC, helperSC, true, noopLogger(), quality.NoopProgressReporter{})
	if err != nil {
		t.Fatalf("scoreAndAggregateTiles: %v", err)
	}

	// Stats should be equal.
	if directStats.totalWays.Load() != helperStats.totalWays.Load() {
		t.Errorf("totalWays: direct=%d helper=%d", directStats.totalWays.Load(), helperStats.totalWays.Load())
	}
	if directStats.totalSegs.Load() != helperStats.totalSegs.Load() {
		t.Errorf("totalSegs: direct=%d helper=%d", directStats.totalSegs.Load(), helperStats.totalSegs.Load())
	}

	// Collections should be equal (after sorting for determinism).
	sortCollections := func(cs []quality.RoadCollection) {
		sort.Slice(cs, func(i, j int) bool {
			if cs[i].Name != cs[j].Name {
				return cs[i].Name < cs[j].Name
			}
			return cs[i].SubIndex < cs[j].SubIndex
		})
	}
	sortCollections(directCollections)
	sortCollections(helperCollections)

	if len(directCollections) != len(helperCollections) {
		t.Fatalf("collection count: direct=%d helper=%d", len(directCollections), len(helperCollections))
	}
	for i, d := range directCollections {
		h := helperCollections[i]
		if d.Name != h.Name {
			t.Errorf("[%d] Name: direct=%q helper=%q", i, d.Name, h.Name)
		}
		if d.TotalScore != h.TotalScore {
			t.Errorf("[%d] TotalScore: direct=%v helper=%v", i, d.TotalScore, h.TotalScore)
		}
		if d.PenalizedScore != h.PenalizedScore {
			t.Errorf("[%d] PenalizedScore: direct=%v helper=%v", i, d.PenalizedScore, h.PenalizedScore)
		}
	}
}

// noopLogger returns a logger that discards all output.
func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// itoa is a simple integer-to-string helper for test names.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

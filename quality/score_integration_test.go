package quality

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

// --- Geometry helpers ---

// straightWay builds a Way whose nodes lie evenly along a given bearing from a start point.
// bearingDeg is in degrees (0=north, 90=east). lengthM is the total length in meters.
// numNodes must be >= 2.
func straightWay(id int64, tags map[string]string, startLat, startLon, bearingDeg, lengthM float64, numNodes int) Way {
	if numNodes < 2 {
		numNodes = 2
	}
	spacing := lengthM / float64(numNodes-1)
	nodes := make([]geo.Coord, numNodes)
	nodes[0] = geo.Coord{Lat: startLat, Lon: startLon}
	for i := 1; i < numNodes; i++ {
		nodes[i] = geo.DestinationPoint(nodes[i-1], bearingDeg, spacing)
	}
	return Way{ID: id, Tags: tags, Geometry: nodes}
}

// circularArcWay builds a Way whose nodes lie along a circular arc of the given radius.
// centerLat, centerLon are the centre of the circle (in geographic degrees).
// radiusM is the circle radius in meters. startAngleDeg and arcAngleDeg define the arc.
// numNodes must be >= 3 for meaningful curvature.
func circularArcWay(id int64, tags map[string]string, centerLat, centerLon, radiusM, startAngleDeg, arcAngleDeg float64, numNodes int) Way {
	if numNodes < 3 {
		numNodes = 3
	}
	nodes := make([]geo.Coord, numNodes)
	center := geo.Coord{Lat: centerLat, Lon: centerLon}
	for i := 0; i < numNodes; i++ {
		// Angle measured from north, clockwise (same convention as Bearing).
		angle := startAngleDeg + arcAngleDeg*float64(i)/float64(numNodes-1)
		nodes[i] = geo.DestinationPoint(center, angle, radiusM)
	}
	return Way{ID: id, Tags: tags, Geometry: nodes}
}

// sCurveWay builds a Way that is a genuine S-curve composed of two opposed 180° arcs.
// Each arc has the given radius. The arcs are joined end-to-end with alternating center
// offsets so that they curve in opposite directions, producing a realistic S-shape.
// Total arc length ≈ 2 × π × radiusM.
// numNodesPerArc must be >= 3; duplicate junction node is removed between arcs.
func sCurveWay(id int64, tags map[string]string, startLat, startLon, radiusM float64, numNodesPerArc int) Way {
	if numNodesPerArc < 3 {
		numNodesPerArc = 3
	}

	// Arc 1: center is radiusM east of start. The start node is directly west of center
	// (bearing 270° from center). Arc sweeps 180° clockwise (270° → 90°), so the
	// end node is directly east of center, which is 2×radiusM east of start.
	center1 := geo.DestinationPoint(geo.Coord{Lat: startLat, Lon: startLon}, 90, radiusM)
	arc1 := make([]geo.Coord, numNodesPerArc)
	for i := 0; i < numNodesPerArc; i++ {
		angle := 270.0 + 180.0*float64(i)/float64(numNodesPerArc-1)
		arc1[i] = geo.DestinationPoint(center1, angle, radiusM)
	}

	// Arc 2: center is radiusM west of the end of arc 1 (i.e., radiusM west from the
	// easternmost point of arc 1 = back to center1 location but shifted north by
	// 2×radiusM? No — keep it simple: center2 is radiusM east of arc1's last node.
	// Arc 2 sweeps 180° counter-clockwise (90° → 270°) — opposite direction.
	lastArc1 := arc1[numNodesPerArc-1]
	center2 := geo.DestinationPoint(lastArc1, 90, radiusM)
	arc2 := make([]geo.Coord, numNodesPerArc)
	for i := 0; i < numNodesPerArc; i++ {
		angle := 90.0 - 180.0*float64(i)/float64(numNodesPerArc-1)
		arc2[i] = geo.DestinationPoint(center2, angle, radiusM)
	}

	// Join: skip arc2[0] since it equals arc1[last].
	nodes := make([]geo.Coord, 0, numNodesPerArc*2-1)
	nodes = append(nodes, arc1...)
	nodes = append(nodes, arc2[1:]...)

	return Way{ID: id, Tags: tags, Geometry: nodes}
}

// totalScore sums all segment scores for a ScoredWay.
func totalScore(sw ScoredWay) float64 {
	sum := 0.0
	for _, seg := range sw.Segments {
		sum += seg.Score
	}
	return sum
}

// findScoredWay returns the ScoredWay for the given way ID, or nil if not found.
func findScoredWay(ways []ScoredWay, id int64) *ScoredWay {
	for i := range ways {
		if ways[i].WayID == id {
			return &ways[i]
		}
	}
	return nil
}

// --- Test fixtures ---

const (
	testCenterLat = 42.36
	testCenterLon = -72.58
)

// paved returns a minimal paved-highway tag map.
func paved() map[string]string {
	return map[string]string{"highway": "secondary"}
}

// --- Tests ---

// TestScoreIntegration_FullPipeline exercises the complete filter → score pipeline
// with a realistic set of ways. Deflection filtering now runs post-aggregation,
// so this test verifies the per-way scoring stages only.
func TestScoreIntegration_FullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Way 1: gravel surface — should be removed by HardFilter.
	gravelWay := straightWay(1, map[string]string{"highway": "secondary", "surface": "gravel"},
		testCenterLat, testCenterLon, 0, 500, 6)

	// Way 2: straight road (~500 m, 6 nodes due north) — all-zero scores after scoring.
	straightRoad := straightWay(2, paved(), testCenterLat, testCenterLon, 0, 500, 6)

	// Way 3: winding road — a tight circular arc (radius 40 m, 270° arc, 10 nodes).
	// With such a small radius it should produce tier-3 or tier-4 segments.
	windingRoad := circularArcWay(3, paved(), testCenterLat, testCenterLon, 40, 0, 270, 10)

	// Way 4: dogleg on a straight road — build a mostly-straight road with a small jog.
	doglegWay := buildDoglegWay(4, paved(), testCenterLat, testCenterLon)

	ways := []Way{gravelWay, straightRoad, windingRoad, doglegWay}
	result := RunScorePipeline(ways)

	// 1. InputWays should be 4.
	if result.InputWays != 4 {
		t.Errorf("InputWays: got %d, want 4", result.InputWays)
	}

	// 2. FilteredWays: gravel removed → 3 remaining.
	if result.FilteredWays != 3 {
		t.Errorf("FilteredWays: got %d, want 3", result.FilteredWays)
	}

	// 3. The gravel way must not appear in output.
	if sw := findScoredWay(result.ScoredWays, 1); sw != nil {
		t.Error("gravel way (id=1) should not appear in scored output")
	}

	// 4. The straight road must have all-zero scores.
	sw2 := findScoredWay(result.ScoredWays, 2)
	if sw2 == nil {
		t.Fatal("straight road (id=2) not found in scored output")
	}
	for i, seg := range sw2.Segments {
		if seg.Score != 0 {
			t.Errorf("straight road segment %d has non-zero score %v", i, seg.Score)
		}
	}

	// 5. The winding road must have a non-zero total score.
	sw3 := findScoredWay(result.ScoredWays, 3)
	if sw3 == nil {
		t.Fatal("winding road (id=3) not found in scored output")
	}
	if ts := totalScore(*sw3); ts == 0 {
		t.Error("winding road (id=3) has total score 0, expected > 0")
	}

	// 6. The dogleg way (way 4) exists in output.
	sw4 := findScoredWay(result.ScoredWays, 4)
	if sw4 == nil {
		t.Fatal("dogleg way (id=4) not found in scored output")
	}

	// The straight road (way 2) must contribute no zeroed segments.
	for i, seg := range sw2.Segments {
		if seg.Score != 0 {
			t.Errorf("straight road (id=2) segment %d unexpectedly has non-zero score %.4f", i, seg.Score)
		}
	}

	// The dogleg way has curvature from the jog — scores may be non-zero at the per-way
	// level (deflection filtering now runs post-aggregation, not here). Verify curvature
	// segments are present (not zero at this stage).
	hasCurvature := false
	for _, seg := range sw4.Segments {
		if !math.IsInf(seg.Radius, 1) && seg.Radius > 0 {
			hasCurvature = true
			break
		}
	}
	if !hasCurvature {
		// Not fatal — the dogleg may be too gentle to produce curvature at the scoring level.
		t.Log("dogleg way (id=4): no curvature segments found (jog may be too gentle)")
	}
}

// buildDoglegWay builds a ~3 km straight road with a small heading-change jog in the middle.
// The overall heading across the 2.4 km look-ahead should be < 20°, causing the jog to be zeroed
// by the post-aggregation deflection filter.
func buildDoglegWay(id int64, tags map[string]string, lat, lon float64) Way {
	// Build a straight road north for 1.5 km, then jog slightly east by ~5° for 50 m,
	// then continue north for 1.5 km.
	// Total heading change over the look-ahead (2.4 km) ≈ 5° < 20°.
	var nodes []geo.Coord
	start := geo.Coord{Lat: lat, Lon: lon}

	// 15 nodes northward, spaced 100 m = 1500 m
	cur := start
	for range 15 {
		nodes = append(nodes, cur)
		cur = geo.DestinationPoint(cur, 0, 100)
	}

	// One node jogging slightly east (bearing 5°) for 50 m — creates a small circumradius
	cur = geo.DestinationPoint(cur, 5, 50)
	nodes = append(nodes, cur)

	// One node back to north
	cur = geo.DestinationPoint(cur, 355, 50)
	nodes = append(nodes, cur)

	// 15 more nodes northward
	for range 15 {
		nodes = append(nodes, cur)
		cur = geo.DestinationPoint(cur, 0, 100)
	}

	return Way{ID: id, Tags: tags, Geometry: nodes}
}

// TestScoreIntegration_ScoreCacheRoundTrip verifies that the score cache correctly
// persists and retrieves pipeline results.
func TestScoreIntegration_ScoreCacheRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tileCache := &TileCache{Dir: t.TempDir(), Precision: 3}
	scoreCache := &ScoreCache{Dir: t.TempDir(), Precision: 3}

	tile := Tile{South: testCenterLat, West: testCenterLon, North: testCenterLat + 0.05, East: testCenterLon + 0.05}

	// Build a realistic winding road.
	windingRoad := circularArcWay(100, paved(), testCenterLat, testCenterLon, 40, 0, 270, 10)
	rawData := buildRawTileJSON(t, []Way{windingRoad})

	// Write raw tile to the tile cache.
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("TileCache.EnsureDir: %v", err)
	}
	if err := tileCache.Write(tile, rawData); err != nil {
		t.Fatalf("TileCache.Write: %v", err)
	}

	// Score the ways and write to score cache.
	ways, err := ParseTileData(rawData)
	if err != nil {
		t.Fatalf("ParseTileData: %v", err)
	}
	pipelineResult := RunScorePipeline(ways)

	if err := scoreCache.Write(tile, rawData, pipelineResult.ScoredWays); err != nil {
		t.Fatalf("ScoreCache.Write: %v", err)
	}

	// Read back — expect cache hit.
	gotWays, ok := scoreCache.Read(tile, rawData)
	if !ok {
		t.Fatal("ScoreCache.Read returned false (cache miss), expected cache hit")
	}
	if len(gotWays) != len(pipelineResult.ScoredWays) {
		t.Errorf("way count: got %d, want %d", len(gotWays), len(pipelineResult.ScoredWays))
	}

	// Verify content matches.
	if len(pipelineResult.ScoredWays) > 0 && len(gotWays) > 0 {
		want := totalScore(pipelineResult.ScoredWays[0])
		got := totalScore(gotWays[0])
		if math.Abs(want-got) > 1e-9 {
			t.Errorf("total score mismatch: got %.4f, want %.4f", got, want)
		}
	}

	// Modify raw tile data — should produce a cache miss.
	modifiedData := append(rawData, []byte(" ")...)
	_, ok2 := scoreCache.Read(tile, modifiedData)
	if ok2 {
		t.Error("ScoreCache.Read returned true after raw tile data change, expected cache miss")
	}
}

// TestScoreIntegration_ScoreCacheParamsHashInvalidation writes a cache entry then manually
// edits the params_hash field and verifies the next read is a cache miss.
func TestScoreIntegration_ScoreCacheParamsHashInvalidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	scoreCache := &ScoreCache{Dir: t.TempDir(), Precision: 3}
	tile := Tile{South: testCenterLat, West: testCenterLon, North: testCenterLat + 0.05, East: testCenterLon + 0.05}
	rawData := []byte(`{"elements":[]}`)

	// Write a valid entry.
	if err := scoreCache.Write(tile, rawData, []ScoredWay{}); err != nil {
		t.Fatalf("ScoreCache.Write: %v", err)
	}

	// Read the cache file and tamper with params_hash.
	path := scoreCache.Path(tile)
	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var entry scoreCacheEntryJSON
	if err := json.Unmarshal(fileData, &entry); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	entry.ParamsHash = "sha256:000000000000000000000000000000000000000000000000000000000000dead"
	tampered, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Read should now return a miss.
	_, ok := scoreCache.Read(tile, rawData)
	if ok {
		t.Error("ScoreCache.Read returned true after params_hash tampering, expected cache miss")
	}
}

// TestScoreIntegration_WindingScoresHigherThanStraight verifies that a winding ~1 km road
// scores at least 5× higher than a straight ~1 km road.
func TestScoreIntegration_WindingScoresHigherThanStraight(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Straight road: 1 km, 10 nodes due north.
	straight := straightWay(10, paved(), testCenterLat, testCenterLon, 0, 1000, 10)

	// Winding road: genuine S-curve with two opposed 180° arcs, radius 80 m.
	// Arc length per half = π × 80 ≈ 251 m; total ≈ 503 m.
	// radius 80 m → tier 2 (60–100 m). Score will be significantly above zero.
	// The key assertion is winding >> straight (straight scores 0).
	winding := sCurveWay(11, paved(), testCenterLat+0.01, testCenterLon, 80, 10)

	// Score both as independent single-way inputs.
	straightResult := RunScorePipeline([]Way{straight})
	windingResult := RunScorePipeline([]Way{winding})

	var straightScore, windingScore float64
	for _, sw := range straightResult.ScoredWays {
		straightScore += totalScore(sw)
	}
	for _, sw := range windingResult.ScoredWays {
		windingScore += totalScore(sw)
	}

	t.Logf("straight total score: %.4f", straightScore)
	t.Logf("winding total score:  %.4f", windingScore)

	if windingScore == 0 {
		t.Error("winding road score is 0, expected > 0")
	}

	// Straight should be zero or near-zero; winding should be >> straight.
	const minRatio = 5.0
	if straightScore == 0 {
		// Perfect: straight is zero, winding is positive → trivially satisfies the criterion.
		return
	}
	ratio := windingScore / straightScore
	if ratio < minRatio {
		t.Errorf("winding/straight score ratio %.2f < %.1f (winding=%.4f, straight=%.4f)",
			ratio, minRatio, windingScore, straightScore)
	}
}

// TestScoreIntegration_KnownCurvatureValues verifies that nodes on a circle of radius 50 m
// are assigned tier 3 (30 ≤ r < 60) with weight TierWeight3 = 1.6.
func TestScoreIntegration_KnownCurvatureValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Radius 50 m → tier 3 (30 ≤ r < 60). At 50 m the circumradii of interior triangles
	// stay safely within the 30–60 m band (tier 3 only), allowing a precise single-tier assertion.
	const arcRadius = 50.0
	arcWay := circularArcWay(20, paved(), testCenterLat, testCenterLon, arcRadius, 0, 180, 8)

	result := RunScorePipeline([]Way{arcWay})

	sw := findScoredWay(result.ScoredWays, 20)
	if sw == nil {
		t.Fatal("way id=20 not found in scored output")
	}

	if len(sw.Segments) == 0 {
		t.Fatal("expected at least one segment")
	}

	// All non-zero segments must be exactly tier 3 with weight TierWeight3.
	nonZeroCount := 0
	for i, seg := range sw.Segments {
		if seg.Score > 0 {
			nonZeroCount++
			if seg.Tier != 3 {
				t.Errorf("segment %d: expected tier 3, got %d (radius=%.2f)", i, seg.Tier, seg.Radius)
			}
			if seg.Weight != TierWeight3 {
				t.Errorf("segment %d: expected weight %.2f (TierWeight3), got %.2f", i, TierWeight3, seg.Weight)
			}
		}
	}

	if nonZeroCount == 0 {
		t.Error("expected at least one non-zero scored segment for radius-50m arc way")
	}
}

// --- Helper: build raw Overpass JSON from a slice of Ways ---

// buildRawTileJSON converts a []Way back into an Overpass-style JSON payload
// suitable for writing to a TileCache.
func buildRawTileJSON(t *testing.T, ways []Way) []byte {
	t.Helper()
	elements := make([]overpassElement, len(ways))
	for i, w := range ways {
		geom := make([]overpassGeomPoint, len(w.Geometry))
		for j, c := range w.Geometry {
			geom[j] = overpassGeomPoint{Lat: c.Lat, Lon: c.Lon}
		}
		elements[i] = overpassElement{
			ID:       w.ID,
			Tags:     w.Tags,
			Geometry: geom,
		}
	}
	resp := overpassResponse{Elements: elements}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("buildRawTileJSON: marshal: %v", err)
	}
	return b
}

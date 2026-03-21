package quality

import (
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

func TestRoadCollection_DisplayName(t *testing.T) {
	tests := []struct {
		name     string
		rc       RoadCollection
		expected string
	}{
		{
			name:     "SubIndex 0 returns bare name",
			rc:       RoadCollection{Name: "Route 100", SubIndex: 0},
			expected: "Route 100",
		},
		{
			name:     "SubIndex 1 returns name with suffix (2)",
			rc:       RoadCollection{Name: "Route 100", SubIndex: 1},
			expected: "Route 100 (2)",
		},
		{
			name:     "SubIndex 2 returns name with suffix (3)",
			rc:       RoadCollection{Name: "Route 100", SubIndex: 2},
			expected: "Route 100 (3)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rc.DisplayName()
			if got != tt.expected {
				t.Errorf("DisplayName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGroupWaysByName(t *testing.T) {
	ways := ScoredWays{
		{WayID: 1, Tags: map[string]string{"name": "Route 1"}},
		{WayID: 2, Tags: map[string]string{"name": "Route 1"}},
		{WayID: 3, Tags: map[string]string{"name": "Route 2"}},
		{WayID: 4, Tags: map[string]string{}},           // no name
		{WayID: 5, Tags: map[string]string{"name": ""}}, // empty name
		{WayID: 6, Tags: nil},                           // nil tags
	}

	got := GroupWaysByName(ways)

	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(got))
	}

	r1 := got["Route 1"]
	if len(r1) != 2 {
		t.Errorf("expected 2 ways in Route 1, got %d", len(r1))
	}

	r2 := got["Route 2"]
	if len(r2) != 1 {
		t.Errorf("expected 1 way in Route 2, got %d", len(r2))
	}

	if _, ok := got[""]; ok {
		t.Error("unnamed ways should be excluded")
	}
}

func TestGroupWaysByName_AllUnnamed(t *testing.T) {
	ways := ScoredWays{
		{WayID: 1, Tags: map[string]string{}},
	}
	got := GroupWaysByName(ways)
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d groups", len(got))
	}
}

// makeWay creates a ScoredWay with a single segment between start and end.
func makeWay(id int64, start, end geo.Coord) ScoredWay {
	return ScoredWay{
		WayID: id,
		Segments: []ScoredSegment{
			{Start: start, End: end},
		},
	}
}

// TestGroupWays_SemicolonSeparatedRef verifies that a way with a
// semicolon-separated ref tag (e.g. "US 209;PA 901") appears in groups for
// each individual ref value. This is standard OSM tagging for roads with
// multiple route designations.
func TestGroupWays_SemicolonSeparatedRef(t *testing.T) {
	ways := ScoredWays{
		// Way with a single ref — should appear in "PA 345" group.
		{WayID: 1, Tags: map[string]string{"name": "Main Street", "ref": "PA 345"}},
		// Way with a semicolon-separated ref — should appear in BOTH
		// "US 209" and "PA 901" groups.
		{WayID: 2, Tags: map[string]string{"name": "Pottsville Minersville Highway", "ref": "US 209;PA 901"}},
		// Way with only "PA 901" ref — should appear in "PA 901" group.
		{WayID: 3, Tags: map[string]string{"name": "Sunbury Road", "ref": "PA 901"}},
	}

	groups := GroupWays(ways)

	// "PA 345" group should have way 1.
	if g := groups["PA 345"]; len(g) != 1 || g[0].WayID != 1 {
		t.Errorf("PA 345 group: got %d ways, want 1 (way 1)", len(g))
	}

	// "PA 901" group should have BOTH way 2 (from "US 209;PA 901") and way 3.
	pa901 := groups["PA 901"]
	if len(pa901) != 2 {
		t.Errorf("PA 901 group: got %d ways, want 2 (ways 2 and 3)", len(pa901))
	} else {
		ids := map[int64]bool{}
		for _, w := range pa901 {
			ids[w.WayID] = true
		}
		if !ids[2] {
			t.Error("PA 901 group: missing way 2 (ref='US 209;PA 901')")
		}
		if !ids[3] {
			t.Error("PA 901 group: missing way 3 (ref='PA 901')")
		}
	}

	// "US 209" group should have way 2.
	us209 := groups["US 209"]
	if len(us209) != 1 || us209[0].WayID != 2 {
		t.Errorf("US 209 group: got %d ways, want 1 (way 2)", len(us209))
	}

	// Way 2 should NOT appear under the unsplit key "US 209;PA 901".
	if g := groups["US 209;PA 901"]; len(g) != 0 {
		t.Errorf("'US 209;PA 901' group should not exist as a literal key, got %d ways", len(g))
	}

	// Name groups should still work normally.
	if g := groups["Pottsville Minersville Highway"]; len(g) != 1 || g[0].WayID != 2 {
		t.Errorf("Pottsville Minersville Highway group: got %d ways, want 1", len(g))
	}
	if g := groups["Sunbury Road"]; len(g) != 1 || g[0].WayID != 3 {
		t.Errorf("Sunbury Road group: got %d ways, want 1", len(g))
	}
}

func TestFindConnectedComponents_TwoClusters(t *testing.T) {
	// Cluster A: two nearby ways in Vermont (~44°N, -72°W)
	a1 := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	a2 := makeWay(2, geo.Coord{Lat: 44.001, Lon: -72.0}, geo.Coord{Lat: 44.002, Lon: -72.0})

	// Cluster B: two nearby ways far away (~36°N, -80°W)
	b1 := makeWay(3, geo.Coord{Lat: 36.0, Lon: -80.0}, geo.Coord{Lat: 36.001, Lon: -80.0})
	b2 := makeWay(4, geo.Coord{Lat: 36.001, Lon: -80.0}, geo.Coord{Lat: 36.002, Lon: -80.0})

	ways := ScoredWays{a1, a2, b1, b2}
	components := FindConnectedComponents(ways, ConnectedEndpointProximityM)

	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}

	sizes := map[int]int{}
	for _, comp := range components {
		sizes[len(comp)]++
	}
	if sizes[2] != 2 {
		t.Errorf("expected two components of size 2, got sizes: %v", sizes)
	}
}

func TestFindConnectedComponents_OneCluster(t *testing.T) {
	// Three consecutive ways, each endpoint within 100m of the next.
	w1 := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.0005, Lon: -72.0})
	w2 := makeWay(2, geo.Coord{Lat: 44.0005, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	w3 := makeWay(3, geo.Coord{Lat: 44.001, Lon: -72.0}, geo.Coord{Lat: 44.0015, Lon: -72.0})

	components := FindConnectedComponents(ScoredWays{w1, w2, w3}, ConnectedEndpointProximityM)

	if len(components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(components))
	}
	if len(components[0]) != 3 {
		t.Errorf("expected 3 ways in component, got %d", len(components[0]))
	}
}

func TestFindConnectedComponents_SingleWay(t *testing.T) {
	w := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	components := FindConnectedComponents(ScoredWays{w}, ConnectedEndpointProximityM)
	if len(components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(components))
	}
}

func TestFindConnectedComponents_Empty(t *testing.T) {
	components := FindConnectedComponents(nil, ConnectedEndpointProximityM)
	if len(components) != 0 {
		t.Errorf("expected 0 components, got %d", len(components))
	}
}

func TestFindConnectedComponents_WayNoSegments(t *testing.T) {
	// A way with no segments should still appear in a component by itself.
	noSeg := ScoredWay{WayID: 1}
	w := makeWay(2, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})

	components := FindConnectedComponents(ScoredWays{noSeg, w}, ConnectedEndpointProximityM)
	// They can't be connected (no-seg way has no endpoints), so expect 2 components.
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}
}

func TestFindConnectedComponents_AllDisconnected(t *testing.T) {
	// Three valid ways each placed on a different continent — no two endpoints
	// are within ConnectedEndpointProximityM of each other.
	// Expected: each way becomes its own singleton component.
	wNA := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})   // Vermont
	wEU := makeWay(2, geo.Coord{Lat: 48.8, Lon: 2.35}, geo.Coord{Lat: 48.801, Lon: 2.35})     // Paris
	wAU := makeWay(3, geo.Coord{Lat: -33.8, Lon: 151.2}, geo.Coord{Lat: -33.801, Lon: 151.2}) // Sydney

	components := FindConnectedComponents(ScoredWays{wNA, wEU, wAU}, ConnectedEndpointProximityM)

	if len(components) != 3 {
		t.Fatalf("expected 3 singleton components, got %d", len(components))
	}
	for i, comp := range components {
		if len(comp) != 1 {
			t.Errorf("component %d: expected 1 way, got %d", i, len(comp))
		}
	}
}

func TestOrderWays_Chain(t *testing.T) {
	// Three ways that form a chain but are provided in shuffled order.
	// Use 0.0002° steps (~22m at 44°N) so shared endpoints are well within proximity.
	w1 := makeWay(1, geo.Coord{Lat: 44.0000, Lon: -72.0}, geo.Coord{Lat: 44.0002, Lon: -72.0})
	w2 := makeWay(2, geo.Coord{Lat: 44.0002, Lon: -72.0}, geo.Coord{Lat: 44.0004, Lon: -72.0})
	w3 := makeWay(3, geo.Coord{Lat: 44.0004, Lon: -72.0}, geo.Coord{Lat: 44.0006, Lon: -72.0})

	// Shuffle: provide w3, w1, w2
	shuffled := ScoredWays{w3, w1, w2}
	ordered := OrderWays(shuffled)

	if len(ordered) != 3 {
		t.Fatalf("expected 3 ways, got %d", len(ordered))
	}

	// Verify each way's end connects to the next way's start (within proximity).
	for i := 0; i < len(ordered)-1; i++ {
		_, end, _ := wayEndpoints(ordered[i])
		start, _, _ := wayEndpoints(ordered[i+1])
		d := geo.Haversine(end, start)
		if d > ConnectedEndpointProximityM {
			t.Errorf("gap between way %d and %d: %.1f m (want <= %.1f m)", i, i+1, d, ConnectedEndpointProximityM)
		}
	}
}

func TestOrderWays_SingleWay(t *testing.T) {
	w := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	ordered := OrderWays(ScoredWays{w})
	if len(ordered) != 1 {
		t.Fatalf("expected 1 way, got %d", len(ordered))
	}
}

func TestOrderWays_Empty(t *testing.T) {
	ordered := OrderWays(nil)
	if len(ordered) != 0 {
		t.Errorf("expected 0 ways, got %d", len(ordered))
	}
}

func TestOrderWays_ReverseNeeded(t *testing.T) {
	// w2 is stored in the reverse direction; OrderWays should flip it.
	// Use 0.0002° steps (~22m at 44°N) so shared endpoints are well within proximity.
	w1 := makeWay(1, geo.Coord{Lat: 44.0000, Lon: -72.0}, geo.Coord{Lat: 44.0002, Lon: -72.0})
	// w2 reversed: end is close to w1's end, start is farther.
	w2 := makeWay(2, geo.Coord{Lat: 44.0004, Lon: -72.0}, geo.Coord{Lat: 44.0002, Lon: -72.0})

	ordered := OrderWays(ScoredWays{w1, w2})
	if len(ordered) != 2 {
		t.Fatalf("expected 2 ways, got %d", len(ordered))
	}

	_, end, _ := wayEndpoints(ordered[0])
	start, _, _ := wayEndpoints(ordered[1])
	d := geo.Haversine(end, start)
	if d > ConnectedEndpointProximityM {
		t.Errorf("gap after ordering: %.1f m (want <= %.1f m)", d, ConnectedEndpointProximityM)
	}
}

// makeScoredWay creates a ScoredWay with explicitly specified segments.
func makeScoredWay(id int64, tags map[string]string, segs []ScoredSegment) ScoredWay {
	return ScoredWay{WayID: id, Tags: tags, Segments: segs}
}

// makeSeg creates a ScoredSegment with the given tier and length.
// The start/end coordinates are zero, which means the deflection filter will
// compute 0° bearing change and zero any scored segment. Use makeCurvySeg or
// makeStraightSegAt for segments that need to survive deflection filtering.
func makeSeg(tier int, length float64) ScoredSegment {
	weight := 0.0
	switch tier {
	case 1:
		weight = TierWeight1
	case 2:
		weight = TierWeight2
	case 3:
		weight = TierWeight3
	case 4:
		weight = TierWeight4
	}
	return ScoredSegment{
		Tier:   tier,
		Weight: weight,
		Length: length,
		Score:  length * weight,
	}
}

// curvySegments builds n segments that form a genuine S-curve, ensuring each
// window of 2400m has > 20° cumulative heading change so they survive the
// deflection filter. Each segment is ~segLen meters.
// The path curves NE then NW alternately, simulating a winding road.
func curvySegments(id int64, n int, tier int, segLen float64, startLat, startLon float64) []ScoredSegment {
	weight := 0.0
	switch tier {
	case 1:
		weight = TierWeight1
	case 2:
		weight = TierWeight2
	case 3:
		weight = TierWeight3
	case 4:
		weight = TierWeight4
	}

	const (
		latPerM = 1.0 / 111000.0
		lonPerM = 1.0 / 78500.0
	)

	// Build a sinusoidal path: each segment veers alternately NE and NW by ~45°.
	// This produces ~90° cumulative change per pair, well above the 20° threshold.
	segs := make([]ScoredSegment, n)
	lat := startLat
	lon := startLon
	for i := 0; i < n; i++ {
		var dLat, dLon float64
		if i%2 == 0 {
			// Bearing ~45° (NE): equal north and east components
			dLat = segLen * 0.707 * latPerM
			dLon = segLen * 0.707 * lonPerM
		} else {
			// Bearing ~315° (NW): equal north and west components
			dLat = segLen * 0.707 * latPerM
			dLon = -segLen * 0.707 * lonPerM
		}
		endLat := lat + dLat
		endLon := lon + dLon
		l := geo.Haversine(geo.Coord{Lat: lat, Lon: lon}, geo.Coord{Lat: endLat, Lon: endLon})
		segs[i] = ScoredSegment{
			WayID:  id,
			Start:  geo.Coord{Lat: lat, Lon: lon},
			End:    geo.Coord{Lat: endLat, Lon: endLon},
			Length: l,
			Tier:   tier,
			Weight: weight,
			Score:  l * weight,
		}
		lat = endLat
		lon = endLon
	}
	return segs
}

func TestSplitAtStraightGaps_LongStraightInMiddle(t *testing.T) {
	// Curvy — straight (>StraightGapSplitM) — curvy
	curvySeg := makeSeg(1, 500.0)
	straightSeg := makeSeg(0, StraightGapSplitM+1)
	curvySeg2 := makeSeg(2, 400.0)

	ways := ScoredWays{
		makeScoredWay(1, nil, []ScoredSegment{curvySeg}),
		makeScoredWay(2, nil, []ScoredSegment{straightSeg}),
		makeScoredWay(3, nil, []ScoredSegment{curvySeg2}),
	}

	groups := SplitAtStraightGaps(ways, StraightGapSplitM)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(groups[0]) != 1 {
		t.Errorf("group 0: expected 1 segment, got %d", len(groups[0]))
	}
	if groups[0][0].Tier != 1 {
		t.Errorf("group 0 segment should be tier 1")
	}
	if len(groups[1]) != 1 {
		t.Errorf("group 1: expected 1 segment, got %d", len(groups[1]))
	}
	if groups[1][0].Tier != 2 {
		t.Errorf("group 1 segment should be tier 2")
	}
}

func TestSplitAtStraightGaps_ShortStraightKeptTogether(t *testing.T) {
	// Short straight run (below threshold) should not cause a split.
	curvySeg := makeSeg(1, 500.0)
	shortStraight := makeSeg(0, StraightGapSplitM-1)
	curvySeg2 := makeSeg(2, 400.0)

	ways := ScoredWays{
		makeScoredWay(1, nil, []ScoredSegment{curvySeg, shortStraight, curvySeg2}),
	}

	groups := SplitAtStraightGaps(ways, StraightGapSplitM)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0]) != 3 {
		t.Errorf("group 0: expected 3 segments, got %d", len(groups[0]))
	}
}

func TestSplitAtStraightGaps_AllTierZero(t *testing.T) {
	// All segments are tier 0 and total length > threshold: single collection with 0 segments.
	ways := ScoredWays{
		makeScoredWay(1, nil, []ScoredSegment{makeSeg(0, StraightGapSplitM+500)}),
	}

	groups := SplitAtStraightGaps(ways, StraightGapSplitM)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group (empty), got %d", len(groups))
	}
	if len(groups[0]) != 0 {
		t.Errorf("expected empty segment slice, got %d segments", len(groups[0]))
	}
}

func TestSplitAtStraightGaps_Empty(t *testing.T) {
	groups := SplitAtStraightGaps(nil, StraightGapSplitM)
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestAggregate_Empty(t *testing.T) {
	collections := Aggregate(nil)
	if len(collections) != 0 {
		t.Errorf("expected 0 collections, got %d", len(collections))
	}
}

func TestAggregate_SingleWayRoad(t *testing.T) {
	// A single named way with one curvy segment.
	seg := makeSeg(2, 300.0)
	ways := ScoredWays{
		makeScoredWay(1, map[string]string{"name": "Winding Way", "highway": "secondary"}, []ScoredSegment{seg}),
	}

	collections := Aggregate(ways)

	if len(collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(collections))
	}
	rc := collections[0]
	if rc.Name != "Winding Way" {
		t.Errorf("Name = %q, want %q", rc.Name, "Winding Way")
	}
	if rc.SubIndex != 0 {
		t.Errorf("SubIndex = %d, want 0", rc.SubIndex)
	}
	if len(rc.WayIDs) != 1 || rc.WayIDs[0] != 1 {
		t.Errorf("WayIDs = %v, want [1]", rc.WayIDs)
	}
	if len(rc.HighwayTypes) != 1 || rc.HighwayTypes[0] != "secondary" {
		t.Errorf("HighwayTypes = %v, want [secondary]", rc.HighwayTypes)
	}
}

func TestAggregateScoreComputation(t *testing.T) {
	// Build segments with genuine bearing changes so they survive the deflection filter.
	// Use curvySegments: 4 segments alternating NE/NW, each ~250m, total ~1000m for seg1 tier.
	// Then 2 more segments for seg2 tier.
	seg1Segs := curvySegments(10, 4, 1, 250.0, 44.0, -72.0)
	// seg2 continues from where seg1 ends
	lastSeg1 := seg1Segs[len(seg1Segs)-1]
	seg2Segs := curvySegments(10, 2, 2, 250.0, lastSeg1.End.Lat, lastSeg1.End.Lon)

	allSegs := append(seg1Segs, seg2Segs...)

	ways := ScoredWays{
		makeScoredWay(10, map[string]string{"name": "Score Road"}, allSegs),
	}

	collections := Aggregate(ways)

	if len(collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(collections))
	}
	rc := collections[0]

	// Compute expected score from the actual segments (after deflection filter may act).
	// Since the segments form a genuine curve, they should survive deflection.
	expectedScore := 0.0
	expectedLength := 0.0
	for _, seg := range allSegs {
		expectedScore += seg.Score
		expectedLength += seg.Length
	}

	if rc.TotalScore == 0 && expectedScore > 0 {
		t.Errorf("TotalScore = 0, want > 0 (expected ~%v)", expectedScore)
	}
	if rc.TotalLength == 0 {
		t.Errorf("TotalLength = 0, want > 0")
	}
	if rc.TotalLength > 0 && rc.ScorePerKm == 0 && rc.TotalScore > 0 {
		t.Errorf("ScorePerKm = 0, want > 0")
	}
}

func TestAggregateScoreComputation_ZeroLength(t *testing.T) {
	// A way with a zero-length straight segment should not produce NaN ScorePerKm.
	ways := ScoredWays{
		makeScoredWay(1, map[string]string{"name": "Zero Road"}, []ScoredSegment{makeSeg(0, 0.0)}),
	}
	collections := Aggregate(ways)
	if len(collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(collections))
	}
	if collections[0].ScorePerKm != 0.0 {
		t.Errorf("ScorePerKm should be 0 for zero-length road, got %v", collections[0].ScorePerKm)
	}
}

func TestAggregateSubIndexing(t *testing.T) {
	// Two curvy sections of the same named road separated by a long straight gap.
	// They share name and highway but are split into two collections.
	// The curvy segments use actual bearing changes so they survive the deflection filter.
	curvy1Segs := curvySegments(1, 4, 1, 200.0, 44.0, -72.0)
	last1 := curvy1Segs[len(curvy1Segs)-1]

	// Long straight gap: must be > StraightGapSplitM and also not zeroed by deflection
	// (tier 0 segments don't enter the deflection filter at all since Score==0).
	// Place the gap immediately after the curvy section.
	gapStart := last1.End
	gapLen := StraightGapSplitM + 100
	gapEnd := geo.Coord{Lat: gapStart.Lat + gapLen/111000.0, Lon: gapStart.Lon}
	gapSeg := ScoredSegment{
		WayID: 1, Tier: 0, Weight: 0, Length: gapLen, Score: 0,
		Start: gapStart, End: gapEnd,
	}

	// Second curvy section starts after the gap.
	curvy2Segs := curvySegments(1, 4, 2, 200.0, gapEnd.Lat, gapEnd.Lon)

	allSegs := append(append(curvy1Segs, gapSeg), curvy2Segs...)
	ways := ScoredWays{
		makeScoredWay(1, map[string]string{"name": "Split Road"}, allSegs),
	}

	collections := Aggregate(ways)

	if len(collections) != 2 {
		t.Fatalf("expected 2 collections after split, got %d", len(collections))
	}

	subIndices := map[int]bool{}
	for _, rc := range collections {
		if rc.Name != "Split Road" {
			t.Errorf("unexpected name %q", rc.Name)
		}
		subIndices[rc.SubIndex] = true
	}

	if !subIndices[0] || !subIndices[1] {
		t.Errorf("expected sub-indices 0 and 1, got %v", subIndices)
	}
}

// TestAggregate_TwoDisconnectedClusters verifies that two geographically
// separated groups of ways sharing the same name each become their own
// RoadCollection with distinct sub-indices (0 and 1).
func TestAggregate_TwoDisconnectedClusters(t *testing.T) {
	// Cluster A: two connected ways in Vermont (~44°N, -72°W), tier-1 curved.
	segA1 := ScoredSegment{Tier: 1, Weight: TierWeight1, Length: 500.0, Score: 500.0 * TierWeight1,
		Start: geo.Coord{Lat: 44.000, Lon: -72.0}, End: geo.Coord{Lat: 44.001, Lon: -72.0}}
	segA2 := ScoredSegment{Tier: 1, Weight: TierWeight1, Length: 500.0, Score: 500.0 * TierWeight1,
		Start: geo.Coord{Lat: 44.001, Lon: -72.0}, End: geo.Coord{Lat: 44.002, Lon: -72.0}}
	wA1 := ScoredWay{WayID: 1, Tags: map[string]string{"name": "Mountain Road", "highway": "secondary"}, Segments: []ScoredSegment{segA1}}
	wA2 := ScoredWay{WayID: 2, Tags: map[string]string{"name": "Mountain Road", "highway": "secondary"}, Segments: []ScoredSegment{segA2}}

	// Cluster B: two connected ways in North Carolina (~36°N, -80°W), tier-2 curved.
	segB1 := ScoredSegment{Tier: 2, Weight: TierWeight2, Length: 400.0, Score: 400.0 * TierWeight2,
		Start: geo.Coord{Lat: 36.000, Lon: -80.0}, End: geo.Coord{Lat: 36.001, Lon: -80.0}}
	segB2 := ScoredSegment{Tier: 2, Weight: TierWeight2, Length: 400.0, Score: 400.0 * TierWeight2,
		Start: geo.Coord{Lat: 36.001, Lon: -80.0}, End: geo.Coord{Lat: 36.002, Lon: -80.0}}
	wB1 := ScoredWay{WayID: 3, Tags: map[string]string{"name": "Mountain Road", "highway": "tertiary"}, Segments: []ScoredSegment{segB1}}
	wB2 := ScoredWay{WayID: 4, Tags: map[string]string{"name": "Mountain Road", "highway": "tertiary"}, Segments: []ScoredSegment{segB2}}

	collections := Aggregate(ScoredWays{wA1, wA2, wB1, wB2})

	if len(collections) != 2 {
		t.Fatalf("expected 2 collections for two disconnected clusters, got %d", len(collections))
	}

	subIndices := map[int]bool{}
	for _, rc := range collections {
		if rc.Name != "Mountain Road" {
			t.Errorf("unexpected collection name %q", rc.Name)
		}
		subIndices[rc.SubIndex] = true
		if len(rc.WayIDs) != 2 {
			t.Errorf("SubIndex %d: expected 2 WayIDs (one per way in cluster), got %v", rc.SubIndex, rc.WayIDs)
		}
	}
	if !subIndices[0] || !subIndices[1] {
		t.Errorf("expected sub-indices 0 and 1, got %v", subIndices)
	}
}

// TestSplitOrderingGaps_NoGaps: chain of 5 ways with endpoints < 100m apart.
// Expect 1 chunk with all 5 ways.
func TestSplitOrderingGaps_NoGaps(t *testing.T) {
	// 5 ways chained north, each ~22m apart (0.0002° lat steps).
	baseLat := 44.0
	step := 0.0002 // ~22m
	ways := make(ScoredWays, 5)
	for i := range 5 {
		lat0 := baseLat + float64(i)*step
		lat1 := lat0 + step
		ways[i] = makeWay(int64(i+1), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat1, Lon: -72.0})
	}

	got := SplitOrderingGaps(ways, ConnectedEndpointProximityM)

	if len(got) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got))
	}
	if len(got[0]) != 5 {
		t.Fatalf("expected 5 ways in chunk 0, got %d", len(got[0]))
	}
}

// TestSplitOrderingGaps_GapInMiddle: ways 0-3 connected, 2km gap, ways 4-5 connected.
// Expect 2 chunks: one with 4 ways (IDs 1-4) and one with 2 ways (IDs 5-6).
func TestSplitOrderingGaps_GapInMiddle(t *testing.T) {
	baseLat := 44.0
	step := 0.0002 // ~22m

	var ways ScoredWays
	// Ways 0-3: connected chain
	for i := range 4 {
		lat0 := baseLat + float64(i)*step
		lat1 := lat0 + step
		ways = append(ways, makeWay(int64(i+1), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat1, Lon: -72.0}))
	}
	// Way 4 is 2km north of way 3's end — creating a gap > 100m
	gapLat := baseLat + float64(4)*step + 2000.0/111000.0
	ways = append(ways, makeWay(5, geo.Coord{Lat: gapLat, Lon: -72.0}, geo.Coord{Lat: gapLat + step, Lon: -72.0}))
	// Way 5: connected to way 4
	ways = append(ways, makeWay(6, geo.Coord{Lat: gapLat + step, Lon: -72.0}, geo.Coord{Lat: gapLat + 2*step, Lon: -72.0}))

	got := SplitOrderingGaps(ways, ConnectedEndpointProximityM)

	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	// Chunk 0: ways 1-4
	if len(got[0]) != 4 {
		t.Fatalf("chunk 0: expected 4 ways, got %d", len(got[0]))
	}
	for i, w := range got[0] {
		if w.WayID != int64(i+1) {
			t.Errorf("chunk 0 way[%d].WayID = %d, want %d", i, w.WayID, i+1)
		}
	}
	// Chunk 1: ways 5-6
	if len(got[1]) != 2 {
		t.Fatalf("chunk 1: expected 2 ways, got %d", len(got[1]))
	}
	if got[1][0].WayID != 5 {
		t.Errorf("chunk 1 way[0].WayID = %d, want 5", got[1][0].WayID)
	}
	if got[1][1].WayID != 6 {
		t.Errorf("chunk 1 way[1].WayID = %d, want 6", got[1][1].WayID)
	}
}

// TestSplitOrderingGaps_GapAtStart: first way is far from the rest.
// Expect 2 chunks: one with 1 way (ID 1) and one with 5 ways (IDs 2-6).
func TestSplitOrderingGaps_GapAtStart(t *testing.T) {
	step := 0.0002 // ~22m

	// Way 0: isolated far away
	var ways ScoredWays
	ways = append(ways, makeWay(1, geo.Coord{Lat: 40.0, Lon: -72.0}, geo.Coord{Lat: 40.0 + step, Lon: -72.0}))

	// Ways 1-5: connected chain starting at 44°N
	baseLat := 44.0
	for i := range 5 {
		lat0 := baseLat + float64(i)*step
		lat1 := lat0 + step
		ways = append(ways, makeWay(int64(i+2), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat1, Lon: -72.0}))
	}

	got := SplitOrderingGaps(ways, ConnectedEndpointProximityM)

	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	// Chunk 0: way 1 (the isolated stray)
	if len(got[0]) != 1 {
		t.Fatalf("chunk 0: expected 1 way, got %d", len(got[0]))
	}
	if got[0][0].WayID != 1 {
		t.Errorf("chunk 0 way[0].WayID = %d, want 1", got[0][0].WayID)
	}
	// Chunk 1: ways 2-6
	if len(got[1]) != 5 {
		t.Fatalf("chunk 1: expected 5 ways, got %d", len(got[1]))
	}
	for i, w := range got[1] {
		if w.WayID != int64(i+2) {
			t.Errorf("chunk 1 way[%d].WayID = %d, want %d", i, w.WayID, i+2)
		}
	}
}

// TestSplitOrderingGaps_MultipleGaps: three chunks separated by gaps.
// Expect 3 chunks: 2 ways, 4 ways, 1 way.
func TestSplitOrderingGaps_MultipleGaps(t *testing.T) {
	step := 0.0002 // ~22m

	var ways ScoredWays
	// Chunk A: 2 ways at 40°N
	for i := range 2 {
		lat0 := 40.0 + float64(i)*step
		ways = append(ways, makeWay(int64(len(ways)+1), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat0 + step, Lon: -72.0}))
	}
	// Chunk B: 4 ways at 44°N (the longest)
	for i := range 4 {
		lat0 := 44.0 + float64(i)*step
		ways = append(ways, makeWay(int64(len(ways)+1), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat0 + step, Lon: -72.0}))
	}
	// Chunk C: 1 way at 48°N
	ways = append(ways, makeWay(int64(len(ways)+1), geo.Coord{Lat: 48.0, Lon: -72.0}, geo.Coord{Lat: 48.0 + step, Lon: -72.0}))

	got := SplitOrderingGaps(ways, ConnectedEndpointProximityM)

	if len(got) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(got))
	}
	// Chunk A: 2 ways (IDs 1, 2)
	if len(got[0]) != 2 {
		t.Fatalf("chunk 0: expected 2 ways, got %d", len(got[0]))
	}
	for i, w := range got[0] {
		if w.WayID != int64(i+1) {
			t.Errorf("chunk 0 way[%d].WayID = %d, want %d", i, w.WayID, i+1)
		}
	}
	// Chunk B: 4 ways (IDs 3, 4, 5, 6)
	if len(got[1]) != 4 {
		t.Fatalf("chunk 1: expected 4 ways, got %d", len(got[1]))
	}
	for i, w := range got[1] {
		if w.WayID != int64(i+3) {
			t.Errorf("chunk 1 way[%d].WayID = %d, want %d", i, w.WayID, i+3)
		}
	}
	// Chunk C: 1 way (ID 7)
	if len(got[2]) != 1 {
		t.Fatalf("chunk 2: expected 1 way, got %d", len(got[2]))
	}
	if got[2][0].WayID != 7 {
		t.Errorf("chunk 2 way[0].WayID = %d, want 7", got[2][0].WayID)
	}
}

// TestSplitOrderingGaps_SingleWay: returns 1 chunk containing the single way.
func TestSplitOrderingGaps_SingleWay(t *testing.T) {
	w := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	got := SplitOrderingGaps(ScoredWays{w}, ConnectedEndpointProximityM)
	if len(got) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got))
	}
	if len(got[0]) != 1 {
		t.Fatalf("expected 1 way in chunk 0, got %d", len(got[0]))
	}
	if got[0][0].WayID != 1 {
		t.Errorf("WayID = %d, want 1", got[0][0].WayID)
	}
}

// TestSplitOrderingGaps_EmptyInput: returns nil/empty.
func TestSplitOrderingGaps_EmptyInput(t *testing.T) {
	got := SplitOrderingGaps(nil, ConnectedEndpointProximityM)
	if len(got) != 0 {
		t.Errorf("expected 0 chunks, got %d", len(got))
	}
	got2 := SplitOrderingGaps(ScoredWays{}, ConnectedEndpointProximityM)
	if len(got2) != 0 {
		t.Errorf("expected 0 chunks for empty slice, got %d", len(got2))
	}
}

// TestSplitOrderingGaps_ValleyCreekRegression verifies that a chain with an
// overlapping retrace way is handled correctly. The scenario:
//   - 5 ways forming a north-to-south chain
//   - 1 way that overlaps the middle (its nearest endpoint is close to chain
//     end after greedy ordering, but it creates a >100m jump back into the chain)
//
// After SplitOrderingGaps, the main chain (5 ways) and the stray (1 way) are
// returned as separate chunks. The main chain chunk must have no internal gaps.
func TestSplitOrderingGaps_ValleyCreekRegression(t *testing.T) {
	step := 0.0002 // ~22m per step at 44°N

	// Build main chain: ways 1-5 progressing north.
	var mainChain ScoredWays
	for i := range 5 {
		lat0 := 44.0 + float64(i)*step
		mainChain = append(mainChain, makeWay(int64(i+1),
			geo.Coord{Lat: lat0, Lon: -72.0},
			geo.Coord{Lat: lat0 + step, Lon: -72.0},
		))
	}

	// Simulate greedy ordering appending a stray way whose start is 2km south
	// of the main chain — a large gap that SplitOrderingGaps must detect.
	strayWay := makeWay(6,
		geo.Coord{Lat: 44.0 - 2000.0/111000.0, Lon: -72.0}, // 2km south of chain start
		geo.Coord{Lat: 44.0 - 1800.0/111000.0, Lon: -72.0},
	)
	orderedWithStray := append(mainChain, strayWay)

	result := SplitOrderingGaps(orderedWithStray, ConnectedEndpointProximityM)

	// Expect 2 chunks: the main chain (5 ways) and the stray (1 way).
	if len(result) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(result))
	}

	mainChunkIdx := -1
	for i, chunk := range result {
		if len(chunk) == 5 {
			mainChunkIdx = i
			break
		}
	}
	if mainChunkIdx == -1 {
		t.Fatalf("no chunk with 5 ways found; chunk sizes: %v", func() []int {
			sizes := make([]int, len(result))
			for i, c := range result {
				sizes[i] = len(c)
			}
			return sizes
		}())
	}

	// Verify no large gaps in the main chain chunk.
	mainResult := result[mainChunkIdx]
	for i := 0; i < len(mainResult)-1; i++ {
		_, end, _ := wayEndpoints(mainResult[i])
		start, _, _ := wayEndpoints(mainResult[i+1])
		d := geo.Haversine(end, start)
		if d > ConnectedEndpointProximityM {
			t.Errorf("gap between ways %d and %d: %.1f m (want <= %.1f m)", i, i+1, d, ConnectedEndpointProximityM)
		}
	}
}

// TestSplitOrderingGaps_AllChunksPreserved verifies that no ways are lost when
// there is a large gap (PA 901 scenario): ways 1-6 connected, 2km gap, ways 7-10
// connected. Both chunks must be returned and the total way count must equal the
// input count.
func TestSplitOrderingGaps_AllChunksPreserved(t *testing.T) {
	step := 0.0002 // ~22m

	var ways ScoredWays
	// Chain A: ways 1-6 at 44°N
	for i := range 6 {
		lat0 := 44.0 + float64(i)*step
		ways = append(ways, makeWay(int64(i+1), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat0 + step, Lon: -72.0}))
	}
	// 2km gap
	gapLat := 44.0 + float64(6)*step + 2000.0/111000.0
	// Chain B: ways 7-10
	for i := range 4 {
		lat0 := gapLat + float64(i)*step
		ways = append(ways, makeWay(int64(i+7), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat0 + step, Lon: -72.0}))
	}

	got := SplitOrderingGaps(ways, ConnectedEndpointProximityM)

	// Must return exactly 2 chunks.
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}

	// Chunk 0: 6 ways (IDs 1-6).
	if len(got[0]) != 6 {
		t.Fatalf("chunk 0: expected 6 ways, got %d", len(got[0]))
	}
	for i, w := range got[0] {
		if w.WayID != int64(i+1) {
			t.Errorf("chunk 0 way[%d].WayID = %d, want %d", i, w.WayID, i+1)
		}
	}

	// Chunk 1: 4 ways (IDs 7-10).
	if len(got[1]) != 4 {
		t.Fatalf("chunk 1: expected 4 ways, got %d", len(got[1]))
	}
	for i, w := range got[1] {
		if w.WayID != int64(i+7) {
			t.Errorf("chunk 1 way[%d].WayID = %d, want %d", i, w.WayID, i+7)
		}
	}

	// Total way count must equal input count — no data lost.
	total := 0
	for _, chunk := range got {
		total += len(chunk)
	}
	if total != len(ways) {
		t.Errorf("total ways across all chunks = %d, want %d (no data lost)", total, len(ways))
	}
}

func TestAggregate_UnnamedWaysExcluded(t *testing.T) {
	// Ways without a name tag should produce no collections.
	ways := ScoredWays{
		makeScoredWay(1, map[string]string{"highway": "tertiary"}, []ScoredSegment{makeSeg(1, 500.0)}),
	}
	collections := Aggregate(ways)
	if len(collections) != 0 {
		t.Errorf("expected 0 collections for unnamed ways, got %d", len(collections))
	}
}

// TestOrderWays_ExactCoordMatch verifies that three ways with exact coordinate
// matches at endpoints are chained correctly when given in shuffled order.
func TestOrderWays_ExactCoordMatch(t *testing.T) {
	p1 := geo.Coord{Lat: 44.000, Lon: -72.0}
	p2 := geo.Coord{Lat: 44.001, Lon: -72.0}
	p3 := geo.Coord{Lat: 44.002, Lon: -72.0}
	p4 := geo.Coord{Lat: 44.003, Lon: -72.0}

	w1 := makeWay(1, p1, p2)
	w2 := makeWay(2, p2, p3)
	w3 := makeWay(3, p3, p4)

	// Shuffled input
	ordered := OrderWays(ScoredWays{w2, w3, w1})

	if len(ordered) != 3 {
		t.Fatalf("expected 3 ways, got %d", len(ordered))
	}

	// Verify chain via exact geo.Coord equality (end[i] == start[i+1])
	for i := 0; i < len(ordered)-1; i++ {
		_, end, _ := wayEndpoints(ordered[i])
		start, _, _ := wayEndpoints(ordered[i+1])
		if end != start {
			t.Errorf("way[%d].end %v != way[%d].start %v", i, end, i+1, start)
		}
	}
}

// TestOrderWays_OnewayRespected_NoReverse verifies that oneway=yes ways are
// not reversed even when given in reverse order.
func TestOrderWays_OnewayRespected_NoReverse(t *testing.T) {
	p1 := geo.Coord{Lat: 44.000, Lon: -72.0}
	p2 := geo.Coord{Lat: 44.001, Lon: -72.0}
	p3 := geo.Coord{Lat: 44.002, Lon: -72.0}

	onewayTags := map[string]string{"oneway": "yes"}

	// Way A: p1 -> p2 (oneway)
	wayA := makeScoredWay(1, onewayTags, []ScoredSegment{{Start: p1, End: p2}})
	// Way B: p2 -> p3 (oneway)
	wayB := makeScoredWay(2, onewayTags, []ScoredSegment{{Start: p2, End: p3}})

	// Input in reverse order: [B, A]
	ordered := OrderWays(ScoredWays{wayB, wayA})

	if len(ordered) != 2 {
		t.Fatalf("expected 2 ways, got %d", len(ordered))
	}

	// The output should be [A, B] — A's end == B's start
	if ordered[0].WayID != 1 {
		t.Errorf("expected ordered[0].WayID=1 (way A), got %d", ordered[0].WayID)
	}
	if ordered[1].WayID != 2 {
		t.Errorf("expected ordered[1].WayID=2 (way B), got %d", ordered[1].WayID)
	}

	// A's segments must NOT be reversed: start should be p1
	startA, _, _ := wayEndpoints(ordered[0])
	if startA != p1 {
		t.Errorf("way A start = %v, want %v (must not be reversed)", startA, p1)
	}
}

// TestOrderWays_DualCarriageway_NoUTurn tests that the algorithm follows the
// northbound path (1->2->4) through a divided highway and does not U-turn by
// chaining the southbound carriageway (way 3) between ways 2 and 4.
func TestOrderWays_DualCarriageway_NoUTurn(t *testing.T) {
	// Approach from south (bidirectional)
	pS := geo.Coord{Lat: 40.000, Lon: -75.0}
	pDiv := geo.Coord{Lat: 40.001, Lon: -75.0} // diverge point
	pN := geo.Coord{Lat: 40.002, Lon: -75.0}   // merge point north
	pEnd := geo.Coord{Lat: 40.003, Lon: -75.0} // continuation north

	onewayTags := map[string]string{"oneway": "yes"}

	// Way 1: approach (bidirectional): pS -> pDiv
	way1 := makeScoredWay(1, nil, []ScoredSegment{{Start: pS, End: pDiv}})
	// Way 2: NB carriageway (oneway): pDiv -> pN
	way2 := makeScoredWay(2, onewayTags, []ScoredSegment{{Start: pDiv, End: pN}})
	// Way 3: SB carriageway (oneway): pN -> pDiv (reversed geographic direction)
	way3 := makeScoredWay(3, onewayTags, []ScoredSegment{{Start: pN, End: pDiv}})
	// Way 4: continuation north (bidirectional): pN -> pEnd
	way4 := makeScoredWay(4, nil, []ScoredSegment{{Start: pN, End: pEnd}})

	// Shuffled input
	ordered := OrderWays(ScoredWays{way3, way1, way4, way2})

	if len(ordered) != 4 {
		t.Fatalf("expected 4 ways in output, got %d", len(ordered))
	}

	// All 4 way IDs must be present
	ids := make(map[int64]bool)
	for _, w := range ordered {
		ids[w.WayID] = true
	}
	for _, id := range []int64{1, 2, 3, 4} {
		if !ids[id] {
			t.Errorf("way %d missing from output", id)
		}
	}

	// Find the positions of ways 1, 2, 4 in the output
	pos := make(map[int64]int)
	for i, w := range ordered {
		pos[w.WayID] = i
	}

	// Ways 1, 2, 4 must be consecutive in that order (northbound path)
	if pos[1] >= pos[2] {
		t.Errorf("way 1 (pos %d) must come before way 2 (pos %d)", pos[1], pos[2])
	}
	if pos[2] >= pos[4] {
		t.Errorf("way 2 (pos %d) must come before way 4 (pos %d)", pos[2], pos[4])
	}
	if pos[4]-pos[2] != 1 {
		t.Errorf("way 4 must immediately follow way 2: pos[2]=%d pos[4]=%d", pos[2], pos[4])
	}

	// No adjacent pair within the connected northbound chain (1->2->4) should
	// have a bearing reversal > 150 degrees. Way 3 may be appended as a
	// disconnected fallback so we only check the three connected ways.
	nbChainIDs := []int64{1, 2, 4}
	for i := 0; i < len(nbChainIDs)-1; i++ {
		fromID := nbChainIDs[i]
		toID := nbChainIDs[i+1]
		var fromWay, toWay ScoredWay
		for _, w := range ordered {
			if w.WayID == fromID {
				fromWay = w
			}
			if w.WayID == toID {
				toWay = w
			}
		}
		exitB := wayExitBearing(fromWay)
		entryB := wayEntryBearing(toWay)
		diff := geo.AngleDiff(exitB, entryB)
		if diff > 150 {
			t.Errorf("bearing reversal of %.1f° between way %d and way %d (want <= 150°)",
				diff, fromID, toID)
		}
	}
}

// TestOrderWays_SameTypeEndpoints_Reversal tests that when two non-oneway ways
// share a first-first endpoint, one gets reversed to form a continuous chain.
func TestOrderWays_SameTypeEndpoints_Reversal(t *testing.T) {
	pCommon := geo.Coord{Lat: 44.000, Lon: -72.0}
	pNorth := geo.Coord{Lat: 44.001, Lon: -72.0}
	pSouth := geo.Coord{Lat: 43.999, Lon: -72.0}

	// Way A: pCommon -> pNorth (heading north)
	wayA := makeWay(1, pCommon, pNorth)
	// Way B: pCommon -> pSouth (heading south, starts at same point as A)
	wayB := makeWay(2, pCommon, pSouth)

	// Input: [A, B]
	ordered := OrderWays(ScoredWays{wayA, wayB})

	if len(ordered) != 2 {
		t.Fatalf("expected 2 ways, got %d", len(ordered))
	}

	// The output must form a continuous chain: end[0] == start[1]
	_, end0, _ := wayEndpoints(ordered[0])
	start1, _, _ := wayEndpoints(ordered[1])
	if end0 != start1 {
		t.Errorf("chain broken: ordered[0].end %v != ordered[1].start %v", end0, start1)
	}
}

// TestOrderWays_DegreeOneStart verifies that the algorithm starts from a
// degree-1 node (route terminus) when one exists.
func TestOrderWays_DegreeOneStart(t *testing.T) {
	// Five ways forming a linear chain: 1->2, 2->3, 3->4, 4->5, 5->6
	c1 := geo.Coord{Lat: 44.000, Lon: -72.0}
	c2 := geo.Coord{Lat: 44.001, Lon: -72.0}
	c3 := geo.Coord{Lat: 44.002, Lon: -72.0}
	c4 := geo.Coord{Lat: 44.003, Lon: -72.0}
	c5 := geo.Coord{Lat: 44.004, Lon: -72.0}
	c6 := geo.Coord{Lat: 44.005, Lon: -72.0}

	w1 := makeWay(1, c1, c2)
	w2 := makeWay(2, c2, c3)
	w3 := makeWay(3, c3, c4)
	w4 := makeWay(4, c4, c5)
	w5 := makeWay(5, c5, c6)

	// Shuffled input: [way3, way5, way1, way4, way2]
	ordered := OrderWays(ScoredWays{w3, w5, w1, w4, w2})

	if len(ordered) != 5 {
		t.Fatalf("expected 5 ways, got %d", len(ordered))
	}

	// The first way's start or the last way's end must be a terminus (c1 or c6)
	firstStart, _, _ := wayEndpoints(ordered[0])
	_, lastEnd, _ := wayEndpoints(ordered[len(ordered)-1])

	isTerminus := firstStart == c1 || firstStart == c6 || lastEnd == c1 || lastEnd == c6
	if !isTerminus {
		t.Errorf("output does not start or end at a terminus: firstStart=%v lastEnd=%v", firstStart, lastEnd)
	}

	// Verify the chain is fully connected
	for i := 0; i < len(ordered)-1; i++ {
		_, end, _ := wayEndpoints(ordered[i])
		start, _, _ := wayEndpoints(ordered[i+1])
		if end != start {
			t.Errorf("chain broken between way[%d] and way[%d]: end=%v start=%v", i, i+1, end, start)
		}
	}
}

// TestOrderWays_BearingDisambiguation verifies that at a junction with one
// way continuing north and one heading south, the algorithm picks the
// northward continuation rather than the U-turn.
func TestOrderWays_BearingDisambiguation(t *testing.T) {
	pA := geo.Coord{Lat: 44.000, Lon: -72.0}
	pJunction := geo.Coord{Lat: 44.001, Lon: -72.0}
	pB := geo.Coord{Lat: 44.002, Lon: -72.0}
	pC := geo.Coord{Lat: 44.0005, Lon: -72.0} // between pA and pJunction

	// Way A: pA -> pJunction (heading north to junction)
	wayA := makeWay(1, pA, pJunction)
	// Way B: pJunction -> pB (continuing north from junction)
	wayB := makeWay(2, pJunction, pB)
	// Way C: pJunction -> pC (heading south from junction — a U-turn from A)
	wayC := makeWay(3, pJunction, pC)

	// Input shuffled: [A, C, B]
	ordered := OrderWays(ScoredWays{wayA, wayC, wayB})

	if len(ordered) != 3 {
		t.Fatalf("expected 3 ways, got %d", len(ordered))
	}

	// Way A and Way B must be adjacent in the output: when the algorithm
	// traverses through the junction (pJunction) from A's direction, it should
	// pick B (continuing north) over C (U-turn south). The start way depends
	// on which degree-1 node findStartIndex picks (map iteration order), so we
	// check adjacency rather than absolute position.
	aIdx := -1
	for i, w := range ordered {
		if w.WayID == 1 {
			aIdx = i
			break
		}
	}
	if aIdx == -1 {
		t.Fatal("way A (WayID=1) not found in output")
	}

	// Check that B is adjacent to A (either A→B or B→A depending on traversal direction)
	foundAdjacentB := false
	if aIdx+1 < len(ordered) && ordered[aIdx+1].WayID == 2 {
		foundAdjacentB = true
	}
	if aIdx-1 >= 0 && ordered[aIdx-1].WayID == 2 {
		foundAdjacentB = true
	}
	if !foundAdjacentB {
		ids := make([]int64, len(ordered))
		for i, w := range ordered {
			ids[i] = w.WayID
		}
		t.Errorf("way B (WayID=2) should be adjacent to way A (WayID=1) but ordering is %v", ids)
	}
}

// TestOrderWays_DisconnectedSubgraph verifies that a disconnected way is
// appended at the end and all ways are present in the output.
func TestOrderWays_DisconnectedSubgraph(t *testing.T) {
	// Three connected ways
	p1 := geo.Coord{Lat: 44.000, Lon: -72.0}
	p2 := geo.Coord{Lat: 44.001, Lon: -72.0}
	p3 := geo.Coord{Lat: 44.002, Lon: -72.0}
	p4 := geo.Coord{Lat: 44.003, Lon: -72.0}

	wayA := makeWay(1, p1, p2)
	wayB := makeWay(2, p2, p3)
	wayC := makeWay(3, p3, p4)

	// Way D is far away, disconnected
	wayD := makeWay(4, geo.Coord{Lat: 36.0, Lon: -80.0}, geo.Coord{Lat: 36.001, Lon: -80.0})

	// Input shuffled: [B, D, A, C]
	ordered := OrderWays(ScoredWays{wayB, wayD, wayA, wayC})

	if len(ordered) != 4 {
		t.Fatalf("expected 4 ways in output, got %d", len(ordered))
	}

	// All way IDs must be present
	ids := make(map[int64]bool)
	for _, w := range ordered {
		ids[w.WayID] = true
	}
	for _, id := range []int64{1, 2, 3, 4} {
		if !ids[id] {
			t.Errorf("way %d missing from output", id)
		}
	}
}

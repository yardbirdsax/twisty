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
	ways := []ScoredWay{
		{WayID: 1, Tags: map[string]string{"name": "Route 1"}},
		{WayID: 2, Tags: map[string]string{"name": "Route 1"}},
		{WayID: 3, Tags: map[string]string{"name": "Route 2"}},
		{WayID: 4, Tags: map[string]string{}},              // no name
		{WayID: 5, Tags: map[string]string{"name": ""}},    // empty name
		{WayID: 6, Tags: nil},                               // nil tags
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
	ways := []ScoredWay{
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

func TestFindConnectedComponents_TwoClusters(t *testing.T) {
	// Cluster A: two nearby ways in Vermont (~44°N, -72°W)
	a1 := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	a2 := makeWay(2, geo.Coord{Lat: 44.001, Lon: -72.0}, geo.Coord{Lat: 44.002, Lon: -72.0})

	// Cluster B: two nearby ways far away (~36°N, -80°W)
	b1 := makeWay(3, geo.Coord{Lat: 36.0, Lon: -80.0}, geo.Coord{Lat: 36.001, Lon: -80.0})
	b2 := makeWay(4, geo.Coord{Lat: 36.001, Lon: -80.0}, geo.Coord{Lat: 36.002, Lon: -80.0})

	ways := []ScoredWay{a1, a2, b1, b2}
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

	components := FindConnectedComponents([]ScoredWay{w1, w2, w3}, ConnectedEndpointProximityM)

	if len(components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(components))
	}
	if len(components[0]) != 3 {
		t.Errorf("expected 3 ways in component, got %d", len(components[0]))
	}
}

func TestFindConnectedComponents_SingleWay(t *testing.T) {
	w := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	components := FindConnectedComponents([]ScoredWay{w}, ConnectedEndpointProximityM)
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

	components := FindConnectedComponents([]ScoredWay{noSeg, w}, ConnectedEndpointProximityM)
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

	components := FindConnectedComponents([]ScoredWay{wNA, wEU, wAU}, ConnectedEndpointProximityM)

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
	shuffled := []ScoredWay{w3, w1, w2}
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
	ordered := OrderWays([]ScoredWay{w})
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

	ordered := OrderWays([]ScoredWay{w1, w2})
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

	ways := []ScoredWay{
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

	ways := []ScoredWay{
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
	ways := []ScoredWay{
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
	ways := []ScoredWay{
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

	ways := []ScoredWay{
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
	ways := []ScoredWay{
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
	ways := []ScoredWay{
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

	collections := Aggregate([]ScoredWay{wA1, wA2, wB1, wB2})

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
func TestSplitOrderingGaps_NoGaps(t *testing.T) {
	// 5 ways chained north, each ~22m apart (0.0002° lat steps).
	baseLat := 44.0
	step := 0.0002 // ~22m
	ways := make([]ScoredWay, 5)
	for i := range 5 {
		lat0 := baseLat + float64(i)*step
		lat1 := lat0 + step
		ways[i] = makeWay(int64(i+1), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat1, Lon: -72.0})
	}

	got := SplitOrderingGaps(ways, ConnectedEndpointProximityM)

	if len(got) != 5 {
		t.Fatalf("expected 5 ways, got %d", len(got))
	}
}

// TestSplitOrderingGaps_GapInMiddle: ways 0-3 connected, 2km gap, ways 4-5 connected.
// Should return ways 0-3 (longer chunk).
func TestSplitOrderingGaps_GapInMiddle(t *testing.T) {
	baseLat := 44.0
	step := 0.0002 // ~22m

	var ways []ScoredWay
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

	if len(got) != 4 {
		t.Fatalf("expected 4 ways (longest chunk 0-3), got %d", len(got))
	}
	for i, w := range got {
		if w.WayID != int64(i+1) {
			t.Errorf("way[%d].WayID = %d, want %d", i, w.WayID, i+1)
		}
	}
}

// TestSplitOrderingGaps_GapAtStart: first way is far from the rest. Returns ways 1-5.
func TestSplitOrderingGaps_GapAtStart(t *testing.T) {
	step := 0.0002 // ~22m

	// Way 0: isolated far away
	var ways []ScoredWay
	ways = append(ways, makeWay(1, geo.Coord{Lat: 40.0, Lon: -72.0}, geo.Coord{Lat: 40.0 + step, Lon: -72.0}))

	// Ways 1-5: connected chain starting at 44°N
	baseLat := 44.0
	for i := range 5 {
		lat0 := baseLat + float64(i)*step
		lat1 := lat0 + step
		ways = append(ways, makeWay(int64(i+2), geo.Coord{Lat: lat0, Lon: -72.0}, geo.Coord{Lat: lat1, Lon: -72.0}))
	}

	got := SplitOrderingGaps(ways, ConnectedEndpointProximityM)

	if len(got) != 5 {
		t.Fatalf("expected 5 ways (longer chunk 1-5), got %d", len(got))
	}
	// WayIDs should be 2-6
	for i, w := range got {
		if w.WayID != int64(i+2) {
			t.Errorf("way[%d].WayID = %d, want %d", i, w.WayID, i+2)
		}
	}
}

// TestSplitOrderingGaps_MultipleGaps: three chunks separated by gaps. Returns the longest.
func TestSplitOrderingGaps_MultipleGaps(t *testing.T) {
	step := 0.0002 // ~22m

	var ways []ScoredWay
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

	if len(got) != 4 {
		t.Fatalf("expected 4 ways (longest chunk B), got %d", len(got))
	}
	// Chunk B starts at index 2 → WayIDs 3,4,5,6
	for i, w := range got {
		if w.WayID != int64(i+3) {
			t.Errorf("way[%d].WayID = %d, want %d", i, w.WayID, i+3)
		}
	}
}

// TestSplitOrderingGaps_SingleWay: returns the single way unchanged.
func TestSplitOrderingGaps_SingleWay(t *testing.T) {
	w := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	got := SplitOrderingGaps([]ScoredWay{w}, ConnectedEndpointProximityM)
	if len(got) != 1 {
		t.Fatalf("expected 1 way, got %d", len(got))
	}
	if got[0].WayID != 1 {
		t.Errorf("WayID = %d, want 1", got[0].WayID)
	}
}

// TestSplitOrderingGaps_EmptyInput: returns nil/empty.
func TestSplitOrderingGaps_EmptyInput(t *testing.T) {
	got := SplitOrderingGaps(nil, ConnectedEndpointProximityM)
	if len(got) != 0 {
		t.Errorf("expected 0 ways, got %d", len(got))
	}
	got2 := SplitOrderingGaps([]ScoredWay{}, ConnectedEndpointProximityM)
	if len(got2) != 0 {
		t.Errorf("expected 0 ways for empty slice, got %d", len(got2))
	}
}

// TestSplitOrderingGaps_ValleyCreekRegression verifies that a chain with an
// overlapping retrace way is handled correctly. The scenario:
//   - 5 ways forming a north-to-south chain
//   - 1 way that overlaps the middle (its nearest endpoint is close to chain
//     end after greedy ordering, but it creates a >100m jump back into the chain)
//
// After SplitOrderingGaps the output chain should have no large gaps.
func TestSplitOrderingGaps_ValleyCreekRegression(t *testing.T) {
	step := 0.0002 // ~22m per step at 44°N

	// Build main chain: ways 1-5 progressing north.
	var mainChain []ScoredWay
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

	if len(result) != 5 {
		t.Fatalf("expected 5 ways (main chain only), got %d", len(result))
	}

	// Verify no large gaps in result.
	for i := 0; i < len(result)-1; i++ {
		_, end, _ := wayEndpoints(result[i])
		start, _, _ := wayEndpoints(result[i+1])
		d := geo.Haversine(end, start)
		if d > ConnectedEndpointProximityM {
			t.Errorf("gap between ways %d and %d: %.1f m (want <= %.1f m)", i, i+1, d, ConnectedEndpointProximityM)
		}
	}
}

func TestAggregate_UnnamedWaysExcluded(t *testing.T) {
	// Ways without a name tag should produce no collections.
	ways := []ScoredWay{
		makeScoredWay(1, map[string]string{"highway": "tertiary"}, []ScoredSegment{makeSeg(1, 500.0)}),
	}
	collections := Aggregate(ways)
	if len(collections) != 0 {
		t.Errorf("expected 0 collections for unnamed ways, got %d", len(collections))
	}
}

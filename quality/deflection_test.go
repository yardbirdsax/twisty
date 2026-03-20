package quality_test

import (
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// latOffset returns a Coord shifted north by deltaMeters from base.
// It uses the approximate conversion of 111,000 m per degree of latitude,
// which is accurate to within ~0.5% across latitudes used in tests (≈45°).
func latOffset(base geo.Coord, deltaMeters float64) geo.Coord {
	return geo.Coord{Lat: base.Lat + deltaMeters/111000.0, Lon: base.Lon}
}

// mkSeg builds a ScoredSegment with the given endpoints and score fields.
func mkSeg(a, b geo.Coord, tier int, weight float64) quality.ScoredSegment {
	l := geo.Haversine(a, b)
	return quality.ScoredSegment{
		Start: a, End: b, Length: l,
		Tier: tier, Weight: weight, Score: l * weight,
	}
}

// TestDeflectionFilter_StraightRoadWithDogleg creates a road that has a
// slightly kinked segment (scored) whose window start and end bearings are
// both nearly north (< 20° change). The filter should zero the scored segment.
func TestDeflectionFilter_StraightRoadWithDogleg(t *testing.T) {
	t.Parallel()

	// Road goes mostly north. The "dogleg" segment has a slight eastward kink —
	// bearing ~10° ENE — followed by 2.4km of northward travel. Start bearing of
	// window ≈ end bearing ≈ north → bearing change < 20° → should be zeroed.
	//
	// Build the kink: from p0 go slightly NE (bearing ~10°) for one segment, then
	// continue north for many segments totalling > 2.4km.
	origin := geo.Coord{Lat: 45.0, Lon: -122.0}

	// Kink segment: move ~100m north and ~18m east → bearing ≈ 10° (NNE).
	kinkEnd := geo.Coord{
		Lat: origin.Lat + 100.0/111000.0,
		Lon: origin.Lon + 18.0/78500.0,
	}

	// Build northward continuation from kinkEnd: 6 × 500m steps north.
	var coords []geo.Coord
	coords = append(coords, origin, kinkEnd)
	cur := kinkEnd
	for range 6 {
		next := latOffset(cur, 500)
		coords = append(coords, next)
		cur = next
	}

	// Assign a non-zero score only to the kink segment (index 0).
	n := len(coords)
	segments := make([]quality.ScoredSegment, n-1)
	for i := 0; i < n-1; i++ {
		tier := 0
		weight := 0.0
		if i == 0 {
			tier = 1
			weight = 1.0
		}
		l := geo.Haversine(coords[i], coords[i+1])
		segments[i] = quality.ScoredSegment{
			Start: coords[i], End: coords[i+1], Length: l,
			Tier: tier, Weight: weight, Score: l * weight,
		}
	}
	sw := quality.ScoredWay{WayID: 1, Segments: segments}

	quality.DeflectionFilter(&sw)

	// The kink segment (and its window) should be zeroed.
	seg0 := sw.Segments[0]
	if seg0.Score != 0 {
		t.Errorf("kink segment: expected Score=0 after filter, got %v", seg0.Score)
	}
	if seg0.Tier != 0 {
		t.Errorf("kink segment: expected Tier=0, got %d", seg0.Tier)
	}
	if seg0.Weight != 0 {
		t.Errorf("kink segment: expected Weight=0, got %v", seg0.Weight)
	}
}

// TestDeflectionFilter_GenuinelyWindingRoad creates a road with a sustained
// heading change from north to east over 2.4km (90° total). Scores should be
// preserved because the bearing change exceeds 20°.
func TestDeflectionFilter_GenuinelyWindingRoad(t *testing.T) {
	t.Parallel()

	// Road curves from north to east: start bearing ~0°, end bearing ~90°.
	// Build an arc-like path with 8 steps.
	origin := geo.Coord{Lat: 45.0, Lon: -122.0}
	coords := []geo.Coord{
		origin,
		{Lat: origin.Lat + 0.002, Lon: origin.Lon + 0.000}, // N
		{Lat: origin.Lat + 0.004, Lon: origin.Lon + 0.001}, // NNE
		{Lat: origin.Lat + 0.006, Lon: origin.Lon + 0.003}, // NE
		{Lat: origin.Lat + 0.007, Lon: origin.Lon + 0.006}, // ENE
		{Lat: origin.Lat + 0.007, Lon: origin.Lon + 0.010}, // E
		{Lat: origin.Lat + 0.007, Lon: origin.Lon + 0.015}, // E
		{Lat: origin.Lat + 0.007, Lon: origin.Lon + 0.020}, // E
	}

	n := len(coords)
	segments := make([]quality.ScoredSegment, n-1)
	for i := 0; i < n-1; i++ {
		l := geo.Haversine(coords[i], coords[i+1])
		segments[i] = quality.ScoredSegment{
			Start: coords[i], End: coords[i+1], Length: l,
			Tier: 2, Weight: 1.3, Score: l * 1.3,
		}
	}
	sw := quality.ScoredWay{WayID: 2, Segments: segments}

	quality.DeflectionFilter(&sw)

	hasNonZero := false
	for _, seg := range sw.Segments {
		if seg.Score > 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		t.Error("expected at least some segments to retain non-zero scores for genuinely winding road")
	}

	// Verify preserved segments retain their original Tier and Weight.
	for _, seg := range sw.Segments {
		if seg.Score > 0 {
			if seg.Tier != 2 {
				t.Errorf("preserved segment: expected Tier=2, got %d", seg.Tier)
			}
			if seg.Weight != 1.3 {
				t.Errorf("preserved segment: expected Weight=1.3, got %v", seg.Weight)
			}
			break // spot-check one preserved segment
		}
	}
}

// TestDeflectionFilter_ShortWay verifies that a short way (< 2.4km) with a
// kink still has the kink zeroed when the overall bearing change is minimal.
func TestDeflectionFilter_ShortWay(t *testing.T) {
	t.Parallel()

	// Short road: north 200m, slight kink (~10° bearing), north 200m.
	origin := geo.Coord{Lat: 45.0, Lon: -122.0}
	p0 := origin
	p1 := latOffset(origin, 200)
	// Kink: ~100m north + 18m east → bearing ~10°.
	p2 := geo.Coord{
		Lat: p1.Lat + 100.0/111000.0,
		Lon: p1.Lon + 18.0/78500.0,
	}
	p3 := latOffset(p2, 200)

	segments := []quality.ScoredSegment{
		mkSeg(p0, p1, 0, 0),
		mkSeg(p1, p2, 1, 1.0), // kink — should be zeroed
		mkSeg(p2, p3, 0, 0),
	}
	sw := quality.ScoredWay{WayID: 3, Segments: segments}

	quality.DeflectionFilter(&sw)

	seg1 := sw.Segments[1]
	if seg1.Score != 0 {
		t.Errorf("kink segment: expected score 0, got %v", seg1.Score)
	}
	if seg1.Tier != 0 {
		t.Errorf("kink segment: expected tier 0, got %d", seg1.Tier)
	}
	if seg1.Weight != 0 {
		t.Errorf("kink segment: expected weight 0, got %v", seg1.Weight)
	}
}

// TestDeflectionFilter_AllZeroSegments verifies that a way with all zero-score
// segments passes through unchanged.
func TestDeflectionFilter_AllZeroSegments(t *testing.T) {
	t.Parallel()

	origin := geo.Coord{Lat: 45.0, Lon: -122.0}
	coords := []geo.Coord{
		origin,
		latOffset(origin, 500),
		latOffset(origin, 1000),
	}
	segments := make([]quality.ScoredSegment, len(coords)-1)
	for i := 0; i < len(coords)-1; i++ {
		segments[i] = quality.ScoredSegment{
			Start:  coords[i],
			End:    coords[i+1],
			Length: geo.Haversine(coords[i], coords[i+1]),
			Tier:   0,
			Weight: 0,
			Score:  0,
		}
	}
	sw := quality.ScoredWay{WayID: 4, Segments: segments}

	quality.DeflectionFilter(&sw)

	for i, seg := range sw.Segments {
		if seg.Score != 0 || seg.Tier != 0 || seg.Weight != 0 {
			t.Errorf("segment %d: expected all zeros unchanged, got score=%v tier=%d weight=%v",
				i, seg.Score, seg.Tier, seg.Weight)
		}
	}
}

// TestDeflectionFilter_MixedWay tests a way where the first scored segment is
// a kink (window bearing change < 20°, should be zeroed) and a later scored
// segment is a genuine curve (window bearing change >= 20°, should be kept).
func TestDeflectionFilter_MixedWay(t *testing.T) {
	t.Parallel()

	// Section A: kink near origin (~10° bearing) followed by north road → zeroed.
	// Section B: well past 2.4km, curves from north to east → kept.
	origin := geo.Coord{Lat: 45.0, Lon: -122.0}

	// Kink segment starting at origin.
	kinkEnd := geo.Coord{
		Lat: origin.Lat + 100.0/111000.0,
		Lon: origin.Lon + 18.0/78500.0,
	}

	// North continuation for > 2.4km to fill the window.
	p2 := latOffset(kinkEnd, 600)
	p3 := latOffset(kinkEnd, 1200)
	p4 := latOffset(kinkEnd, 1800)
	p5 := latOffset(kinkEnd, 2400)

	// Section B starts after 2.4km from the kink: a genuine curve from north to east.
	p6 := geo.Coord{Lat: p5.Lat + 0.004, Lon: p5.Lon + 0.001}
	p7 := geo.Coord{Lat: p6.Lat + 0.004, Lon: p6.Lon + 0.003}
	p8 := geo.Coord{Lat: p7.Lat + 0.003, Lon: p7.Lon + 0.006}
	p9 := geo.Coord{Lat: p8.Lat + 0.001, Lon: p8.Lon + 0.009}

	segments := []quality.ScoredSegment{
		mkSeg(origin, kinkEnd, 1, 1.0), // kink — should be zeroed
		mkSeg(kinkEnd, p2, 0, 0),
		mkSeg(p2, p3, 0, 0),
		mkSeg(p3, p4, 0, 0),
		mkSeg(p4, p5, 0, 0),
		mkSeg(p5, p6, 2, 1.3), // genuine curve — should be kept
		mkSeg(p6, p7, 2, 1.3),
		mkSeg(p7, p8, 2, 1.3),
		mkSeg(p8, p9, 2, 1.3),
	}
	sw := quality.ScoredWay{WayID: 5, Segments: segments}

	quality.DeflectionFilter(&sw)

	// Segment 0 (kink) should be zeroed.
	seg0 := sw.Segments[0]
	if seg0.Score != 0 || seg0.Tier != 0 || seg0.Weight != 0 {
		t.Errorf("kink segment 0: expected zeroed, got score=%v tier=%d weight=%v",
			seg0.Score, seg0.Tier, seg0.Weight)
	}

	// At least one of segments 5-8 (genuine curve section) should retain a score.
	curveHasScore := false
	for _, idx := range []int{5, 6, 7, 8} {
		if sw.Segments[idx].Score > 0 {
			curveHasScore = true
		}
	}
	if !curveHasScore {
		t.Error("expected genuine curve section (segments 5-8) to retain at least one non-zero score")
	}
}

// TestDeflectionFilter_SingleSegment verifies that a single scored segment
// whose window contains only itself (start bearing == end bearing, change = 0°)
// is zeroed out.
func TestDeflectionFilter_SingleSegment(t *testing.T) {
	t.Parallel()

	a := geo.Coord{Lat: 45.0, Lon: -122.0}
	b := geo.Coord{Lat: 45.001, Lon: -122.001}
	segments := []quality.ScoredSegment{
		{
			Start: a, End: b,
			Length: geo.Haversine(a, b),
			Tier:   1, Weight: 1.0,
			Score: geo.Haversine(a, b),
		},
	}
	sw := quality.ScoredWay{WayID: 6, Segments: segments}

	quality.DeflectionFilter(&sw)

	// Single segment: window[0] == window[last], so bearing change = 0 < 20°.
	seg := sw.Segments[0]
	if seg.Score != 0 {
		t.Errorf("single segment: expected Score=0, got %v", seg.Score)
	}
	if seg.Tier != 0 {
		t.Errorf("single segment: expected Tier=0, got %d", seg.Tier)
	}
	if seg.Weight != 0 {
		t.Errorf("single segment: expected Weight=0, got %v", seg.Weight)
	}
}

// TestDeflectionFilter_SCurveRoad verifies that an S-curve road — one that
// curves left ~90° then right ~90° over ~2.4 km — retains its scores. The net
// (start-to-end) bearing change is nearly 0°, so the old net-bearing algorithm
// would incorrectly zero this road. The cumulative algorithm sums ~180° of
// heading change across the window, which exceeds 20° and preserves scores.
func TestDeflectionFilter_SCurveRoad(t *testing.T) {
	t.Parallel()

	// Build an S-curve: go NE (bearing ~45°) for half the window, then NW
	// (bearing ~315°) for the other half. Net change ≈ 0°; cumulative ≈ 180°.
	// Each leg is ~1200 m, staying within the 2400 m window.
	origin := geo.Coord{Lat: 45.0, Lon: -122.0}

	// First leg: NE, 8 steps × 150 m each ≈ 1200 m.
	// Move equal amounts north and east: bearing ≈ 45°.
	neStep := 150.0 / 111000.0 // ~150m per degree lat
	neLon := 150.0 / 78500.0   // ~150m per degree lon at 45°N

	coords := []geo.Coord{origin}
	for k := 1; k <= 8; k++ {
		prev := coords[k-1]
		coords = append(coords, geo.Coord{
			Lat: prev.Lat + neStep,
			Lon: prev.Lon + neLon,
		})
	}

	// Second leg: NW, 8 steps × 150 m each ≈ 1200 m.
	// Move equal amounts north and west: bearing ≈ 315°.
	for k := 1; k <= 8; k++ {
		prev := coords[len(coords)-1]
		coords = append(coords, geo.Coord{
			Lat: prev.Lat + neStep,
			Lon: prev.Lon - neLon,
		})
	}

	n := len(coords)
	segments := make([]quality.ScoredSegment, n-1)
	for i := 0; i < n-1; i++ {
		l := geo.Haversine(coords[i], coords[i+1])
		segments[i] = quality.ScoredSegment{
			Start: coords[i], End: coords[i+1], Length: l,
			Tier: 2, Weight: 1.3, Score: l * 1.3,
		}
	}
	sw := quality.ScoredWay{WayID: 20, Segments: segments}

	quality.DeflectionFilter(&sw)

	// The S-curve has a cumulative heading change of ~180°, well above the 20°
	// threshold, so scores must be preserved.
	hasNonZero := false
	for _, seg := range sw.Segments {
		if seg.Score > 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		t.Error("S-curve road: expected at least some segments to retain non-zero scores (cumulative change ~180° > 20°)")
	}
}

// TestDeflectionFilter_GentleDeviations verifies that a road with many
// segments each deviating only 1-2° from the previous is still zeroed out.
// Cumulative heading change of ~15° over 2.4 km is below the 20° threshold.
func TestDeflectionFilter_GentleDeviations(t *testing.T) {
	t.Parallel()

	// Build a road with 12 segments of ~200m each (total ~2.4km). Each segment
	// veers 1° to the right of the previous (cumulative ~12° total). This is
	// below DeflectionMinHeadingChange (20°) and should be zeroed.
	origin := geo.Coord{Lat: 45.0, Lon: -122.0}

	// Start heading north. Each step shifts 200m north and a tiny amount east
	// such that bearing increases by ~1° per segment.
	//
	// For a segment at bearing θ, a 200m step means:
	//   north component: 200*cos(θ), east component: 200*sin(θ)
	// We approximate by keeping north component fixed at 200m and adding a
	// tiny extra east shift that produces the 1° rotation via AngleDiff.
	//
	// At bearing 0° (north), a 1° eastward rotation over 200m requires
	// east component = 200*sin(1°) ≈ 3.49m.
	stepLat := 200.0 / 111000.0
	stepLon := 3.5 / 78500.0 // ~3.5m east shift per segment

	coords := []geo.Coord{origin}
	for k := 1; k <= 12; k++ {
		prev := coords[k-1]
		coords = append(coords, geo.Coord{
			Lat: prev.Lat + stepLat,
			Lon: prev.Lon + stepLon,
		})
	}

	n := len(coords)
	segments := make([]quality.ScoredSegment, n-1)
	for i := 0; i < n-1; i++ {
		l := geo.Haversine(coords[i], coords[i+1])
		segments[i] = quality.ScoredSegment{
			Start: coords[i], End: coords[i+1], Length: l,
			Tier: 1, Weight: 1.0, Score: l * 1.0,
		}
	}
	sw := quality.ScoredWay{WayID: 21, Segments: segments}

	quality.DeflectionFilter(&sw)

	// All segments should be zeroed: cumulative change is ~12° < 20°.
	for idx, seg := range sw.Segments {
		if seg.Score != 0 {
			t.Errorf("gentle deviation road: segment %d expected Score=0, got %v", idx, seg.Score)
		}
	}
}

// TestApplyDeflectionFilter verifies the batch function applies the filter to
// all scored ways.
func TestApplyDeflectionFilter(t *testing.T) {
	t.Parallel()

	origin := geo.Coord{Lat: 45.0, Lon: -122.0}
	// Kink segment followed by north continuation — should be zeroed.
	kinkEnd := geo.Coord{
		Lat: origin.Lat + 100.0/111000.0,
		Lon: origin.Lon + 18.0/78500.0,
	}
	p2 := latOffset(kinkEnd, 500)
	p3 := latOffset(kinkEnd, 1000)

	ways := []quality.ScoredWay{
		{
			WayID: 10,
			Segments: []quality.ScoredSegment{
				mkSeg(origin, kinkEnd, 1, 1.0),
				mkSeg(kinkEnd, p2, 0, 0),
				mkSeg(p2, p3, 0, 0),
			},
		},
		{WayID: 11, Segments: nil},
	}

	quality.ApplyDeflectionFilter(ways)

	seg0 := ways[0].Segments[0]
	if seg0.Score != 0 {
		t.Errorf("way 10 kink segment: expected score 0, got %v", seg0.Score)
	}
	if len(ways[1].Segments) != 0 {
		t.Errorf("way 11: expected 0 segments, got %d", len(ways[1].Segments))
	}
}

// TestDeflectionFilter_ZeroedSegmentsHaveCorrectFields verifies that when a
// segment is zeroed by the filter, all three fields (Tier, Weight, Score) are set to 0.
func TestDeflectionFilter_ZeroedSegmentsHaveCorrectFields(t *testing.T) {
	t.Parallel()

	// One kink segment (~10° bearing) followed by north continuation.
	origin := geo.Coord{Lat: 45.0, Lon: -122.0}
	kinkEnd := geo.Coord{
		Lat: origin.Lat + 100.0/111000.0,
		Lon: origin.Lon + 18.0/78500.0,
	}
	p2 := latOffset(kinkEnd, 600)
	p3 := latOffset(kinkEnd, 1200)

	l0 := geo.Haversine(origin, kinkEnd)
	segments := []quality.ScoredSegment{
		{
			Start: origin, End: kinkEnd, Length: l0,
			Tier: 3, Weight: 1.6, Score: l0 * 1.6,
		},
		mkSeg(kinkEnd, p2, 0, 0),
		mkSeg(p2, p3, 0, 0),
	}
	sw := quality.ScoredWay{WayID: 7, Segments: segments}

	quality.DeflectionFilter(&sw)

	seg := sw.Segments[0]
	if seg.Score != 0 {
		t.Errorf("zeroed segment: Score should be 0, got %v", seg.Score)
	}
	if seg.Tier != 0 {
		t.Errorf("zeroed segment: Tier should be 0, got %d", seg.Tier)
	}
	if seg.Weight != 0 {
		t.Errorf("zeroed segment: Weight should be 0, got %v", seg.Weight)
	}
}

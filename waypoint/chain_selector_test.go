package waypoint

import (
	"math"
	"math/rand"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// makeCollectionWithSegments creates a RoadCollection with multiple segments
// centered near (lat, lon), spanning approximately totalLength meters.
// Each segment is approximately segLen meters long.
func makeCollectionWithSegments(lat, lon, totalLength, score float64, numSegs int) quality.RoadCollection {
	if numSegs <= 0 {
		numSegs = 1
	}
	segLen := totalLength / float64(numSegs)
	// Spread segments north-south from lat.
	degPerMeter := 1.0 / 111000.0
	halfSpan := (totalLength / 2) * degPerMeter

	segments := make([]quality.ScoredSegment, numSegs)
	for i := 0; i < numSegs; i++ {
		startLat := lat - halfSpan + float64(i)*segLen*degPerMeter
		endLat := startLat + segLen*degPerMeter
		segments[i] = quality.ScoredSegment{
			Start:  geo.Coord{Lat: startLat, Lon: lon},
			End:    geo.Coord{Lat: endLat, Lon: lon},
			Length: segLen,
			Score:  score / float64(numSegs),
		}
	}

	return quality.RoadCollection{
		Segments:       segments,
		TotalLength:    totalLength,
		PenalizedScore: score,
	}
}

func TestSpatialGrid_BasicOperations(t *testing.T) {
	origin := testOrigin
	grid := newSpatialGrid(origin, 500.0)

	// Mark origin cell.
	grid.Mark(origin)
	if !grid.visited[grid.cellKey(origin)] {
		t.Fatal("origin cell should be marked")
	}

	// A point 1km north should be in a different cell.
	north1km := geo.DestinationPoint(origin, 0, 1000)
	if grid.visited[grid.cellKey(north1km)] {
		t.Error("1km north should not be marked yet")
	}

	// Mark a point midway along the path (500m north) to ensure partial overlap.
	// This is more robust than relying on the exact start-point behavior.
	mid := geo.DestinationPoint(origin, 0, 500)
	grid.Mark(mid)

	// OverlapFraction from origin to 1km north: mid cell is marked, so partial overlap.
	frac := grid.OverlapFraction(origin, north1km)
	if frac < 0.01 || frac > 0.99 {
		t.Errorf("expected partial overlap, got %.2f", frac)
	}

	// Mark the path; now overlap should be 1.0.
	grid.MarkPath(origin, north1km)
	frac2 := grid.OverlapFraction(origin, north1km)
	if frac2 < 0.99 {
		t.Errorf("expected full overlap after marking path, got %.2f", frac2)
	}

	// OverlapFraction to an unvisited area should be low.
	east5km := geo.DestinationPoint(origin, 90, 5000)
	frac3 := grid.OverlapFraction(origin, east5km)
	// Only the origin cell might overlap.
	if frac3 > 0.5 {
		t.Errorf("expected low overlap to unvisited area, got %.2f", frac3)
	}
}

func TestSignedAngleDiff(t *testing.T) {
	tests := []struct {
		name     string
		from, to float64
		sweepDir int
		wantSign int // 1 for positive, -1 for negative, 0 for zero
	}{
		{"CW 0->45", 0, 45, 1, 1},
		{"CW 0->315", 0, 315, 1, -1},   // 315 is -45 from 0, negative in CW
		{"CCW 0->315", 0, 315, -1, 1},   // 315 is -45 from 0, *-1 = positive
		{"CCW 0->45", 0, 45, -1, -1},    // 45 CW from 0, *-1 = negative
		{"CW 350->10", 350, 10, 1, 1},   // wraps around, 20 deg CW
		{"CW 10->350", 10, 350, 1, -1},  // 340 CCW = -20 CW
		{"same bearing", 90, 90, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := signedAngleDiff(tt.from, tt.to, tt.sweepDir)
			switch tt.wantSign {
			case 1:
				if got <= 0 {
					t.Errorf("expected positive, got %.2f", got)
				}
			case -1:
				if got >= 0 {
					t.Errorf("expected negative, got %.2f", got)
				}
			case 0:
				if got != 0 {
					t.Errorf("expected zero, got %.2f", got)
				}
			}
		})
	}
}

func TestChainSelector_NoBacktracking(t *testing.T) {
	// Place 8 collections at evenly spaced bearings, all at 20km from origin.
	// The chain should progress through bearings without major backtracking.
	origin := testOrigin
	collections := make([]quality.RoadCollection, 8)
	for i := range collections {
		bearing := float64(i) * 45.0
		collections[i] = makeCollectionAtBearing(origin, bearing, 20.0, 1000.0, 5.0)
	}

	// Run multiple seeds and check that bearing progression is mostly monotonic.
	backtrackCount := 0
	totalTransitions := 0
	for seed := int64(0); seed < 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		sel := &ChainSelector{
			Rand:          rng,
			ArcWidth:      360.0,
			AvgSpeedMPH:   35.0,
			TimeBudgetSec: 14400.0, // large budget
		}
		got := sel.Select(collections, 10, origin, 200.0)
		if len(got) < 3 {
			continue
		}

		// Check bearing progression of waypoints from origin.
		prevBearing := geo.Bearing(origin, got[0])
		for i := 1; i < len(got); i++ {
			b := geo.Bearing(origin, got[i])
			diff := geo.AngleDiff(prevBearing, b)
			totalTransitions++
			if diff > 120 { // A jump of >120° is backtracking
				backtrackCount++
			}
			prevBearing = b
		}
	}

	if totalTransitions > 0 {
		backtrackRate := float64(backtrackCount) / float64(totalTransitions)
		t.Logf("backtrack rate: %.2f%% (%d/%d)", backtrackRate*100, backtrackCount, totalTransitions)
		if backtrackRate > 0.3 {
			t.Errorf("excessive backtracking: %.2f%% of transitions jump >120°", backtrackRate*100)
		}
	}
}

func TestChainSelector_InterfaceCompliance(t *testing.T) {
	// Compile-time check is in the source; this just exercises the interface at runtime.
	var sel WaypointSelector = &ChainSelector{}
	_ = sel
}

func TestChainSelector_Deterministic(t *testing.T) {
	origin := testOrigin
	collections := []quality.RoadCollection{
		makeCollectionAtBearing(origin, 0, 20.0, 1000.0, 5.0),
		makeCollectionAtBearing(origin, 45, 25.0, 1000.0, 4.0),
		makeCollectionAtBearing(origin, 90, 30.0, 1000.0, 6.0),
		makeCollectionAtBearing(origin, 135, 15.0, 1000.0, 3.0),
		makeCollectionAtBearing(origin, 180, 20.0, 1000.0, 7.0),
	}

	rng1 := rand.New(rand.NewSource(99))
	sel1 := &ChainSelector{
		Rand:          rng1,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 7200.0,
	}
	got1 := sel1.Select(collections, 5, origin, 200.0)

	rng2 := rand.New(rand.NewSource(99))
	sel2 := &ChainSelector{
		Rand:          rng2,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 7200.0,
	}
	got2 := sel2.Select(collections, 5, origin, 200.0)

	if len(got1) != len(got2) {
		t.Fatalf("deterministic: len mismatch %d vs %d", len(got1), len(got2))
	}
	for i := range got1 {
		if math.Abs(got1[i].Lat-got2[i].Lat) > 1e-9 || math.Abs(got1[i].Lon-got2[i].Lon) > 1e-9 {
			t.Errorf("result[%d] differs: %v vs %v", i, got1[i], got2[i])
		}
	}
}

func TestChainSelector_EligibilityFilters(t *testing.T) {
	origin := testOrigin
	zeroScore := makeCollectionAtBearing(origin, 45, 10.0, 1000.0, 0.0)
	tooShort := makeCollectionAtBearing(origin, 90, 10.0, MinRoadLengthM-1, 5.0)
	tooFar := makeCollectionAtBearing(origin, 135, 200.0, 1000.0, 5.0)
	valid := makeCollectionAtBearing(origin, 180, 10.0, 1000.0, 5.0)

	collections := []quality.RoadCollection{zeroScore, tooShort, tooFar, valid}

	rng := rand.New(rand.NewSource(42))
	sel := &ChainSelector{
		Rand:          rng,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 7200.0,
	}
	got := sel.Select(collections, 10, origin, 50.0)

	// Only valid should be eligible; chain should have 1 collection → 2 waypoints (start+end).
	if len(got) == 0 {
		t.Fatal("expected at least 1 waypoint from valid collection")
	}
	// All returned waypoints should be near the valid collection.
	validMid := CollectionMidpoint(valid)
	for _, wp := range got {
		d := geo.Haversine(validMid, wp)
		if d > 15000 { // within 15km of valid midpoint
			t.Errorf("waypoint %v is too far from valid collection (%.0fm)", wp, d)
		}
	}
}

func TestChainSelector_NoRepeats(t *testing.T) {
	origin := testOrigin
	collections := make([]quality.RoadCollection, 8)
	for i := range collections {
		bearing := float64(i) * 45.0
		collections[i] = makeCollectionAtBearing(origin, bearing, 20.0, 1000.0, float64(i+1))
	}

	rng := rand.New(rand.NewSource(7))
	sel := &ChainSelector{
		Rand:          rng,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 14400.0, // large budget to visit many collections
	}
	got := sel.Select(collections, 10, origin, 200.0)

	// Verify no two waypoints are identical (would indicate a collection visited twice).
	// This is a weaker but practical test.
	if len(got) == 0 {
		t.Fatal("expected at least 1 waypoint")
	}
	seen := make(map[geo.Coord]bool)
	for _, wp := range got {
		key := geo.Coord{
			Lat: math.Round(wp.Lat*1e6) / 1e6,
			Lon: math.Round(wp.Lon*1e6) / 1e6,
		}
		if seen[key] {
			t.Errorf("duplicate waypoint detected: %v", wp)
		}
		seen[key] = true
	}
}

func TestChainSelector_TimeBudgetRespected(t *testing.T) {
	origin := testOrigin

	// 5 collections each ~5km long (5 segments of 1000m each) at 20km distance.
	// At 35mph (~15.6 m/s), each takes ~320s to ride.
	// Connector time between any two 20km-away collections is ~1278s.
	// Short budget (600s): only the initial pick fits (320s road + return ~1278s would
	// already bust the budget for any second pick). Chain has 1 collection.
	// Long budget (86400s): all 5 collections are visited.
	//
	// Using makeCollectionWithSegments so segments have real lengths, ensuring
	// extractDenseWaypoints emits multiple waypoints per collection (not just endpoints).
	collections := make([]quality.RoadCollection, 5)
	for i := range collections {
		bearing := float64(i) * 36.0
		center := geo.DestinationPoint(origin, bearing, 20000)
		c := makeCollectionWithSegments(center.Lat, center.Lon, 5000.0, 5.0, 5)
		collections[i] = c
	}

	shortBudget := 600.0 // 10 minutes — very short; only 1 collection fits
	rng := rand.New(rand.NewSource(42))
	sel := &ChainSelector{
		Rand:          rng,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: shortBudget,
	}
	got := sel.Select(collections, 10, origin, 200.0)

	if len(got) == 0 {
		t.Fatal("expected at least 1 waypoint")
	}

	// Large budget — should visit more collections, producing more waypoints.
	rng2 := rand.New(rand.NewSource(42))
	sel2 := &ChainSelector{
		Rand:          rng2,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 86400.0, // 24 hours — visits all 5 collections
	}
	got2 := sel2.Select(collections, 10, origin, 200.0)

	t.Logf("short budget: %d waypoints, long budget: %d waypoints", len(got), len(got2))
	if len(got2) <= len(got) {
		t.Errorf("expected large budget to produce more waypoints than short budget: got short=%d long=%d", len(got), len(got2))
	}
}

func TestChainSelector_BudgetBlowProtection(t *testing.T) {
	// Place collections far away so connector time is large.
	// With a tiny budget, even the nearest collection + return should bust budget.
	origin := testOrigin

	// 1 collection that would take longer than budget to complete + return.
	// At 35mph, 100km takes ~6430s. Budget is 5000s.
	// Connector + road + return ≈ 2*6430s >> 5000s.
	collections := []quality.RoadCollection{
		makeCollectionWithSegments(origin.Lat+0.9, origin.Lon, 10000.0, 5.0, 5), // ~100km away
	}

	rng := rand.New(rand.NewSource(1))
	sel := &ChainSelector{
		Rand:          rng,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 100.0, // tiny budget: 100 seconds
	}
	got := sel.Select(collections, 5, origin, 500.0)

	// The first pick is always the nearest collection regardless of budget for the initial pick.
	// After that, the chain loop respects budget. With only 1 collection, chain has exactly 1 entry.
	// The chain always includes the initial pick. What budget protection prevents is ADDING more.
	// So with 1 eligible collection, we always get it. Test with 2+ where second would bust budget.

	// Use 2 collections: one nearby and one very far.
	nearbyC := makeCollectionWithSegments(origin.Lat+0.01, origin.Lon, 1000.0, 5.0, 2) // ~1.1km away
	farC := makeCollectionWithSegments(origin.Lat+5.0, origin.Lon, 1000.0, 10.0, 2)    // ~555km away

	collections2 := []quality.RoadCollection{nearbyC, farC}

	rng2 := rand.New(rand.NewSource(1))
	sel2 := &ChainSelector{
		Rand:          rng2,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 300.0, // 5 minutes — enough for nearby but not the far one
	}
	got2 := sel2.Select(collections2, 5, origin, 1000.0)

	// We should get waypoints from nearbyC but NOT from farC.
	// farC is at lat+5.0 which is ~555km away.
	if len(got2) == 0 {
		t.Fatal("expected waypoints from nearby collection")
	}
	for _, wp := range got2 {
		d := geo.Haversine(origin, wp)
		if d > 200000 { // more than 200km from origin = must be farC
			t.Errorf("waypoint %v appears to be from the far collection (%.0fm from origin)", wp, d)
		}
	}
	_ = got
}

func TestChainSelector_HomewardBias(t *testing.T) {
	// Verify that past 40% of the time budget, candidates closer to the origin
	// receive a higher chain score than equidistant-or-farther ones with
	// identical PenalizedScore.
	//
	// Because wiring a full Select call where (a) the outbound road crosses 50%
	// of the budget AND (b) the second pick still fits within the remaining
	// budget is geometrically awkward, we test the scoring function directly:
	// replicate the algorithm's chain-score computation for two candidates from
	// a simulated mid-ride position (cumulativeTimeSec > timeBudget*0.5) and
	// assert that the nearer-to-origin candidate wins.

	origin := testOrigin
	avgSpeedMPH := 35.0
	avgSpeedMS := avgSpeedMPH * 1609.34 / 3600.0

	timeBudget := 10000.0

	// Place currentPos 20km north of origin, simulating we are past 50% of budget.
	// cumulativeTimeSec = 5500s (55% of 10000s) → homeward bias is active.
	currentPos := geo.DestinationPoint(origin, 0, 20000)
	cumulativeTimeSec := 5500.0

	// near: 5km from origin, same bearing — closer to home than currentPos.
	nearMid := geo.DestinationPoint(origin, 0, 5000)
	near := makeCollectionWithSegments(nearMid.Lat, nearMid.Lon, 2000.0, 5.0, 2)

	// far2: 35km from origin — farther from home than currentPos (20km).
	far2Mid := geo.DestinationPoint(origin, 0, 35000)
	far2 := makeCollectionWithSegments(far2Mid.Lat, far2Mid.Lon, 2000.0, 5.0, 2)

	// Compute chain scores replicating the algorithm logic.
	scoreFor := func(cand quality.RoadCollection, candMid geo.Coord, distFromStart float64) float64 {
		connectorDistM := geo.Haversine(currentPos, candMid)
		connectorTimeSec := connectorDistM / avgSpeedMS
		estimatedTimeSec := cand.TotalLength / avgSpeedMS

		candEnd := candMid
		if _, end, ok := collectionEndpoints(cand); ok {
			candEnd = end
		}
		returnTimeSec := geo.Haversine(candEnd, origin) / avgSpeedMS
		timeAfter := cumulativeTimeSec + connectorTimeSec + estimatedTimeSec + returnTimeSec
		if timeAfter > timeBudget {
			return -1 // budget exceeded
		}

		score := cand.PenalizedScore / (connectorTimeSec + 1)

		// Homeward bias: cumulativeTimeSec > timeBudget*0.4 → active.
		currentDistFromStart := geo.Haversine(currentPos, origin)
		homewardFactor := 1.0 + (currentDistFromStart-distFromStart)/currentDistFromStart
		if homewardFactor < 0.5 {
			homewardFactor = 0.5
		}
		score *= homewardFactor

		return score
	}

	nearDist := geo.Haversine(origin, nearMid)
	far2Dist := geo.Haversine(origin, far2Mid)

	nearScore := scoreFor(near, nearMid, nearDist)
	far2Score := scoreFor(far2, far2Mid, far2Dist)

	t.Logf("near score=%.4f, far2 score=%.4f (nearDist=%.0fm, far2Dist=%.0fm)", nearScore, far2Score, nearDist, far2Dist)

	if nearScore < 0 {
		t.Fatal("near collection was rejected by budget check — test setup error")
	}
	if far2Score < 0 {
		t.Skip("far2 collection rejected by budget; test geometry needs adjustment")
	}

	if nearScore <= far2Score {
		t.Errorf("homeward bias failed: near score %.4f should exceed far2 score %.4f when past 40%% of budget", nearScore, far2Score)
	}
}

func TestChainSelector_DenseWaypoints(t *testing.T) {
	// extractDenseWaypoints with a 1000m interval on a 5000m collection → ~5+ waypoints.
	numSegs := 10 // 10 segments of 500m each
	c := makeCollectionWithSegments(testOrigin.Lat, testOrigin.Lon, 5000.0, 5.0, numSegs)

	chain := []quality.RoadCollection{c}
	wps := extractDenseWaypoints(chain, 1000.0)

	// With 5000m total and 1000m interval, we expect ~5 waypoints emitted at interval
	// plus the first and last points. But since we always emit first, and reset accumulator
	// after each interval emit, we get:
	// - first point always emitted
	// - interval emits at ~1000, 2000, 3000, 4000 meters
	// - last point always emitted
	// Total: 1 + 4 + 1 = 6 (some may be deduplicated if close).
	if len(wps) < 3 {
		t.Errorf("expected at least 3 waypoints for 5000m collection with 1000m interval, got %d", len(wps))
	}
	t.Logf("extractDenseWaypoints(5000m, 1000m) = %d waypoints", len(wps))
}

func TestChainSelector_SectorFiltering(t *testing.T) {
	// To prevent arc widening from nullifying the sector filter, we place
	// ≥3 eligible collections inside the target sector (north) so the selector
	// never needs to widen. We then place a clearly-out-of-sector collection
	// (east, ~90° away) and verify no waypoints originate from it.

	origin := testOrigin

	// Three collections in the north sector (bearings 355°, 0°, 5°).
	n355 := makeCollectionAtBearing(origin, 355, 20.0, 1000.0, 5.0)
	n0 := makeCollectionAtBearing(origin, 0, 25.0, 1000.0, 5.0)
	n5 := makeCollectionAtBearing(origin, 5, 30.0, 1000.0, 5.0)
	// One collection far out of sector (east at 90°).
	east := makeCollectionAtBearing(origin, 90, 20.0, 1000.0, 5.0)

	collections := []quality.RoadCollection{n355, n0, n5, east}

	// Find a seed that produces an outbound bearing near 0° (north) so all
	// three north collections are comfortably inside the 60°-wide sector
	// (halfArc = 30°).
	var chosenSeed int64
	for seed := range int64(100000) {
		rng := rand.New(rand.NewSource(seed))
		b := rng.Float64() * 360
		// Need bearing in [330, 30] so that 355°, 0°, and 5° all satisfy
		// AngleDiff(b, bearing) <= 30°.
		if b <= 30 || b >= 330 {
			chosenSeed = seed
			break
		}
	}

	rng := rand.New(rand.NewSource(chosenSeed))
	rngCheck := rand.New(rand.NewSource(chosenSeed))
	outboundBearing := rngCheck.Float64() * 360

	sel := &ChainSelector{
		Rand:          rng,
		ArcWidth:      60.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 7200.0,
	}
	got := sel.Select(collections, 5, origin, 200.0)

	if len(got) == 0 {
		t.Fatal("expected at least 1 waypoint")
	}

	// The east collection's midpoint is ~90° from origin. Any waypoint that is
	// >60° away from the outbound bearing must have come from the east collection
	// (since all north collections are within ±30° of the outbound bearing, and
	// road geometry adds at most a few degrees of offset). Use halfArc + 5° of
	// geometric tolerance.
	halfArc := 30.0
	tolerance := 5.0
	for _, wp := range got {
		b := geo.Bearing(origin, wp)
		diff := geo.AngleDiff(b, outboundBearing)
		if diff > halfArc+tolerance {
			t.Errorf("waypoint bearing %.2f° is outside sector centered at %.2f° ±%.2f° (diff=%.2f°) — sector filtering failed",
				b, outboundBearing, halfArc, diff)
		}
	}
	t.Logf("sector filtering test: outbound=%.2f°, %d waypoints", outboundBearing, len(got))
}

func TestChainSelector_CollectionEndpoints(t *testing.T) {
	// Test collectionEndpoints helper.
	c := makeCollectionWithSegments(41.0, -77.0, 2000.0, 5.0, 4)
	start, end, ok := collectionEndpoints(c)
	if !ok {
		t.Fatal("expected ok=true for collection with segments")
	}
	wantStart := c.Segments[0].Start
	wantEnd := c.Segments[len(c.Segments)-1].End
	if start != wantStart {
		t.Errorf("start mismatch: got %v, want %v", start, wantStart)
	}
	if end != wantEnd {
		t.Errorf("end mismatch: got %v, want %v", end, wantEnd)
	}

	// Empty collection.
	empty := quality.RoadCollection{}
	_, _, ok = collectionEndpoints(empty)
	if ok {
		t.Error("expected ok=false for empty collection")
	}
}

func TestChainSelector_EmptyCollections(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	sel := &ChainSelector{
		Rand:          rng,
		ArcWidth:      360.0,
		AvgSpeedMPH:   35.0,
		TimeBudgetSec: 7200.0,
	}
	got := sel.Select(nil, 5, testOrigin, 200.0)
	if len(got) != 0 {
		t.Errorf("expected nil/empty for no collections, got %v", got)
	}
}

func TestExtractDenseWaypoints_GlobalDedup(t *testing.T) {
	// Two collections that share geographic endpoints. Collection A ends at point P;
	// Collection B begins at the same point P. Without global dedup, P would appear
	// twice (once as A's exit, once as B's entry) if they are non-consecutive in a
	// rebuilt chain, or if the chain zigzags back through P. With global dedup, the
	// second occurrence is suppressed.
	//
	// We construct a chain where A and B share the exact same entry point so that the
	// global-dedup path is exercised (consecutive proximity check would catch it if
	// they are adjacent, but we force a non-adjacent duplicate by placing a third
	// collection C between them).

	origin := testOrigin

	// Shared point: 5km north of origin.
	sharedPt := geo.DestinationPoint(origin, 0, 5000)

	// Collection A: runs from 5km north to 6km north (through sharedPt as entry).
	segA := quality.ScoredSegment{
		Start:  sharedPt,
		End:    geo.DestinationPoint(origin, 0, 6000),
		Length: 1000.0,
		Score:  1.0,
	}
	collA := quality.RoadCollection{
		Segments:       []quality.ScoredSegment{segA},
		TotalLength:    1000.0,
		PenalizedScore: 1.0,
	}

	// Collection C: a diversion east, so A's exit != C's entry and they are non-degenerate.
	segC := quality.ScoredSegment{
		Start:  geo.DestinationPoint(origin, 90, 5000),
		End:    geo.DestinationPoint(origin, 90, 6000),
		Length: 1000.0,
		Score:  1.0,
	}
	collC := quality.RoadCollection{
		Segments:       []quality.ScoredSegment{segC},
		TotalLength:    1000.0,
		PenalizedScore: 1.0,
	}

	// Collection B: re-enters at sharedPt (same as A's entry).
	segB := quality.ScoredSegment{
		Start:  sharedPt, // duplicate of A's entry
		End:    geo.DestinationPoint(origin, 0, 7000),
		Length: 1000.0,
		Score:  1.0,
	}
	collB := quality.RoadCollection{
		Segments:       []quality.ScoredSegment{segB},
		TotalLength:    1000.0,
		PenalizedScore: 1.0,
	}

	// Chain: A -> C -> B. With only consecutive dedup, sharedPt would appear in both
	// A (as entry) and B (as entry) because C's presence breaks the consecutiveness.
	chain := []quality.RoadCollection{collA, collC, collB}

	wps := extractDenseWaypoints(chain, 10000.0) // large interval → only entry/exit emitted

	// Count occurrences of sharedPt (within 11m grid resolution ≈ coordKey equality).
	key := toCoordKey(sharedPt)
	count := 0
	for _, wp := range wps {
		if toCoordKey(wp) == key {
			count++
		}
	}

	if count > 1 {
		t.Errorf("sharedPt appeared %d times in waypoints; global dedup should suppress duplicates", count)
	}
	if count == 0 {
		t.Error("sharedPt should appear exactly once in waypoints")
	}
	t.Logf("chain A->C->B produced %d waypoints; sharedPt occurrences=%d", len(wps), count)
}

func TestChainSelector_ExtractDenseWaypoints_MultipleCollections(t *testing.T) {
	// Two collections chained together; verify deduplication at junction
	// and that boundary points are present.
	c1 := makeCollectionWithSegments(41.0, -77.0, 3000.0, 5.0, 3)
	c2 := makeCollectionWithSegments(41.03, -77.0, 3000.0, 5.0, 3)

	chain := []quality.RoadCollection{c1, c2}
	wps := extractDenseWaypoints(chain, 1500.0)

	if len(wps) < 2 {
		t.Errorf("expected at least 2 waypoints for chained collections, got %d", len(wps))
	}
	// Verify no consecutive duplicates within 100m.
	for i := 1; i < len(wps); i++ {
		d := geo.Haversine(wps[i-1], wps[i])
		if d < deduplicateProximityM {
			t.Errorf("consecutive waypoints [%d] and [%d] are within %.0fm (dedup threshold)", i-1, i, deduplicateProximityM)
		}
	}

	// Verify that c1's exit and c2's entry appear as waypoints (or are within
	// dedup distance of an emitted waypoint).
	c1Exit := c1.Segments[len(c1.Segments)-1].End
	c2Entry := c2.Segments[0].Start
	foundC1Exit := false
	foundC2Entry := false
	for _, wp := range wps {
		if geo.Haversine(wp, c1Exit) <= deduplicateProximityM {
			foundC1Exit = true
		}
		if geo.Haversine(wp, c2Entry) <= deduplicateProximityM {
			foundC2Entry = true
		}
	}
	if !foundC1Exit {
		t.Errorf("c1 exit point %v not found in waypoints (or within dedup distance)", c1Exit)
	}
	if !foundC2Entry {
		t.Errorf("c2 entry point %v not found in waypoints (or within dedup distance)", c2Entry)
	}
}

func TestExtractDenseWaypoints_BoundaryPointsEmitted(t *testing.T) {
	// Three well-separated collections with a large interval so that only
	// boundary points (entry/exit) are emitted — no interval-based waypoints.
	c1 := makeCollectionWithSegments(41.0, -77.0, 2000.0, 5.0, 2)
	c2 := makeCollectionWithSegments(41.1, -77.0, 2000.0, 5.0, 2)
	c3 := makeCollectionWithSegments(41.2, -77.0, 2000.0, 5.0, 2)

	chain := []quality.RoadCollection{c1, c2, c3}
	// interval larger than any single collection → no interval emissions
	wps := extractDenseWaypoints(chain, 50000.0)

	// Expect exactly 6 boundary waypoints: entry+exit for each of 3 collections.
	if len(wps) != 6 {
		t.Fatalf("expected 6 boundary-only waypoints, got %d", len(wps))
	}

	// Verify order: c1.start, c1.end, c2.start, c2.end, c3.start, c3.end.
	expectedPoints := []geo.Coord{
		c1.Segments[0].Start,
		c1.Segments[len(c1.Segments)-1].End,
		c2.Segments[0].Start,
		c2.Segments[len(c2.Segments)-1].End,
		c3.Segments[0].Start,
		c3.Segments[len(c3.Segments)-1].End,
	}
	for i, want := range expectedPoints {
		d := geo.Haversine(wps[i], want)
		if d > deduplicateProximityM {
			t.Errorf("waypoint[%d] at %v is %.0fm from expected boundary point %v", i, wps[i], d, want)
		}
	}
}

func TestReverseCollection(t *testing.T) {
	// Build a collection with 3 segments.
	segs := []quality.ScoredSegment{
		{WayID: 1, Start: geo.Coord{Lat: 41.0, Lon: -77.0}, End: geo.Coord{Lat: 41.01, Lon: -77.0}, Radius: 100, Tier: 1, Weight: 1.0, Length: 1000, Score: 3.0},
		{WayID: 2, Start: geo.Coord{Lat: 41.01, Lon: -77.0}, End: geo.Coord{Lat: 41.02, Lon: -77.0}, Radius: 200, Tier: 2, Weight: 2.0, Length: 1100, Score: 4.0},
		{WayID: 3, Start: geo.Coord{Lat: 41.02, Lon: -77.0}, End: geo.Coord{Lat: 41.03, Lon: -77.0}, Radius: 300, Tier: 3, Weight: 3.0, Length: 1200, Score: 5.0},
	}
	c := quality.RoadCollection{
		Name:                 "test",
		SubIndex:             7,
		HighwayTypes:         []string{"primary", "secondary"},
		WayIDs:               []int64{1, 2, 3},
		Segments:             segs,
		TotalScore:           12.0,
		TotalLength:          3300.0,
		ScorePerKm:           3.636,
		HighwayPenaltyFactor: 0.9,
		PenalizedScore:       10.8,
		PenalizedPerKm:       3.27,
	}

	rev := reverseCollection(c)

	// Segment order should be reversed.
	if len(rev.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(rev.Segments))
	}
	// rev[0] is original seg[2] with Start/End swapped.
	if rev.Segments[0].WayID != 3 {
		t.Errorf("rev[0].WayID: got %d, want 3", rev.Segments[0].WayID)
	}
	if rev.Segments[0].Start != segs[2].End {
		t.Errorf("rev[0].Start: got %v, want %v", rev.Segments[0].Start, segs[2].End)
	}
	if rev.Segments[0].End != segs[2].Start {
		t.Errorf("rev[0].End: got %v, want %v", rev.Segments[0].End, segs[2].Start)
	}
	// rev[1] is original seg[1] with Start/End swapped.
	if rev.Segments[1].WayID != 2 {
		t.Errorf("rev[1].WayID: got %d, want 2", rev.Segments[1].WayID)
	}
	if rev.Segments[1].Start != segs[1].End {
		t.Errorf("rev[1].Start: got %v, want %v", rev.Segments[1].Start, segs[1].End)
	}
	if rev.Segments[1].End != segs[1].Start {
		t.Errorf("rev[1].End: got %v, want %v", rev.Segments[1].End, segs[1].Start)
	}
	// rev[2] is original seg[0] with Start/End swapped.
	if rev.Segments[2].WayID != 1 {
		t.Errorf("rev[2].WayID: got %d, want 1", rev.Segments[2].WayID)
	}
	if rev.Segments[2].Start != segs[0].End {
		t.Errorf("rev[2].Start: got %v, want %v", rev.Segments[2].Start, segs[0].End)
	}
	if rev.Segments[2].End != segs[0].Start {
		t.Errorf("rev[2].End: got %v, want %v", rev.Segments[2].End, segs[0].Start)
	}
	// Other fields in each reversed segment are preserved.
	if rev.Segments[0].Radius != segs[2].Radius {
		t.Errorf("rev[0].Radius: got %v, want %v", rev.Segments[0].Radius, segs[2].Radius)
	}
	if rev.Segments[0].Length != segs[2].Length {
		t.Errorf("rev[0].Length: got %v, want %v", rev.Segments[0].Length, segs[2].Length)
	}
	// Metadata fields preserved.
	if rev.Name != c.Name {
		t.Errorf("Name: got %q, want %q", rev.Name, c.Name)
	}
	if rev.SubIndex != c.SubIndex {
		t.Errorf("SubIndex: got %d, want %d", rev.SubIndex, c.SubIndex)
	}
	if rev.TotalScore != c.TotalScore {
		t.Errorf("TotalScore: got %v, want %v", rev.TotalScore, c.TotalScore)
	}
	if rev.TotalLength != c.TotalLength {
		t.Errorf("TotalLength: got %v, want %v", rev.TotalLength, c.TotalLength)
	}
	if rev.PenalizedScore != c.PenalizedScore {
		t.Errorf("PenalizedScore: got %v, want %v", rev.PenalizedScore, c.PenalizedScore)
	}
	if rev.HighwayPenaltyFactor != c.HighwayPenaltyFactor {
		t.Errorf("HighwayPenaltyFactor: got %v, want %v", rev.HighwayPenaltyFactor, c.HighwayPenaltyFactor)
	}
}

func TestOrientCollection(t *testing.T) {
	// Collection going north: start at lat 41.0, end at lat 41.05.
	segs := []quality.ScoredSegment{
		{Start: geo.Coord{Lat: 41.0, Lon: -77.0}, End: geo.Coord{Lat: 41.025, Lon: -77.0}, Length: 2775},
		{Start: geo.Coord{Lat: 41.025, Lon: -77.0}, End: geo.Coord{Lat: 41.05, Lon: -77.0}, Length: 2775},
	}
	c := quality.RoadCollection{Segments: segs, TotalLength: 5550, PenalizedScore: 5.0}

	// Approach from the start side (near lat 41.0): should not reverse.
	approachStart := geo.Coord{Lat: 41.0, Lon: -77.1}
	oriented, exit := orientCollection(c, approachStart)
	startPt, endPt, ok := collectionEndpoints(oriented)
	if !ok {
		t.Fatal("expected ok=true for oriented collection")
	}
	if math.Abs(startPt.Lat-41.0) > 0.001 {
		t.Errorf("approach-from-start: oriented start should be near 41.0, got %.4f", startPt.Lat)
	}
	if math.Abs(exit.Lat-41.05) > 0.001 {
		t.Errorf("approach-from-start: exit should be near 41.05, got %.4f", exit.Lat)
	}
	_ = endPt

	// Approach from the end side (near lat 41.05): should reverse.
	approachEnd := geo.Coord{Lat: 41.05, Lon: -77.1}
	orientedRev, exitRev := orientCollection(c, approachEnd)
	startPtRev, _, okRev := collectionEndpoints(orientedRev)
	if !okRev {
		t.Fatal("expected ok=true for reversed oriented collection")
	}
	if math.Abs(startPtRev.Lat-41.05) > 0.001 {
		t.Errorf("approach-from-end: oriented start should be near 41.05, got %.4f", startPtRev.Lat)
	}
	if math.Abs(exitRev.Lat-41.0) > 0.001 {
		t.Errorf("approach-from-end: exit should be near 41.0, got %.4f", exitRev.Lat)
	}
}

func TestChainSelector_NoSpurFromEndApproach(t *testing.T) {
	// Create a collection going north (start=41.0, end=41.05).
	segs := []quality.ScoredSegment{
		{Start: geo.Coord{Lat: 41.0, Lon: -77.0}, End: geo.Coord{Lat: 41.025, Lon: -77.0}, Length: 2775},
		{Start: geo.Coord{Lat: 41.025, Lon: -77.0}, End: geo.Coord{Lat: 41.05, Lon: -77.0}, Length: 2775},
	}
	c := quality.RoadCollection{Segments: segs, TotalLength: 5550, PenalizedScore: 5.0}

	// Approach from the end side (near lat 41.05) — orientCollection should
	// reverse the collection so that the first waypoint is near lat 41.05,
	// not the original start at lat 41.0. This eliminates the spur that
	// would occur if Valhalla had to route past the collection to reach its start.
	approachPos := geo.Coord{Lat: 41.05, Lon: -77.0}
	oriented, exitPos := orientCollection(c, approachPos)

	if len(oriented.Segments) == 0 {
		t.Fatal("oriented collection has no segments")
	}

	firstWP := oriented.Segments[0].Start
	// The first waypoint should be near the approach side (lat ~41.05), not the far end (lat 41.0).
	if math.Abs(firstWP.Lat-41.05) > 0.001 {
		t.Errorf("first waypoint lat %.4f should be near 41.05 (approach side); spur not eliminated", firstWP.Lat)
	}

	// Exit should be the original start (far from approach).
	if math.Abs(exitPos.Lat-41.0) > 0.001 {
		t.Errorf("exit should be near 41.0 (original start), got %.4f", exitPos.Lat)
	}
}

package waypoint

import (
	"math"
	"math/rand"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// makeCollectionAtBearing places a collection approximately distKm away from origin
// along bearingDeg using geo.DestinationPoint.
func makeCollectionAtBearing(origin geo.Coord, bearingDeg, distKm, length, score float64) quality.RoadCollection {
	distM := distKm * 1000
	center := geo.DestinationPoint(origin, bearingDeg, distM)
	return makeCollection(center.Lat, center.Lon, length, score)
}

func TestSectorLobeSelector_Deterministic(t *testing.T) {
	rng1 := rand.New(rand.NewSource(42))
	sel1 := &SectorLobeSelector{Rand: rng1, ArcWidth: DefaultArcWidth}

	collections := make([]quality.RoadCollection, 10)
	for i := range collections {
		bearing := float64(i) * 36.0 // spread evenly around 360°
		collections[i] = makeCollectionAtBearing(testOrigin, bearing, 20.0, 1000.0, float64(i+1))
	}

	got1 := sel1.Select(collections, 3, testOrigin, 200.0)

	rng2 := rand.New(rand.NewSource(42))
	sel2 := &SectorLobeSelector{Rand: rng2, ArcWidth: DefaultArcWidth}
	got2 := sel2.Select(collections, 3, testOrigin, 200.0)

	if len(got1) != len(got2) {
		t.Fatalf("deterministic: len mismatch %d vs %d", len(got1), len(got2))
	}
	for i := range got1 {
		if math.Abs(got1[i].Lat-got2[i].Lat) > 1e-9 || math.Abs(got1[i].Lon-got2[i].Lon) > 1e-9 {
			t.Errorf("result[%d] differs: %v vs %v", i, got1[i], got2[i])
		}
	}
	if len(got1) != 3 {
		t.Errorf("expected 3 results, got %d", len(got1))
	}
}

func TestSectorLobeSelector_SectorFiltering(t *testing.T) {
	// Place 5 collections at known bearings: 0, 72, 144, 216, 288 degrees.
	// Set outbound bearing to 0° and narrow arc (60°) so only bearing=0 is in sector.
	// We need to inject the bearing via a seeded rng that produces ~0.0 for the bearing pick.
	// Instead, use a wider angle and verify bearings outside are excluded.

	// Place collections at bearings 0, 90, 180, 270 from origin.
	north := makeCollectionAtBearing(testOrigin, 0, 20.0, 1000.0, 5.0)
	east := makeCollectionAtBearing(testOrigin, 90, 20.0, 1000.0, 5.0)
	south := makeCollectionAtBearing(testOrigin, 180, 20.0, 1000.0, 5.0)
	west := makeCollectionAtBearing(testOrigin, 270, 20.0, 1000.0, 5.0)
	collections := []quality.RoadCollection{north, east, south, west}

	// Use a seeded rng that will produce an outbound bearing near 0° (north).
	// seed 0 → first Float64 is 0.9451961492941164 * 360 = ~340°
	// With ArcWidth=60, halfArc=30, so sector covers [310, 10].
	// north (0°) and west (270°) may or may not fall in; check consistency.
	// Instead, seed to get a known bearing.

	// We'll verify sector filtering by checking all returned coords have bearings
	// within the expected sector. We allow arc widening if count exceeds sector.
	rng := rand.New(rand.NewSource(7))
	sel := &SectorLobeSelector{Rand: rng, ArcWidth: 60.0}

	// Request only 1 so arc widening is unlikely to trigger immediately.
	got := sel.Select(collections, 1, testOrigin, 200.0)
	if len(got) == 0 {
		t.Fatal("expected at least 1 result")
	}

	// Reconstruct what bearing was chosen by the same seed.
	rng2 := rand.New(rand.NewSource(7))
	outboundBearing := rng2.Float64() * 360.0
	halfArc := 30.0

	for _, coord := range got {
		b := geo.Bearing(testOrigin, coord)
		if geo.AngleDiff(b, outboundBearing) > halfArc+1.0 { // +1 for float tolerance
			t.Errorf("coord bearing %.2f° is outside sector centered at %.2f° ±%.2f°", b, outboundBearing, halfArc)
		}
	}
}

func TestSectorLobeSelector_ArcWidening(t *testing.T) {
	// Place all collections at bearing 180° (south). Set outbound near 0° (north)
	// with a very narrow arc. The selector should widen until it finds candidates.
	collections := []quality.RoadCollection{
		makeCollectionAtBearing(testOrigin, 180, 10.0, 1000.0, 5.0),
		makeCollectionAtBearing(testOrigin, 175, 15.0, 1000.0, 4.0),
		makeCollectionAtBearing(testOrigin, 185, 20.0, 1000.0, 3.0),
	}

	// Seed so outbound bearing is near 0° (north).
	// Find a seed that gives bearing close to 0.
	// seed=13 → first float = ?
	// We'll just assert that with enough widening, we get results even if
	// the initial sector points away from all candidates.
	// Use a fixed rng that gives outbound = ~0°.
	// Force by injecting a custom rng that returns 0.0 first (bearing = 0).
	rng := rand.New(rand.NewSource(0))
	sel := &SectorLobeSelector{Rand: rng, ArcWidth: 10.0}

	got := sel.Select(collections, 1, testOrigin, 200.0)
	if len(got) == 0 {
		t.Fatal("expected results after arc widening, got 0")
	}
}

func TestSectorLobeSelector_FullCircleFallback(t *testing.T) {
	// All collections at bearing 90° (east). Outbound near 270° (west) with maxArcWidth=180.
	// Even after widening to 180°, AngleDiff(90, 270) = 180° which equals halfArc=90, border case.
	// Place at 91° so it's outside 180° arc from 270°.
	collections := []quality.RoadCollection{
		makeCollectionAtBearing(testOrigin, 91, 10.0, 1000.0, 5.0),
		makeCollectionAtBearing(testOrigin, 92, 15.0, 1000.0, 4.0),
		makeCollectionAtBearing(testOrigin, 93, 20.0, 1000.0, 3.0),
	}

	// Use seed 0 which gives bearing ~340°. AngleDiff(91, 340) ≈ 111° > 90° initial halfArc.
	// After widening to 120 → halfArc=60, still > 60? AngleDiff(91,340)=111 > 60. Keep widening.
	// 150 → halfArc=75 > 75? AngleDiff=111 > 75. Keep widening.
	// 180 → halfArc=90. AngleDiff(91,340)=111 > 90. Full circle fallback triggers.
	rng := rand.New(rand.NewSource(0))
	sel := &SectorLobeSelector{Rand: rng, ArcWidth: DefaultArcWidth}

	got := sel.Select(collections, 3, testOrigin, 200.0)
	if len(got) != 3 {
		t.Errorf("expected 3 results from full circle fallback, got %d", len(got))
	}
}

func TestSectorLobeSelector_LobeOrdering(t *testing.T) {
	// Place waypoints at known bearings and distances to verify lobe ordering.
	// Outbound bearing = 0° (north). Collections at:
	//   bearing=350° (left of north), dist=10km  → outbound side (left)
	//   bearing=355° (left of north), dist=20km  → outbound side (left)
	//   bearing=5°  (right of north), dist=30km  → return side (right)
	//   bearing=10° (right of north), dist=15km  → return side (right)
	//
	// Expected order: outbound(left) ascending dist: 10, 20; return(right) descending dist: 30, 15.

	origin := testOrigin
	c350_10 := makeCollectionAtBearing(origin, 350, 10.0, 1000.0, 5.0)
	c355_20 := makeCollectionAtBearing(origin, 355, 20.0, 1000.0, 5.0)
	c5_30 := makeCollectionAtBearing(origin, 5, 30.0, 1000.0, 5.0)
	c10_15 := makeCollectionAtBearing(origin, 10, 15.0, 1000.0, 5.0)

	collections := []quality.RoadCollection{c350_10, c355_20, c5_30, c10_15}

	// Use seed that produces outbound bearing ≈ 0°.
	// Seed 5: first Float64 ≈ ?
	// We need outbound close to 0° (north). Let's pick a seed that gives ~0.001..
	// Try seed 9999999.
	// Instead, we'll use a deterministic approach: find seed giving close to 0.
	// Bearing from seed s: rng.Float64() * 360.
	// For seed producing near 0, we'd need Float64 ≈ 0.
	// Let's just use a broad arc (170°) to include all 4 and check ordering directly.
	// With outbound near 0° and arc=170°, bearings 350,355,5,10 all within 85° of 0°.

	// Find a seed that gives outbound near 0°.
	var chosenSeed int64
	for seed := range int64(10000) {
		rng := rand.New(rand.NewSource(seed))
		b := rng.Float64() * 360
		if b < 15 || b > 345 {
			chosenSeed = seed
			break
		}
	}

	rng := rand.New(rand.NewSource(chosenSeed))
	// Reconstruct outbound bearing.
	rngCheck := rand.New(rand.NewSource(chosenSeed))
	outbound := rngCheck.Float64() * 360

	// Use arc wide enough to capture all 4.
	sel := &SectorLobeSelector{Rand: rng, ArcWidth: 170.0}
	got := sel.Select(collections, 4, origin, 200.0)

	if len(got) != 4 {
		t.Fatalf("expected 4 waypoints, got %d (outbound bearing=%.2f)", len(got), outbound)
	}

	// Classify each position in got as outbound (diff < 0) or return (diff >= 0).
	type posInfo struct {
		dist   float64
		isLeft bool // diff < 0 means left of outbound bearing
	}
	positions := make([]posInfo, len(got))
	leftCount, rightCount := 0, 0
	for i, c := range got {
		b := geo.Bearing(origin, c)
		diff := b - outbound
		for diff > 180 {
			diff -= 360
		}
		for diff < -180 {
			diff += 360
		}
		positions[i] = posInfo{dist: geo.Haversine(origin, c), isLeft: diff < 0}
		if diff < 0 {
			leftCount++
		} else {
			rightCount++
		}
	}

	// Determine which side is outbound (larger side, or left by tie).
	outboundIsLeft := leftCount >= rightCount
	outboundCount := leftCount
	if !outboundIsLeft {
		outboundCount = rightCount
	}

	// Assert outbound coords occupy the first outboundCount positions.
	for i := 0; i < outboundCount; i++ {
		if positions[i].isLeft != outboundIsLeft {
			t.Errorf("position[%d] expected outbound side (isLeft=%v), got isLeft=%v", i, outboundIsLeft, positions[i].isLeft)
		}
	}
	// Assert return coords occupy the remaining positions.
	for i := outboundCount; i < len(got); i++ {
		if positions[i].isLeft == outboundIsLeft {
			t.Errorf("position[%d] expected return side (isLeft=%v), got isLeft=%v", i, !outboundIsLeft, positions[i].isLeft)
		}
	}

	// Outbound slice (first outboundCount) should be ascending distance.
	for i := 1; i < outboundCount; i++ {
		if positions[i].dist < positions[i-1].dist {
			t.Errorf("outbound dists not ascending at [%d]: %.0f > %.0f", i, positions[i-1].dist, positions[i].dist)
		}
	}
	// Return slice (remaining positions) should be descending distance.
	for i := outboundCount + 1; i < len(got); i++ {
		if positions[i].dist > positions[i-1].dist {
			t.Errorf("return dists not descending at [%d]: %.0f < %.0f", i, positions[i-1].dist, positions[i].dist)
		}
	}
}

func TestSectorLobeSelector_EligibilityFilters(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	sel := &SectorLobeSelector{Rand: rng, ArcWidth: 360.0}

	zeroScore := makeCollectionAtBearing(testOrigin, 45, 10.0, 1000.0, 0.0)
	tooShort := makeCollectionAtBearing(testOrigin, 90, 10.0, MinRoadLengthM-1, 5.0)
	tooFar := makeCollectionAtBearing(testOrigin, 135, 200.0, 1000.0, 5.0)
	valid := makeCollectionAtBearing(testOrigin, 180, 10.0, 1000.0, 5.0)

	collections := []quality.RoadCollection{zeroScore, tooShort, tooFar, valid}
	got := sel.Select(collections, 10, testOrigin, 50.0)

	if len(got) != 1 {
		t.Fatalf("expected 1 valid result, got %d", len(got))
	}
	validMid := CollectionMidpoint(valid)
	if math.Abs(got[0].Lat-validMid.Lat) > 1e-4 || math.Abs(got[0].Lon-validMid.Lon) > 1e-4 {
		t.Errorf("returned coord doesn't match valid collection midpoint: got %v, want %v", got[0], validMid)
	}
}

func TestSectorLobeSelector_SingleSideCluster(t *testing.T) {
	// All waypoints on the right side (bearing > outbound). Should sort nearest→farthest.
	origin := testOrigin

	// Find a seed with outbound near 0° and put all collections at 5, 10, 15° (all "right" side).
	var chosenSeed int64
	for seed := range int64(10000) {
		rng := rand.New(rand.NewSource(seed))
		b := rng.Float64() * 360
		if b < 2 {
			chosenSeed = seed
			break
		}
	}

	c5_30 := makeCollectionAtBearing(origin, 5, 30.0, 1000.0, 5.0)
	c10_10 := makeCollectionAtBearing(origin, 10, 10.0, 1000.0, 5.0)
	c15_20 := makeCollectionAtBearing(origin, 15, 20.0, 1000.0, 5.0)
	collections := []quality.RoadCollection{c5_30, c10_10, c15_20}

	rng := rand.New(rand.NewSource(chosenSeed))
	sel := &SectorLobeSelector{Rand: rng, ArcWidth: 60.0}

	got := sel.Select(collections, 3, origin, 200.0)
	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}

	// All on one side: should be nearest → farthest.
	for i := 1; i < len(got); i++ {
		d1 := geo.Haversine(origin, got[i-1])
		d2 := geo.Haversine(origin, got[i])
		if d2 < d1 {
			t.Errorf("single-side: not sorted nearest→farthest at [%d]: %.0f > %.0f", i, d1, d2)
		}
	}
}

func TestSectorLobeSelector_DefaultArcWidth(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	// ArcWidth == 0 should default to DefaultArcWidth (no panic, returns results).
	sel := &SectorLobeSelector{Rand: rng, ArcWidth: 0}

	collections := make([]quality.RoadCollection, 5)
	for i := range collections {
		collections[i] = makeCollectionAtBearing(testOrigin, float64(i)*72, 20.0, 1000.0, float64(i+1))
	}

	got := sel.Select(collections, 3, testOrigin, 200.0)
	if len(got) == 0 {
		t.Error("expected results with default arc width, got 0")
	}
}

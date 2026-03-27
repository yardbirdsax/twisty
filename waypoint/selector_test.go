package waypoint

import (
	"math"
	"math/rand"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// makeCollection is a test helper that creates a RoadCollection with a single
// segment centered at the given coord.
func makeCollection(lat, lon, length, penalizedScore float64) quality.RoadCollection {
	half := 0.001 // ~111m; just enough to have a midpoint near lat/lon
	return quality.RoadCollection{
		Segments: []quality.ScoredSegment{
			{
				Start: geo.Coord{Lat: lat - half, Lon: lon - half},
				End:   geo.Coord{Lat: lat + half, Lon: lon + half},
			},
		},
		TotalLength:    length,
		PenalizedScore: penalizedScore,
	}
}

// origin used across tests: roughly central Pennsylvania
var testOrigin = geo.Coord{Lat: 41.0, Lon: -77.0}

// nearby returns a coord that is approximately distKm away from testOrigin to the north.
func nearby(distKm float64) geo.Coord {
	// 1 degree lat ≈ 111 km
	return geo.Coord{Lat: testOrigin.Lat + distKm/111.0, Lon: testOrigin.Lon}
}

func TestWeightedRandomSelector_Deterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	sel := &WeightedRandomSelector{Rand: rng}

	collections := make([]quality.RoadCollection, 10)
	for i := range collections {
		// Place midpoints within 50 km at slightly different bearings.
		lat := testOrigin.Lat + float64(i+1)*0.1
		lon := testOrigin.Lon + float64(i+1)*0.05
		collections[i] = makeCollection(lat, lon, 1000.0, float64(i+1))
	}

	got1 := sel.Select(collections, 3, testOrigin, 200.0)

	// Re-seed with same seed for second run.
	rng2 := rand.New(rand.NewSource(42))
	sel2 := &WeightedRandomSelector{Rand: rng2}
	got2 := sel2.Select(collections, 3, testOrigin, 200.0)

	if len(got1) != len(got2) {
		t.Fatalf("deterministic test: len mismatch %d vs %d", len(got1), len(got2))
	}
	for i := range got1 {
		if math.Abs(got1[i].Lat-got2[i].Lat) > 1e-9 || math.Abs(got1[i].Lon-got2[i].Lon) > 1e-9 {
			t.Errorf("result[%d] differs between runs: %v vs %v", i, got1[i], got2[i])
		}
	}
	if len(got1) != 3 {
		t.Errorf("expected 3 results, got %d", len(got1))
	}
}

func TestWeightedRandomSelector_ZeroScoreExcluded(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	sel := &WeightedRandomSelector{Rand: rng}

	// Two eligible, one with zero score.
	zeroMid := nearby(10.0)
	collections := []quality.RoadCollection{
		makeCollection(zeroMid.Lat, zeroMid.Lon, 1000.0, 0.0), // zero score — excluded
		makeCollection(nearby(20.0).Lat, nearby(20.0).Lon, 1000.0, 5.0),
		makeCollection(nearby(30.0).Lat, nearby(30.0).Lon, 1000.0, 3.0),
	}

	got := sel.Select(collections, 3, testOrigin, 200.0)

	// Only 2 eligible, so should get 2.
	if len(got) != 2 {
		t.Fatalf("expected 2 results (zero-score excluded), got %d", len(got))
	}
	for _, c := range got {
		if math.Abs(c.Lat-zeroMid.Lat) < 1e-4 && math.Abs(c.Lon-zeroMid.Lon) < 1e-4 {
			t.Errorf("zero-score collection midpoint was selected")
		}
	}
}

func TestWeightedRandomSelector_ShortLengthExcluded(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	sel := &WeightedRandomSelector{Rand: rng}

	shortMid := nearby(10.0)
	collections := []quality.RoadCollection{
		makeCollection(shortMid.Lat, shortMid.Lon, MinRoadLengthM-1, 5.0), // too short
		makeCollection(nearby(20.0).Lat, nearby(20.0).Lon, 1000.0, 5.0),
		makeCollection(nearby(30.0).Lat, nearby(30.0).Lon, 1000.0, 3.0),
	}

	got := sel.Select(collections, 3, testOrigin, 200.0)

	if len(got) != 2 {
		t.Fatalf("expected 2 results (short collection excluded), got %d", len(got))
	}
	for _, c := range got {
		if math.Abs(c.Lat-shortMid.Lat) < 1e-4 && math.Abs(c.Lon-shortMid.Lon) < 1e-4 {
			t.Errorf("short collection midpoint was selected")
		}
	}
}

func TestWeightedRandomSelector_OutsideRadiusExcluded(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	sel := &WeightedRandomSelector{Rand: rng}

	// Place one collection far away (> 50 km).
	farMid := geo.Coord{Lat: testOrigin.Lat + 2.0, Lon: testOrigin.Lon} // ~222 km north
	collections := []quality.RoadCollection{
		makeCollection(farMid.Lat, farMid.Lon, 1000.0, 5.0), // outside 50 km radius
		makeCollection(nearby(10.0).Lat, nearby(10.0).Lon, 1000.0, 5.0),
		makeCollection(nearby(20.0).Lat, nearby(20.0).Lon, 1000.0, 3.0),
	}

	got := sel.Select(collections, 3, testOrigin, 50.0)

	if len(got) != 2 {
		t.Fatalf("expected 2 results (outside-radius excluded), got %d", len(got))
	}
	for _, c := range got {
		if math.Abs(c.Lat-farMid.Lat) < 1e-4 {
			t.Errorf("outside-radius collection midpoint was selected")
		}
	}
}

func TestWeightedRandomSelector_FewerEligibleThanCount(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	sel := &WeightedRandomSelector{Rand: rng}

	collections := []quality.RoadCollection{
		makeCollection(nearby(10.0).Lat, nearby(10.0).Lon, 1000.0, 5.0),
		makeCollection(nearby(20.0).Lat, nearby(20.0).Lon, 1000.0, 3.0),
	}

	got := sel.Select(collections, 10, testOrigin, 200.0)

	if len(got) != 2 {
		t.Errorf("expected 2 results (all eligible), got %d", len(got))
	}
}

func TestWeightedRandomSelector_BearingSorted(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	sel := &WeightedRandomSelector{Rand: rng}

	// Place three collections at known bearings from testOrigin.
	// North (~0°), East (~90°), South (~180°).
	north := geo.Coord{Lat: testOrigin.Lat + 0.2, Lon: testOrigin.Lon}
	east := geo.Coord{Lat: testOrigin.Lat, Lon: testOrigin.Lon + 0.2}
	south := geo.Coord{Lat: testOrigin.Lat - 0.2, Lon: testOrigin.Lon}

	collections := []quality.RoadCollection{
		makeCollection(south.Lat, south.Lon, 1000.0, 5.0),
		makeCollection(east.Lat, east.Lon, 1000.0, 5.0),
		makeCollection(north.Lat, north.Lon, 1000.0, 5.0),
	}

	got := sel.Select(collections, 3, testOrigin, 200.0)

	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}

	// Verify bearings are non-decreasing.
	for i := 1; i < len(got); i++ {
		prevBearing := geo.Bearing(testOrigin, got[i-1])
		curBearing := geo.Bearing(testOrigin, got[i])
		if curBearing < prevBearing {
			t.Errorf("bearings not sorted: [%d]=%.2f > [%d]=%.2f", i-1, prevBearing, i, curBearing)
		}
	}
}

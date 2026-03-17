package route

import (
	"math"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

func TestScoreStraightLine(t *testing.T) {
	// Three points along the same longitude, increasing latitude (due north).
	r := Route{
		Points: []geo.Coord{
			{Lat: 40.0, Lon: -74.0},
			{Lat: 40.5, Lon: -74.0},
			{Lat: 41.0, Lon: -74.0},
		},
	}
	ScoreRoute(&r)
	if r.Stats.AngularDensity >= 0.01 {
		t.Errorf("expected AngularDensity < 0.01 for straight line, got %f", r.Stats.AngularDensity)
	}
	if r.Stats.Score < 0 {
		t.Errorf("expected non-negative Score, got %f", r.Stats.Score)
	}
}

func TestScoreRightAngleTurn(t *testing.T) {
	// Three points forming a 90-degree turn:
	// Go north then turn east.
	p0 := geo.Coord{Lat: 40.0, Lon: -74.0}
	p1 := geo.Coord{Lat: 40.1, Lon: -74.0}
	p2 := geo.Coord{Lat: 40.1, Lon: -73.9}

	b1 := geo.Bearing(p0, p1)
	b2 := geo.Bearing(p1, p2)
	diff := geo.AngleDiff(b1, b2)

	// The angle difference at the turn should be close to 90 degrees.
	if math.Abs(diff-90.0) > 1.0 {
		t.Errorf("expected ~90 degree turn, got %f", diff)
	}

	r := Route{Points: []geo.Coord{p0, p1, p2}}
	ScoreRoute(&r)
	if r.Stats.Score < 0 {
		t.Errorf("expected non-negative Score, got %f", r.Stats.Score)
	}
	if r.Stats.AdjustedScore != r.Stats.Score {
		t.Errorf("expected AdjustedScore == Score before road quality, got %f vs %f",
			r.Stats.AdjustedScore, r.Stats.Score)
	}

	// AngularDensity * totalDist(km) should reconstruct the ~90-degree heading change.
	totalDist := geo.Haversine(p0, p1) + geo.Haversine(p1, p2)
	totalDistKm := totalDist / 1000
	reconstructed := r.Stats.AngularDensity * totalDistKm
	if math.Abs(reconstructed-90.0) > 1.0 {
		t.Errorf("expected AngularDensity * totalDistKm ≈ 90, got %f (AngularDensity=%f, totalDistKm=%f)",
			reconstructed, r.Stats.AngularDensity, totalDistKm)
	}
}

func TestScoreIndirectness(t *testing.T) {
	// Approximate a semicircle by sampling points along a half-circle.
	// The straight-line distance is the diameter; the road distance is pi*r,
	// so indirectness = 2r / (pi*r) = 2/pi ≈ 0.637.
	const n = 50
	const radius = 0.5 // degrees (rough approximation)
	const centerLat = 40.0
	const centerLon = -74.0

	points := make([]geo.Coord, n+1)
	for i := 0; i <= n; i++ {
		angle := math.Pi * float64(i) / float64(n) // 0 to pi
		points[i] = geo.Coord{
			Lat: centerLat + radius*math.Sin(angle),
			Lon: centerLon + radius*math.Cos(angle),
		}
	}

	r := Route{Points: points}
	ScoreRoute(&r)

	// For a semicircle, indirectness should be well below 1 (around 0.637).
	if r.Stats.Indirectness >= 0.75 {
		t.Errorf("expected Indirectness < 0.75 for semicircle, got %f", r.Stats.Indirectness)
	}
	if r.Stats.Indirectness < 0 || r.Stats.Indirectness > 1 {
		t.Errorf("Indirectness out of [0,1] range: %f", r.Stats.Indirectness)
	}
}

func TestScoreZeroLengthRoute(t *testing.T) {
	// Zero points - should not panic.
	r0 := Route{Points: []geo.Coord{}}
	ScoreRoute(&r0)

	// One point - should not panic.
	r1 := Route{Points: []geo.Coord{{Lat: 40.0, Lon: -74.0}}}
	ScoreRoute(&r1)

	// Two identical points - should not panic (totalDist == 0).
	r2 := Route{Points: []geo.Coord{
		{Lat: 40.0, Lon: -74.0},
		{Lat: 40.0, Lon: -74.0},
	}}
	ScoreRoute(&r2)
	if r2.Stats != (CurvatureStats{}) {
		t.Errorf("expected zero CurvatureStats for zero-distance route, got %+v", r2.Stats)
	}
}

func TestScoreAllAdjustedEqualsScore(t *testing.T) {
	routes := []Route{
		{Points: []geo.Coord{
			{Lat: 40.0, Lon: -74.0},
			{Lat: 40.5, Lon: -74.0},
			{Lat: 41.0, Lon: -74.0},
		}},
		{Points: []geo.Coord{
			{Lat: 40.0, Lon: -74.0},
			{Lat: 40.1, Lon: -74.0},
			{Lat: 40.1, Lon: -73.9},
		}},
	}
	ScoreAll(routes)
	for i, r := range routes {
		if r.Stats.AdjustedScore != r.Stats.Score {
			t.Errorf("route %d: AdjustedScore %f != Score %f", i, r.Stats.AdjustedScore, r.Stats.Score)
		}
		if r.Stats.Score < 0 {
			t.Errorf("route %d: Score is negative: %f", i, r.Stats.Score)
		}
	}
}

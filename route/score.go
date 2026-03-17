package route

import "github.com/yardbirdsax/twisty/geo"

// CurvatureStats holds curvature scoring data for a route.
type CurvatureStats struct {
	Indirectness   float64 // straight-line / road distance (0-1; lower = more indirect)
	AngularDensity float64 // degrees of heading change per km
	Score          float64 // combined curvature score (higher = twistier)
	AdjustedScore  float64 // score after road quality penalties (set in Task 007)
}

// ScoreRoute computes curvature statistics for a decoded route polyline.
// It populates route.Stats.Indirectness, AngularDensity, and Score.
func ScoreRoute(r *Route) {
	points := r.Points
	if len(points) < 2 {
		return
	}

	// Total road distance
	totalDist := 0.0
	for i := 0; i < len(points)-1; i++ {
		totalDist += geo.Haversine(points[i], points[i+1])
	}

	if totalDist == 0 {
		return
	}

	// Indirectness
	straightLine := geo.Haversine(points[0], points[len(points)-1])
	indirectness := straightLine / totalDist
	if indirectness < 0 {
		indirectness = 0
	}
	if indirectness > 1 {
		indirectness = 1
	}

	// Angular density
	totalHeadingChange := 0.0
	for i := 1; i < len(points)-1; i++ {
		b1 := geo.Bearing(points[i-1], points[i])
		b2 := geo.Bearing(points[i], points[i+1])
		totalHeadingChange += geo.AngleDiff(b1, b2)
	}
	angularDensity := totalHeadingChange / (totalDist / 1000)

	// Combined score
	score := angularDensity*0.7 + (1.0-indirectness)*1000*0.3

	r.Stats = CurvatureStats{
		Indirectness:   indirectness,
		AngularDensity: angularDensity,
		Score:          score,
		AdjustedScore:  score,
	}
}

// ScoreAll calls ScoreRoute on each route in the slice.
func ScoreAll(routes []Route) {
	for i := range routes {
		ScoreRoute(&routes[i])
	}
}

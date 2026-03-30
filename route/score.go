package route

import "github.com/yardbirdsax/twisty/geo"

// CurvatureStats holds curvature scoring data for a route.
type CurvatureStats struct {
	Indirectness   float64 // straight-line / road distance (0-1; lower = more indirect)
	AngularDensity float64 // degrees of heading change per km
	Score          float64 // combined curvature score (higher = twistier)
	AdjustedScore  float64 // score after road quality penalties (set in Task 007)
}

// normalize scales value into [0, 1] relative to [minVal, maxVal].
// If minVal == maxVal the range is degenerate and 1.0 is returned.
func normalize(value, minVal, maxVal float64) float64 {
	if maxVal == minVal {
		return 1.0
	}
	return (value - minVal) / (maxVal - minVal)
}

// minMax returns the minimum and maximum values in vals.
// If vals is empty both return values are 0.
func minMax(vals []float64) (float64, float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals[1:] {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	return lo, hi
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
	indirectness = max(0, min(1, indirectness))

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

// SelectRoute returns the index of the best route given the twist factor [0, 1].
// twist=0.0 favors the fastest route; twist=1.0 favors the twistiest.
// If there is only one route, returns 0.
func SelectRoute(routes []Route, twist float64) int {
	if len(routes) == 1 {
		return 0
	}

	// Find min/max duration.
	durs := make([]float64, len(routes))
	for i, r := range routes {
		durs[i] = r.Duration
	}
	minDur, maxDur := minMax(durs)

	// Find min/max adjusted score.
	scores := make([]float64, len(routes))
	for i, r := range routes {
		scores[i] = r.Stats.AdjustedScore
	}
	minScore, maxScore := minMax(scores)

	bestIdx := 0
	bestSel := -1.0
	for i, r := range routes {
		normDur := normalize(r.Duration, minDur, maxDur)
		normScore := normalize(r.Stats.AdjustedScore, minScore, maxScore)

		sel := twist*normScore + (1.0-twist)*(1.0-normDur)
		if sel > bestSel {
			bestSel = sel
			bestIdx = i
		}
	}

	return bestIdx
}

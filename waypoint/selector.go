package waypoint

import (
	"math/rand"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

const MinRoadLengthM = 500.0 // collections shorter than this are excluded

// resolveRNG returns rng.Float64 if rng is non-nil, otherwise rand.Float64.
func resolveRNG(rng *rand.Rand) func() float64 {
	if rng != nil {
		return rng.Float64
	}
	return rand.Float64
}

// WeightedRandomSelector samples road collections without replacement,
// with probability proportional to PenalizedScore.
type WeightedRandomSelector struct {
	Rand *rand.Rand // injectable for deterministic testing; nil uses global rand
}

// Compile-time assertion that WeightedRandomSelector implements WaypointSelector.
var _ WaypointSelector = (*WeightedRandomSelector)(nil)

// Select samples up to count road collections weighted by PenalizedScore,
// filters to those within radiusKm of origin, and returns their midpoints
// sorted by clockwise bearing from origin.
func (s *WeightedRandomSelector) Select(
	collections []quality.RoadCollection,
	count int,
	origin geo.Coord,
	radiusKm float64,
) []geo.Coord {
	radiusM := radiusKm * 1000.0

	// Build eligible list: PenalizedScore > 0, TotalLength >= MinRoadLengthM,
	// and midpoint within radiusKm of origin.
	type eligible struct {
		collection quality.RoadCollection
		weight     float64
	}
	var eligibles []eligible
	for _, c := range collections {
		if c.PenalizedScore <= 0 {
			continue
		}
		if c.TotalLength < MinRoadLengthM {
			continue
		}
		mid := CollectionMidpoint(c)
		if geo.Haversine(origin, mid) > radiusM {
			continue
		}
		eligibles = append(eligibles, eligible{collection: c, weight: c.PenalizedScore})
	}

	// If fewer eligible collections than requested, use all.
	n := min(count, len(eligibles))

	// Build weights slice for sampling.
	weights := make([]float64, len(eligibles))
	for i, e := range eligibles {
		weights[i] = e.weight
	}

	indices := WeightedSampleWithoutReplacement(s.Rand, weights, n)

	coords := make([]geo.Coord, 0, len(indices))
	for _, idx := range indices {
		coords = append(coords, CollectionMidpoint(eligibles[idx].collection))
	}

	SortByBearing(origin, coords)
	return coords
}

// WeightedSampleWithoutReplacement draws n indices from weights without replacement.
// Weights are modified in place (zeroed on draw).
func WeightedSampleWithoutReplacement(rng *rand.Rand, weights []float64, n int) []int {
	result := make([]int, 0, n)

	float64Fn := resolveRNG(rng)

	for len(result) < n {
		// Compute total weight.
		totalWeight := 0.0
		for _, w := range weights {
			totalWeight += w
		}
		if totalWeight <= 0 {
			break
		}

		// Pick a random value in [0, totalWeight).
		r := float64Fn() * totalWeight

		// Binary-search style: accumulate until we reach r.
		cumulative := 0.0
		chosen := -1
		for i, w := range weights {
			if w <= 0 {
				continue
			}
			cumulative += w
			if r < cumulative {
				chosen = i
				break
			}
		}

		// Fallback: pick last non-zero weight index if floating point edge case.
		if chosen == -1 {
			for i := len(weights) - 1; i >= 0; i-- {
				if weights[i] > 0 {
					chosen = i
					break
				}
			}
		}

		if chosen == -1 {
			break
		}

		result = append(result, chosen)
		weights[chosen] = 0
	}

	return result
}

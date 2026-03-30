package waypoint

import (
	"math"
	"math/rand"
	"sort"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// DefaultArcWidth is the default angular width of the sector in degrees.
const DefaultArcWidth = 90.0

// arcWideningStep is the amount to widen the arc when too few candidates exist.
const arcWideningStep = 30.0

// maxArcWidth is the widest the sector can grow before falling back to full circle.
const maxArcWidth = 180.0

// SectorLobeSelector picks a random outbound direction and concentrates waypoints
// in an angular sector to produce a lobe-shaped route that gets to twisty roads
// quickly and works its way back gradually.
type SectorLobeSelector struct {
	Start    geo.Coord  // origin for bearing/distance calculations
	ArcWidth float64    // sector width in degrees (default: 90)
	Rand     *rand.Rand // injectable for deterministic testing; nil uses global rand
}

// Compile-time assertion.
var _ WaypointSelector = (*SectorLobeSelector)(nil)

// sectorCandidate holds an eligible collection with precomputed bearing and distance.
type sectorCandidate struct {
	collection quality.RoadCollection
	midpoint   geo.Coord
	bearing    float64
	distance   float64
}

// inSector reports whether bearing is within ±halfArc degrees of center,
// handling 360°/0° wraparound.
func inSector(bearing, center, halfArc float64) bool {
	return geo.AngleDiff(bearing, center) <= halfArc
}

// Select picks up to count waypoints concentrated in a random angular sector from
// origin, ordered to produce a lobe-shaped route.
func (s *SectorLobeSelector) Select(
	collections []quality.RoadCollection,
	count int,
	origin geo.Coord,
	radiusKm float64,
) []geo.Coord {
	arcWidth := s.ArcWidth
	if arcWidth == 0 {
		arcWidth = DefaultArcWidth
	}

	radiusM := radiusKm * 1000.0

	// Pick a random outbound bearing (0–360°).
	float64Fn := resolveRNG(s.Rand)
	outboundBearing := float64Fn() * 360.0

	// Build the eligible list.
	var eligibles []sectorCandidate
	for _, c := range collections {
		if c.PenalizedScore <= 0 {
			continue
		}
		if c.TotalLength < MinRoadLengthM {
			continue
		}
		mid := CollectionMidpoint(c)
		dist := geo.Haversine(origin, mid)
		if dist > radiusM {
			continue
		}
		bearing := geo.Bearing(origin, mid)
		eligibles = append(eligibles, sectorCandidate{
			collection: c,
			midpoint:   mid,
			bearing:    bearing,
			distance:   dist,
		})
	}

	// Filter to sector; widen if needed.
	currentArc := arcWidth
	var sectorCandidates []sectorCandidate
	for {
		sectorCandidates = sectorCandidates[:0]
		halfArc := currentArc / 2
		for _, e := range eligibles {
			if inSector(e.bearing, outboundBearing, halfArc) {
				sectorCandidates = append(sectorCandidates, e)
			}
		}
		if len(sectorCandidates) >= count {
			break
		}
		if currentArc >= maxArcWidth {
			break
		}
		currentArc += arcWideningStep
		if currentArc > maxArcWidth {
			currentArc = maxArcWidth
		}
	}

	// Full circle fallback if sector still has too few.
	if len(sectorCandidates) < count && len(sectorCandidates) < len(eligibles) {
		sectorCandidates = eligibles
	}

	// Score-weighted sample without replacement.
	n := min(count, len(sectorCandidates))
	weights := make([]float64, len(sectorCandidates))
	for i, c := range sectorCandidates {
		weights[i] = c.collection.PenalizedScore
	}
	indices := WeightedSampleWithoutReplacement(s.Rand, weights, n)

	selected := make([]sectorCandidate, 0, len(indices))
	for _, idx := range indices {
		selected = append(selected, sectorCandidates[idx])
	}

	// Order for lobe shape: partition into left and right of outbound bearing.
	// Left: bearing < outbound (with wraparound), Right: bearing >= outbound.
	var left, right []sectorCandidate
	for _, c := range selected {
		diff := c.bearing - outboundBearing
		diff = math.Mod(math.Mod(diff+180, 360)+360, 360) - 180
		if diff < 0 {
			left = append(left, c)
		} else {
			right = append(right, c)
		}
	}

	// Determine outbound and return sides.
	var outbound, ret []sectorCandidate
	if len(left) >= len(right) {
		outbound = left
		ret = right
	} else {
		outbound = right
		ret = left
	}

	// If all waypoints on one side, sort nearest → farthest.
	if len(outbound) == 0 || len(ret) == 0 {
		all := append(outbound, ret...)
		sort.Slice(all, func(i, j int) bool {
			return all[i].distance < all[j].distance
		})
		return extractCoords(all)
	}

	// Outbound: sort nearest first (ascending distance).
	sort.Slice(outbound, func(i, j int) bool {
		return outbound[i].distance < outbound[j].distance
	})
	// Return: sort farthest first (descending distance).
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].distance > ret[j].distance
	})

	all := append(outbound, ret...)
	return extractCoords(all)
}

func extractCoords(candidates []sectorCandidate) []geo.Coord {
	coords := make([]geo.Coord, len(candidates))
	for i, c := range candidates {
		coords[i] = c.midpoint
	}
	return coords
}

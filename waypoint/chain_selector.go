package waypoint

import (
	"math"
	"math/rand"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// waypointInterval controls spacing between extracted waypoints in meters.
const waypointInterval = 2000.0

// deduplicateProximityM is the distance threshold for deduplicating consecutive waypoints.
const deduplicateProximityM = 100.0

// ChainSelector builds a continuous chain of scored road collections
// and extracts dense waypoints along their geometry to produce a
// loop-shaped route that follows twisty roads rather than letting
// Valhalla fill gaps with boring connectors.
type ChainSelector struct {
	Start         geo.Coord  // origin point
	ArcWidth      float64    // sector width in degrees (default: 120)
	AvgSpeedMPH   float64    // for time estimation (default: DefaultAvgSpeedMPH)
	TimeBudgetSec float64    // total ride time budget in seconds
	Rand          *rand.Rand // injectable for deterministic testing
}

// Compile-time assertion.
var _ WaypointSelector = (*ChainSelector)(nil)

// chainCandidate holds an eligible collection with precomputed fields.
type chainCandidate struct {
	index             int
	collection        quality.RoadCollection
	midpoint          geo.Coord
	bearing           float64
	distanceFromStart float64
	estimatedTimeSec  float64
}

// Select builds a chain of road collections and extracts dense waypoints.
// The count parameter is ignored; waypoint count is determined from chain length.
func (s *ChainSelector) Select(
	collections []quality.RoadCollection,
	count int,
	origin geo.Coord,
	radiusKm float64,
) []geo.Coord {
	avgSpeedMPH := s.AvgSpeedMPH
	if avgSpeedMPH == 0 {
		avgSpeedMPH = DefaultAvgSpeedMPH
	}
	avgSpeedMS := avgSpeedMPH * 1609.34 / 3600.0

	arcWidth := s.ArcWidth
	if arcWidth == 0 {
		arcWidth = 120.0
	}

	radiusM := radiusKm * 1000.0

	// Step A: Build eligible list.
	var eligibles []chainCandidate
	for i, c := range collections {
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
		estimatedTimeSec := c.TotalLength / avgSpeedMS
		eligibles = append(eligibles, chainCandidate{
			index:             i,
			collection:        c,
			midpoint:          mid,
			bearing:           bearing,
			distanceFromStart: dist,
			estimatedTimeSec:  estimatedTimeSec,
		})
	}

	if len(eligibles) == 0 {
		return nil
	}

	// Step B: Pick a random outbound direction and filter to sector.
	float64Fn := rand.Float64
	if s.Rand != nil {
		float64Fn = s.Rand.Float64
	}
	outboundBearing := float64Fn() * 360.0

	// Filter to sector; widen if needed.
	currentArc := arcWidth
	var sectorEligibles []chainCandidate
	for {
		sectorEligibles = sectorEligibles[:0]
		halfArc := currentArc / 2
		for _, e := range eligibles {
			if inSector(e.bearing, outboundBearing, halfArc) {
				sectorEligibles = append(sectorEligibles, e)
			}
		}
		if len(sectorEligibles) >= 3 {
			break
		}
		if currentArc >= 360.0 {
			break
		}
		currentArc += 30.0
		if currentArc > 360.0 {
			currentArc = 360.0
		}
	}

	// Full circle fallback if still too few.
	if len(sectorEligibles) < 3 {
		sectorEligibles = eligibles
	}

	if len(sectorEligibles) == 0 {
		return nil
	}

	// Step C: Build the chain (greedy).
	timeBudgetSec := s.TimeBudgetSec

	chain := make([]quality.RoadCollection, 0)
	visited := make(map[int]bool)
	cumulativeTimeSec := 0.0
	currentPos := origin

	// Initial pick: nearest eligible collection aligned with outbound bearing.
	bestInitIdx := -1
	bestInitScore := -math.MaxFloat64
	for i, e := range sectorEligibles {
		d := geo.Haversine(origin, e.midpoint)
		if d == 0 {
			d = 1
		}
		bearingAlign := 1.0 + math.Cos(geo.AngleDiff(outboundBearing, e.bearing)*math.Pi/180)
		score := (1.0 / d) * bearingAlign
		if score > bestInitScore {
			bestInitScore = score
			bestInitIdx = i
		}
	}

	if bestInitIdx < 0 {
		return nil
	}

	first := sectorEligibles[bestInitIdx]
	oriented, exitPos := orientCollection(first.collection, origin)
	chain = append(chain, oriented)
	visited[bestInitIdx] = true
	currentPos = exitPos
	cumulativeTimeSec += first.estimatedTimeSec

	// Initialize bearing sweep and spatial grid.
	sweepDir := 1 // CW
	if float64Fn() < 0.5 {
		sweepDir = -1 // CCW
	}
	expectedBearing := geo.Bearing(origin, exitPos)

	grid := newSpatialGrid(origin, 500.0)
	grid.MarkSegments(oriented)
	grid.MarkPath(origin, first.midpoint)

	// Chain loop.
	for cumulativeTimeSec < timeBudgetSec*0.85 {
		bestScore := -math.MaxFloat64
		bestCandIdx := -1

		for i, e := range sectorEligibles {
			if visited[i] {
				continue
			}

			// Fix (a): During outbound leg, enforce monotonic distance from origin
			// AND reject candidates on the opposite side of origin from currentPos.
			if cumulativeTimeSec <= timeBudgetSec*0.4 {
				currentDistFromOrigin := geo.Haversine(currentPos, origin)
				if e.distanceFromStart < currentDistFromOrigin*0.8 {
					continue
				}
				// Skip candidates that are roughly opposite currentPos relative
				// to origin — reaching them would require crossing through origin.
				if currentDistFromOrigin > 500 {
					bearingOriginToCurrent := geo.Bearing(origin, currentPos)
					bearingOriginToCandidate := geo.Bearing(origin, e.midpoint)
					if geo.AngleDiff(bearingOriginToCurrent, bearingOriginToCandidate) > 120 {
						continue
					}
				}
			}

			// Fix (c): Use nearest endpoint distance instead of midpoint.
			connectorDistM := geo.Haversine(currentPos, e.midpoint)
			if start, end, ok := collectionEndpoints(e.collection); ok {
				dStart := geo.Haversine(currentPos, start)
				dEnd := geo.Haversine(currentPos, end)
				if dStart < connectorDistM {
					connectorDistM = dStart
				}
				if dEnd < connectorDistM {
					connectorDistM = dEnd
				}
			}
			connectorTimeSec := connectorDistM / avgSpeedMS
			totalCandidateTimeSec := connectorTimeSec + e.estimatedTimeSec

			// Estimate return time from the far endpoint after hypothetical orientation.
			candEnd := e.midpoint
			if candStart, cEnd, ok := collectionEndpoints(e.collection); ok {
				candEnd = cEnd
				// After orientation, exit will be the endpoint farther from currentPos.
				if geo.Haversine(currentPos, cEnd) < geo.Haversine(currentPos, candStart) {
					candEnd = candStart
				}
			}
			returnTimeSec := geo.Haversine(candEnd, origin) / avgSpeedMS

			timeAfter := cumulativeTimeSec + totalCandidateTimeSec + returnTimeSec
			if timeAfter > timeBudgetSec {
				continue
			}

			chainScore := e.collection.PenalizedScore / (1 + math.Log1p(connectorTimeSec))

			// Fix (b): Trajectory-relative bearing from currentPos, not origin.
			candidateBearing := geo.Bearing(currentPos, e.midpoint)
			delta := signedAngleDiff(expectedBearing, candidateBearing, sweepDir)
			if delta < 0 {
				chainScore *= 0.3
			} else {
				chainScore *= 1.0 + 0.5*math.Exp(-delta/30.0)
			}

			// Spatial overlap penalty.
			overlapFrac := grid.OverlapFraction(currentPos, e.midpoint)
			chainScore *= 1.0 - 0.7*overlapFrac

			// Homeward bias past halfway point.
			if cumulativeTimeSec > timeBudgetSec*0.4 {
				currentDistFromStart := geo.Haversine(currentPos, origin)
				if currentDistFromStart > 0 {
					homewardFactor := 1.0 + (currentDistFromStart-e.distanceFromStart)/currentDistFromStart
					homewardFactor = math.Max(homewardFactor, 0.5)
					chainScore *= homewardFactor
				}
			}

			if chainScore > bestScore {
				bestScore = chainScore
				bestCandIdx = i
			}
		}

		if bestCandIdx < 0 {
			break
		}

		cand := sectorEligibles[bestCandIdx]
		// Fix (c): Use nearest endpoint distance for actual time accounting.
		connectorDistM := geo.Haversine(currentPos, cand.midpoint)
		if start, end, ok := collectionEndpoints(cand.collection); ok {
			dStart := geo.Haversine(currentPos, start)
			dEnd := geo.Haversine(currentPos, end)
			if dStart < connectorDistM {
				connectorDistM = dStart
			}
			if dEnd < connectorDistM {
				connectorDistM = dEnd
			}
		}
		connectorTimeSec := connectorDistM / avgSpeedMS

		oriented, exitPos := orientCollection(cand.collection, currentPos)
		chain = append(chain, oriented)
		visited[bestCandIdx] = true

		// Update sweep state and spatial grid before moving currentPos.
		expectedBearing = geo.Bearing(currentPos, exitPos)
		grid.MarkPath(currentPos, cand.midpoint)
		grid.MarkSegments(oriented)

		currentPos = exitPos
		cumulativeTimeSec += connectorTimeSec + cand.estimatedTimeSec
	}

	// Step D: Extract dense waypoints from the chain.
	return extractDenseWaypoints(chain, waypointInterval)
}

// collectionEndpoints returns the start and end coordinates of a RoadCollection
// from its first and last segments.
func collectionEndpoints(c quality.RoadCollection) (start, end geo.Coord, ok bool) {
	if len(c.Segments) == 0 {
		return geo.Coord{}, geo.Coord{}, false
	}
	return c.Segments[0].Start, c.Segments[len(c.Segments)-1].End, true
}

// reverseCollection returns a new RoadCollection with segments in reverse order
// and each segment's Start/End swapped. All metadata fields are preserved.
func reverseCollection(c quality.RoadCollection) quality.RoadCollection {
	n := len(c.Segments)
	reversed := make([]quality.ScoredSegment, n)
	for i, seg := range c.Segments {
		reversed[n-1-i] = quality.ScoredSegment{
			WayID:  seg.WayID,
			Start:  seg.End,
			End:    seg.Start,
			Radius: seg.Radius,
			Tier:   seg.Tier,
			Weight: seg.Weight,
			Length: seg.Length,
			Score:  seg.Score,
		}
	}
	return quality.RoadCollection{
		Name:                 c.Name,
		SubIndex:             c.SubIndex,
		HighwayTypes:         c.HighwayTypes,
		WayIDs:               c.WayIDs,
		Segments:             reversed,
		TotalScore:           c.TotalScore,
		TotalLength:          c.TotalLength,
		ScorePerKm:           c.ScorePerKm,
		HighwayPenaltyFactor: c.HighwayPenaltyFactor,
		PenalizedScore:       c.PenalizedScore,
		PenalizedPerKm:       c.PenalizedPerKm,
	}
}

// orientCollection returns the collection oriented so that its start is closer
// to approachPos, along with the exit coordinate (the far end after traversal).
func orientCollection(c quality.RoadCollection, approachPos geo.Coord) (quality.RoadCollection, geo.Coord) {
	start, end, ok := collectionEndpoints(c)
	if !ok {
		return c, approachPos
	}
	if geo.Haversine(approachPos, end) < geo.Haversine(approachPos, start) {
		return reverseCollection(c), start
	}
	return c, end
}

// coordKey is a spatial grid key for global waypoint deduplication (~11m resolution).
type coordKey struct{ lat, lon int }

func toCoordKey(c geo.Coord) coordKey {
	return coordKey{int(c.Lat * 10000), int(c.Lon * 10000)}
}

// extractDenseWaypoints walks a chain of collections and emits waypoints
// at approximately intervalM-meter spacing along the actual road geometry.
// Each collection's entry and exit points are always emitted (subject to
// deduplication), and the accumulated distance counter resets at each
// collection boundary so that Valhalla receives explicit guidance at every
// collection transition.
// Fix (d): Global deduplication — waypoints already seen anywhere in the chain
// are suppressed, preventing U-turns when the chain revisits an area.
func extractDenseWaypoints(chain []quality.RoadCollection, intervalM float64) []geo.Coord {
	if len(chain) == 0 {
		return nil
	}

	var waypoints []geo.Coord
	seen := make(map[coordKey]bool)

	appendWP := func(wp geo.Coord) {
		// Consecutive proximity check.
		if len(waypoints) > 0 && geo.Haversine(waypoints[len(waypoints)-1], wp) <= deduplicateProximityM {
			return
		}
		// Global dedup check.
		k := toCoordKey(wp)
		if seen[k] {
			return
		}
		seen[k] = true
		waypoints = append(waypoints, wp)
	}

	for _, c := range chain {
		if len(c.Segments) == 0 {
			continue
		}

		// Emit entry point of this collection.
		appendWP(c.Segments[0].Start)

		// Walk segments at interval spacing, reset per collection.
		accumulated := 0.0
		for _, seg := range c.Segments {
			accumulated += seg.Length
			if accumulated >= intervalM {
				appendWP(seg.End)
				accumulated = 0.0
			}
		}

		// Emit exit point of this collection.
		appendWP(c.Segments[len(c.Segments)-1].End)
	}

	return waypoints
}

// spatialGrid tracks visited geographic cells to penalize route repetition.
type spatialGrid struct {
	cellSizeM float64
	originLat float64
	originLon float64
	cosLat    float64
	visited   map[[2]int]bool
}

func newSpatialGrid(origin geo.Coord, cellSizeM float64) *spatialGrid {
	return &spatialGrid{
		cellSizeM: cellSizeM,
		originLat: origin.Lat,
		originLon: origin.Lon,
		cosLat:    math.Cos(origin.Lat * math.Pi / 180),
		visited:   make(map[[2]int]bool),
	}
}

func (g *spatialGrid) cellKey(c geo.Coord) [2]int {
	const metersPerDegLat = 111000.0
	dy := (c.Lat - g.originLat) * metersPerDegLat
	dx := (c.Lon - g.originLon) * metersPerDegLat * g.cosLat
	return [2]int{int(math.Floor(dy / g.cellSizeM)), int(math.Floor(dx / g.cellSizeM))}
}

func (g *spatialGrid) Mark(c geo.Coord) {
	g.visited[g.cellKey(c)] = true
}

// MarkSegments marks all segment start/end points of a collection.
func (g *spatialGrid) MarkSegments(c quality.RoadCollection) {
	for _, seg := range c.Segments {
		g.Mark(seg.Start)
		g.Mark(seg.End)
	}
}

// MarkPath samples points along a great-circle path and marks each cell.
func (g *spatialGrid) MarkPath(from, to geo.Coord) {
	dist := geo.Haversine(from, to)
	steps := int(dist / (g.cellSizeM / 2))
	if steps < 1 {
		steps = 1
	}
	bearing := geo.Bearing(from, to)
	for i := 0; i <= steps; i++ {
		d := dist * float64(i) / float64(steps)
		pt := geo.DestinationPoint(from, bearing, d)
		g.Mark(pt)
	}
}

// OverlapFraction returns the fraction of sampled corridor cells already visited.
func (g *spatialGrid) OverlapFraction(from, to geo.Coord) float64 {
	dist := geo.Haversine(from, to)
	steps := int(dist / (g.cellSizeM / 2))
	if steps < 1 {
		steps = 1
	}
	bearing := geo.Bearing(from, to)
	total := 0
	overlap := 0
	for i := 0; i <= steps; i++ {
		d := dist * float64(i) / float64(steps)
		pt := geo.DestinationPoint(from, bearing, d)
		total++
		if g.visited[g.cellKey(pt)] {
			overlap++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(overlap) / float64(total)
}

// signedAngleDiff returns the angular difference from `from` to `to` bearing,
// positive in the given sweep direction (1 for CW, -1 for CCW).
func signedAngleDiff(from, to float64, sweepDir int) float64 {
	diff := to - from
	for diff > 180 {
		diff -= 360
	}
	for diff < -180 {
		diff += 360
	}
	return diff * float64(sweepDir)
}


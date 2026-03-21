package quality

import (
	"math"

	"github.com/yardbirdsax/twisty/geo"
)

// ScoredSegment holds the curvature score for a single segment between two nodes.
type ScoredSegment struct {
	WayID  int64     `json:"way_id"` // OSM way ID this segment belongs to
	Start  geo.Coord `json:"start"`
	End    geo.Coord `json:"end"`
	Radius float64   `json:"radius"` // circumradius in meters; +Inf for straight segments
	Tier   int       `json:"tier"`
	Weight float64   `json:"weight"`
	Length float64   `json:"length"` // Haversine distance in meters
	Score  float64   `json:"score"`  // Length * Weight
}

// ScoredWay holds scored segments for a single OSM way.
type ScoredWay struct {
	WayID    int64             `json:"way_id"`
	Tags     map[string]string `json:"tags"`
	Segments []ScoredSegment   `json:"segments"`
}

// ScoreWay computes curvature scores for all segments in a way.
// Ways with fewer than 3 nodes receive zero-score segments.
func ScoreWay(w Way) ScoredWay {
	n := len(w.Geometry)

	if n < 2 {
		return ScoredWay{WayID: w.ID, Tags: w.Tags}
	}

	if n == 2 {
		length := geo.Haversine(w.Geometry[0], w.Geometry[1])
		return ScoredWay{
			WayID: w.ID,
			Tags:  w.Tags,
			Segments: []ScoredSegment{
				{
					WayID:  w.ID,
					Start:  w.Geometry[0],
					End:    w.Geometry[1],
					Radius: math.Inf(1),
					Tier:   0,
					Weight: 0,
					Length: length,
					Score:  0,
				},
			},
		}
	}

	// Compute circumradii for all consecutive triples.
	// radii[i] = circumradius of triangle (i, i+1, i+2)
	radii := make([]float64, n-2)
	for i := 0; i < n-2; i++ {
		radii[i] = geo.Circumradius(w.Geometry[i], w.Geometry[i+1], w.Geometry[i+2])
	}

	// Assign min radius to each segment.
	// Segment i connects nodes i and i+1.
	// Segment 0 only participates in triangle 0 (nodes 0,1,2).
	// Segment i (1 <= i <= n-3) participates in triangles i-1 and i.
	// Segment n-2 only participates in triangle n-3 (nodes n-3,n-2,n-1).
	segments := make([]ScoredSegment, n-1)
	for i := 0; i < n-1; i++ {
		var radius float64
		switch i {
		case 0:
			radius = radii[0]
		case n - 2:
			radius = radii[n-3]
		default:
			radius = math.Min(radii[i-1], radii[i])
		}

		tier, weight := AssignTier(radius)
		length := geo.Haversine(w.Geometry[i], w.Geometry[i+1])
		segments[i] = ScoredSegment{
			WayID:  w.ID,
			Start:  w.Geometry[i],
			End:    w.Geometry[i+1],
			Radius: radius,
			Tier:   tier,
			Weight: weight,
			Length: length,
			Score:  length * weight,
		}
	}

	return ScoredWay{
		WayID:    w.ID,
		Tags:     w.Tags,
		Segments: segments,
	}
}

type ScoredWays []ScoredWay

// ScoreWays scores all ways and returns scored results.
func ScoreWays(ways []Way) ScoredWays {
	result := make(ScoredWays, len(ways))
	for i, w := range ways {
		result[i] = ScoreWay(w)
	}
	return result
}

func (sw ScoredWays) ToWays(ways Ways) []Way {
	waysSlice := make([]Way, len(sw))
	for i, w := range sw {
		waysSlice[i] = ways[w.WayID]
	}
	return waysSlice
}

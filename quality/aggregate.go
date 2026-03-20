package quality

import "fmt"

// RoadCollection represents a named group of contiguous road segments
// that have been aggregated from individual scored ways.
type RoadCollection struct {
	Name         string
	SubIndex     int      // 0-based index when a named road is split into multiple collections
	HighwayTypes []string // unique highway types present in this collection
	WayIDs       []int64
	Segments     []ScoredSegment

	TotalScore  float64
	TotalLength float64 // meters
	ScorePerKm  float64

	// Populated by penalty stage (stage 6)
	PenaltyFactor  float64
	PenalizedScore float64
	PenalizedPerKm float64
}

// DisplayName returns the name of the collection, with a 1-based suffix in
// parentheses when SubIndex > 0.
//
//	SubIndex 0: "Route 100"
//	SubIndex 1: "Route 100 (2)"
//	SubIndex 2: "Route 100 (3)"
func (r RoadCollection) DisplayName() string {
	if r.SubIndex == 0 {
		return r.Name
	}
	return fmt.Sprintf("%s (%d)", r.Name, r.SubIndex+1)
}

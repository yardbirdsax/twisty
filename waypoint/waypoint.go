package waypoint

import (
	"math"
	"sort"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// DefaultAvgSpeedMPH is the default average speed used when deriving a search radius.
const DefaultAvgSpeedMPH = 35.0

// WaypointSelector selects waypoints from a set of scored road collections
// to use as intermediate stops in a Valhalla loop route.
type WaypointSelector interface {
	Select(collections []quality.RoadCollection, count int, origin geo.Coord, radiusKm float64) []geo.Coord
}

// DeriveRadius computes the geographic search radius in km from a time budget
// (hours) and average speed (mph).
//
//	total_km  = avgSpeedMPH * 1.60934 * timeHours
//	radius_km = total_km / (2π)
func DeriveRadius(timeHours, avgSpeedMPH float64) float64 {
	totalKm := avgSpeedMPH * 1.60934 * timeHours
	return totalKm / (2 * math.Pi)
}

// CollectionMidpoint returns the geographic midpoint of a RoadCollection
// by picking the middle element of its Segments slice. Returns the midpoint
// between Start and End of that segment.
func CollectionMidpoint(c quality.RoadCollection) geo.Coord {
	if len(c.Segments) == 0 {
		return geo.Coord{}
	}
	mid := c.Segments[len(c.Segments)/2]
	return geo.Coord{
		Lat: (mid.Start.Lat + mid.End.Lat) / 2,
		Lon: (mid.Start.Lon + mid.End.Lon) / 2,
	}
}

// SortByBearing sorts coords in-place by their clockwise bearing from origin.
func SortByBearing(origin geo.Coord, coords []geo.Coord) {
	sort.Slice(coords, func(i, j int) bool {
		bi := geo.Bearing(origin, coords[i])
		bj := geo.Bearing(origin, coords[j])
		return bi < bj
	})
}

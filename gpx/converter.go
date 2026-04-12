package gpx

import (
	"fmt"

	"github.com/yardbirdsax/twisty/geo"
)

// ConvertRouteToGPX converts RouteData into a GPX 1.1 file at outPath.
// Input: a RouteData containing named waypoints and decoded track points.
// Output: a GPX file with labeled <wpt> elements for each stop and a <trkseg> for the route polyline.
// Returns an error if the data contains no points or any coordinate is out of range.
func ConvertRouteToGPX(data *RouteData, outPath string) error {
	if len(data.RouteWaypoints) == 0 && len(data.TrackPoints) == 0 {
		return fmt.Errorf("route must contain either waypoints or track points")
	}

	// Validate all waypoint coordinates
	for i, wp := range data.RouteWaypoints {
		if !isValidLatitude(wp.Latitude) || !isValidLongitude(wp.Longitude) {
			return fmt.Errorf("waypoint %d (%q) has invalid coordinates: lat=%f, lon=%f", i, wp.Name, wp.Latitude, wp.Longitude)
		}
	}

	// Validate all track point coordinates
	for i, tp := range data.TrackPoints {
		if !isValidLatitude(tp.Latitude) || !isValidLongitude(tp.Longitude) {
			return fmt.Errorf("track point %d has invalid coordinates: lat=%f, lon=%f", i, tp.Latitude, tp.Longitude)
		}
	}

	// Convert RouteWaypoints to the existing Waypoint type
	wpts := make([]Waypoint, len(data.RouteWaypoints))
	for i, rw := range data.RouteWaypoints {
		wpts[i] = Waypoint{
			Lat:  rw.Latitude,
			Lon:  rw.Longitude,
			Name: rw.Name,
		}
	}

	// Convert TrackCoords to geo.Coord (used by existing WriteGPXWithWaypoints)
	coords := make([]geo.Coord, len(data.TrackPoints))
	for i, tp := range data.TrackPoints {
		coords[i] = geo.Coord{Lat: tp.Latitude, Lon: tp.Longitude}
	}

	trackName := fmt.Sprintf("%s to %s", data.StartName, data.DestinationName)
	return WriteGPXWithWaypoints(outPath, coords, wpts, trackName)
}

func isValidLatitude(lat float64) bool {
	return lat >= -90 && lat <= 90
}

func isValidLongitude(lon float64) bool {
	return lon >= -180 && lon <= 180
}

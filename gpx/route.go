package gpx

// RouteData represents the extracted route from Google Maps.
type RouteData struct {
	StartName       string
	DestinationName string
	RouteWaypoints  []RouteWaypoint
	TrackPoints     []TrackCoord
}

// RouteWaypoint represents a significant point on the route (start, stop, destination).
// Distinct from the existing Waypoint type which is the GPX XML element.
type RouteWaypoint struct {
	Name      string
	Latitude  float64
	Longitude float64
}

// TrackCoord represents a geographic coordinate along the route track.
// Distinct from the existing TrackPoint type which is the GPX XML element.
type TrackCoord struct {
	Latitude  float64
	Longitude float64
}

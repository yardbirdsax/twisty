package gpx

// testRouteData returns sample route data for use in tests.
func testRouteData() *RouteData {
	return &RouteData{
		StartName:       "Home",
		DestinationName: "Work",
		RouteWaypoints: []RouteWaypoint{
			{Name: "Home", Latitude: 40.7128, Longitude: -74.0060},
			{Name: "Gas Station", Latitude: 40.7300, Longitude: -74.0000},
			{Name: "Work", Latitude: 40.7580, Longitude: -73.9855},
		},
		TrackPoints: []TrackCoord{
			{Latitude: 40.7128, Longitude: -74.0060},
			{Latitude: 40.7135, Longitude: -74.0059},
			{Latitude: 40.7300, Longitude: -74.0000},
			{Latitude: 40.7500, Longitude: -73.9900},
			{Latitude: 40.7580, Longitude: -73.9855},
		},
	}
}

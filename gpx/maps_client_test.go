package gpx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc is an http.RoundTripper implemented as a function.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newTestMapsClient returns a MapsClient whose HTTP calls are intercepted by fn.
func newTestMapsClient(fn roundTripFunc) *MapsClient {
	return &MapsClient{
		accessToken: "test-token",
		httpClient:  &http.Client{Transport: fn},
	}
}

// jsonResponse creates a 200 OK http.Response whose body is the JSON encoding of v.
func jsonResponse(v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(b))),
	}
}

// errResponse returns an HTTP response with the given status code and a plain body.
func errResponse(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}



func TestValidateGoogleMapsURL(t *testing.T) {
	tests := []struct {
		url       string
		shouldErr bool
	}{
		{"https://maps.google.com/maps/dir/Home/Work", false},
		{"https://maps.app.goo.gl/abc123", false},
		{"https://google.com/maps/dir/A/B", false},
		{"https://example.com/maps", true},
		{"not a url", true},
	}

	for _, tt := range tests {
		err := ValidateGoogleMapsURL(tt.url)
		if (err != nil) != tt.shouldErr {
			t.Errorf("ValidateGoogleMapsURL(%q): expected error=%v, got %v", tt.url, tt.shouldErr, err)
		}
	}
}

func TestDecodePolyline(t *testing.T) {
	// Example polyline from Google's documentation
	encoded := "_p~iF~ps|U_ulLnnqC_mqNvxq`@"

	points, err := decodePolyline(encoded)
	if err != nil {
		t.Fatalf("decodePolyline failed: %v", err)
	}

	if len(points) == 0 {
		t.Error("expected points, got none")
	}

	// First point should be approximately (38.5, -120.2) per Google's docs
	if len(points) > 0 {
		if points[0].Latitude < 38 || points[0].Latitude > 39 {
			t.Errorf("first point latitude out of range: %f", points[0].Latitude)
		}
	}
}

func TestParseSharedLinkPathWaypoints(t *testing.T) {
	client := NewMapsClient("test-token")

	waypoints, err := client.parseSharedLink("https://maps.google.com/maps/dir/New%20York/Boston")
	if err != nil {
		t.Fatalf("parseSharedLink failed: %v", err)
	}

	if len(waypoints) != 2 {
		t.Errorf("expected 2 waypoints, got %d", len(waypoints))
	}
}

func TestParseSharedLinkThreeStops(t *testing.T) {
	client := NewMapsClient("test-token")

	waypoints, err := client.parseSharedLink("https://maps.google.com/maps/dir/Home/Office/Gym")
	if err != nil {
		t.Fatalf("parseSharedLink failed: %v", err)
	}

	if len(waypoints) != 3 {
		t.Errorf("expected 3 waypoints, got %d", len(waypoints))
	}
}

func TestBuildRouteData_TwoLegs(t *testing.T) {
	// Construct a fake two-leg route: Home -> Midpoint -> Work
	route := directionsRoute{
		Legs: []directionsLeg{
			{
				StartLocation: latlng{Lat: 40.7128, Lng: -74.0060},
				EndLocation:   latlng{Lat: 40.7300, Lng: -74.0000},
				Polyline:      polylineEncoded{Points: ""},
			},
			{
				StartLocation: latlng{Lat: 40.7300, Lng: -74.0000},
				EndLocation:   latlng{Lat: 40.7580, Lng: -73.9855},
				Polyline:      polylineEncoded{Points: ""},
			},
		},
		OverviewPolyline: polylineEncoded{Points: ""},
	}
	waypoints := []string{"Home", "Midpoint", "Work"}

	data, err := buildRouteData(route, waypoints)
	if err != nil {
		t.Fatalf("buildRouteData: %v", err)
	}

	if data.StartName != "Home" {
		t.Errorf("StartName = %q, want %q", data.StartName, "Home")
	}
	if data.DestinationName != "Work" {
		t.Errorf("DestinationName = %q, want %q", data.DestinationName, "Work")
	}
	// Expect 3 waypoints: Home, Midpoint, Work
	if len(data.RouteWaypoints) != 3 {
		t.Fatalf("RouteWaypoints len = %d, want 3", len(data.RouteWaypoints))
	}
	if data.RouteWaypoints[0].Name != "Home" {
		t.Errorf("waypoint[0].Name = %q, want %q", data.RouteWaypoints[0].Name, "Home")
	}
	if data.RouteWaypoints[1].Name != "Midpoint" {
		t.Errorf("waypoint[1].Name = %q, want %q", data.RouteWaypoints[1].Name, "Midpoint")
	}
	if data.RouteWaypoints[2].Name != "Work" {
		t.Errorf("waypoint[2].Name = %q, want %q", data.RouteWaypoints[2].Name, "Work")
	}
	// Verify start coordinates
	if data.RouteWaypoints[0].Latitude != 40.7128 {
		t.Errorf("waypoint[0].Latitude = %f, want 40.7128", data.RouteWaypoints[0].Latitude)
	}
	// Verify destination coordinates
	if data.RouteWaypoints[2].Latitude != 40.7580 {
		t.Errorf("waypoint[2].Latitude = %f, want 40.7580", data.RouteWaypoints[2].Latitude)
	}
}

func TestBuildRouteData_NoLegs(t *testing.T) {
	route := directionsRoute{Legs: nil}
	_, err := buildRouteData(route, []string{"A", "B"})
	if err == nil {
		t.Error("buildRouteData with no legs should return an error")
	}
}

func TestBuildRouteData_WithOverviewPolyline(t *testing.T) {
	// "_p~iF~ps|U_ulLnnqC_mqNvxq`@" decodes to 3 points
	route := directionsRoute{
		Legs: []directionsLeg{
			{
				StartLocation: latlng{Lat: 38.5, Lng: -120.2},
				EndLocation:   latlng{Lat: 40.7, Lng: -120.95},
			},
		},
		OverviewPolyline: polylineEncoded{Points: "_p~iF~ps|U_ulLnnqC_mqNvxq`@"},
	}
	waypoints := []string{"Start", "End"}

	data, err := buildRouteData(route, waypoints)
	if err != nil {
		t.Fatalf("buildRouteData: %v", err)
	}
	if len(data.TrackPoints) == 0 {
		t.Error("expected track points from overview polyline, got none")
	}
}

func TestCallDirectionsAPI_OK(t *testing.T) {
	fakeRoute := directionsRoute{
		Legs: []directionsLeg{
			{
				StartLocation: latlng{Lat: 40.0, Lng: -74.0},
				EndLocation:   latlng{Lat: 41.0, Lng: -75.0},
			},
		},
	}
	fakeResp := directionsAPIResponse{Status: "OK", Routes: []directionsRoute{fakeRoute}}

	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(fakeResp), nil
	})

	routes, err := client.callDirectionsAPI(context.Background(), []string{"A", "B"})
	if err != nil {
		t.Fatalf("callDirectionsAPI: %v", err)
	}
	if len(routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(routes))
	}
}

func TestCallDirectionsAPI_NonOKStatus(t *testing.T) {
	fakeResp := directionsAPIResponse{Status: "ZERO_RESULTS", ErrorMessage: "no route found"}

	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(fakeResp), nil
	})

	_, err := client.callDirectionsAPI(context.Background(), []string{"A", "B"})
	if err == nil {
		t.Error("expected error for non-OK API status")
	}
}

func TestCallDirectionsAPI_HTTPError(t *testing.T) {
	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		return errResponse(http.StatusInternalServerError, "server error"), nil
	})

	_, err := client.callDirectionsAPI(context.Background(), []string{"A", "B"})
	if err == nil {
		t.Error("expected error for HTTP 500 response")
	}
}

func TestGetRoute_InvalidURL(t *testing.T) {
	client := NewMapsClient("test-token")
	_, err := client.GetRoute(context.Background(), "not-a-maps-url")
	if err == nil {
		t.Error("GetRoute should return error for invalid URL")
	}
}

func TestGetRoute_NoRoutes(t *testing.T) {
	fakeResp := directionsAPIResponse{Status: "OK", Routes: []directionsRoute{}}

	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(fakeResp), nil
	})

	_, err := client.GetRoute(context.Background(), "https://maps.google.com/maps/dir/Home/Work")
	if err == nil {
		t.Error("GetRoute should return error when API returns no routes")
	}
}

func TestGetRoute_Success(t *testing.T) {
	fakeRoute := directionsRoute{
		Legs: []directionsLeg{
			{
				StartLocation: latlng{Lat: 40.7128, Lng: -74.0060},
				EndLocation:   latlng{Lat: 40.7580, Lng: -73.9855},
			},
		},
		OverviewPolyline: polylineEncoded{Points: "_p~iF~ps|U_ulLnnqC_mqNvxq`@"},
	}
	fakeResp := directionsAPIResponse{Status: "OK", Routes: []directionsRoute{fakeRoute}}

	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(fakeResp), nil
	})

	data, err := client.GetRoute(context.Background(), "https://maps.google.com/maps/dir/Home/Work")
	if err != nil {
		t.Fatalf("GetRoute: %v", err)
	}
	if data.StartName != "Home" {
		t.Errorf("StartName = %q, want %q", data.StartName, "Home")
	}
	if data.DestinationName != "Work" {
		t.Errorf("DestinationName = %q, want %q", data.DestinationName, "Work")
	}
}

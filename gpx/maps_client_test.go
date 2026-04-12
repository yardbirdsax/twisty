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
		apiKey:     "test-token",
		httpClient: &http.Client{Transport: fn},
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

func TestWaypointFromString_Coordinate(t *testing.T) {
	wp := waypointFromString("40.238122,-75.526346")
	if wp.Location == nil {
		t.Fatal("expected Location to be set for coordinate string")
	}
	if wp.Address != "" {
		t.Errorf("expected Address to be empty, got %q", wp.Address)
	}
	if wp.Location.LatLng.Latitude != 40.238122 {
		t.Errorf("Latitude = %f, want 40.238122", wp.Location.LatLng.Latitude)
	}
	if wp.Location.LatLng.Longitude != -75.526346 {
		t.Errorf("Longitude = %f, want -75.526346", wp.Location.LatLng.Longitude)
	}
}

func TestWaypointFromString_Address(t *testing.T) {
	wp := waypointFromString("Speedway, 14233 Kutztown Rd, Fleetwood, PA 19522")
	if wp.Location != nil {
		t.Error("expected Location to be nil for address string")
	}
	if wp.Address != "Speedway, 14233 Kutztown Rd, Fleetwood, PA 19522" {
		t.Errorf("Address = %q, want %q", wp.Address, "Speedway, 14233 Kutztown Rd, Fleetwood, PA 19522")
	}
}

func TestBuildRouteData_TwoLegs(t *testing.T) {
	route := &routesRoute{
		Legs: []routesLeg{
			{
				StartLocation: routesLocationResult{LatLng: routesLatLng{Latitude: 40.7128, Longitude: -74.0060}},
				EndLocation:   routesLocationResult{LatLng: routesLatLng{Latitude: 40.7300, Longitude: -74.0000}},
			},
			{
				StartLocation: routesLocationResult{LatLng: routesLatLng{Latitude: 40.7300, Longitude: -74.0000}},
				EndLocation:   routesLocationResult{LatLng: routesLatLng{Latitude: 40.7580, Longitude: -73.9855}},
			},
		},
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
	if data.RouteWaypoints[0].Latitude != 40.7128 {
		t.Errorf("waypoint[0].Latitude = %f, want 40.7128", data.RouteWaypoints[0].Latitude)
	}
	if data.RouteWaypoints[2].Latitude != 40.7580 {
		t.Errorf("waypoint[2].Latitude = %f, want 40.7580", data.RouteWaypoints[2].Latitude)
	}
}

func TestBuildRouteData_NoLegs(t *testing.T) {
	route := &routesRoute{Legs: nil}
	_, err := buildRouteData(route, []string{"A", "B"})
	if err == nil {
		t.Error("buildRouteData with no legs should return an error")
	}
}

func TestBuildRouteData_WithOverviewPolyline(t *testing.T) {
	// "_p~iF~ps|U_ulLnnqC_mqNvxq`@" decodes to 3 points
	route := &routesRoute{
		Legs: []routesLeg{
			{
				StartLocation: routesLocationResult{LatLng: routesLatLng{Latitude: 38.5, Longitude: -120.2}},
				EndLocation:   routesLocationResult{LatLng: routesLatLng{Latitude: 40.7, Longitude: -120.95}},
			},
		},
		Polyline: routesPolyline{EncodedPolyline: "_p~iF~ps|U_ulLnnqC_mqNvxq`@"},
	}
	waypoints := []string{"Start", "End"}

	data, err := buildRouteData(route, waypoints)
	if err != nil {
		t.Fatalf("buildRouteData: %v", err)
	}
	if len(data.TrackPoints) == 0 {
		t.Error("expected track points from polyline, got none")
	}
}

func TestCallRoutesAPI_OK(t *testing.T) {
	fakeRoute := routesRoute{
		Legs: []routesLeg{
			{
				StartLocation: routesLocationResult{LatLng: routesLatLng{Latitude: 40.0, Longitude: -74.0}},
				EndLocation:   routesLocationResult{LatLng: routesLatLng{Latitude: 41.0, Longitude: -75.0}},
			},
		},
	}
	fakeResp := routesResponse{Routes: []routesRoute{fakeRoute}}

	var capturedReq *http.Request
	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		capturedReq = req
		return jsonResponse(fakeResp), nil
	})

	route, err := client.callRoutesAPI(context.Background(), []string{"A", "B"})
	if err != nil {
		t.Fatalf("callRoutesAPI: %v", err)
	}
	if route == nil {
		t.Fatal("expected a route, got nil")
	}

	// Verify POST method
	if capturedReq.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", capturedReq.Method)
	}
	// Verify API key is in query string, not Authorization header
	if key := capturedReq.URL.Query().Get("key"); key != "test-token" {
		t.Errorf("key query param = %q, want %q", key, "test-token")
	}
	if auth := capturedReq.Header.Get("Authorization"); auth != "" {
		t.Errorf("Authorization header should not be set, got %q", auth)
	}
	// Verify X-Goog-FieldMask header
	if mask := capturedReq.Header.Get("X-Goog-FieldMask"); mask == "" {
		t.Error("X-Goog-FieldMask header should be set")
	}
}

func TestCallRoutesAPI_HTTPError(t *testing.T) {
	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		return errResponse(http.StatusInternalServerError, "server error"), nil
	})

	_, err := client.callRoutesAPI(context.Background(), []string{"A", "B"})
	if err == nil {
		t.Error("expected error for HTTP 500 response")
	}
}

func TestCallRoutesAPI_NoRoutes(t *testing.T) {
	fakeResp := routesResponse{Routes: []routesRoute{}}

	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(fakeResp), nil
	})

	_, err := client.callRoutesAPI(context.Background(), []string{"A", "B"})
	if err == nil {
		t.Error("expected error when no routes returned")
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
	fakeResp := routesResponse{Routes: []routesRoute{}}

	client := newTestMapsClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(fakeResp), nil
	})

	_, err := client.GetRoute(context.Background(), "https://maps.google.com/maps/dir/Home/Work")
	if err == nil {
		t.Error("GetRoute should return error when API returns no routes")
	}
}

func TestGetRoute_Success(t *testing.T) {
	fakeRoute := routesRoute{
		Legs: []routesLeg{
			{
				StartLocation: routesLocationResult{LatLng: routesLatLng{Latitude: 40.7128, Longitude: -74.0060}},
				EndLocation:   routesLocationResult{LatLng: routesLatLng{Latitude: 40.7580, Longitude: -73.9855}},
			},
		},
		Polyline: routesPolyline{EncodedPolyline: "_p~iF~ps|U_ulLnnqC_mqNvxq`@"},
	}
	fakeResp := routesResponse{Routes: []routesRoute{fakeRoute}}

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

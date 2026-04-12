package gpx

import (
	"testing"
)

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

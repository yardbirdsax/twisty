package gpx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertRouteToGPX(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	data := &RouteData{
		StartName:       "Home",
		DestinationName: "Work",
		RouteWaypoints: []RouteWaypoint{
			{Name: "Home", Latitude: 40.7128, Longitude: -74.0060},
			{Name: "Work", Latitude: 40.7580, Longitude: -73.9855},
		},
		TrackPoints: []TrackCoord{
			{Latitude: 40.7128, Longitude: -74.0060},
			{Latitude: 40.7200, Longitude: -74.0050},
			{Latitude: 40.7580, Longitude: -73.9855},
		},
	}

	if err := ConvertRouteToGPX(data, outPath); err != nil {
		t.Fatalf("ConvertRouteToGPX failed: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	xml := string(content)
	if !strings.Contains(xml, `<?xml version="1.0"`) {
		t.Error("XML declaration missing")
	}
	if !strings.Contains(xml, `version="1.1"`) {
		t.Error("GPX version attribute missing")
	}
	if !strings.Contains(xml, "<wpt") {
		t.Error("waypoint elements missing")
	}
	if !strings.Contains(xml, "<trk>") {
		t.Error("track element missing")
	}
	if !strings.Contains(xml, "<trkpt") {
		t.Error("track point elements missing")
	}
	if !strings.Contains(xml, "Home") {
		t.Error("waypoint name 'Home' not found in XML")
	}
	if !strings.Contains(xml, "Work") {
		t.Error("waypoint name 'Work' not found in XML")
	}
}

func TestConvertRouteToGPX_InvalidCoordinates(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	tests := []struct {
		name    string
		data    *RouteData
		wantErr bool
	}{
		{
			name: "invalid latitude",
			data: &RouteData{
				StartName:       "A",
				DestinationName: "B",
				RouteWaypoints: []RouteWaypoint{
					{Name: "A", Latitude: 91, Longitude: 0},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid longitude",
			data: &RouteData{
				StartName:       "A",
				DestinationName: "B",
				RouteWaypoints: []RouteWaypoint{
					{Name: "A", Latitude: 0, Longitude: 181},
				},
			},
			wantErr: true,
		},
		{
			name: "valid boundary coordinates",
			data: &RouteData{
				StartName:       "A",
				DestinationName: "B",
				RouteWaypoints: []RouteWaypoint{
					{Name: "A", Latitude: 90, Longitude: 180},
					{Name: "B", Latitude: -90, Longitude: -180},
				},
				TrackPoints: []TrackCoord{
					{Latitude: 90, Longitude: 180},
					{Latitude: -90, Longitude: -180},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ConvertRouteToGPX(tt.data, outPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRouteToGPX: expected error=%v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestConvertRouteToGPX_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	data := &RouteData{
		StartName:       "A",
		DestinationName: "B",
		RouteWaypoints:  []RouteWaypoint{},
		TrackPoints:     []TrackCoord{},
	}

	if err := ConvertRouteToGPX(data, outPath); err == nil {
		t.Error("ConvertRouteToGPX should fail for empty route")
	}
}

func TestConvertRouteToGPX_LargeRoute(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	trackPoints := make([]TrackCoord, 10000)
	for i := 0; i < 10000; i++ {
		trackPoints[i] = TrackCoord{
			Latitude:  40.0 + float64(i)*0.00001,
			Longitude: -74.0 + float64(i)*0.00001,
		}
	}

	data := &RouteData{
		StartName:       "Start",
		DestinationName: "End",
		RouteWaypoints: []RouteWaypoint{
			{Name: "Start", Latitude: 40.0, Longitude: -74.0},
			{Name: "End", Latitude: 40.1, Longitude: -73.9},
		},
		TrackPoints: trackPoints,
	}

	if err := ConvertRouteToGPX(data, outPath); err != nil {
		t.Fatalf("ConvertRouteToGPX failed for large route: %v", err)
	}
}

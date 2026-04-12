package gpx

import (
	"path/filepath"
	"testing"
)

func TestGPXCoordinateBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		lat     float64
		lon     float64
		wantErr bool
	}{
		{"valid center", 0, 0, false},
		{"valid north pole", 90, 0, false},
		{"valid south pole", -90, 0, false},
		{"valid antimeridian east", 0, 180, false},
		{"valid antimeridian west", 0, -180, false},
		{"invalid north", 90.1, 0, true},
		{"invalid south", -90.1, 0, true},
		{"invalid east", 0, 180.1, true},
		{"invalid west", 0, -180.1, true},
	}

	tmpDir := t.TempDir()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &RouteData{
				StartName:       "A",
				DestinationName: "B",
				RouteWaypoints: []RouteWaypoint{
					{Name: "A", Latitude: tt.lat, Longitude: tt.lon},
					{Name: "B", Latitude: 0, Longitude: 0},
				},
				TrackPoints: []TrackCoord{
					{Latitude: tt.lat, Longitude: tt.lon},
					{Latitude: 0, Longitude: 0},
				},
			}
			outPath := filepath.Join(tmpDir, tt.name+".gpx")
			err := ConvertRouteToGPX(data, outPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRouteToGPX: expected error=%v, got %v", tt.wantErr, err)
			}
		})
	}
}

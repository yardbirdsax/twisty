package gpx

import (
	"testing"
)

func TestParseSharedLinkVariations(t *testing.T) {
	client := NewMapsClient("test-token")

	tests := []struct {
		name          string
		url           string
		wantCount     int
		wantWaypoints []string
		wantErr       bool
	}{
		{
			name:      "two-stop path URL",
			url:       "https://maps.google.com/maps/dir/New%20York/Boston",
			wantCount: 2,
		},
		{
			name:      "three-stop path URL",
			url:       "https://maps.google.com/maps/dir/Home/Office/Gym",
			wantCount: 3,
		},
		{
			name:    "single location (no destination)",
			url:     "https://maps.google.com/maps/dir/OnlyOneStop",
			wantErr: true,
		},
		{
			name:      "full URL with viewport and data segments",
			url:       "https://www.google.com/maps/dir/40.238122,-75.526346/40.3625144,-75.6776312/40.445452,-75.802636/Speedway,+14233+Kutztown+Rd,+Fleetwood,+PA+19522/@40.2381255,-75.5323253,668m/data=!3m1!1e3!4m11!4m10!1m0!1m0!1m0!1m5!1m1!1s0x89c5d6cd37212bc1:0xd23839ad4c4b15f0!2m2!1d-75.8399983!2d40.4856114!3e0!5m1!1e4?entry=ttu&g_ep=EgoyMDI2MDQwOC4wIKXMDSoASAFQ",
			wantCount: 4,
			wantWaypoints: []string{
				"40.238122,-75.526346",
				"40.3625144,-75.6776312",
				"40.445452,-75.802636",
				"Speedway, 14233 Kutztown Rd, Fleetwood, PA 19522",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			waypoints, err := client.parseSharedLink(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSharedLink: expected error=%v, got %v", tt.wantErr, err)
			}
			if !tt.wantErr && len(waypoints) != tt.wantCount {
				t.Errorf("parseSharedLink: expected %d waypoints, got %d", tt.wantCount, len(waypoints))
			}
			if tt.wantWaypoints != nil {
				for i, want := range tt.wantWaypoints {
					if i >= len(waypoints) {
						t.Errorf("parseSharedLink: missing waypoint[%d]: want %q", i, want)
						continue
					}
					if waypoints[i] != want {
						t.Errorf("parseSharedLink: waypoint[%d] = %q, want %q", i, waypoints[i], want)
					}
				}
			}
		})
	}
}

func TestValidateGoogleMapsURLVariations(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://maps.google.com/maps/dir/Home/Work", false},
		{"https://maps.app.goo.gl/abc123", false},
		{"https://google.com/maps/dir/A/B", false},
		{"https://www.google.com/maps/dir/A/B", false},
		{"https://example.com/maps/dir/A/B", true},
		{"https://notmaps.google.com/something", true},
		{"not a url at all", true},
		{"", true},
	}

	for _, tt := range tests {
		err := ValidateGoogleMapsURL(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateGoogleMapsURL(%q): expected error=%v, got %v", tt.url, tt.wantErr, err)
		}
	}
}

package gpx

import (
	"testing"
)

func TestParseSharedLinkVariations(t *testing.T) {
	client := NewMapsClient("test-token")

	tests := []struct {
		name      string
		url       string
		wantCount int
		wantErr   bool
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

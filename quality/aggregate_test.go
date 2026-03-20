package quality

import "testing"

func TestRoadCollection_DisplayName(t *testing.T) {
	tests := []struct {
		name     string
		rc       RoadCollection
		expected string
	}{
		{
			name:     "SubIndex 0 returns bare name",
			rc:       RoadCollection{Name: "Route 100", SubIndex: 0},
			expected: "Route 100",
		},
		{
			name:     "SubIndex 1 returns name with suffix (2)",
			rc:       RoadCollection{Name: "Route 100", SubIndex: 1},
			expected: "Route 100 (2)",
		},
		{
			name:     "SubIndex 2 returns name with suffix (3)",
			rc:       RoadCollection{Name: "Route 100", SubIndex: 2},
			expected: "Route 100 (3)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rc.DisplayName()
			if got != tt.expected {
				t.Errorf("DisplayName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

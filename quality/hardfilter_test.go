package quality

import (
	"testing"
)

func TestIsHardFiltered(t *testing.T) {
	tests := []struct {
		name string
		tags map[string]string
		want bool
	}{
		// Unpaved surfaces — filtered
		{name: "surface=gravel", tags: map[string]string{"surface": "gravel"}, want: true},
		{name: "surface=dirt", tags: map[string]string{"surface": "dirt"}, want: true},
		{name: "surface=unpaved", tags: map[string]string{"surface": "unpaved"}, want: true},
		{name: "surface=mud", tags: map[string]string{"surface": "mud"}, want: true},
		{name: "surface=sand", tags: map[string]string{"surface": "sand"}, want: true},
		// Paved surfaces — not filtered
		{name: "surface=asphalt", tags: map[string]string{"surface": "asphalt"}, want: false},
		{name: "surface=paved", tags: map[string]string{"surface": "paved"}, want: false},
		// No surface tag — not filtered
		{name: "no surface tag", tags: map[string]string{"highway": "tertiary"}, want: false},
		// Access restrictions — filtered
		{name: "access=private", tags: map[string]string{"access": "private"}, want: true},
		{name: "access=no", tags: map[string]string{"access": "no"}, want: true},
		// Access allowed — not filtered
		{name: "access=yes", tags: map[string]string{"access": "yes"}, want: false},
		// motor_vehicle restrictions — filtered
		{name: "motor_vehicle=no", tags: map[string]string{"motor_vehicle": "no"}, want: true},
		{name: "motor_vehicle=private", tags: map[string]string{"motor_vehicle": "private"}, want: true},
		// Non-motor-vehicle highway types — filtered
		{name: "highway=track", tags: map[string]string{"highway": "track"}, want: true},
		{name: "highway=path", tags: map[string]string{"highway": "path"}, want: true},
		{name: "highway=footway", tags: map[string]string{"highway": "footway"}, want: true},
		{name: "highway=cycleway", tags: map[string]string{"highway": "cycleway"}, want: true},
		{name: "highway=bridleway", tags: map[string]string{"highway": "bridleway"}, want: true},
		{name: "highway=steps", tags: map[string]string{"highway": "steps"}, want: true},
		// Residential highways — filtered; other motor-vehicle highways — not filtered
		{name: "highway=residential", tags: map[string]string{"highway": "residential"}, want: true},
		{name: "highway=secondary", tags: map[string]string{"highway": "secondary"}, want: false},
		// Multiple disqualifying tags — filtered (OR logic)
		{name: "surface=gravel and access=private", tags: map[string]string{"surface": "gravel", "access": "private"}, want: true},
		// No tags at all — not filtered
		{name: "no tags", tags: map[string]string{}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isHardFiltered(tc.tags)
			if got != tc.want {
				t.Errorf("isHardFiltered(%v) = %v, want %v", tc.tags, got, tc.want)
			}
		})
	}
}

func TestHardFilter(t *testing.T) {
	ways := []Way{
		{ID: 1, Tags: map[string]string{"highway": "secondary"}},
		{ID: 2, Tags: map[string]string{"surface": "gravel"}},
		{ID: 3, Tags: map[string]string{"highway": "residential"}},
		{ID: 4, Tags: map[string]string{"access": "private"}},
		{ID: 5, Tags: map[string]string{"highway": "tertiary"}},
		{ID: 6, Tags: map[string]string{"highway": "footway"}},
	}

	// Copy input to verify the original is not modified.
	inputCopy := make([]Way, len(ways))
	copy(inputCopy, ways)

	result := HardFilter(ways)

	// Verify original input is unchanged.
	if len(ways) != len(inputCopy) {
		t.Fatalf("input slice length changed: got %d, want %d", len(ways), len(inputCopy))
	}
	for i := range ways {
		if ways[i].ID != inputCopy[i].ID {
			t.Errorf("input[%d].ID changed: got %d, want %d", i, ways[i].ID, inputCopy[i].ID)
		}
	}

	// Verify correct ways are returned.
	wantIDs := []int64{1, 5}
	if len(result) != len(wantIDs) {
		t.Fatalf("HardFilter returned %d ways, want %d", len(result), len(wantIDs))
	}
	for i, w := range result {
		if w.ID != wantIDs[i] {
			t.Errorf("result[%d].ID = %d, want %d", i, w.ID, wantIDs[i])
		}
	}
}

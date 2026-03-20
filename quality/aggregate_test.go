package quality

import (
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

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

func TestGroupWaysByName(t *testing.T) {
	ways := []ScoredWay{
		{WayID: 1, Tags: map[string]string{"name": "Route 1"}},
		{WayID: 2, Tags: map[string]string{"name": "Route 1"}},
		{WayID: 3, Tags: map[string]string{"name": "Route 2"}},
		{WayID: 4, Tags: map[string]string{}},              // no name
		{WayID: 5, Tags: map[string]string{"name": ""}},    // empty name
		{WayID: 6, Tags: nil},                               // nil tags
	}

	got := GroupWaysByName(ways)

	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(got))
	}

	r1 := got["Route 1"]
	if len(r1) != 2 {
		t.Errorf("expected 2 ways in Route 1, got %d", len(r1))
	}

	r2 := got["Route 2"]
	if len(r2) != 1 {
		t.Errorf("expected 1 way in Route 2, got %d", len(r2))
	}

	if _, ok := got[""]; ok {
		t.Error("unnamed ways should be excluded")
	}
}

func TestGroupWaysByName_AllUnnamed(t *testing.T) {
	ways := []ScoredWay{
		{WayID: 1, Tags: map[string]string{}},
	}
	got := GroupWaysByName(ways)
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d groups", len(got))
	}
}

// makeWay creates a ScoredWay with a single segment between start and end.
func makeWay(id int64, start, end geo.Coord) ScoredWay {
	return ScoredWay{
		WayID: id,
		Segments: []ScoredSegment{
			{Start: start, End: end},
		},
	}
}

func TestFindConnectedComponents_TwoClusters(t *testing.T) {
	// Cluster A: two nearby ways in Vermont (~44°N, -72°W)
	a1 := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	a2 := makeWay(2, geo.Coord{Lat: 44.001, Lon: -72.0}, geo.Coord{Lat: 44.002, Lon: -72.0})

	// Cluster B: two nearby ways far away (~36°N, -80°W)
	b1 := makeWay(3, geo.Coord{Lat: 36.0, Lon: -80.0}, geo.Coord{Lat: 36.001, Lon: -80.0})
	b2 := makeWay(4, geo.Coord{Lat: 36.001, Lon: -80.0}, geo.Coord{Lat: 36.002, Lon: -80.0})

	ways := []ScoredWay{a1, a2, b1, b2}
	components := FindConnectedComponents(ways, ConnectedEndpointProximityM)

	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}

	sizes := map[int]int{}
	for _, comp := range components {
		sizes[len(comp)]++
	}
	if sizes[2] != 2 {
		t.Errorf("expected two components of size 2, got sizes: %v", sizes)
	}
}

func TestFindConnectedComponents_OneCluster(t *testing.T) {
	// Three consecutive ways, each endpoint within 100m of the next.
	w1 := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.0005, Lon: -72.0})
	w2 := makeWay(2, geo.Coord{Lat: 44.0005, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	w3 := makeWay(3, geo.Coord{Lat: 44.001, Lon: -72.0}, geo.Coord{Lat: 44.0015, Lon: -72.0})

	components := FindConnectedComponents([]ScoredWay{w1, w2, w3}, ConnectedEndpointProximityM)

	if len(components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(components))
	}
	if len(components[0]) != 3 {
		t.Errorf("expected 3 ways in component, got %d", len(components[0]))
	}
}

func TestFindConnectedComponents_SingleWay(t *testing.T) {
	w := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	components := FindConnectedComponents([]ScoredWay{w}, ConnectedEndpointProximityM)
	if len(components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(components))
	}
}

func TestFindConnectedComponents_Empty(t *testing.T) {
	components := FindConnectedComponents(nil, ConnectedEndpointProximityM)
	if len(components) != 0 {
		t.Errorf("expected 0 components, got %d", len(components))
	}
}

func TestFindConnectedComponents_WayNoSegments(t *testing.T) {
	// A way with no segments should still appear in a component by itself.
	noSeg := ScoredWay{WayID: 1}
	w := makeWay(2, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})

	components := FindConnectedComponents([]ScoredWay{noSeg, w}, ConnectedEndpointProximityM)
	// They can't be connected (no-seg way has no endpoints), so expect 2 components.
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}
}

func TestFindConnectedComponents_AllDisconnected(t *testing.T) {
	// Three valid ways each placed on a different continent — no two endpoints
	// are within ConnectedEndpointProximityM of each other.
	// Expected: each way becomes its own singleton component.
	wNA := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})   // Vermont
	wEU := makeWay(2, geo.Coord{Lat: 48.8, Lon: 2.35}, geo.Coord{Lat: 48.801, Lon: 2.35})     // Paris
	wAU := makeWay(3, geo.Coord{Lat: -33.8, Lon: 151.2}, geo.Coord{Lat: -33.801, Lon: 151.2}) // Sydney

	components := FindConnectedComponents([]ScoredWay{wNA, wEU, wAU}, ConnectedEndpointProximityM)

	if len(components) != 3 {
		t.Fatalf("expected 3 singleton components, got %d", len(components))
	}
	for i, comp := range components {
		if len(comp) != 1 {
			t.Errorf("component %d: expected 1 way, got %d", i, len(comp))
		}
	}
}

func TestOrderWays_Chain(t *testing.T) {
	// Three ways that form a chain but are provided in shuffled order.
	// Use 0.0002° steps (~22m at 44°N) so shared endpoints are well within proximity.
	w1 := makeWay(1, geo.Coord{Lat: 44.0000, Lon: -72.0}, geo.Coord{Lat: 44.0002, Lon: -72.0})
	w2 := makeWay(2, geo.Coord{Lat: 44.0002, Lon: -72.0}, geo.Coord{Lat: 44.0004, Lon: -72.0})
	w3 := makeWay(3, geo.Coord{Lat: 44.0004, Lon: -72.0}, geo.Coord{Lat: 44.0006, Lon: -72.0})

	// Shuffle: provide w3, w1, w2
	shuffled := []ScoredWay{w3, w1, w2}
	ordered := OrderWays(shuffled)

	if len(ordered) != 3 {
		t.Fatalf("expected 3 ways, got %d", len(ordered))
	}

	// Verify each way's end connects to the next way's start (within proximity).
	for i := 0; i < len(ordered)-1; i++ {
		_, end, _ := wayEndpoints(ordered[i])
		start, _, _ := wayEndpoints(ordered[i+1])
		d := geo.Haversine(end, start)
		if d > ConnectedEndpointProximityM {
			t.Errorf("gap between way %d and %d: %.1f m (want <= %.1f m)", i, i+1, d, ConnectedEndpointProximityM)
		}
	}
}

func TestOrderWays_SingleWay(t *testing.T) {
	w := makeWay(1, geo.Coord{Lat: 44.0, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
	ordered := OrderWays([]ScoredWay{w})
	if len(ordered) != 1 {
		t.Fatalf("expected 1 way, got %d", len(ordered))
	}
}

func TestOrderWays_Empty(t *testing.T) {
	ordered := OrderWays(nil)
	if len(ordered) != 0 {
		t.Errorf("expected 0 ways, got %d", len(ordered))
	}
}

func TestOrderWays_ReverseNeeded(t *testing.T) {
	// w2 is stored in the reverse direction; OrderWays should flip it.
	// Use 0.0002° steps (~22m at 44°N) so shared endpoints are well within proximity.
	w1 := makeWay(1, geo.Coord{Lat: 44.0000, Lon: -72.0}, geo.Coord{Lat: 44.0002, Lon: -72.0})
	// w2 reversed: end is close to w1's end, start is farther.
	w2 := makeWay(2, geo.Coord{Lat: 44.0004, Lon: -72.0}, geo.Coord{Lat: 44.0002, Lon: -72.0})

	ordered := OrderWays([]ScoredWay{w1, w2})
	if len(ordered) != 2 {
		t.Fatalf("expected 2 ways, got %d", len(ordered))
	}

	_, end, _ := wayEndpoints(ordered[0])
	start, _, _ := wayEndpoints(ordered[1])
	d := geo.Haversine(end, start)
	if d > ConnectedEndpointProximityM {
		t.Errorf("gap after ordering: %.1f m (want <= %.1f m)", d, ConnectedEndpointProximityM)
	}
}

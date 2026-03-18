package quality

import (
	"math/rand"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

func TestBuildSpatialGrid_Empty(t *testing.T) {
	g := BuildSpatialGrid(nil, 0.01)
	if len(g.cells) != 0 {
		t.Errorf("expected no cells for empty ways, got %d", len(g.cells))
	}
	result := g.NearestWayGrid(geo.Coord{Lat: 40.0, Lon: -74.0}, 100.0)
	if result != nil {
		t.Error("expected nil for empty grid, got a way")
	}
}

func TestNearestWayGrid_ExactMatch(t *testing.T) {
	node := geo.Coord{Lat: 35.5951, Lon: -82.5515}
	ways := []Way{
		{
			Tags:     map[string]string{"highway": "primary"},
			Geometry: []geo.Coord{node},
		},
	}
	g := BuildSpatialGrid(ways, 0.01)
	result := g.NearestWayGrid(node, 1.0)
	if result == nil {
		t.Fatal("expected a way at exact match, got nil")
	}
	if result != &ways[0] {
		t.Error("expected the matching way pointer")
	}
}

func TestNearestWayGrid_MaxDistRespected(t *testing.T) {
	// Place a way node ~100 m north of the query point.
	// 0.001 degree latitude ≈ 111 m.
	wayNode := geo.Coord{Lat: 40.001, Lon: -74.0}
	query := geo.Coord{Lat: 40.0, Lon: -74.0}

	ways := []Way{
		{
			Tags:     map[string]string{"highway": "secondary"},
			Geometry: []geo.Coord{wayNode},
		},
	}
	g := BuildSpatialGrid(ways, 0.01)

	// Distance is ~111 m; querying with maxDist=50 should return nil.
	result := g.NearestWayGrid(query, 50.0)
	if result != nil {
		t.Errorf("expected nil for maxDist=50 m when way is ~111 m away, got a way")
	}

	// Querying with maxDist=200 should find it.
	result = g.NearestWayGrid(query, 200.0)
	if result == nil {
		t.Error("expected a way for maxDist=200 m when way is ~111 m away, got nil")
	}

	// Assert the actual Haversine distance is in the expected range (~111 m).
	actualDist := geo.Haversine(query, wayNode)
	if actualDist < 100.0 || actualDist > 130.0 {
		t.Errorf("expected Haversine distance between query and wayNode to be 100–130 m, got %.2f m", actualDist)
	}
}

func TestNearestWayGrid_MatchesBruteForce(t *testing.T) {
	// Generate 200 random ways with 5 nodes each in a 0.5°×0.5° bounding box.
	const (
		numWays        = 200
		nodesPerWay    = 5
		numQueryPoints = 500
		baseLat        = 35.0
		baseLon        = -83.0
		boxSize        = 0.5
		maxDist        = 30.0
	)

	rng := rand.New(rand.NewSource(42))

	ways := make([]Way, numWays)
	for i := range ways {
		geom := make([]geo.Coord, nodesPerWay)
		for j := range geom {
			geom[j] = geo.Coord{
				Lat: baseLat + rng.Float64()*boxSize,
				Lon: baseLon + rng.Float64()*boxSize,
			}
		}
		ways[i] = Way{
			Tags:     map[string]string{"highway": "primary"},
			Geometry: geom,
		}
	}

	g := BuildSpatialGrid(ways, 0.01)

	for q := range numQueryPoints {
		p := geo.Coord{
			Lat: baseLat + rng.Float64()*boxSize,
			Lon: baseLon + rng.Float64()*boxSize,
		}

		brute := NearestWay(p, ways, maxDist)
		grid := g.NearestWayGrid(p, maxDist)

		if brute != grid {
			t.Errorf("query %d: brute-force returned %v but grid returned %v for point %v",
				q, brute, grid, p)
		}
	}
}

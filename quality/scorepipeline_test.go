package quality_test

import (
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

// straightWayGeometry returns a geometry of n collinear nodes running north
// from origin with ~20m spacing.
func straightWayGeometry(origin geo.Coord, n int) []geo.Coord {
	coords := make([]geo.Coord, n)
	coords[0] = origin
	for i := 1; i < n; i++ {
		coords[i] = geo.Coord{
			Lat: origin.Lat + float64(i)*20.0/111000.0,
			Lon: origin.Lon,
		}
	}
	return coords
}

// curvyWayGeometry returns a geometry that produces non-zero curvature scores.
//
// Structure:
//  1. A 6-node zigzag (5 segments) with tight turns (~20 m steps) that produce
//     non-zero curvature scores (circumradii < 60 m, tier >= 3).
func curvyWayGeometry(origin geo.Coord) []geo.Coord {
	const (
		step    = 20.0
		latPerM = 1.0 / 111000.0
		lonPerM = 1.0 / 78500.0
	)
	latStep := step * latPerM
	lonStep := step * lonPerM

	// Zigzag section: 6 nodes with tight turns (~20 m), all non-zero scores.
	zigzag := []geo.Coord{
		origin,
		{Lat: origin.Lat + latStep, Lon: origin.Lon},              // N
		{Lat: origin.Lat + latStep, Lon: origin.Lon + lonStep},     // E
		{Lat: origin.Lat + 2*latStep, Lon: origin.Lon + 2*lonStep}, // NE
		{Lat: origin.Lat + 2*latStep, Lon: origin.Lon + 3*lonStep}, // E
		{Lat: origin.Lat + latStep, Lon: origin.Lon + 4*lonStep},   // SE
	}

	return zigzag
}

func TestRunScorePipeline_EndToEnd(t *testing.T) {
	t.Parallel()

	origin := geo.Coord{Lat: 45.0, Lon: -122.0}

	ways := []quality.Way{
		// Should be filtered by hard filter (surface=gravel).
		{
			ID:       1,
			Tags:     map[string]string{"surface": "gravel", "highway": "secondary"},
			Geometry: straightWayGeometry(origin, 5),
		},
		// Should be filtered by hard filter (access=private).
		{
			ID:       2,
			Tags:     map[string]string{"access": "private", "highway": "secondary"},
			Geometry: straightWayGeometry(geo.Coord{Lat: 45.1, Lon: -122.0}, 5),
		},
		// Should pass hard filter and score near zero (straight road).
		{
			ID:       3,
			Tags:     map[string]string{"highway": "secondary"},
			Geometry: straightWayGeometry(geo.Coord{Lat: 45.2, Lon: -122.0}, 6),
		},
		// Should pass hard filter and score > 0 (curvy road).
		{
			ID:       4,
			Tags:     map[string]string{"highway": "secondary"},
			Geometry: curvyWayGeometry(geo.Coord{Lat: 45.3, Lon: -122.0}),
		},
	}

	result := quality.RunScorePipeline(ways)

	// InputWays must match total input.
	if result.InputWays != 4 {
		t.Errorf("InputWays = %d, want 4", result.InputWays)
	}

	// FilteredWays must exclude the 2 filtered ways.
	if result.FilteredWays != 2 {
		t.Errorf("FilteredWays = %d, want 2", result.FilteredWays)
	}

	// ScoredWays should have one entry per filtered way.
	if len(result.ScoredWays) != 2 {
		t.Fatalf("len(ScoredWays) = %d, want 2", len(result.ScoredWays))
	}

	// Find the straight and curvy ways in the results.
	var straightWay, curvyWay *quality.ScoredWay
	for i := range result.ScoredWays {
		switch result.ScoredWays[i].WayID {
		case 3:
			straightWay = &result.ScoredWays[i]
		case 4:
			curvyWay = &result.ScoredWays[i]
		}
	}
	if straightWay == nil {
		t.Fatal("straight way (ID=3) not found in ScoredWays")
	}
	if curvyWay == nil {
		t.Fatal("curvy way (ID=4) not found in ScoredWays")
	}

	// The straight way should have all zero-score segments.
	for i, seg := range straightWay.Segments {
		if seg.Score != 0 {
			t.Errorf("straight way segment %d: expected Score=0, got %v", i, seg.Score)
		}
	}

	// The curvy way should have at least one non-zero scored segment.
	// (Deflection filtering now runs post-aggregation, not here.)
	curvyHasScore := false
	for _, seg := range curvyWay.Segments {
		if seg.Score > 0 {
			curvyHasScore = true
			break
		}
	}
	if !curvyHasScore {
		t.Error("curvy way: expected at least one segment with Score > 0")
	}

	// TotalSegments should match actual segment count in ScoredWays.
	totalSegs := 0
	for _, sw := range result.ScoredWays {
		totalSegs += len(sw.Segments)
	}
	if result.TotalSegments != totalSegs {
		t.Errorf("TotalSegments = %d, want %d", result.TotalSegments, totalSegs)
	}
}

func TestRunScorePipeline_TagsPropagated(t *testing.T) {
	t.Parallel()

	origin := geo.Coord{Lat: 45.0, Lon: -122.0}
	ways := []quality.Way{
		{
			ID:       10,
			Tags:     map[string]string{"highway": "secondary", "name": "Curvy Lane"},
			Geometry: curvyWayGeometry(origin),
		},
	}

	result := quality.RunScorePipeline(ways)

	if len(result.ScoredWays) != 1 {
		t.Fatalf("expected 1 scored way, got %d", len(result.ScoredWays))
	}

	sw := result.ScoredWays[0]
	if sw.Tags == nil {
		t.Fatal("ScoredWay.Tags is nil, expected tags to be propagated")
	}
	if sw.Tags["highway"] != "secondary" {
		t.Errorf("Tags[highway]: got %q, want %q", sw.Tags["highway"], "secondary")
	}
	if sw.Tags["name"] != "Curvy Lane" {
		t.Errorf("Tags[name]: got %q, want %q", sw.Tags["name"], "Curvy Lane")
	}
}

func TestRunScorePipeline_EmptyInput(t *testing.T) {
	t.Parallel()

	result := quality.RunScorePipeline(nil)

	if result.InputWays != 0 {
		t.Errorf("InputWays = %d, want 0", result.InputWays)
	}
	if result.FilteredWays != 0 {
		t.Errorf("FilteredWays = %d, want 0", result.FilteredWays)
	}
	if result.TotalSegments != 0 {
		t.Errorf("TotalSegments = %d, want 0", result.TotalSegments)
	}
	if len(result.ScoredWays) != 0 {
		t.Errorf("len(ScoredWays) = %d, want 0", len(result.ScoredWays))
	}
}

func TestRunScorePipeline_AllFiltered(t *testing.T) {
	t.Parallel()

	origin := geo.Coord{Lat: 45.0, Lon: -122.0}
	ways := []quality.Way{
		{
			ID:       1,
			Tags:     map[string]string{"surface": "gravel"},
			Geometry: straightWayGeometry(origin, 5),
		},
		{
			ID:       2,
			Tags:     map[string]string{"access": "private"},
			Geometry: straightWayGeometry(geo.Coord{Lat: 45.1, Lon: -122.0}, 5),
		},
		{
			ID:       3,
			Tags:     map[string]string{"highway": "footway"},
			Geometry: straightWayGeometry(geo.Coord{Lat: 45.2, Lon: -122.0}, 5),
		},
	}

	result := quality.RunScorePipeline(ways)

	if result.InputWays != 3 {
		t.Errorf("InputWays = %d, want 3", result.InputWays)
	}
	if result.FilteredWays != 0 {
		t.Errorf("FilteredWays = %d, want 0", result.FilteredWays)
	}
	if len(result.ScoredWays) != 0 {
		t.Errorf("len(ScoredWays) = %d, want 0", len(result.ScoredWays))
	}
	if result.TotalSegments != 0 {
		t.Errorf("TotalSegments = %d, want 0", result.TotalSegments)
	}
}

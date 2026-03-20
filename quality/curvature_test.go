package quality_test

import (
	"math"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

const tolerance = 0.02 // 2% relative tolerance

func approxEqual(a, b, tol float64) bool {
	if a == b {
		return true
	}
	if b == 0 {
		return math.Abs(a) < tol
	}
	return math.Abs(a-b)/math.Abs(b) <= tol
}

func TestScoreWay_EmptyGeometry(t *testing.T) {
	t.Parallel()

	w := quality.Way{ID: 1, Geometry: []geo.Coord{}}
	sw := quality.ScoreWay(w)

	if len(sw.Segments) != 0 {
		t.Errorf("expected 0 segments, got %d", len(sw.Segments))
	}
	if sw.WayID != 1 {
		t.Errorf("expected WayID 1, got %d", sw.WayID)
	}
}

func TestScoreWay_OneNode(t *testing.T) {
	t.Parallel()

	w := quality.Way{ID: 2, Geometry: []geo.Coord{{Lat: 45.0, Lon: -122.0}}}
	sw := quality.ScoreWay(w)

	if len(sw.Segments) != 0 {
		t.Errorf("expected 0 segments, got %d", len(sw.Segments))
	}
}

func TestScoreWay_TwoNodes(t *testing.T) {
	t.Parallel()

	a := geo.Coord{Lat: 45.0, Lon: -122.0}
	b := geo.Coord{Lat: 45.001, Lon: -122.0}
	w := quality.Way{ID: 3, Geometry: []geo.Coord{a, b}}
	sw := quality.ScoreWay(w)

	if len(sw.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(sw.Segments))
	}
	seg := sw.Segments[0]
	if !math.IsInf(seg.Radius, 1) {
		t.Errorf("expected +Inf radius, got %v", seg.Radius)
	}
	if seg.Tier != 0 {
		t.Errorf("expected tier 0, got %d", seg.Tier)
	}
	if seg.Score != 0 {
		t.Errorf("expected score 0, got %v", seg.Score)
	}
	if seg.Length <= 0 {
		t.Errorf("expected positive length, got %v", seg.Length)
	}
}

func TestScoreWay_ThreeNodes_Straight(t *testing.T) {
	t.Parallel()

	// Three nearly collinear points; circumradius will be enormous (tier 0).
	// Use a very large radius triangle (all three points far apart on a line).
	a := geo.Coord{Lat: 45.000, Lon: -122.0}
	b := geo.Coord{Lat: 45.001, Lon: -122.0}
	c := geo.Coord{Lat: 45.002, Lon: -122.0}
	w := quality.Way{ID: 4, Geometry: []geo.Coord{a, b, c}}
	sw := quality.ScoreWay(w)

	if len(sw.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(sw.Segments))
	}
	for i, seg := range sw.Segments {
		// Collinear points produce a very large radius (tier 0 = straight).
		if seg.Tier != 0 {
			t.Errorf("segment %d: expected tier 0 for collinear points, got tier %d (radius=%v)", i, seg.Tier, seg.Radius)
		}
		if seg.Score != 0 {
			t.Errorf("segment %d: expected score 0, got %v", i, seg.Score)
		}
	}
}

func TestScoreWay_ThreeNodes_TightCurve(t *testing.T) {
	t.Parallel()

	// Three points forming a tight right-angle triangle with ~20m sides.
	// Place points roughly 20m apart in two perpendicular directions.
	// 1 degree lat ~ 111000m, so 20m ~ 0.00018 degrees lat.
	// 1 degree lon at lat=45 ~ 78500m, so 20m ~ 0.000255 degrees lon.
	a := geo.Coord{Lat: 45.000000, Lon: -122.000000}
	b := geo.Coord{Lat: 45.000180, Lon: -122.000000} // ~20m north
	c := geo.Coord{Lat: 45.000180, Lon: -121.999745} // ~20m east of b
	w := quality.Way{ID: 5, Geometry: []geo.Coord{a, b, c}}
	sw := quality.ScoreWay(w)

	if len(sw.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(sw.Segments))
	}
	// Both segments share the same circumradius from triangle (a,b,c).
	for i, seg := range sw.Segments {
		if math.IsInf(seg.Radius, 1) {
			t.Errorf("segment %d: expected finite radius for tight curve, got +Inf", i)
		}
		if seg.Tier == 0 {
			t.Errorf("segment %d: expected non-zero tier for tight curve, got tier 0 (radius=%v)", i, seg.Radius)
		}
		if seg.Score <= 0 {
			t.Errorf("segment %d: expected positive score, got %v", i, seg.Score)
		}
	}
	// Verify Score = Length * Weight
	for i, seg := range sw.Segments {
		want := seg.Length * seg.Weight
		if !approxEqual(seg.Score, want, tolerance) {
			t.Errorf("segment %d: Score=%v, want Length*Weight=%v", i, seg.Score, want)
		}
	}
}

func TestScoreWay_FourNodes_InteriorMinRadius(t *testing.T) {
	t.Parallel()

	// Four nodes: straight start, tight curve in the middle, straight end.
	// Nodes: A(straight), B, C(tight), D(straight)
	// Triangle (A,B,C) is tight; triangle (B,C,D) is straight-ish.
	// Interior segment (B,C) should get min radius from both triangles.
	A := geo.Coord{Lat: 45.000000, Lon: -122.000000}
	B := geo.Coord{Lat: 45.000180, Lon: -122.000000} // ~20m north
	C := geo.Coord{Lat: 45.000180, Lon: -121.999745} // ~20m east of B (tight corner at B)
	D := geo.Coord{Lat: 45.000000, Lon: -121.999745} // ~20m south of C

	w := quality.Way{ID: 6, Geometry: []geo.Coord{A, B, C, D}}
	sw := quality.ScoreWay(w)

	if len(sw.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(sw.Segments))
	}

	// Compute circumradii directly for verification.
	r0 := geo.Circumradius(A, B, C) // triangle 0: tight
	r1 := geo.Circumradius(B, C, D) // triangle 1: tight (same shape mirrored)

	// Segment 0 (A,B): uses triangle 0 only → radius = r0
	seg0 := sw.Segments[0]
	if !approxEqual(seg0.Radius, r0, tolerance) {
		t.Errorf("segment 0: radius=%v, want %v", seg0.Radius, r0)
	}

	// Segment 1 (B,C): uses min(r0, r1)
	seg1 := sw.Segments[1]
	wantR1 := math.Min(r0, r1)
	if !approxEqual(seg1.Radius, wantR1, tolerance) {
		t.Errorf("segment 1 (interior): radius=%v, want min(%v,%v)=%v", seg1.Radius, r0, r1, wantR1)
	}

	// Segment 2 (C,D): uses triangle 1 only → radius = r1
	seg2 := sw.Segments[2]
	if !approxEqual(seg2.Radius, r1, tolerance) {
		t.Errorf("segment 2: radius=%v, want %v", seg2.Radius, r1)
	}
}

func TestScoreWay_FiveNodes_MixedCurvature(t *testing.T) {
	t.Parallel()

	// Five nodes: tight curve followed by a straight section.
	// Tight: A, B, C (right-angle ~20m sides)
	// Straight: C, D, E (collinear)
	A := geo.Coord{Lat: 45.000000, Lon: -122.000000}
	B := geo.Coord{Lat: 45.000180, Lon: -122.000000}
	C := geo.Coord{Lat: 45.000180, Lon: -121.999745}
	D := geo.Coord{Lat: 45.000360, Lon: -121.999745} // straight north from C
	E := geo.Coord{Lat: 45.000540, Lon: -121.999745} // straight north from D

	w := quality.Way{ID: 7, Geometry: []geo.Coord{A, B, C, D, E}}
	sw := quality.ScoreWay(w)

	if len(sw.Segments) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(sw.Segments))
	}

	// Segments 0,1,2 touch the tight triangle; segment 3 is purely straight.
	// Segment 0 (A,B): triangle(A,B,C) → tight → score > 0
	if sw.Segments[0].Score <= 0 {
		t.Errorf("segment 0: expected positive score for curvy segment, got %v", sw.Segments[0].Score)
	}
	// Segment 3 (D,E): triangle(C,D,E) → collinear → score = 0
	if sw.Segments[3].Score != 0 {
		t.Errorf("segment 3: expected 0 score for straight segment, got %v", sw.Segments[3].Score)
	}
}

func TestScoreWay_ScoreCalculation(t *testing.T) {
	t.Parallel()

	// Verify Score = Length * Weight for a known tight curve segment.
	a := geo.Coord{Lat: 45.000000, Lon: -122.000000}
	b := geo.Coord{Lat: 45.000180, Lon: -122.000000}
	c := geo.Coord{Lat: 45.000180, Lon: -121.999745}
	w := quality.Way{ID: 8, Geometry: []geo.Coord{a, b, c}}
	sw := quality.ScoreWay(w)

	if len(sw.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(sw.Segments))
	}
	for i, seg := range sw.Segments {
		wantScore := seg.Length * seg.Weight
		if !approxEqual(seg.Score, wantScore, tolerance) {
			t.Errorf("segment %d: Score=%v want Length*Weight=%v*%v=%v",
				i, seg.Score, seg.Length, seg.Weight, wantScore)
		}
	}
}

func TestScoreWays_Batch(t *testing.T) {
	t.Parallel()

	ways := []quality.Way{
		{ID: 10, Geometry: []geo.Coord{{Lat: 45.0, Lon: -122.0}, {Lat: 45.001, Lon: -122.0}}},
		{ID: 11, Geometry: []geo.Coord{}},
	}
	results := quality.ScoreWays(ways)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].WayID != 10 {
		t.Errorf("expected WayID 10, got %d", results[0].WayID)
	}
	if len(results[0].Segments) != 1 {
		t.Errorf("expected 1 segment for 2-node way, got %d", len(results[0].Segments))
	}
	if results[1].WayID != 11 {
		t.Errorf("expected WayID 11, got %d", results[1].WayID)
	}
	if len(results[1].Segments) != 0 {
		t.Errorf("expected 0 segments for empty way, got %d", len(results[1].Segments))
	}
}

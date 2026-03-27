package route

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

// TestStitchLegs_TwoLegs verifies that two independently delta-encoded legs
// are stitched without duplicating the boundary point.
func TestStitchLegs_TwoLegs(t *testing.T) {
	// Leg 1: three points, delta-encoded from zero.
	leg1Coords := []geo.Coord{
		{Lat: 40.0, Lon: -75.0},
		{Lat: 40.1, Lon: -75.1},
		{Lat: 40.2, Lon: -75.2},
	}
	// Leg 2 starts at the same point as leg 1 ends, then continues.
	// It is independently delta-encoded from zero (not from the end of leg 1).
	leg2Coords := []geo.Coord{
		{Lat: 40.2, Lon: -75.2},
		{Lat: 40.3, Lon: -75.3},
		{Lat: 40.4, Lon: -75.4},
	}

	legs := []valhallaLeg{
		{Shape: encodePolylineP6(leg1Coords)},
		{Shape: encodePolylineP6(leg2Coords)},
	}

	got, err := stitchLegs(legs)
	if err != nil {
		t.Fatalf("stitchLegs returned error: %v", err)
	}

	// Expected: leg1Coords + leg2Coords[1:] — 3 + 2 = 5 points, no duplicate at boundary.
	wantLen := 5
	if len(got) != wantLen {
		t.Fatalf("len(got) = %d, want %d", len(got), wantLen)
	}

	// Verify boundary: got[2] should match leg1 end and leg2 start (40.2, -75.2).
	const eps = 1e-4
	boundary := got[2]
	if math.Abs(boundary.Lat-40.2) > eps || math.Abs(boundary.Lon-(-75.2)) > eps {
		t.Errorf("boundary point = %v, want {40.2, -75.2}", boundary)
	}

	// Verify got[3] is from leg2Coords[1].
	next := got[3]
	if math.Abs(next.Lat-40.3) > eps || math.Abs(next.Lon-(-75.3)) > eps {
		t.Errorf("got[3] = %v, want {40.3, -75.3}", next)
	}
}

// TestStitchLegs_SingleLeg verifies that a single-leg response produces the
// same result as decoding the leg's shape directly.
func TestStitchLegs_SingleLeg(t *testing.T) {
	coords := []geo.Coord{
		{Lat: 40.19, Lon: -75.54},
		{Lat: 40.20, Lon: -75.53},
		{Lat: 40.21, Lon: -75.52},
	}
	legs := []valhallaLeg{
		{Shape: encodePolylineP6(coords)},
	}

	got, err := stitchLegs(legs)
	if err != nil {
		t.Fatalf("stitchLegs returned error: %v", err)
	}
	if len(got) != len(coords) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(coords))
	}

	const eps = 1e-4
	for i, want := range coords {
		if math.Abs(got[i].Lat-want.Lat) > eps || math.Abs(got[i].Lon-want.Lon) > eps {
			t.Errorf("got[%d] = %v, want %v", i, got[i], want)
		}
	}
}

// TestStitchLegs_EmptyLegs verifies that an empty legs slice returns an error.
func TestStitchLegs_EmptyLegs(t *testing.T) {
	_, err := stitchLegs([]valhallaLeg{})
	if err == nil {
		t.Fatal("expected error for empty legs, got nil")
	}
}

// TestFetchLoopRoute_Unit_TwoLegs exercises the full FetchLoopRoute path
// against a synthetic server with two legs, verifying correct stitching,
// duration/distance accumulation, and ScoreRoute invocation.
func TestFetchLoopRoute_Unit_TwoLegs(t *testing.T) {
	leg1Coords := []geo.Coord{
		{Lat: 40.0, Lon: -75.0},
		{Lat: 40.1, Lon: -75.1},
		{Lat: 40.2, Lon: -75.2},
	}
	leg2Coords := []geo.Coord{
		{Lat: 40.2, Lon: -75.2},
		{Lat: 40.3, Lon: -75.3},
		{Lat: 40.0, Lon: -75.0},
	}

	payload := valhallaResponse{
		Trip: valhallaTrip{
			Summary: valhallaSummary{Time: 3600.0, Length: 100.0},
			Legs: []valhallaLeg{
				{
					Shape:   encodePolylineP6(leg1Coords),
					Summary: valhallaSummary{Time: 1800.0, Length: 50.0},
				},
				{
					Shape:   encodePolylineP6(leg2Coords),
					Summary: valhallaSummary{Time: 1800.0, Length: 50.0},
				},
			},
		},
	}

	srv := makeValhallaServer(t, payload)
	defer srv.Close()

	start := geo.Coord{Lat: 40.0, Lon: -75.0}
	waypoints := []geo.Coord{{Lat: 40.2, Lon: -75.2}}

	r, err := fetchLoopRouteFromURL(srv.URL, start, waypoints)
	if err != nil {
		t.Fatalf("fetchLoopRouteFromURL returned error: %v", err)
	}

	// Duration should equal sum of per-leg times.
	const eps = 1e-3
	if math.Abs(r.Duration-3600.0) > eps {
		t.Errorf("Duration = %v, want 3600.0", r.Duration)
	}

	// Distance should equal sum of per-leg lengths * 1000.
	wantDist := 100.0 * 1000
	if math.Abs(r.Distance-wantDist) > eps {
		t.Errorf("Distance = %v, want %v", r.Distance, wantDist)
	}

	// 3 + 2 = 5 stitched points (boundary dedup drops leg2Coords[0]).
	wantPoints := 5
	if len(r.Points) != wantPoints {
		t.Errorf("len(Points) = %d, want %d", len(r.Points), wantPoints)
	}

	// ScoreRoute should have been called — Stats.Score is set when len(Points) >= 2.
	// We only verify the struct is non-zero (Score field computed).
	if r.Stats.Score == 0 && len(r.Points) >= 2 {
		t.Error("Stats.Score is 0; expected ScoreRoute to have been called")
	}
}

// TestFetchLoopRoute_ManeuversPopulated verifies that maneuver data returned
// by Valhalla is extracted and surfaced in Route.Maneuvers with resolved locations.
func TestFetchLoopRoute_ManeuversPopulated(t *testing.T) {
	legCoords := []geo.Coord{
		{Lat: 40.0, Lon: -75.0},
		{Lat: 40.1, Lon: -75.1},
		{Lat: 40.2, Lon: -75.2},
	}

	payload := valhallaResponse{
		Trip: valhallaTrip{
			Summary: valhallaSummary{Time: 600.0, Length: 10.0},
			Legs: []valhallaLeg{
				{
					Shape:   encodePolylineP6(legCoords),
					Summary: valhallaSummary{Time: 600.0, Length: 10.0},
					Maneuvers: []valhallaManeuver{
						{
							Instruction:     "Head north on Main St",
							StreetNames:     []string{"Main St"},
							Time:            120.0,
							Length:          2.5,
							BeginShapeIndex: 0,
							Type:            1,
						},
						{
							Instruction:     "Turn right onto Oak Ave",
							StreetNames:     []string{"Oak Ave"},
							Time:            480.0,
							Length:          7.5,
							BeginShapeIndex: 1,
							Type:            10,
						},
					},
				},
			},
		},
	}

	srv := makeValhallaServer(t, payload)
	defer srv.Close()

	start := geo.Coord{Lat: 40.0, Lon: -75.0}
	r, err := fetchLoopRouteFromURL(srv.URL, start, []geo.Coord{{Lat: 40.1, Lon: -75.1}})
	if err != nil {
		t.Fatalf("fetchLoopRouteFromURL returned error: %v", err)
	}

	if len(r.Maneuvers) != 2 {
		t.Fatalf("expected 2 maneuvers, got %d", len(r.Maneuvers))
	}

	const eps = 1e-4

	m0 := r.Maneuvers[0]
	if m0.Instruction != "Head north on Main St" {
		t.Errorf("maneuver[0].Instruction = %q, want %q", m0.Instruction, "Head north on Main St")
	}
	if math.Abs(m0.TimeSec-120.0) > eps {
		t.Errorf("maneuver[0].TimeSec = %v, want 120.0", m0.TimeSec)
	}
	if math.Abs(m0.LengthKm-2.5) > eps {
		t.Errorf("maneuver[0].LengthKm = %v, want 2.5", m0.LengthKm)
	}
	// BeginShapeIndex 0 → legCoords[0]
	if math.Abs(m0.Location.Lat-40.0) > eps || math.Abs(m0.Location.Lon-(-75.0)) > eps {
		t.Errorf("maneuver[0].Location = %v, want {40.0, -75.0}", m0.Location)
	}

	m1 := r.Maneuvers[1]
	if m1.Instruction != "Turn right onto Oak Ave" {
		t.Errorf("maneuver[1].Instruction = %q, want %q", m1.Instruction, "Turn right onto Oak Ave")
	}
	// BeginShapeIndex 1 → legCoords[1]
	if math.Abs(m1.Location.Lat-40.1) > eps || math.Abs(m1.Location.Lon-(-75.1)) > eps {
		t.Errorf("maneuver[1].Location = %v, want {40.1, -75.1}", m1.Location)
	}
}

// TestFetchLoopRoute_Unit_Non200Status verifies error handling for non-200 HTTP status.
func TestFetchLoopRoute_Unit_Non200Status(t *testing.T) {
	srv := makeValhallaServerWithStatus(t, http.StatusBadRequest)
	defer srv.Close()

	start := geo.Coord{Lat: 40.0, Lon: -75.0}
	_, err := fetchLoopRouteFromURL(srv.URL, start, nil)
	if err == nil {
		t.Fatal("expected error for non-200 HTTP status, got nil")
	}
	if !strings.Contains(err.Error(), "valhalla loop route") || !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want it to mention 'valhalla loop route' and status 400", err.Error())
	}
}

// TestFetchLoopRoute_Unit_200WithErrorBody verifies that a 200 response whose
// body contains a Valhalla error JSON object is treated as an error.
func TestFetchLoopRoute_Unit_200WithErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":"No route found"}`))
	}))
	defer srv.Close()

	start := geo.Coord{Lat: 40.0, Lon: -75.0}
	_, err := fetchLoopRouteFromURL(srv.URL, start, nil)
	if err == nil {
		t.Fatal("expected error for 200 response with error body, got nil")
	}
	if !strings.Contains(err.Error(), "No route found") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "No route found")
	}
}

// makeChunkingServer creates a test server that counts calls, validates that
// each call has ≤ maxValhallaLocations locations, and returns a synthetic
// single-leg response where the shape connects the first and last locations.
func makeChunkingServer(t *testing.T, callCount *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*callCount++

		var req valhallaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if len(req.Locations) > maxValhallaLocations {
			t.Errorf("call %d: %d locations exceeds limit %d", *callCount, len(req.Locations), maxValhallaLocations)
			http.Error(w, `{"error":"Exceeded max locations"}`, http.StatusBadRequest)
			return
		}

		// Build a single-leg response with one point per location.
		coords := make([]geo.Coord, len(req.Locations))
		for i, loc := range req.Locations {
			coords[i] = geo.Coord{Lat: loc.Lat, Lon: loc.Lon}
		}

		payload := valhallaResponse{
			Trip: valhallaTrip{
				Summary: valhallaSummary{Time: 600.0, Length: 10.0},
				Legs: []valhallaLeg{
					{
						Shape:   encodePolylineP6(coords),
						Summary: valhallaSummary{Time: 600.0, Length: 10.0},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
}

// TestChunkedRoute_ManyWaypoints verifies that >18 waypoints triggers multiple
// Valhalla calls and the results are stitched correctly.
func TestChunkedRoute_ManyWaypoints(t *testing.T) {
	callCount := 0
	srv := makeChunkingServer(t, &callCount)
	defer srv.Close()

	start := geo.Coord{Lat: 40.0, Lon: -75.0}
	// 40 waypoints → 42 locations → needs 3 chunks (20 + 20 + 4).
	waypoints := make([]geo.Coord, 40)
	for i := range waypoints {
		waypoints[i] = geo.Coord{Lat: 40.0 + float64(i+1)*0.01, Lon: -75.0}
	}

	r, err := fetchLoopRouteFromURL(srv.URL, start, waypoints)
	if err != nil {
		t.Fatalf("fetchLoopRouteFromURL returned error: %v", err)
	}

	// 42 locations → chunks of 20 + 20 + 4 = 3 calls.
	if callCount != 3 {
		t.Errorf("expected 3 Valhalla calls for 42 locations, got %d", callCount)
	}

	// Duration and distance should be summed across all chunks.
	if r.Duration == 0 {
		t.Error("expected non-zero Duration")
	}
	if r.Distance == 0 {
		t.Error("expected non-zero Distance")
	}

	// Points should be non-empty and start/end near the start point.
	if len(r.Points) == 0 {
		t.Fatal("expected non-empty Points")
	}
	const eps = 1e-4
	if math.Abs(r.Points[0].Lat-start.Lat) > eps || math.Abs(r.Points[0].Lon-start.Lon) > eps {
		t.Errorf("first point %v should be near start %v", r.Points[0], start)
	}
	lastPt := r.Points[len(r.Points)-1]
	if math.Abs(lastPt.Lat-start.Lat) > eps || math.Abs(lastPt.Lon-start.Lon) > eps {
		t.Errorf("last point %v should be near start %v", lastPt, start)
	}
}

// TestChunkedRoute_ExactlyAtLimit verifies that 18 waypoints (20 locations)
// triggers exactly one Valhalla call (no chunking).
func TestChunkedRoute_ExactlyAtLimit(t *testing.T) {
	callCount := 0
	srv := makeChunkingServer(t, &callCount)
	defer srv.Close()

	start := geo.Coord{Lat: 40.0, Lon: -75.0}
	waypoints := make([]geo.Coord, 18) // 18 + 2 = 20 locations
	for i := range waypoints {
		waypoints[i] = geo.Coord{Lat: 40.0 + float64(i+1)*0.01, Lon: -75.0}
	}

	_, err := fetchLoopRouteFromURL(srv.URL, start, waypoints)
	if err != nil {
		t.Fatalf("fetchLoopRouteFromURL returned error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 Valhalla call for 20 locations, got %d", callCount)
	}
}

// TestChunkedRoute_OneOverLimit verifies that 19 waypoints (21 locations)
// triggers exactly 2 Valhalla calls.
func TestChunkedRoute_OneOverLimit(t *testing.T) {
	callCount := 0
	srv := makeChunkingServer(t, &callCount)
	defer srv.Close()

	start := geo.Coord{Lat: 40.0, Lon: -75.0}
	waypoints := make([]geo.Coord, 19) // 19 + 2 = 21 locations
	for i := range waypoints {
		waypoints[i] = geo.Coord{Lat: 40.0 + float64(i+1)*0.01, Lon: -75.0}
	}

	_, err := fetchLoopRouteFromURL(srv.URL, start, waypoints)
	if err != nil {
		t.Fatalf("fetchLoopRouteFromURL returned error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 Valhalla calls for 21 locations, got %d", callCount)
	}
}

// TestChunkedRoute_NoDuplicatePoints verifies that chunk boundary points
// are not duplicated in the stitched output.
func TestChunkedRoute_NoDuplicatePoints(t *testing.T) {
	callCount := 0
	srv := makeChunkingServer(t, &callCount)
	defer srv.Close()

	start := geo.Coord{Lat: 40.0, Lon: -75.0}
	waypoints := make([]geo.Coord, 25) // 27 locations → 2 chunks
	for i := range waypoints {
		waypoints[i] = geo.Coord{Lat: 40.0 + float64(i+1)*0.01, Lon: -75.0}
	}

	r, err := fetchLoopRouteFromURL(srv.URL, start, waypoints)
	if err != nil {
		t.Fatalf("fetchLoopRouteFromURL returned error: %v", err)
	}

	// Check that no two consecutive points are identical.
	for i := 1; i < len(r.Points); i++ {
		if r.Points[i] == r.Points[i-1] {
			t.Errorf("duplicate consecutive point at index %d: %v", i, r.Points[i])
		}
	}
}

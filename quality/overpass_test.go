package quality

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/route"
)

// --- BoundingBox ---

func TestBoundingBoxBasic(t *testing.T) {
	routes := [][]geo.Coord{
		{
			{Lat: 40.0, Lon: -74.0},
			{Lat: 41.0, Lon: -73.0},
		},
	}
	s, w, n, e := BoundingBox(routes, 0.1)
	if s != 39.9 {
		t.Errorf("south: got %.1f, want 39.9", s)
	}
	if w != -74.1 {
		t.Errorf("west: got %.1f, want -74.1", w)
	}
	if n != 41.1 {
		t.Errorf("north: got %.1f, want 41.1", n)
	}
	if e != -72.9 {
		t.Errorf("east: got %.1f, want -72.9", e)
	}
}

func TestBoundingBoxMultipleRoutes(t *testing.T) {
	routes := [][]geo.Coord{
		{{Lat: 40.0, Lon: -74.0}, {Lat: 40.5, Lon: -73.5}},
		{{Lat: 39.5, Lon: -74.5}, {Lat: 41.0, Lon: -73.0}},
	}
	s, w, n, e := BoundingBox(routes, 0.0)
	if s != 39.5 {
		t.Errorf("south: got %v, want 39.5", s)
	}
	if w != -74.5 {
		t.Errorf("west: got %v, want -74.5", w)
	}
	if n != 41.0 {
		t.Errorf("north: got %v, want 41.0", n)
	}
	if e != -73.0 {
		t.Errorf("east: got %v, want -73.0", e)
	}
}

// --- IsDisqualifying ---

func TestIsDisqualifyingAccessPrivate(t *testing.T) {
	if !IsDisqualifying(map[string]string{"access": "private"}) {
		t.Error("expected true for access=private")
	}
}

func TestIsDisqualifyingAccessNo(t *testing.T) {
	if !IsDisqualifying(map[string]string{"access": "no"}) {
		t.Error("expected true for access=no")
	}
}

func TestIsDisqualifyingHighwayTrack(t *testing.T) {
	if !IsDisqualifying(map[string]string{"highway": "track"}) {
		t.Error("expected true for highway=track")
	}
}

func TestIsDisqualifyingHighwayPath(t *testing.T) {
	if !IsDisqualifying(map[string]string{"highway": "path"}) {
		t.Error("expected true for highway=path")
	}
}

func TestIsDisqualifyingHighwayFootway(t *testing.T) {
	if !IsDisqualifying(map[string]string{"highway": "footway"}) {
		t.Error("expected true for highway=footway")
	}
}

func TestIsDisqualifyingHighwayCycleway(t *testing.T) {
	if !IsDisqualifying(map[string]string{"highway": "cycleway"}) {
		t.Error("expected true for highway=cycleway")
	}
}

func TestIsDisqualifyingMotorVehicleNo(t *testing.T) {
	if !IsDisqualifying(map[string]string{"motor_vehicle": "no"}) {
		t.Error("expected true for motor_vehicle=no")
	}
}

func TestIsDisqualifyingMotorVehiclePrivate(t *testing.T) {
	if !IsDisqualifying(map[string]string{"motor_vehicle": "private"}) {
		t.Error("expected true for motor_vehicle=private")
	}
}

func TestIsDisqualifyingNormal(t *testing.T) {
	if IsDisqualifying(map[string]string{"highway": "primary"}) {
		t.Error("expected false for highway=primary")
	}
}

// --- PenaltyFactor ---

func TestPenaltyFactorUnpaved(t *testing.T) {
	for _, s := range []string{"unpaved", "gravel", "dirt", "mud", "sand"} {
		f := PenaltyFactor(map[string]string{"surface": s})
		if f != 1.0 {
			t.Errorf("surface=%s: expected 1.0, got %f", s, f)
		}
	}
}

func TestPenaltyFactorCompacted(t *testing.T) {
	for _, s := range []string{"compacted", "fine_gravel"} {
		f := PenaltyFactor(map[string]string{"surface": s})
		if f != 0.5 {
			t.Errorf("surface=%s: expected 0.5, got %f", s, f)
		}
	}
}

func TestPenaltyFactorUnclassifiedNoSurface(t *testing.T) {
	f := PenaltyFactor(map[string]string{"highway": "unclassified"})
	if f != 0.25 {
		t.Errorf("expected 0.25 for unclassified highway with no surface, got %f", f)
	}
}

func TestPenaltyFactorService(t *testing.T) {
	f := PenaltyFactor(map[string]string{"highway": "service"})
	if f != 0.25 {
		t.Errorf("expected 0.25 for highway=service, got %f", f)
	}
}

func TestPenaltyFactorPaved(t *testing.T) {
	f := PenaltyFactor(map[string]string{"highway": "primary", "surface": "asphalt"})
	if f != 0.0 {
		t.Errorf("expected 0.0 for paved primary highway, got %f", f)
	}
}

// --- NearestWay ---

func TestNearestWayFound(t *testing.T) {
	ways := []Way{
		{
			Tags:     map[string]string{"highway": "primary"},
			Geometry: []geo.Coord{{Lat: 40.0, Lon: -74.0}},
		},
	}
	p := geo.Coord{Lat: 40.0001, Lon: -74.0001} // very close
	w := NearestWay(p, ways, 50.0)
	if w == nil {
		t.Error("expected a way within 50m, got nil")
	}
}

func TestNearestWayNotFound(t *testing.T) {
	ways := []Way{
		{
			Tags:     map[string]string{"highway": "primary"},
			Geometry: []geo.Coord{{Lat: 40.0, Lon: -74.0}},
		},
	}
	p := geo.Coord{Lat: 41.0, Lon: -75.0} // far away
	w := NearestWay(p, ways, 30.0)
	if w != nil {
		t.Error("expected nil for distant point")
	}
}

func TestNearestWayEmpty(t *testing.T) {
	w := NearestWay(geo.Coord{Lat: 40.0, Lon: -74.0}, nil, 30.0)
	if w != nil {
		t.Error("expected nil for empty ways")
	}
}

// --- FetchWays (via httptest) ---

func TestFetchWaysSuccess(t *testing.T) {
	response := overpassResponse{
		Elements: []overpassElement{
			{
				Tags: map[string]string{"highway": "primary"},
				Geometry: []overpassGeomPoint{
					{Lat: 40.0, Lon: -74.0},
					{Lat: 40.1, Lon: -74.0},
				},
			},
		},
	}
	body, _ := json.Marshal(response)

	var capturedBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ := io.ReadAll(r.Body)
		capturedBody = string(rawBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer ts.Close()

	// Temporarily override the client and URL by calling through a test helper.
	// Since FetchWays hardcodes the URL, we test it via the internal helper.
	ways, err := fetchWaysFromURL(ts.URL+"/api/interpreter", 39.9, -74.1, 40.2, -73.9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ways) != 1 {
		t.Fatalf("expected 1 way, got %d", len(ways))
	}
	if ways[0].Tags["highway"] != "primary" {
		t.Errorf("expected highway=primary, got %s", ways[0].Tags["highway"])
	}
	if len(ways[0].Geometry) != 2 {
		t.Errorf("expected 2 geometry points, got %d", len(ways[0].Geometry))
	}
	// Assert that the POST body contains the full highway filter string to guard against
	// accidental removal or truncation of any term in HighwayFilter.
	if !strings.Contains(capturedBody, url.QueryEscape(HighwayFilter)) {
		t.Errorf("POST body does not contain expected highway filter; want %q in: %s", url.QueryEscape(HighwayFilter), capturedBody)
	}
}

func TestFetchWaysHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	_, err := fetchWaysFromURL(ts.URL+"/api/interpreter", 39.9, -74.1, 40.2, -73.9)
	if err == nil {
		t.Error("expected error on HTTP 503, got nil")
	}
}

// --- ApplyQuality ---

func TestApplyQualityNilWaysFallback(t *testing.T) {
	routes := []route.Route{
		{
			Points: []geo.Coord{{Lat: 40.0, Lon: -74.0}, {Lat: 40.1, Lon: -74.0}},
			Stats:  route.CurvatureStats{Score: 100.0, AdjustedScore: 100.0},
		},
	}
	// Should not panic, just print fallback warning.
	ApplyQuality(routes, nil)
	// AdjustedScore should be unchanged.
	if routes[0].Stats.AdjustedScore != 100.0 {
		t.Errorf("expected AdjustedScore unchanged on nil ways, got %f", routes[0].Stats.AdjustedScore)
	}
}

func TestApplyQualityDisqualifyingReducesScore(t *testing.T) {
	// Build a route with two points very close together.
	p0 := geo.Coord{Lat: 40.0, Lon: -74.0}
	p1 := geo.Coord{Lat: 40.001, Lon: -74.0}

	// Place a disqualifying way right at the midpoint.
	midLat := (p0.Lat + p1.Lat) / 2
	midLon := (p0.Lon + p1.Lon) / 2
	ways := []Way{
		{
			Tags:     map[string]string{"access": "private"},
			Geometry: []geo.Coord{{Lat: midLat, Lon: midLon}},
		},
	}

	routes := []route.Route{
		{
			Points: []geo.Coord{p0, p1},
			Stats:  route.CurvatureStats{Score: 200.0, AdjustedScore: 100.0},
		},
	}

	ApplyQuality(routes, ways)

	if routes[0].Stats.AdjustedScore >= 100.0 {
		t.Errorf("expected AdjustedScore < 100.0 for disqualifying way, got AdjustedScore=%f", routes[0].Stats.AdjustedScore)
	}
}

func TestApplyQualityNoWaysNearby(t *testing.T) {
	routes := []route.Route{
		{
			Points: []geo.Coord{{Lat: 40.0, Lon: -74.0}, {Lat: 40.001, Lon: -74.0}},
			Stats:  route.CurvatureStats{Score: 100.0, AdjustedScore: 100.0},
		},
	}
	// Ways are far away.
	ways := []Way{
		{
			Tags:     map[string]string{"highway": "primary"},
			Geometry: []geo.Coord{{Lat: 50.0, Lon: -80.0}},
		},
	}
	ApplyQuality(routes, ways)
	if routes[0].Stats.AdjustedScore != 100.0 {
		t.Errorf("expected AdjustedScore unchanged when no ways nearby, got %f", routes[0].Stats.AdjustedScore)
	}
}

func TestApplyQualityDisqualifyingFractionWarning(t *testing.T) {
	// Build a route whose entire length maps to a disqualifying way,
	// which ensures disqualifiedFraction > 0.10 and triggers the warning.
	p0 := geo.Coord{Lat: 40.0, Lon: -74.0}
	p1 := geo.Coord{Lat: 40.001, Lon: -74.0}

	midLat := (p0.Lat + p1.Lat) / 2
	midLon := (p0.Lon + p1.Lon) / 2
	ways := []Way{
		{
			Tags:     map[string]string{"access": "private"},
			Geometry: []geo.Coord{{Lat: midLat, Lon: midLon}},
		},
	}

	routes := []route.Route{
		{
			Points: []geo.Coord{p0, p1},
			Stats:  route.CurvatureStats{Score: 100.0, AdjustedScore: 100.0},
		},
	}

	// Redirect os.Stdout to a pipe so we can capture the warning.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	ApplyQuality(routes, ways)

	w.Close()
	os.Stdout = origStdout

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	const wantSubstr = "WARNING: route 0 has"
	if !strings.Contains(output, wantSubstr) {
		t.Errorf("expected stdout to contain %q, got: %q", wantSubstr, output)
	}
}

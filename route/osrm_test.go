package route

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
)

// makeOSRMServer returns a test HTTP server serving the given osrmResponse as JSON.
func makeOSRMServer(t *testing.T, payload osrmResponse) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encoding OSRM response: %v", err)
		}
	}))
	return srv
}

// fetchRoutesViaServer calls the test server and parses the OSRM response.
func fetchRoutesViaServer(t *testing.T, payload osrmResponse) ([]Route, error) {
	t.Helper()
	srv := makeOSRMServer(t, payload)
	defer srv.Close()

	resp, err := http.Get(srv.URL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET test server: %v", err)
	}
	defer resp.Body.Close()

	return parseOSRMResponse(resp)
}

func TestFetchRoutes_Unit(t *testing.T) {
	// "_p~iF~ps|U_ulLnnqC_mqNvxq`@" is the canonical Google polyline example
	// encoding three points: (38.5,-120.2), (40.7,-120.95), (43.252,-126.453)
	encoded := "_p~iF~ps|U_ulLnnqC_mqNvxq`@"

	payload := osrmResponse{
		Code: "Ok",
		Routes: []osrmRoute{
			{
				Geometry: encoded,
				Duration: 3600.0,
				Distance: 500000.0,
			},
		},
	}

	routes, err := fetchRoutesViaServer(t, payload)
	if err != nil {
		t.Fatalf("parseOSRMResponse returned error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}

	r := routes[0]
	if len(r.Points) == 0 {
		t.Error("expected non-empty Points slice")
	}
	if r.Duration != 3600.0 {
		t.Errorf("Duration = %v, want 3600.0", r.Duration)
	}
	if r.Distance != 500000.0 {
		t.Errorf("Distance = %v, want 500000.0", r.Distance)
	}

	// Verify the decoded points match the canonical example.
	wantPoints := []geo.Coord{
		{Lat: 38.5, Lon: -120.2},
		{Lat: 40.7, Lon: -120.95},
		{Lat: 43.252, Lon: -126.453},
	}
	if len(r.Points) != len(wantPoints) {
		t.Fatalf("Points length = %d, want %d", len(r.Points), len(wantPoints))
	}
	const epsilon = 1e-5
	for i, want := range wantPoints {
		got := r.Points[i]
		if math.Abs(got.Lat-want.Lat) > epsilon || math.Abs(got.Lon-want.Lon) > epsilon {
			t.Errorf("Points[%d] = %v, want %v", i, got, want)
		}
	}
}

func TestFetchRoutes_Unit_NonOkCode(t *testing.T) {
	payload := osrmResponse{
		Code:   "InvalidQuery",
		Routes: nil,
	}

	_, err := fetchRoutesViaServer(t, payload)
	if err == nil {
		t.Fatal("expected error for non-Ok code, got nil")
	}
	if err.Error() != "OSRM error: InvalidQuery" {
		t.Errorf("error = %q, want %q", err.Error(), "OSRM error: InvalidQuery")
	}
}

func TestFetchRoutes_Unit_EmptyRoutes(t *testing.T) {
	payload := osrmResponse{
		Code:   "Ok",
		Routes: []osrmRoute{},
	}

	_, err := fetchRoutesViaServer(t, payload)
	if err == nil {
		t.Fatal("expected error for empty routes, got nil")
	}
	if err.Error() != "no routes found" {
		t.Errorf("error = %q, want %q", err.Error(), "no routes found")
	}
}

func TestFetchRoutes_Unit_MultipleRoutes(t *testing.T) {
	encoded := "_p~iF~ps|U_ulLnnqC_mqNvxq`@"

	payload := osrmResponse{
		Code: "Ok",
		Routes: []osrmRoute{
			{Geometry: encoded, Duration: 3600.0, Distance: 500000.0},
			{Geometry: encoded, Duration: 4200.0, Distance: 520000.0},
		},
	}

	routes, err := fetchRoutesViaServer(t, payload)
	if err != nil {
		t.Fatalf("parseOSRMResponse returned error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	for i, r := range routes {
		if len(r.Points) == 0 {
			t.Errorf("routes[%d]: expected non-empty Points slice", i)
		}
	}
}

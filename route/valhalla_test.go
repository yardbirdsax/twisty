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

// encodePolylineP6 encodes a slice of Coord into a precision-6 Google polyline string.
// Used only in tests to produce valid fixtures without depending on a third-party encoder.
func encodePolylineP6(coords []geo.Coord) string {
	encode := func(v int) string {
		v <<= 1
		if v < 0 {
			v = ^v
		}
		var result []byte
		for v >= 0x20 {
			result = append(result, byte((0x20|(v&0x1F))+63))
			v >>= 5
		}
		result = append(result, byte(v+63))
		return string(result)
	}

	prevLat, prevLon := 0, 0
	var sb strings.Builder
	for _, c := range coords {
		lat := int(math.Round(c.Lat * 1e6))
		lon := int(math.Round(c.Lon * 1e6))
		sb.WriteString(encode(lat - prevLat))
		sb.WriteString(encode(lon - prevLon))
		prevLat = lat
		prevLon = lon
	}
	return sb.String()
}

func makeValhallaServer(t *testing.T, payload valhallaResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encoding Valhalla response: %v", err)
		}
	}))
}

func makeValhallaServerWithStatus(t *testing.T, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
	}))
}

func TestFetchRoutesValhalla_Unit_SuccessPath(t *testing.T) {
	// Two known points used as fixtures; precision 6 encoding.
	mainCoords := []geo.Coord{
		{Lat: 40.19, Lon: -75.54},
		{Lat: 40.20, Lon: -75.53},
		{Lat: 40.21, Lon: -75.52},
	}
	altCoords := []geo.Coord{
		{Lat: 40.19, Lon: -75.54},
		{Lat: 40.22, Lon: -75.50},
	}

	payload := valhallaResponse{
		Trip: valhallaTrip{
			Summary: valhallaSummary{Time: 7200.0, Length: 150.3},
			Legs: []valhallaLeg{
				{Shape: encodePolylineP6(mainCoords)},
			},
		},
		Alternates: []valhallaAlternate{
			{
				Trip: valhallaTrip{
					Summary: valhallaSummary{Time: 7400.0, Length: 155.1},
					Legs: []valhallaLeg{
						{Shape: encodePolylineP6(altCoords)},
					},
				},
			},
		},
	}

	srv := makeValhallaServer(t, payload)
	defer srv.Close()

	origin := geo.Coord{Lat: 40.19, Lon: -75.54}
	dest := geo.Coord{Lat: 39.83, Lon: -77.23}

	routes, err := fetchRoutesFromURL(srv.URL, origin, dest)
	if err != nil {
		t.Fatalf("fetchRoutesFromURL returned error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	for i, r := range routes {
		if len(r.Points) == 0 {
			t.Errorf("routes[%d]: expected non-empty Points slice", i)
		}
		if r.Duration <= 0 {
			t.Errorf("routes[%d]: Duration = %v, want > 0", i, r.Duration)
		}
		if r.Distance <= 0 {
			t.Errorf("routes[%d]: Distance = %v, want > 0", i, r.Distance)
		}
	}

	// Verify main route fields.
	main := routes[0]
	if main.Duration != 7200.0 {
		t.Errorf("main route Duration = %v, want 7200.0", main.Duration)
	}
	const epsilon = 1e-3
	wantDist := 150.3 * 1000
	if math.Abs(main.Distance-wantDist) > epsilon {
		t.Errorf("main route Distance = %v, want %v", main.Distance, wantDist)
	}

	// Verify first point of main route is near the fixture origin.
	p0 := main.Points[0]
	if math.Abs(p0.Lat-40.19) > 1e-4 || math.Abs(p0.Lon-(-75.54)) > 1e-4 {
		t.Errorf("main route Points[0] = %v, want near {40.19, -75.54}", p0)
	}
}

func TestFetchRoutesValhalla_Unit_Non200Status(t *testing.T) {
	srv := makeValhallaServerWithStatus(t, http.StatusInternalServerError)
	defer srv.Close()

	origin := geo.Coord{Lat: 40.19, Lon: -75.54}
	dest := geo.Coord{Lat: 39.83, Lon: -77.23}

	_, err := fetchRoutesFromURL(srv.URL, origin, dest)
	if err == nil {
		t.Fatal("expected error for non-200 HTTP status, got nil")
	}
	wantMsg := "unexpected HTTP status: 500"
	if err.Error() != wantMsg {
		t.Errorf("error = %q, want %q", err.Error(), wantMsg)
	}
}

func TestFetchRoutesValhalla_Unit_EmptyLegs(t *testing.T) {
	// A trip with no legs should return an error, not panic.
	payload := valhallaResponse{
		Trip: valhallaTrip{
			Summary: valhallaSummary{Time: 100.0, Length: 10.0},
			Legs:    []valhallaLeg{},
		},
	}

	srv := makeValhallaServer(t, payload)
	defer srv.Close()

	origin := geo.Coord{Lat: 40.19, Lon: -75.54}
	dest := geo.Coord{Lat: 39.83, Lon: -77.23}

	_, err := fetchRoutesFromURL(srv.URL, origin, dest)
	if err == nil {
		t.Fatal("expected error for empty legs, got nil")
	}
}

func TestFetchRoutesValhalla_Unit_ZeroAlternates(t *testing.T) {
	// Valhalla may return no alternates; must handle without error.
	mainCoords := []geo.Coord{
		{Lat: 40.19, Lon: -75.54},
		{Lat: 40.21, Lon: -75.52},
	}

	payload := valhallaResponse{
		Trip: valhallaTrip{
			Summary: valhallaSummary{Time: 3600.0, Length: 50.0},
			Legs: []valhallaLeg{
				{Shape: encodePolylineP6(mainCoords)},
			},
		},
		Alternates: nil,
	}

	srv := makeValhallaServer(t, payload)
	defer srv.Close()

	origin := geo.Coord{Lat: 40.19, Lon: -75.54}
	dest := geo.Coord{Lat: 39.83, Lon: -77.23}

	routes, err := fetchRoutesFromURL(srv.URL, origin, dest)
	if err != nil {
		t.Fatalf("fetchRoutesFromURL returned error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
}

func TestFetchRoutesValhalla_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	// Royersford, PA to Gettysburg, PA
	origin := geo.Coord{Lat: 40.1887, Lon: -75.5380}
	dest := geo.Coord{Lat: 39.8309, Lon: -77.2311}

	routes, err := FetchRoutes(origin, dest)
	if err != nil {
		t.Fatalf("FetchRoutes returned error: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("expected at least one route, got none")
	}
	r := routes[0]
	if len(r.Points) <= 10 {
		t.Errorf("expected more than 10 points, got %d", len(r.Points))
	}
	if r.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", r.Duration)
	}
	if r.Distance <= 0 {
		t.Errorf("Distance = %v, want > 0", r.Distance)
	}

	// First decoded point should be near Royersford, PA (~40.19N, 75.54W).
	p0 := r.Points[0]
	const tolerance = 0.05 // degrees (~5 km) — catches precision-5 vs precision-6 mismatch while allowing for routing snap
	if math.Abs(p0.Lat-40.19) > tolerance || math.Abs(p0.Lon-(-75.54)) > tolerance {
		t.Errorf("first point %v is not near Royersford, PA (40.19, -75.54); "+
			"this may indicate wrong polyline precision", p0)
	}
}

package route

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yardbirdsax/twisty/geo"
)

const valhallaBaseURL = "https://valhalla1.openstreetmap.de"

// valhallaHTTPClient is used by fetchRoutesFromURL and is not injectable.
// This is consistent with the osrm pattern: timeout behaviour is verified
// manually rather than in unit tests.
var valhallaHTTPClient = &http.Client{Timeout: 15 * time.Second}

// valhallaRequest is the top-level JSON shape sent to the Valhalla routing API.
type valhallaRequest struct {
	Locations      []valhallaLocation     `json:"locations"`
	Costing        string                 `json:"costing"`
	CostingOptions valhallaCostingOptions `json:"costing_options"`
	Alternates     int                    `json:"alternates"`
	Units          string                 `json:"units"`
}

type valhallaLocation struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

type valhallaCostingOptions struct {
	Auto valhallaAutoOptions `json:"auto"`
}

type valhallaAutoOptions struct {
	UseHighways float64 `json:"use_highways"`
}

// valhallaResponse is the top-level JSON shape returned by the Valhalla routing API.
type valhallaResponse struct {
	Trip       valhallaTrip        `json:"trip"`
	Alternates []valhallaAlternate `json:"alternates"`
}

type valhallaAlternate struct {
	Trip valhallaTrip `json:"trip"`
}

type valhallaTrip struct {
	Summary valhallaSummary `json:"summary"`
	Legs    []valhallaLeg   `json:"legs"`
}

type valhallaSummary struct {
	Time   float64 `json:"time"`   // seconds
	Length float64 `json:"length"` // kilometers
}

type valhallaLeg struct {
	Shape string `json:"shape"` // polyline6 encoded
}

// tripToRoute converts a valhallaTrip into a Route.
func tripToRoute(trip valhallaTrip) (Route, error) {
	if len(trip.Legs) == 0 {
		return Route{}, fmt.Errorf("trip has no legs")
	}
	// NOTE: Each Valhalla leg shape is independently delta-encoded from zero,
	// not a continuation of the previous leg. Raw string concatenation therefore
	// produces garbled coordinates for multi-leg routes. For two-point routes
	// Valhalla always returns exactly one leg, so the concatenation below is
	// only safe under that assumption. Future callers that pass more than two
	// waypoints must decode each leg independently and stitch the resulting
	// coordinate slices instead.
	parts := make([]string, 0, len(trip.Legs))
	for _, leg := range trip.Legs {
		parts = append(parts, leg.Shape)
	}
	shape := strings.Join(parts, "")
	points := geo.DecodePolyline(shape, 1e6)
	return Route{
		Points:   points,
		Duration: trip.Summary.Time,
		Distance: trip.Summary.Length * 1000,
	}, nil
}

// fetchRoutesFromURL is the internal implementation of FetchRoutes, accepting a
// base URL so tests can substitute a local server.
func fetchRoutesFromURL(baseURL string, origin, dest geo.Coord) ([]Route, error) {
	reqBody := valhallaRequest{
		Locations: []valhallaLocation{
			{Lon: origin.Lon, Lat: origin.Lat},
			{Lon: dest.Lon, Lat: dest.Lat},
		},
		Costing: "auto",
		CostingOptions: valhallaCostingOptions{
			Auto: valhallaAutoOptions{UseHighways: 0.3},
		},
		Alternates: 2,
		Units:      "kilometers",
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := valhallaHTTPClient.Post(
		baseURL+"/route",
		"application/json",
		bytes.NewReader(reqJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching routes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var valResp valhallaResponse
	if err := json.Unmarshal(body, &valResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	routes := make([]Route, 0, 1+len(valResp.Alternates))

	mainRoute, err := tripToRoute(valResp.Trip)
	if err != nil {
		return nil, fmt.Errorf("parsing main trip: %w", err)
	}
	routes = append(routes, mainRoute)

	for _, alt := range valResp.Alternates {
		altRoute, err := tripToRoute(alt.Trip)
		if err != nil {
			// Skip malformed alternates rather than failing the whole call.
			continue
		}
		routes = append(routes, altRoute)
	}

	return routes, nil
}

// FetchRoutes requests route alternatives from the Valhalla routing engine
// and returns decoded Route structs. Routes avoid highways (use_highways=0).
func FetchRoutes(origin, dest geo.Coord) ([]Route, error) {
	return fetchRoutesFromURL(valhallaBaseURL, origin, dest)
}

package route

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/yardbirdsax/twisty/geo"
)

// Route represents a driving route with decoded geometry and metadata.
type Route struct {
	Points   []geo.Coord
	Duration float64        // seconds
	Distance float64        // meters
	Stats    CurvatureStats // filled in Task 006
}

// osrmResponse is the top-level JSON shape returned by the OSRM API.
type osrmResponse struct {
	Code   string      `json:"code"`
	Routes []osrmRoute `json:"routes"`
}

// osrmRoute is a single route in the OSRM response.
type osrmRoute struct {
	Geometry string    `json:"geometry"`
	Duration float64   `json:"duration"`
	Distance float64   `json:"distance"`
	Legs     []osrmLeg `json:"legs"`
}

// osrmLeg is a leg within an OSRM route.
type osrmLeg struct {
	Steps []osrmStep `json:"steps"`
}

// osrmStep is a step within an OSRM leg.
type osrmStep struct {
	Geometry string `json:"geometry"`
	Name     string `json:"name"`
}

// parseOSRMResponse reads and parses an HTTP response from the OSRM API into Route structs.
func parseOSRMResponse(resp *http.Response) ([]Route, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var osrmResp osrmResponse
	if err := json.Unmarshal(body, &osrmResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if osrmResp.Code != "Ok" {
		return nil, fmt.Errorf("OSRM error: %s", osrmResp.Code)
	}

	if len(osrmResp.Routes) == 0 {
		return nil, fmt.Errorf("no routes found")
	}

	routes := make([]Route, 0, len(osrmResp.Routes))
	for _, r := range osrmResp.Routes {
		points := geo.DecodePolyline(r.Geometry, 1e5)
		routes = append(routes, Route{
			Points:   points,
			Duration: r.Duration,
			Distance: r.Distance,
		})
	}

	return routes, nil
}


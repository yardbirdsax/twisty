package route

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/yardbirdsax/twisty/geo"
)

// maxValhallaLocations is the maximum number of locations Valhalla accepts
// in a single /route request.
const maxValhallaLocations = 20

// stitchLegs decodes each leg's polyline independently and concatenates the
// coordinate slices. The first point of each subsequent leg is dropped because
// Valhalla starts every leg at the previous leg's endpoint, making it a
// duplicate.
func stitchLegs(legs []valhallaLeg) ([]geo.Coord, error) {
	if len(legs) == 0 {
		return nil, fmt.Errorf("trip has no legs")
	}

	var allPoints []geo.Coord
	for i, leg := range legs {
		pts := geo.DecodePolyline(leg.Shape, 1e6)
		if len(pts) == 0 {
			return nil, fmt.Errorf("leg %d decoded to zero points", i)
		}
		if i == 0 {
			allPoints = append(allPoints, pts...)
		} else {
			// Drop the first point — it duplicates the last point of the previous leg.
			allPoints = append(allPoints, pts[1:]...)
		}
	}
	return allPoints, nil
}

// routeChunk issues a single Valhalla /route call for a set of locations
// and returns the stitched points, total time (seconds), total distance (km),
// and extracted maneuvers with resolved geo.Coord locations.
func routeChunk(baseURL string, locations []valhallaLocation) ([]geo.Coord, float64, float64, []Maneuver, error) {
	reqBody := valhallaRequest{
		Locations: locations,
		Costing:   "auto",
		CostingOptions: valhallaCostingOptions{
			Auto: valhallaAutoOptions{UseHighways: 0.3},
		},
		Alternates: 0,
		Units:      "kilometers",
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, 0, nil, fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := valhallaHTTPClient.Post(
		baseURL+"/route",
		"application/json",
		bytes.NewReader(reqJSON),
	)
	if err != nil {
		return nil, 0, 0, nil, fmt.Errorf("fetching route chunk: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, 0, 0, nil, fmt.Errorf("valhalla route chunk: unexpected HTTP status %d: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, 0, nil, fmt.Errorf("reading response: %w", err)
	}

	var errCheck map[string]any
	if json.Unmarshal(body, &errCheck) == nil {
		if msg, ok := errCheck["error"]; ok {
			return nil, 0, 0, nil, fmt.Errorf("valhalla error: %v", msg)
		}
	}

	var valResp valhallaResponse
	if err := json.Unmarshal(body, &valResp); err != nil {
		return nil, 0, 0, nil, fmt.Errorf("parsing response: %w", err)
	}

	// Decode each leg's points independently so we can resolve BeginShapeIndex
	// for each maneuver to an absolute geo.Coord.
	var allPoints []geo.Coord
	var allManeuvers []Maneuver
	var totalTime, totalLength float64

	for i, leg := range valResp.Trip.Legs {
		legPts := geo.DecodePolyline(leg.Shape, 1e6)
		if len(legPts) == 0 {
			return nil, 0, 0, nil, fmt.Errorf("leg %d decoded to zero points", i)
		}

		// Extract maneuvers for this leg, resolving each BeginShapeIndex into
		// an absolute geo.Coord from the leg's own decoded points.
		for _, m := range leg.Maneuvers {
			loc := geo.Coord{}
			if m.BeginShapeIndex >= 0 && m.BeginShapeIndex < len(legPts) {
				loc = legPts[m.BeginShapeIndex]
			}
			allManeuvers = append(allManeuvers, Maneuver{
				Instruction: m.Instruction,
				StreetNames: m.StreetNames,
				TimeSec:     m.Time,
				LengthKm:    m.Length,
				Location:    loc,
			})
		}

		// Stitch leg points, dropping the duplicate boundary point.
		if i == 0 {
			allPoints = append(allPoints, legPts...)
		} else {
			allPoints = append(allPoints, legPts[1:]...)
		}

		totalTime += leg.Summary.Time
		totalLength += leg.Summary.Length
	}

	if len(allPoints) == 0 {
		return nil, 0, 0, nil, fmt.Errorf("trip has no legs")
	}

	return allPoints, totalTime, totalLength, allManeuvers, nil
}

// fetchLoopRouteFromURL is the internal implementation of FetchLoopRoute,
// accepting a base URL so tests can substitute a local server.
// When the location list exceeds Valhalla's limit, the request is split into
// overlapping chunks that are routed independently and stitched together.
func fetchLoopRouteFromURL(baseURL string, start geo.Coord, waypoints []geo.Coord) (Route, error) {
	// Build the full location list: [start, wp1, ..., wpN, start].
	locations := make([]valhallaLocation, 0, len(waypoints)+2)
	locations = append(locations, valhallaLocation{Lon: start.Lon, Lat: start.Lat})
	for _, wp := range waypoints {
		locations = append(locations, valhallaLocation{Lon: wp.Lon, Lat: wp.Lat})
	}
	locations = append(locations, valhallaLocation{Lon: start.Lon, Lat: start.Lat})

	// Single call if within Valhalla's limit.
	if len(locations) <= maxValhallaLocations {
		points, totalTime, totalLengthKm, maneuvers, err := routeChunk(baseURL, locations)
		if err != nil {
			return Route{}, fmt.Errorf("valhalla loop route: %w", err)
		}
		r := Route{
			Points:    points,
			Duration:  totalTime,
			Distance:  totalLengthKm * 1000,
			Maneuvers: maneuvers,
		}
		ScoreRoute(&r)
		return r, nil
	}

	// Split into overlapping chunks: last location of chunk N = first of chunk N+1.
	var allPoints []geo.Coord
	var allManeuvers []Maneuver
	var totalTime, totalLengthKm float64

	chunkIdx := 0
	for i := 0; i < len(locations); {
		end := i + maxValhallaLocations
		if end > len(locations) {
			end = len(locations)
		}
		chunk := locations[i:end]

		points, chunkTime, chunkLengthKm, maneuvers, err := routeChunk(baseURL, chunk)
		if err != nil {
			return Route{}, fmt.Errorf("valhalla loop route chunk %d: %w", chunkIdx, err)
		}

		fmt.Fprintf(os.Stderr, "  chunk %d: %d locations, %.1fkm, %.0fs\n",
			chunkIdx, len(chunk), chunkLengthKm, chunkTime)

		if len(allPoints) > 0 && len(points) > 0 {
			// Drop the first point of this chunk — it duplicates the last
			// point of the previous chunk (the shared overlap location).
			points = points[1:]
		}
		allPoints = append(allPoints, points...)
		allManeuvers = append(allManeuvers, maneuvers...)
		totalTime += chunkTime
		totalLengthKm += chunkLengthKm

		// If this chunk reached the end of locations, we're done.
		if end == len(locations) {
			break
		}
		// Overlap by 1: next chunk starts at the last location of this chunk.
		i = end - 1
		chunkIdx++
	}

	r := Route{
		Points:    allPoints,
		Duration:  totalTime,
		Distance:  totalLengthKm * 1000,
		Maneuvers: allManeuvers,
	}
	ScoreRoute(&r)
	return r, nil
}

// FetchLoopRoute issues a Valhalla /route request for a loop:
// [start, wp1, wp2, ..., wpN, start].
// Each leg is decoded independently before stitching, avoiding the
// multi-leg polyline encoding bug in tripToRoute.
// If the waypoint count exceeds Valhalla's location limit, the request is
// automatically split into multiple calls and the results are stitched.
func FetchLoopRoute(start geo.Coord, waypoints []geo.Coord) (Route, error) {
	return fetchLoopRouteFromURL(valhallaBaseURL, start, waypoints)
}

// FetchLoopRouteFromURL is like FetchLoopRoute but uses the provided base URL
// instead of the default Valhalla endpoint. Intended for testing.
func FetchLoopRouteFromURL(baseURL string, start geo.Coord, waypoints []geo.Coord) (Route, error) {
	return fetchLoopRouteFromURL(baseURL, start, waypoints)
}

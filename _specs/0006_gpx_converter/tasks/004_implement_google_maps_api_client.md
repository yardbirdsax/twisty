# Task 004: Implement Google Maps API Client and Route Extraction

## Summary

Build the Google Maps API client responsible for extracting route data from shared links. This includes URL validation, API calls to the Directions API, and polyline decoding. The client returns structured route data for subsequent GPX generation.

## Dependencies

Task 001, Task 003 - the route data types and authentication infrastructure must be in place.

## Context: Project Structure

All new files go in the existing `gpx/` package at the project root (e.g., `gpx/url_validator.go`, `gpx/maps_client.go`). There is no `internal/` directory in this project.

**IMPORTANT:** The existing `gpx/gpx.go` defines `Waypoint` (GPX XML element) and `TrackPoint` (GPX XML element). Task 001 added `RouteWaypoint` and `TrackCoord` as the intermediate route data types. The API client works with `RouteWaypoint` and `TrackCoord`, which are then converted to the XML types during GPX generation (Task 005).

## Detailed Directions

### 1. Implement URL Validator

Create `gpx/url_validator.go`:

```go
package gpx

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateGoogleMapsURL checks if the provided URL is a valid Google Maps shared link.
func ValidateGoogleMapsURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	host := parsed.Host
	if !strings.Contains(host, "maps.google.com") &&
		!strings.Contains(host, "maps.app.goo.gl") &&
		!(strings.Contains(host, "google.com") && strings.Contains(parsed.Path, "/maps")) {
		return fmt.Errorf("not a valid Google Maps shared link. Expected format: maps.google.com/maps/dir/... or maps.app.goo.gl/...")
	}

	return nil
}
```

### 2. Implement Google Maps API Client

Create `gpx/maps_client.go`:

```go
package gpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MapsClient handles calls to the Google Maps Directions API.
type MapsClient struct {
	accessToken string
	httpClient  *http.Client
}

// NewMapsClient creates a new Google Maps API client.
func NewMapsClient(accessToken string) *MapsClient {
	return &MapsClient{
		accessToken: accessToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetRoute retrieves route data from a Google Maps shared link.
func (c *MapsClient) GetRoute(ctx context.Context, mapsURL string) (*RouteData, error) {
	if err := ValidateGoogleMapsURL(mapsURL); err != nil {
		return nil, err
	}

	waypoints, err := c.parseSharedLink(mapsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse shared link: %w", err)
	}

	if len(waypoints) < 2 {
		return nil, fmt.Errorf("shared link must contain at least an origin and destination")
	}

	routes, err := c.callDirectionsAPI(ctx, waypoints)
	if err != nil {
		return nil, err
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes found for the given waypoints")
	}

	return buildRouteData(routes[0], waypoints)
}

// parseSharedLink extracts waypoints from a Google Maps shared link URL.
func (c *MapsClient) parseSharedLink(mapsURL string) ([]string, error) {
	parsed, err := url.Parse(mapsURL)
	if err != nil {
		return nil, err
	}

	var waypoints []string

	// Check query parameters (older format: ?saddr=...&daddr=...)
	query := parsed.Query()
	if saddr := query.Get("saddr"); saddr != "" {
		waypoints = append(waypoints, saddr)
	}
	if daddr := query.Get("daddr"); daddr != "" {
		waypoints = append(waypoints, daddr)
	}
	if len(waypoints) > 0 {
		return waypoints, nil
	}

	// Extract from path (/maps/dir/origin/destination)
	path := parsed.Path
	if strings.Contains(path, "/maps/dir/") {
		parts := strings.Split(path, "/maps/dir/")
		if len(parts) > 1 {
			for _, wp := range strings.Split(parts[1], "/") {
				if wp == "" {
					continue
				}
				if decoded, err := url.QueryUnescape(wp); err == nil {
					waypoints = append(waypoints, decoded)
				} else {
					waypoints = append(waypoints, wp)
				}
			}
		}
	}

	if len(waypoints) < 2 {
		return nil, fmt.Errorf("unable to extract waypoints from shared link (need at least origin and destination)")
	}

	return waypoints, nil
}

// callDirectionsAPI calls the Google Maps Directions API.
func (c *MapsClient) callDirectionsAPI(ctx context.Context, waypoints []string) ([]directionsRoute, error) {
	params := url.Values{}
	params.Set("origin", waypoints[0])
	params.Set("destination", waypoints[len(waypoints)-1])
	params.Set("mode", "driving")
	params.Set("overview", "full")

	if len(waypoints) > 2 {
		params.Set("waypoints", strings.Join(waypoints[1:len(waypoints)-1], "|"))
	}

	reqURL := "https://maps.googleapis.com/maps/api/directions/json?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve route data from Google Maps API. Please verify the link is valid and try again: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read API response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API call failed (status %d): %s", resp.StatusCode, string(body))
	}

	var dirResp directionsAPIResponse
	if err := json.Unmarshal(body, &dirResp); err != nil {
		return nil, fmt.Errorf("parse API response: %w", err)
	}

	if dirResp.Status != "OK" {
		return nil, fmt.Errorf("API error: %s", dirResp.ErrorMessage)
	}

	return dirResp.Routes, nil
}

// buildRouteData constructs RouteData from a Directions API response.
func buildRouteData(route directionsRoute, waypoints []string) (*RouteData, error) {
	if len(route.Legs) == 0 {
		return nil, fmt.Errorf("route has no legs")
	}

	data := &RouteData{
		StartName:       waypoints[0],
		DestinationName: waypoints[len(waypoints)-1],
	}

	// Start waypoint
	data.RouteWaypoints = append(data.RouteWaypoints, RouteWaypoint{
		Name:      waypoints[0],
		Latitude:  route.Legs[0].StartLocation.Lat,
		Longitude: route.Legs[0].StartLocation.Lng,
	})

	// Intermediate waypoints (end of each leg except the last)
	for i := 0; i < len(route.Legs)-1; i++ {
		data.RouteWaypoints = append(data.RouteWaypoints, RouteWaypoint{
			Name:      waypoints[i+1],
			Latitude:  route.Legs[i].EndLocation.Lat,
			Longitude: route.Legs[i].EndLocation.Lng,
		})
	}

	// Destination waypoint
	lastLeg := route.Legs[len(route.Legs)-1]
	data.RouteWaypoints = append(data.RouteWaypoints, RouteWaypoint{
		Name:      waypoints[len(waypoints)-1],
		Latitude:  lastLeg.EndLocation.Lat,
		Longitude: lastLeg.EndLocation.Lng,
	})

	// Decode overview polyline for detailed track points
	if route.OverviewPolyline.Points != "" {
		points, err := decodePolyline(route.OverviewPolyline.Points)
		if err != nil {
			return nil, fmt.Errorf("decode polyline: %w", err)
		}
		data.TrackPoints = points
	}

	// Fallback: build track from per-leg polylines
	if len(data.TrackPoints) == 0 {
		for _, leg := range route.Legs {
			if leg.Polyline.Points == "" {
				continue
			}
			points, err := decodePolyline(leg.Polyline.Points)
			if err != nil {
				continue
			}
			data.TrackPoints = append(data.TrackPoints, points...)
		}
	}

	return data, nil
}

// decodePolyline decodes a Google Maps encoded polyline string into coordinates.
// Implements the polyline algorithm: https://developers.google.com/maps/documentation/utilities/polylinealgorithm
func decodePolyline(encoded string) ([]TrackCoord, error) {
	var points []TrackCoord
	index, lat, lng := 0, 0, 0

	for index < len(encoded) {
		result, shift := 0, 0
		for {
			if index >= len(encoded) {
				return nil, fmt.Errorf("unexpected end of encoded polyline")
			}
			b := int(encoded[index]) - 63
			index++
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
		}
		if result&1 != 0 {
			lat += ^(result >> 1)
		} else {
			lat += result >> 1
		}

		result, shift = 0, 0
		for {
			if index >= len(encoded) {
				return nil, fmt.Errorf("unexpected end of encoded polyline")
			}
			b := int(encoded[index]) - 63
			index++
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
		}
		if result&1 != 0 {
			lng += ^(result >> 1)
		} else {
			lng += result >> 1
		}

		points = append(points, TrackCoord{
			Latitude:  float64(lat) / 1e5,
			Longitude: float64(lng) / 1e5,
		})
	}

	return points, nil
}

// API response types (Google Maps Directions API format)

type directionsAPIResponse struct {
	Status       string           `json:"status"`
	ErrorMessage string           `json:"error_message"`
	Routes       []directionsRoute `json:"routes"`
}

type directionsRoute struct {
	Legs             []directionsLeg `json:"legs"`
	OverviewPolyline polylineEncoded `json:"overview_polyline"`
}

type directionsLeg struct {
	StartLocation latlng          `json:"start_location"`
	EndLocation   latlng          `json:"end_location"`
	Polyline      polylineEncoded `json:"polyline"`
}

type polylineEncoded struct {
	Points string `json:"points"`
}

type latlng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}
```

### 3. Write Unit Tests

Create `gpx/maps_client_test.go`:

```go
package gpx

import (
	"testing"
)

func TestValidateGoogleMapsURL(t *testing.T) {
	tests := []struct {
		url       string
		shouldErr bool
	}{
		{"https://maps.google.com/maps/dir/Home/Work", false},
		{"https://maps.app.goo.gl/abc123", false},
		{"https://google.com/maps/dir/A/B", false},
		{"https://example.com/maps", true},
		{"not a url", true},
	}

	for _, tt := range tests {
		err := ValidateGoogleMapsURL(tt.url)
		if (err != nil) != tt.shouldErr {
			t.Errorf("ValidateGoogleMapsURL(%q): expected error=%v, got %v", tt.url, tt.shouldErr, err)
		}
	}
}

func TestDecodePolyline(t *testing.T) {
	// Example polyline from Google's documentation
	encoded := "_p~iF~ps|U_ulLnnqC_mqNvxq`@"

	points, err := decodePolyline(encoded)
	if err != nil {
		t.Fatalf("decodePolyline failed: %v", err)
	}

	if len(points) == 0 {
		t.Error("expected points, got none")
	}

	// First point should be approximately (38.5, -120.2) per Google's docs
	if len(points) > 0 {
		if points[0].Latitude < 38 || points[0].Latitude > 39 {
			t.Errorf("first point latitude out of range: %f", points[0].Latitude)
		}
	}
}

func TestParseSharedLinkPathWaypoints(t *testing.T) {
	client := NewMapsClient("test-token")

	waypoints, err := client.parseSharedLink("https://maps.google.com/maps/dir/New%20York/Boston")
	if err != nil {
		t.Fatalf("parseSharedLink failed: %v", err)
	}

	if len(waypoints) != 2 {
		t.Errorf("expected 2 waypoints, got %d", len(waypoints))
	}
}

func TestParseSharedLinkThreeStops(t *testing.T) {
	client := NewMapsClient("test-token")

	waypoints, err := client.parseSharedLink("https://maps.google.com/maps/dir/Home/Office/Gym")
	if err != nil {
		t.Fatalf("parseSharedLink failed: %v", err)
	}

	if len(waypoints) != 3 {
		t.Errorf("expected 3 waypoints, got %d", len(waypoints))
	}
}
```

### 4. Verify Build

```bash
go build ./...
go test ./gpx/... -v
```

All tests should pass.

## Acceptance Criteria

- [ ] `ValidateGoogleMapsURL` accepts valid Google Maps URLs and rejects invalid ones
- [ ] `MapsClient.GetRoute` calls Directions API and returns `*RouteData`
- [ ] `parseSharedLink` extracts waypoints from URL path and query params
- [ ] `callDirectionsAPI` constructs proper API request with OAuth Bearer token
- [ ] `buildRouteData` creates `RouteData` with `RouteWaypoints` and decoded `TrackPoints`
- [ ] `decodePolyline` correctly decodes polyline-encoded strings into `[]TrackCoord`
- [ ] Only `driving` mode is requested
- [ ] Error messages are specific and actionable
- [ ] Unit tests pass for URL validation, polyline decoding, and waypoint parsing
- [ ] `go build ./...` compiles without errors

## Notes

- API response types are unexported (lowercase) since they are only used inside the `gpx` package.
- `RouteWaypoint` and `TrackCoord` are the intermediate types defined in Task 001's `gpx/route.go`.
- Short URLs (`maps.app.goo.gl`) cannot be parsed from path/query params — they require URL expansion before the API call. Handling of short URLs may need to be addressed separately.

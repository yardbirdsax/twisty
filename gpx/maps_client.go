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
				if strings.HasPrefix(wp, "@") {
					break
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
	Status       string            `json:"status"`
	ErrorMessage string            `json:"error_message"`
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

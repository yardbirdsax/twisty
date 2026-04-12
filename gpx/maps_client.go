package gpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MapsClient handles calls to the Google Routes API.
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

	route, err := c.callRoutesAPI(ctx, waypoints)
	if err != nil {
		return nil, err
	}

	return buildRouteData(route, waypoints)
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

const routesAPIURL = "https://routes.googleapis.com/directions/v2:computeRoutes"

// callRoutesAPI calls the Google Routes API and returns the first route.
func (c *MapsClient) callRoutesAPI(ctx context.Context, waypoints []string) (*routesRoute, error) {
	reqBody := routesRequest{
		Origin:          waypointFromString(waypoints[0]),
		Destination:     waypointFromString(waypoints[len(waypoints)-1]),
		TravelMode:      "DRIVE",
		PolylineQuality: "HIGH_QUALITY",
	}
	if len(waypoints) > 2 {
		for _, wp := range waypoints[1 : len(waypoints)-1] {
			reqBody.Intermediates = append(reqBody.Intermediates, waypointFromString(wp))
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, routesAPIURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("X-Goog-FieldMask", "routes.polyline.encodedPolyline,routes.legs.startLocation,routes.legs.endLocation")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve route from Google Routes API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Routes API call failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var routesResp routesResponse
	if err := json.Unmarshal(respBody, &routesResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(routesResp.Routes) == 0 {
		return nil, fmt.Errorf("no routes found for the given waypoints")
	}

	return &routesResp.Routes[0], nil
}

// waypointFromString converts a raw waypoint string to a routesWaypoint.
// Strings of the form "lat,lng" (both numeric) are treated as coordinates;
// all other strings are treated as addresses.
func waypointFromString(s string) routesWaypoint {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) == 2 {
		lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lng, errLng := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if errLat == nil && errLng == nil {
			return routesWaypoint{
				Location: &routesLocation{
					LatLng: routesLatLng{Latitude: lat, Longitude: lng},
				},
			}
		}
	}
	return routesWaypoint{Address: s}
}

// buildRouteData constructs RouteData from a Routes API response.
func buildRouteData(route *routesRoute, waypoints []string) (*RouteData, error) {
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
		Latitude:  route.Legs[0].StartLocation.LatLng.Latitude,
		Longitude: route.Legs[0].StartLocation.LatLng.Longitude,
	})

	// Intermediate waypoints
	for i := 0; i < len(route.Legs)-1; i++ {
		data.RouteWaypoints = append(data.RouteWaypoints, RouteWaypoint{
			Name:      waypoints[i+1],
			Latitude:  route.Legs[i].EndLocation.LatLng.Latitude,
			Longitude: route.Legs[i].EndLocation.LatLng.Longitude,
		})
	}

	// Destination waypoint
	lastLeg := route.Legs[len(route.Legs)-1]
	data.RouteWaypoints = append(data.RouteWaypoints, RouteWaypoint{
		Name:      waypoints[len(waypoints)-1],
		Latitude:  lastLeg.EndLocation.LatLng.Latitude,
		Longitude: lastLeg.EndLocation.LatLng.Longitude,
	})

	// Decode route-level polyline
	if route.Polyline.EncodedPolyline != "" {
		points, err := decodePolyline(route.Polyline.EncodedPolyline)
		if err != nil {
			return nil, fmt.Errorf("decode polyline: %w", err)
		}
		data.TrackPoints = points
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

// --- Routes API request types ---

type routesRequest struct {
	Origin          routesWaypoint   `json:"origin"`
	Destination     routesWaypoint   `json:"destination"`
	Intermediates   []routesWaypoint `json:"intermediates,omitempty"`
	TravelMode      string           `json:"travelMode"`
	PolylineQuality string           `json:"polylineQuality"`
}

type routesWaypoint struct {
	Location *routesLocation `json:"location,omitempty"`
	Address  string          `json:"address,omitempty"`
}

type routesLocation struct {
	LatLng routesLatLng `json:"latLng"`
}

type routesLatLng struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// --- Routes API response types ---

type routesResponse struct {
	Routes []routesRoute `json:"routes"`
}

type routesRoute struct {
	Legs     []routesLeg    `json:"legs"`
	Polyline routesPolyline `json:"polyline"`
}

type routesLeg struct {
	StartLocation routesLocationResult `json:"startLocation"`
	EndLocation   routesLocationResult `json:"endLocation"`
}

type routesLocationResult struct {
	LatLng routesLatLng `json:"latLng"`
}

type routesPolyline struct {
	EncodedPolyline string `json:"encodedPolyline"`
}

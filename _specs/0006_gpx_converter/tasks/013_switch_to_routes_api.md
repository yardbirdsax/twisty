# Task 013: Switch Maps Client to Google Routes API

## Summary

Replace the Directions API implementation in `gpx/maps_client.go` with the Google Routes API (`routes.googleapis.com/directions/v2:computeRoutes`). The Routes API uses a POST request with a structured JSON body, requires a field mask header, returns a different response shape, and accepts waypoints as either structured lat/lng coordinates or address strings. All affected tests must be rewritten to match the new structure.

## Dependencies

Task 011 (URL parsing fix) and Task 012 (OAuth scope fix) must be completed first.

## Context: What Changes and What Stays

**Stays the same:**
- `MapsClient` struct and `NewMapsClient` constructor
- `GetRoute` signature: `(ctx, mapsURL) (*RouteData, error)`
- `parseSharedLink` — already fixed in Task 011
- `ValidateGoogleMapsURL`
- `decodePolyline` — the Routes API uses the same encoded polyline format
- `buildRouteData` logic (start/intermediate/end waypoints, track points) — only the input types change
- The `roundTripFunc` test helper in `maps_client_test.go`

**Replaced entirely:**
- `callDirectionsAPI` → `callRoutesAPI`
- All Directions API response types (`directionsAPIResponse`, `directionsRoute`, `directionsLeg`, `polylineEncoded`, `latlng`)
- All tests that reference Directions API types

## Detailed Directions

### 1. Replace API Types in `gpx/maps_client.go`

Delete all existing unexported API types and replace with Routes API types:

```go
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
```

### 2. Add a Waypoint Helper

Add a helper that converts a raw waypoint string to a `routesWaypoint`. Strings matching the coordinate pattern `<lat>,<lng>` (both numeric, optional sign and decimal) use `location`; everything else uses `address`:

```go
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
```

Add `"strconv"` to the import block.

### 3. Replace `callDirectionsAPI` with `callRoutesAPI`

Delete `callDirectionsAPI` entirely and replace with:

```go
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
```

### 4. Update `GetRoute`

Replace the `callDirectionsAPI` call:

```go
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
```

### 5. Update `buildRouteData`

Update the signature and implementation to use `*routesRoute`:

```go
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
```

Note: the Routes API returns a single route-level polyline (no per-leg fallback needed) when `HIGH_QUALITY` is requested and the field mask includes `routes.polyline.encodedPolyline`.

### 6. Rewrite Affected Tests in `gpx/maps_client_test.go`

Delete all tests that reference the old Directions API types and replace with equivalents using the new types. Keep `roundTripFunc`, `newTestMapsClient`, `jsonResponse`, and `errResponse` unchanged — they are still valid.

Tests to rewrite (covering the same behaviors):

- `TestBuildRouteData_TwoLegs` — use `routesRoute` with two `routesLeg` entries
- `TestBuildRouteData_NoLegs` — use `routesRoute{Legs: nil}`
- `TestBuildRouteData_WithOverviewPolyline` — use `routesRoute` with `Polyline.EncodedPolyline` set
- `TestCallRoutesAPI_OK` — mock the HTTP transport; verify it POSTs to `routesAPIURL`, sets `Authorization` and `X-Goog-FieldMask` headers, and returns a `*routesRoute`
- `TestCallRoutesAPI_HTTPError` — mock a 500 response
- `TestCallRoutesAPI_NoRoutes` — mock a 200 with an empty `routes` array
- `TestGetRoute_Success` — end-to-end through `GetRoute` with a mocked transport
- `TestGetRoute_NoRoutes` — same but empty routes
- `TestGetRoute_InvalidURL` — no transport needed; bad URL short-circuits

Also add:

- `TestWaypointFromString_Coordinate` — verify `40.238122,-75.526346` produces a `routesWaypoint` with `Location` set and `Address` empty
- `TestWaypointFromString_Address` — verify `Speedway, 14233 Kutztown Rd, Fleetwood, PA 19522` produces a `routesWaypoint` with `Address` set and `Location` nil

### 7. Verify

```bash
go test ./gpx/... -v
go build ./...
```

All tests must pass. Then do an end-to-end smoke test with a real URL:

```bash
./bin/twisty gpx \
  --maps-url "https://www.google.com/maps/dir/40.238122,-75.526346/40.3625144,-75.6776312/40.445452,-75.802636/Speedway,+14233+Kutztown+Rd,+Fleetwood,+PA+19522/@40.2381255,-75.5323253,668m/data=!3m1!1e3!4m11!4m10!1m0!1m0!1m0!1m5!1m1!1s0x89c5d6cd37212bc1:0xd23839ad4c4b15f0!2m2!1d-75.8399983!2d40.4856114!3e0!5m1!1e4?entry=ttu&g_ep=EgoyMDI2MDQwOC4wIKXMDSoASAFQ" \
  --out /tmp/test.gpx
```

## Acceptance Criteria

- [ ] `callDirectionsAPI` and all Directions API types are removed from `gpx/maps_client.go`
- [ ] `callRoutesAPI` POSTs to `https://routes.googleapis.com/directions/v2:computeRoutes`
- [ ] Request body uses structured `routesWaypoint` objects (coordinate strings → `location`, address strings → `address`)
- [ ] Request sets `Content-Type: application/json`, `Authorization: Bearer <token>`, and `X-Goog-FieldMask` headers
- [ ] `buildRouteData` uses `route.Polyline.EncodedPolyline` (route-level polyline, not per-leg)
- [ ] `waypointFromString` correctly distinguishes coordinate strings from address strings
- [ ] `TestWaypointFromString_Coordinate` and `TestWaypointFromString_Address` pass
- [ ] All old Directions API tests are replaced with equivalent Routes API tests
- [ ] `go test ./gpx/... -v` passes with no failures
- [ ] `go build ./...` compiles without errors
- [ ] End-to-end smoke test produces a valid GPX file

## Notes

- The `decodePolyline` function is unchanged — both APIs use Google's encoded polyline format.
- The Routes API has no equivalent of the Directions API's `ZERO_RESULTS` status string. Error cases are indicated by HTTP status codes (4xx/5xx) or an empty `routes` array in a 200 response.
- The `X-Goog-FieldMask` header is required by the Routes API; omitting it returns a 400 error.
- The `strconv` package must be added to the import block in `maps_client.go` for `waypointFromString`.

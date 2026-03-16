# Task 005: OSRM Route Fetching Client

## Summary

Implement the OSRM client in `route/osrm.go` to fetch driving route alternatives and parse them into decoded Route structs using the polyline decoder from Task 003.

## Dependencies

Task 002 (`geo.Coord`), Task 003 (`geo.DecodePolyline`)

## Detailed Directions

### 1. Define the Route Type

In `route/osrm.go`:

```go
package route

import "github.com/yardbirdsax/twisty/geo"

type Route struct {
    Points   []geo.Coord
    Duration float64        // seconds
    Distance float64        // meters
    Stats    CurvatureStats // filled in Task 006
}
```

`CurvatureStats` can be stubbed as an empty struct for now — it will be fleshed out in Task 006.

### 2. Define the OSRM Response Types

Internal JSON types for unmarshaling the OSRM response:

```go
type osrmResponse struct {
    Code   string      `json:"code"`
    Routes []osrmRoute `json:"routes"`
}

type osrmRoute struct {
    Geometry string     `json:"geometry"`
    Duration float64    `json:"duration"`
    Distance float64    `json:"distance"`
    Legs     []osrmLeg  `json:"legs"`
}

type osrmLeg struct {
    Steps []osrmStep `json:"steps"`
}

type osrmStep struct {
    Geometry string `json:"geometry"`
    Name     string `json:"name"`
}
```

### 3. Implement FetchRoutes

```go
// FetchRoutes requests route alternatives from the public OSRM demo server
// and returns decoded Route structs.
func FetchRoutes(origin, dest geo.Coord) ([]Route, error)
```

URL construction:
```
https://router.project-osrm.org/route/v1/driving/{originLon},{originLat};{destLon},{destLat}?alternatives=true&overview=full&geometries=polyline&steps=true
```

Note: OSRM uses `longitude,latitude` order (reversed).

After receiving a 200 response:
1. Unmarshal into `osrmResponse`.
2. If `code \!= "Ok"`: return error `OSRM error: <code>`.
3. If `routes` is empty: return error `no routes found`.
4. For each route: call `geo.DecodePolyline(route.Geometry)` and populate a `Route` struct.

Use an `http.Client` with a 15-second timeout.

### 4. Write Unit Tests

Create `route/osrm_test.go`. Because this function makes a live network request, write one integration test (guarded by a build tag or skipped unless `-integration` is passed):

```go
func TestFetchRoutes_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    // SF to SJ
    origin := geo.Coord{Lat: 37.7749, Lon: -122.4194}
    dest   := geo.Coord{Lat: 37.3382, Lon: -121.8863}
    routes, err := FetchRoutes(origin, dest)
    // ... assertions
}
```

Also write a unit test that mocks the HTTP response using `httptest.NewServer` to test the parsing logic without network access.

## Acceptance Criteria

- [ ] `go build ./...` succeeds
- [ ] Unit test with mocked HTTP response passes (`go test ./route/ -run TestFetchRoutes_Unit`)
- [ ] `FetchRoutes` returns an error (not a panic) when OSRM returns a non-"Ok" code
- [ ] `FetchRoutes` returns an error when OSRM returns 0 routes
- [ ] Each returned `Route` has a non-empty `Points` slice
- [ ] `Duration` and `Distance` are populated from the OSRM response

## Notes

- Do not make rapid repeated requests to the OSRM demo server during development.
- The OSRM demo server is a shared resource; use it conservatively in tests.
- For the unit test, construct a minimal valid OSRM JSON response with a known encoded polyline.
- **Go 1.26:** This project requires Go 1.26. Use `io.ReadAll(resp.Body)` to read the HTTP response body before unmarshaling — `io.ReadAll` received a ~2× speedup and ~50% allocation reduction in this release compared to earlier versions.

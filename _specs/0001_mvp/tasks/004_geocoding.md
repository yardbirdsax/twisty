# Task 004: Nominatim Geocoding Client

## Summary

Implement the `geocode` package: classify user input as raw coordinates or an address, geocode addresses via Nominatim, and print resolution/disambiguation output.

## Dependencies

Task 001 (project structure)

## Detailed Directions

### 1. Define the Result Type

In `geocode/nominatim.go`:

```go
package geocode

import "github.com/yardbirdsax/twisty/geo"

type Result struct {
    Lat         float64
    Lon         float64
    DisplayName string
}
```

### 2. Implement Input Classification

```go
// Classify returns (true, lat, lon) if s looks like "lat,lon" coordinates,
// or (false, 0, 0) if it should be treated as an address.
// If it looks like coordinates but fails to parse, it returns an error.
func Classify(s string) (isCoord bool, lat, lon float64, err error)
```

Rules:
- Split on `,`. If not exactly 2 parts → return `isCoord=false`.
- Try `strconv.ParseFloat` on both parts.
- If both succeed → `isCoord=true, lat=parts[0], lon=parts[1]`.
- If either fails → return `isCoord=false, err=nil` (treat as address string).
- Exception: if the string contains exactly one comma AND one side parses but the other doesn't → return `err` with message `Bad input: expected lat,lon`.

Simpler rule per spec: split on comma; if exactly 2 parts and both parse as float64 → coords. Otherwise → address.

### 3. Implement Nominatim Geocode

```go
// Geocode queries Nominatim for the given address string and returns the
// best result. It prints resolution or disambiguation output to stdout.
// label is "Origin" or "Destination" for display purposes.
func Geocode(address, label string) (Result, error)
```

Request:
```
GET https://nominatim.openstreetmap.org/search?q=<url_encoded_address>&format=jsonv2&limit=5
User-Agent: twistrouter/1.0
```

Parse the JSON response into a slice of:
```go
type nominatimResult struct {
    Lat         string  `json:"lat"`
    Lon         string  `json:"lon"`
    DisplayName string  `json:"display_name"`
    Importance  float64 `json:"importance"`
}
```

Behavior:
- **0 results:** return error `could not geocode <label> "<address>" — no results found`.
- **1 result:** print `<label>: "<address>" → <DisplayName> (<lat>, <lon>)`, return result.
- **2+ results:** use the first result. Print:
  ```
  Origin: "Springfield" → Springfield, Illinois, United States (39.7817, -89.6501)
    Also matched: Springfield, Missouri, United States (37.2090, -93.2923)
    Also matched: Springfield, Massachusetts, United States (42.1015, -72.5898)
    (Use coordinates directly to override, e.g. -origin "39.7817,-89.6501")
  ```
  Print at most 2 "Also matched" lines (i.e., top 3 total).

### 4. Implement Resolve (the main entry point)

```go
// Resolve classifies the input and either returns parsed coords directly
// or calls Geocode. label is "Origin" or "Destination".
func Resolve(input, label string) (Result, error)
```

- If `Classify` returns `isCoord=true`: print `<label>: <lat>, <lon> (coordinates, no geocoding)` and return.
- If `Classify` returns an error: return the error.
- Otherwise: call `Geocode`.

### 5. Rate Limiting

`Resolve` itself does not sleep. The caller (`main.go`) is responsible for sleeping 1 second between the two `Resolve` calls if both inputs require geocoding. Export a helper:

```go
// NeedsGeocode returns true if the input will require a Nominatim request.
func NeedsGeocode(input string) bool
```

## Acceptance Criteria

- [ ] `Classify("37.7749,-122.4194")` returns `isCoord=true` with correct lat/lon
- [ ] `Classify("San Francisco, CA")` returns `isCoord=false`
- [ ] `Geocode` sends `User-Agent: twistrouter/1.0`
- [ ] Single-result geocoding prints the confirmation line
- [ ] Multi-result geocoding prints disambiguation with at most 2 "Also matched" lines
- [ ] Zero results returns a non-nil error and does not print any result
- [ ] `go build ./...` succeeds

## Notes

- `lat` and `lon` in Nominatim responses are JSON strings — parse with `strconv.ParseFloat`.
- Use `net/url.QueryEscape` for the address parameter.
- Create an `http.Client` with a 10-second timeout.
- Do not share an `http.Client` across packages; create one locally.
- **Go 1.26:** This project requires Go 1.26. Use `io.ReadAll(resp.Body)` to read HTTP response bodies — it received a ~2× speedup and ~50% allocation reduction in this release compared to earlier versions. Also available in Go 1.26: `errors.AsType[T](err)` in the `errors` package, a generic alternative to `errors.As()` that returns the typed value directly:
  ```go
  // Instead of:
  var urlErr *url.Error
  if errors.As(err, &urlErr) { ... }
  // Use:
  if urlErr, ok := errors.AsType[*url.Error](err); ok { ... }
  ```

package geocode

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yardbirdsax/twisty/geo"
)

var defaultClient = &http.Client{Timeout: 10 * time.Second}

// Result holds a resolved geographic location.
type Result struct {
	Lat         float64
	Lon         float64
	DisplayName string
}

// ToCoord converts a Result to a geo.Coord.
func (r Result) ToCoord() geo.Coord {
	return geo.Coord{Lat: r.Lat, Lon: r.Lon}
}

// nominatimResult is the JSON shape returned by the Nominatim search API.
type nominatimResult struct {
	Lat         string  `json:"lat"`
	Lon         string  `json:"lon"`
	DisplayName string  `json:"display_name"`
	Importance  float64 `json:"importance"`
}

// Classify returns (true, lat, lon) if s looks like "lat,lon" coordinates,
// or (false, 0, 0) if it should be treated as an address.
// If it looks like coordinates but fails to parse, it returns an error.
func Classify(s string) (isCoord bool, lat, lon float64, err error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return false, 0, 0, nil
	}
	latVal, latErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lonVal, lonErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if latErr == nil && lonErr == nil {
		return true, latVal, lonVal, nil
	}
	if latErr == nil || lonErr == nil {
		return false, 0, 0, fmt.Errorf("Bad input: expected lat,lon")
	}
	return false, 0, 0, nil
}

// parseLatLon parses the Lat and Lon string fields of a nominatimResult into float64 values.
func parseLatLon(r nominatimResult) (float64, float64, error) {
	lat, err := strconv.ParseFloat(r.Lat, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing lat %q: %w", r.Lat, err)
	}
	lon, err := strconv.ParseFloat(r.Lon, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing lon %q: %w", r.Lon, err)
	}
	return lat, lon, nil
}

// NeedsGeocode is a convenience wrapper that returns true if the input requires a geocoding request, without exposing Classify's full return values.
func NeedsGeocode(input string) bool {
	isCoord, _, _, err := Classify(input)
	if err != nil {
		return false
	}
	return !isCoord
}

// processNominatimResults converts raw Nominatim results into a Result,
// printing confirmation or disambiguation output to stdout.
func processNominatimResults(results []nominatimResult, address, label string) (Result, error) {
	if len(results) == 0 {
		return Result{}, fmt.Errorf("could not geocode %s %q — no results found", label, address)
	}

	best := results[0]
	bestLat, bestLon, err := parseLatLon(best)
	if err != nil {
		return Result{}, err
	}

	fmt.Printf("%s: %q → %s (%.4f, %.4f)\n", label, address, best.DisplayName, bestLat, bestLon)

	if len(results) > 1 {
		limit := min(len(results)-1, 2)
		for _, r := range results[1 : 1+limit] {
			rLat, rLon, err := parseLatLon(r)
			if err != nil {
				continue
			}
			fmt.Printf("  Also matched: %s (%.4f, %.4f)\n", r.DisplayName, rLat, rLon)
		}
		fmt.Printf("  (Use coordinates directly to override, e.g. -%s \"%.4f,%.4f\")\n",
			strings.ToLower(label), bestLat, bestLon)
	}

	return Result{Lat: bestLat, Lon: bestLon, DisplayName: best.DisplayName}, nil
}

// Geocode queries Nominatim for the given address string and returns the
// best result. It prints resolution or disambiguation output to stdout.
// label is "Origin" or "Destination" for display purposes.
func Geocode(address, label string) (Result, error) {
	u := url.URL{
		Scheme: "https",
		Host:   "nominatim.openstreetmap.org",
		Path:   "/search",
	}
	q := url.Values{}
	q.Set("q", address)
	q.Set("format", "jsonv2")
	q.Set("limit", "5")
	u.RawQuery = q.Encode()
	reqURL := u.String()

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "twisty/1.0")

	resp, err := defaultClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("geocoding %s %q: %w", label, address, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("reading response: %w", err)
	}

	var results []nominatimResult
	if err := json.Unmarshal(body, &results); err != nil {
		return Result{}, fmt.Errorf("parsing response: %w", err)
	}

	return processNominatimResults(results, address, label)
}

// Resolve classifies the input and either returns parsed coords directly
// or calls Geocode. label is "Origin" or "Destination".
func Resolve(input, label string) (Result, error) {
	isCoord, lat, lon, err := Classify(input)
	if err != nil {
		return Result{}, err
	}
	if isCoord {
		fmt.Printf("%s: %g, %g (coordinates, no geocoding)\n", label, lat, lon)
		return Result{Lat: lat, Lon: lon}, nil
	}
	return Geocode(input, label)
}

package geocode

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClassify_Coordinates(t *testing.T) {
	isCoord, lat, lon, err := Classify("37.7749,-122.4194")
	if err != nil {
		t.Fatalf("Classify returned unexpected error: %v", err)
	}
	if !isCoord {
		t.Fatal("Classify: want isCoord=true, got false")
	}
	if lat != 37.7749 {
		t.Errorf("lat = %v, want 37.7749", lat)
	}
	if lon != -122.4194 {
		t.Errorf("lon = %v, want -122.4194", lon)
	}
}

func TestClassify_Address(t *testing.T) {
	isCoord, lat, lon, err := Classify("San Francisco, CA")
	if err != nil {
		t.Fatalf("Classify returned unexpected error: %v", err)
	}
	if isCoord {
		t.Fatal("Classify: want isCoord=false, got true")
	}
	if lat != 0 || lon != 0 {
		t.Errorf("expected lat=0 lon=0 for address, got lat=%v lon=%v", lat, lon)
	}
}

func TestClassify_MultiComma(t *testing.T) {
	isCoord, _, _, err := Classify("Springfield, IL, USA")
	if err != nil {
		t.Fatalf("Classify returned unexpected error: %v", err)
	}
	if isCoord {
		t.Fatal("Classify: three-part string should not be coords")
	}
}

func TestClassify_NoComma(t *testing.T) {
	isCoord, _, _, err := Classify("London")
	if err != nil {
		t.Fatalf("Classify returned unexpected error: %v", err)
	}
	if isCoord {
		t.Fatal("Classify: no-comma string should not be coords")
	}
}

func TestClassify_BadInput(t *testing.T) {
	_, _, _, err := Classify("37.7749,notacoord")
	if err == nil {
		t.Fatal("Classify: expected error for bad coord, got nil")
	}
	if !strings.Contains(err.Error(), "Bad input: expected lat,lon") {
		t.Errorf("error message %q does not contain 'Bad input: expected lat,lon'", err.Error())
	}
}

func TestNeedsGeocode(t *testing.T) {
	if NeedsGeocode("37.7749,-122.4194") {
		t.Error("NeedsGeocode: coord input should return false")
	}
	if !NeedsGeocode("San Francisco, CA") {
		t.Error("NeedsGeocode: address input should return true")
	}
}

// makeServer returns a test HTTP server that serves Nominatim-shaped JSON.
func makeServer(t *testing.T, results []nominatimResult) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify User-Agent header
		if ua := r.Header.Get("User-Agent"); ua != "twistrouter/1.0" {
			t.Errorf("User-Agent = %q, want twistrouter/1.0", ua)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	return srv
}

// geocodeWithURL calls nominatim at a custom base URL (for testing).
func geocodeWithURL(address, label, baseURL string) (Result, error) {
	client := &http.Client{}
	reqURL := baseURL + "/search?q=" + url.QueryEscape(address) + "&format=jsonv2&limit=5"
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "twistrouter/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}
	var raw []nominatimResult
	if err := json.Unmarshal(body, &raw); err != nil {
		return Result{}, err
	}
	return processNominatimResults(raw, address, label)
}

func TestGeocode_UserAgentAndSingleResult(t *testing.T) {
	results := []nominatimResult{
		{Lat: "37.7749", Lon: "-122.4194", DisplayName: "San Francisco, California, United States", Importance: 0.9},
	}
	srv := makeServer(t, results)
	defer srv.Close()

	got, err := geocodeWithURL("San+Francisco", "Origin", srv.URL)
	if err != nil {
		t.Fatalf("geocodeWithURL returned error: %v", err)
	}
	if got.Lat != 37.7749 || got.Lon != -122.4194 {
		t.Errorf("Result = %+v, want lat=37.7749 lon=-122.4194", got)
	}
	if got.DisplayName != "San Francisco, California, United States" {
		t.Errorf("DisplayName = %q", got.DisplayName)
	}
}

func TestGeocode_ZeroResults(t *testing.T) {
	srv := makeServer(t, []nominatimResult{})
	defer srv.Close()

	_, err := geocodeWithURL("nowhere", "Destination", srv.URL)
	if err == nil {
		t.Fatal("expected error for zero results, got nil")
	}
	if !strings.Contains(err.Error(), "no results found") {
		t.Errorf("error message %q does not contain 'no results found'", err.Error())
	}
}

func TestGeocode_MultipleResults(t *testing.T) {
	results := []nominatimResult{
		{Lat: "39.7817", Lon: "-89.6501", DisplayName: "Springfield, Illinois, United States", Importance: 0.8},
		{Lat: "37.2090", Lon: "-93.2923", DisplayName: "Springfield, Missouri, United States", Importance: 0.7},
		{Lat: "42.1015", Lon: "-72.5898", DisplayName: "Springfield, Massachusetts, United States", Importance: 0.6},
	}
	srv := makeServer(t, results)
	defer srv.Close()

	got, err := geocodeWithURL("Springfield", "Origin", srv.URL)
	if err != nil {
		t.Fatalf("geocodeWithURL returned error: %v", err)
	}
	// Best result should be the first
	if got.Lat != 39.7817 || got.Lon != -89.6501 {
		t.Errorf("Result = %+v, want lat=39.7817 lon=-89.6501", got)
	}
}

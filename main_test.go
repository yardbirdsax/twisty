package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/quality"
)

func TestTermProgressBarStatsString(t *testing.T) {
	tests := []struct {
		name        string
		bar         *termProgressBar
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "no retries no fetches",
			bar:  &termProgressBar{cached: 2, fetched: 3},
			wantContain: []string{"2 cached", "3 fetched"},
			wantAbsent:  []string{"retry", "last:", "avg:"},
		},
		{
			name:        "one retry singular",
			bar:         &termProgressBar{cached: 0, fetched: 1, retries: 1},
			wantContain: []string{"1 retry"},
			wantAbsent:  []string{"retries"},
		},
		{
			name:        "multiple retries plural",
			bar:         &termProgressBar{cached: 0, fetched: 1, retries: 3},
			wantContain: []string{"3 retries"},
			wantAbsent:  []string{"3 retry"},
		},
		{
			name: "one fetch duration",
			bar: &termProgressBar{
				cached:             0,
				fetched:            1,
				fetchCount:         1,
				lastFetchDuration:  8300 * time.Millisecond,
				totalFetchDuration: 8300 * time.Millisecond,
			},
			wantContain: []string{"last: 8.3s", "avg: 8.3s"},
		},
		{
			name: "multiple fetch durations averages correctly",
			bar: &termProgressBar{
				cached:             0,
				fetched:            2,
				fetchCount:         2,
				lastFetchDuration:  4000 * time.Millisecond,
				totalFetchDuration: 10000 * time.Millisecond,
			},
			wantContain: []string{"last: 4.0s", "avg: 5.0s"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.bar.statsString()
			for _, want := range tc.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("statsString() = %q, want it to contain %q", got, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("statsString() = %q, want it NOT to contain %q", got, absent)
				}
			}
		})
	}
}

func TestTermProgressBarRetryRendersImmediately(t *testing.T) {
	var buf bytes.Buffer
	b := &termProgressBar{w: &buf, total: 5, current: 2, cached: 1, fetched: 1}
	b.Retry()
	if b.retries != 1 {
		t.Errorf("Retry() retries = %d, want 1", b.retries)
	}
	out := buf.String()
	if !strings.Contains(out, "1 retry") {
		t.Errorf("Retry() rendered output %q, want it to contain \"1 retry\"", out)
	}
}

func TestTermProgressBarFetchDurationAccumulates(t *testing.T) {
	b := &termProgressBar{}
	b.FetchDuration(5 * time.Second)
	b.FetchDuration(3 * time.Second)
	if b.fetchCount != 2 {
		t.Errorf("fetchCount = %d, want 2", b.fetchCount)
	}
	if b.lastFetchDuration != 3*time.Second {
		t.Errorf("lastFetchDuration = %v, want 3s", b.lastFetchDuration)
	}
	if b.totalFetchDuration != 8*time.Second {
		t.Errorf("totalFetchDuration = %v, want 8s", b.totalFetchDuration)
	}
}

func TestScoreFlagSetParsesValidFlags(t *testing.T) {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	address := fs.String("address", "", "")
	radius := fs.Float64("radius", 25.0, "")
	tileSize := fs.Float64("tile-size", 0.05, "")
	cacheDir := fs.String("cache-dir", "", "")
	noCache := fs.Bool("no-cache", false, "")
	clearScoreCache := fs.Bool("clear-score-cache", false, "")
	verbose := fs.Bool("v", false, "")
	outPath := fs.String("out", "", "")
	minScore := fs.Float64("min-score", 0, "")
	multiColor := fs.Bool("multi-color", false, "")

	err := fs.Parse([]string{
		"-address", "Asheville, NC",
		"-radius", "30",
		"-tile-size", "0.1",
		"-cache-dir", "/tmp/tiles",
		"-no-cache",
		"-clear-score-cache",
		"-v",
		"-out", "roads.kml",
		"-min-score", "300",
		"-multi-color",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if *address != "Asheville, NC" {
		t.Errorf("address = %q, want %q", *address, "Asheville, NC")
	}
	if *radius != 30.0 {
		t.Errorf("radius = %v, want 30.0", *radius)
	}
	if *tileSize != 0.1 {
		t.Errorf("tile-size = %v, want 0.1", *tileSize)
	}
	if *cacheDir != "/tmp/tiles" {
		t.Errorf("cache-dir = %q, want %q", *cacheDir, "/tmp/tiles")
	}
	if !*noCache {
		t.Error("no-cache should be true")
	}
	if !*clearScoreCache {
		t.Error("clear-score-cache should be true")
	}
	if !*verbose {
		t.Error("v should be true")
	}
	if *outPath != "roads.kml" {
		t.Errorf("out = %q, want %q", *outPath, "roads.kml")
	}
	if *minScore != 300.0 {
		t.Errorf("min-score = %v, want 300.0", *minScore)
	}
	if !*multiColor {
		t.Error("multi-color should be true")
	}
}

func TestScoreFlagSetAddressRequired(t *testing.T) {
	var stderr bytes.Buffer
	err := runScore([]string{}, &stderr)
	if err == nil {
		t.Fatal("runScore() expected error when -address is not provided, got nil")
	}
	if !strings.Contains(err.Error(), "-address is required") {
		t.Errorf("runScore() error = %q, want it to contain \"-address is required\"", err.Error())
	}
}

func TestScoreOutFlagRequired(t *testing.T) {
	var stderr bytes.Buffer
	err := runScore([]string{"-address", "Anywhere"}, &stderr)
	if err == nil {
		t.Fatal("runScore() expected error when -out is not provided, got nil")
	}
	if !strings.Contains(err.Error(), "-out is required") {
		t.Errorf("runScore() error = %q, want it to contain \"-out is required\"", err.Error())
	}
}

func TestScoreMinScoreDefaultsToZero(t *testing.T) {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	minScore := fs.Float64("min-score", 0, "")
	if err := fs.Parse([]string{}); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if *minScore != 0 {
		t.Errorf("min-score default = %v, want 0", *minScore)
	}
}

func TestScoreRadiusValidation(t *testing.T) {
	tests := []struct {
		name    string
		radius  string
		wantErr bool
	}{
		{"valid radius", "25", false},
		{"min boundary valid", "0.1", false},
		{"max boundary valid", "50", false},
		{"radius large", "51", false},
		{"radius zero", "0", true},
		{"radius negative", "-5", true},
	}

	// Stub Overpass server that returns an empty result immediately.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"elements":[]}`))
	}))
	defer stub.Close()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			err := runScore([]string{"-address", "0.0,0.0", "-out", "roads.kml", "-radius", tc.radius, "-overpass-url", stub.URL, "-tile-size", "100"}, &stderr)
			// Valid radii will fail later (geocoding), but should NOT fail on radius validation.
			// Invalid radii should fail with a radius error.
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for radius %s, got nil", tc.radius)
				} else if !strings.Contains(err.Error(), "-radius") {
					t.Errorf("expected radius error for %s, got: %v", tc.radius, err)
				}
			} else {
				if err != nil && strings.Contains(err.Error(), "-radius") {
					t.Errorf("unexpected radius error for %s: %v", tc.radius, err)
				}
			}
		})
	}
}

// TestRunScoreE2EWithSyntheticCache verifies the full runScore flow produces a KML file
// when synthetic cached tile data is present. Geocoding is bypassed by passing coordinates
// directly (geocode.Resolve short-circuits when the address is "lat,lon").
func TestRunScoreE2EWithSyntheticCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Pick a stable center and tile parameters.
	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 1.0  // km
		tileSize  = 0.05 // degrees
	)

	// Determine which tiles runScore will request for this center.
	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)
	if len(tiles) == 0 {
		t.Fatal("ComputeTiles returned no tiles")
	}

	// Build a minimal but valid Overpass JSON payload containing a winding road.
	// The way must survive HardFilter (paved secondary, no disqualifying tags).
	syntheticWay := map[string]any{
		"id":   int64(42),
		"tags": map[string]string{"highway": "secondary"},
		"geometry": []map[string]float64{
			{"lat": centerLat, "lon": centerLon},
			{"lat": centerLat + 0.0002, "lon": centerLon + 0.0001},
			{"lat": centerLat + 0.0003, "lon": centerLon - 0.0001},
			{"lat": centerLat + 0.0005, "lon": centerLon + 0.0001},
		},
	}
	payload := map[string]any{
		"elements": []any{syntheticWay},
	}
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal synthetic tile JSON: %v", err)
	}

	// Write the synthetic JSON to a temp tile cache directory for every tile.
	cacheDir := t.TempDir()
	tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, tile := range tiles {
		if err := tileCache.Write(tile, rawJSON); err != nil {
			t.Fatalf("TileCache.Write tile(%v,%v): %v", tile.South, tile.West, err)
		}
	}

	// Prepare output KML path.
	outPath := filepath.Join(t.TempDir(), "output.kml")

	// Run score using coordinate address to bypass geocoding.
	var stderr bytes.Buffer
	err = runScore([]string{
		"-address", "35.0,-82.0",
		"-radius", "1",
		"-tile-size", "0.05",
		"-cache-dir", cacheDir,
		"-out", outPath,
		"-no-cache", // skip score cache reads so the pipeline always runs
	}, &stderr)
	if err != nil {
		t.Fatalf("runScore returned error: %v\nstderr: %s", err, stderr.String())
	}

	// Verify the KML file exists and is non-empty.
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("KML output file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("KML output file is empty")
	}

	// Verify it looks like KML.
	contents, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading KML output: %v", err)
	}
	if !strings.Contains(string(contents), "<kml") {
		t.Errorf("output file does not contain <kml> element; got: %s", string(contents)[:min(200, len(contents))])
	}
}

// TestRunScore_DefaultSingleColorOutput verifies that without -multi-color, the
// default KML output uses single-color per-road rendering (no shared tier styles).
func TestRunScore_DefaultSingleColorOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 1.0
		tileSize  = 0.05
	)

	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)
	if len(tiles) == 0 {
		t.Fatal("ComputeTiles returned no tiles")
	}

	syntheticWay := map[string]any{
		"id":   int64(42),
		"tags": map[string]string{"highway": "secondary"},
		"geometry": []map[string]float64{
			{"lat": centerLat, "lon": centerLon},
			{"lat": centerLat + 0.0002, "lon": centerLon + 0.0001},
			{"lat": centerLat + 0.0003, "lon": centerLon - 0.0001},
			{"lat": centerLat + 0.0005, "lon": centerLon + 0.0001},
		},
	}
	payload := map[string]any{"elements": []any{syntheticWay}}
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal synthetic tile JSON: %v", err)
	}

	cacheDir := t.TempDir()
	tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, tile := range tiles {
		if err := tileCache.Write(tile, rawJSON); err != nil {
			t.Fatalf("TileCache.Write: %v", err)
		}
	}

	outPath := filepath.Join(t.TempDir(), "output.kml")
	var stderr bytes.Buffer
	// Default — no -multi-color flag
	err = runScore([]string{
		"-address", "35.0,-82.0",
		"-radius", "1",
		"-tile-size", "0.05",
		"-cache-dir", cacheDir,
		"-out", outPath,
		"-no-cache",
	}, &stderr)
	if err != nil {
		t.Fatalf("runScore returned error: %v\nstderr: %s", err, stderr.String())
	}

	contents, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading KML output: %v", err)
	}
	// Single-color output should NOT contain shared tier style definitions like "#tier0"
	// It renders each road with an inline style, not a shared style URL
	if strings.Contains(string(contents), "<styleUrl>#tier") {
		t.Errorf("default output contains tier-based styleUrl; expected single-color rendering")
	}
}

// TestRunScore_MultiColorFlag verifies that -multi-color produces per-segment
// tier-based coloring.
func TestRunScore_MultiColorFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		centerLat = 35.0
		centerLon = -82.0
		radius    = 1.0
		tileSize  = 0.05
	)

	tiles := quality.ComputeTiles(centerLat, centerLon, radius, tileSize)
	if len(tiles) == 0 {
		t.Fatal("ComputeTiles returned no tiles")
	}

	syntheticWay := map[string]any{
		"id":   int64(42),
		"tags": map[string]string{"highway": "secondary"},
		"geometry": []map[string]float64{
			{"lat": centerLat, "lon": centerLon},
			{"lat": centerLat + 0.0002, "lon": centerLon + 0.0001},
			{"lat": centerLat + 0.0003, "lon": centerLon - 0.0001},
			{"lat": centerLat + 0.0005, "lon": centerLon + 0.0001},
		},
	}
	payload := map[string]any{"elements": []any{syntheticWay}}
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal synthetic tile JSON: %v", err)
	}

	cacheDir := t.TempDir()
	tileCache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := tileCache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, tile := range tiles {
		if err := tileCache.Write(tile, rawJSON); err != nil {
			t.Fatalf("TileCache.Write: %v", err)
		}
	}

	outPath := filepath.Join(t.TempDir(), "output.kml")
	var stderr bytes.Buffer
	err = runScore([]string{
		"-address", "35.0,-82.0",
		"-radius", "1",
		"-tile-size", "0.05",
		"-cache-dir", cacheDir,
		"-out", outPath,
		"-no-cache",
		"-multi-color",
	}, &stderr)
	if err != nil {
		t.Fatalf("runScore returned error: %v\nstderr: %s", err, stderr.String())
	}

	contents, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading KML output: %v", err)
	}
	// Multi-color output uses shared tier styles
	if !strings.Contains(string(contents), "tier") {
		t.Errorf("-multi-color output does not contain tier style definitions; expected per-segment tier coloring")
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"90d", 90 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"6m", 6 * 30 * 24 * time.Hour, false},
		{"1m", 30 * 24 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * 30 * 24 * time.Hour, false}, // Nm suffix always means months
		{"0d", 0, true},
		{"0m", 0, true},
		{"-1d", 0, true},
		{"", 0, true},
		{"abc", 0, true},
		{"1h30m", 0, true},  // ends in 'm' with non-integer prefix; user likely meant minutes, but 'm' means months
		{"30d30m", 0, true}, // ends in 'm' with non-integer prefix "30d"
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDuration(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseDuration(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseDuration(%q) unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

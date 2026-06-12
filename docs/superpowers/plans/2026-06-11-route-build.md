# Route Build Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `twisty build` command that serves an interactive Leaflet-based web UI for constructing driving routes by clicking waypoints, with live Valhalla routing and async tile-based curvature scoring.

**Architecture:** Single Go file (`route_build.go`) with embedded HTML/JS/CSS, following the `diag_serve.go` pattern. Backend exposes JSON API endpoints called by the frontend on each user interaction. Scoring runs asynchronously using the existing tile pipeline with cache-first behavior.

**Tech Stack:** Go stdlib `net/http`, Cobra CLI, Leaflet 1.9.4 (CDN), existing `route/`, `quality/`, `geo/`, `geocode/`, `gpx/` packages.

**Design Decision — Command Name:** The existing `twisty route` command is a leaf command with `RunE` directly on it. Converting it to a parent command would be a breaking change. Instead, register this as `twisty build` at the top level. This avoids restructuring the existing route command and keeps the CLI backwards-compatible.

---

### Task 1: Register the `build` command with flags

**Files:**
- Create: `route_build.go`
- Modify: `main.go:46-62` (add to root command)

- [ ] **Step 1: Write the failing test**

Create a test that verifies the command exists and parses flags correctly.

```go
// route_build_test.go
package main

import (
	"testing"
)

func TestNewBuildCmd_defaults(t *testing.T) {
	cmd := newBuildCmd()
	if cmd.Use != "build" {
		t.Fatalf("expected Use='build', got %q", cmd.Use)
	}

	f := cmd.Flags()
	port, err := f.GetInt("port")
	if err != nil {
		t.Fatalf("port flag: %v", err)
	}
	if port != 8080 {
		t.Fatalf("expected default port=8080, got %d", port)
	}

	addr, err := f.GetString("address")
	if err != nil {
		t.Fatalf("address flag: %v", err)
	}
	if addr != "" {
		t.Fatalf("expected default address='', got %q", addr)
	}

	overpassURL, err := f.GetString("overpass-url")
	if err != nil {
		t.Fatalf("overpass-url flag: %v", err)
	}
	if overpassURL != "https://overpass-api.de/api/interpreter" {
		t.Fatalf("expected default overpass-url, got %q", overpassURL)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestNewBuildCmd_defaults -v .`
Expected: FAIL — `newBuildCmd` not defined.

- [ ] **Step 3: Write the command skeleton**

```go
// route_build.go
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/yardbirdsax/twisty/quality"
)

type buildParams struct {
	address    string
	port       int
	overpassURL string
	cacheDir   string
	tileSize   float64
	fetchDelay string
	verbose    bool
}

func execBuild(p buildParams) error {
	return fmt.Errorf("not implemented")
}

func newBuildCmd() *cobra.Command {
	var p buildParams

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Interactively build a route on a map with live scoring",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execBuild(p)
		},
	}
	f := cmd.Flags()
	f.StringVar(&p.address, "address", "", "Center address for initial map view (required)")
	f.IntVar(&p.port, "port", 8080, "Port for the local web server")
	f.StringVar(&p.overpassURL, "overpass-url", quality.OverpassBaseURL, "Overpass API endpoint URL")
	f.StringVar(&p.cacheDir, "cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
	f.Float64Var(&p.tileSize, "tile-size", 0.1, "Tile size in degrees")
	f.StringVar(&p.fetchDelay, "fetch-delay", "", "Delay between tile fetches (e.g. 500ms, 2s); default 1s")
	f.BoolVar(&p.verbose, "v", false, "Enable verbose logging to stderr")
	return cmd
}
```

- [ ] **Step 4: Register the command in `main.go`**

In `main.go`, inside `newRootCmd()`, add `newBuildCmd()` to the `AddCommand` call:

```go
cmd.AddCommand(
    newRouteCmd(),
    newFetchCmd(),
    newScoreCmd(),
    newRandomCmd(),
    newOverpassCmd(),
    newGpxCmd(),
    newDiagCmd(),
    newBuildCmd(),
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -run TestNewBuildCmd_defaults -v .`
Expected: PASS

- [ ] **Step 6: Commit**

```
git add route_build.go route_build_test.go main.go
git commit -m "feat(build): register build command with flags"
```

---

### Task 2: Geocode address and start HTTP server

**Files:**
- Modify: `route_build.go`
- Test: `route_build_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestExecBuild_missingAddress(t *testing.T) {
	err := execBuild(buildParams{address: ""})
	if err == nil {
		t.Fatal("expected error for missing address")
	}
	if !strings.Contains(err.Error(), "--address is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

Add `"strings"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestExecBuild_missingAddress -v .`
Expected: FAIL — currently returns "not implemented", not "--address is required".

- [ ] **Step 3: Implement `execBuild` with geocoding and server startup**

Replace the `execBuild` function in `route_build.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yardbirdsax/twisty/geocode"
	"github.com/yardbirdsax/twisty/quality"
)

func execBuild(p buildParams) error {
	if p.address == "" {
		return fmt.Errorf("--address is required")
	}

	center, err := geocode.Resolve(p.address, "Center")
	if err != nil {
		return fmt.Errorf("geocoding address: %w", err)
	}

	cacheDir := p.cacheDir
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = home + "/.twisty/cache/overpass/"
	}

	srv := &buildServer{
		center:      center,
		overpassURL: p.overpassURL,
		cacheDir:    cacheDir,
		tileSize:    p.tileSize,
		fetchDelay:  p.fetchDelay,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)

	addr := fmt.Sprintf(":%d", p.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", p.port, err)
	}

	fmt.Fprintf(os.Stderr, "Map centered on %s (%.5f, %.5f)\n", center.DisplayName, center.Lat, center.Lon)
	fmt.Fprintf(os.Stderr, "Listening on http://localhost:%d\n", p.port)
	return http.Serve(ln, mux)
}

type buildServer struct {
	center      geocode.Result
	overpassURL string
	cacheDir    string
	tileSize    float64
	fetchDelay  string
}

func (s *buildServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := fmt.Sprintf(buildHTML, s.center.Lat, s.center.Lon)
	w.Write([]byte(html))
}
```

- [ ] **Step 4: Add a minimal HTML placeholder**

Add at the bottom of `route_build.go`:

```go
const buildHTML = `<!DOCTYPE html>
<html>
<head>
<title>twisty build</title>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
<style>
  body { margin: 0; padding: 0; }
  #map { position: absolute; top: 0; bottom: 0; width: 100%%; }
</style>
</head>
<body>
<div id="map"></div>
<script>
var map = L.map('map').setView([%f, %f], 13);
L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 19,
    attribution: '&copy; OpenStreetMap contributors'
}).addTo(map);
</script>
</body>
</html>`
```

Note: `%%` is used because the string is a format template with `%f` placeholders for lat/lon.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -run "TestExecBuild_missingAddress|TestNewBuildCmd_defaults" -v .`
Expected: PASS

- [ ] **Step 6: Commit**

```
git add route_build.go route_build_test.go
git commit -m "feat(build): geocode address and start HTTP server with Leaflet map"
```

---

### Task 3: POST `/api/route-leg` endpoint

**Files:**
- Modify: `route_build.go`
- Test: `route_build_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHandleRouteLeg_success(t *testing.T) {
	srv := &buildServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/route-leg", srv.handleRouteLeg)

	body := `{"from":{"lat":40.0,"lon":-75.5},"to":{"lat":40.01,"lon":-75.49}}`
	req := httptest.NewRequest("POST", "/api/route-leg", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp routeLegResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Points) == 0 {
		t.Fatal("expected points in response")
	}
	if resp.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
	if resp.Distance <= 0 {
		t.Fatal("expected positive distance")
	}
}

func TestHandleRouteLeg_badJSON(t *testing.T) {
	srv := &buildServer{}
	req := httptest.NewRequest("POST", "/api/route-leg", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleRouteLeg(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
```

Add `"encoding/json"`, `"net/http"`, `"net/http/httptest"` to test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestHandleRouteLeg" -v .`
Expected: FAIL — `handleRouteLeg` not defined.

- [ ] **Step 3: Implement the route-leg handler**

Add to `route_build.go`:

```go
type routeLegRequest struct {
	From coordJSON `json:"from"`
	To   coordJSON `json:"to"`
}

type coordJSON struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type routeLegResponse struct {
	Points   [][2]float64 `json:"points"`
	Duration float64      `json:"duration"`
	Distance float64      `json:"distance"`
}

func (s *buildServer) handleRouteLeg(w http.ResponseWriter, r *http.Request) {
	var req routeLegRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	origin := geo.Coord{Lat: req.From.Lat, Lon: req.From.Lon}
	dest := geo.Coord{Lat: req.To.Lat, Lon: req.To.Lon}

	routes, err := route.FetchRoutes(origin, dest)
	if err != nil {
		http.Error(w, "routing failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if len(routes) == 0 {
		http.Error(w, "no route found", http.StatusNotFound)
		return
	}

	best := routes[0]
	points := make([][2]float64, len(best.Points))
	for i, p := range best.Points {
		points[i] = [2]float64{p.Lat, p.Lon}
	}

	resp := routeLegResponse{
		Points:   points,
		Duration: best.Duration,
		Distance: best.Distance,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
```

Add imports: `"github.com/yardbirdsax/twisty/geo"`, `"github.com/yardbirdsax/twisty/route"`.

- [ ] **Step 4: Register the endpoint in `execBuild`**

Add to the mux setup in `execBuild`, after the `/` handler:

```go
mux.HandleFunc("/api/route-leg", srv.handleRouteLeg)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -run "TestHandleRouteLeg" -v .`
Expected: `TestHandleRouteLeg_success` PASS (calls real Valhalla API), `TestHandleRouteLeg_badJSON` PASS.

Note: `TestHandleRouteLeg_success` hits the real Valhalla API. If running in CI without network access, tag it with a build constraint or skip. For local development this is fine.

- [ ] **Step 6: Commit**

```
git add route_build.go route_build_test.go
git commit -m "feat(build): add /api/route-leg endpoint with Valhalla routing"
```

---

### Task 4: POST `/api/score` endpoint with tile-based scoring

**Files:**
- Modify: `route_build.go`
- Test: `route_build_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestTilesForPolyline(t *testing.T) {
	points := [][2]float64{
		{40.0, -75.5},
		{40.01, -75.49},
		{40.02, -75.48},
	}
	tiles := tilesForPolyline(points, 0.1)
	if len(tiles) == 0 {
		t.Fatal("expected at least one tile")
	}
	// All points are within ~2km, at 0.1 degree tiles (~11km) they should
	// fall in 1-2 tiles.
	if len(tiles) > 4 {
		t.Fatalf("expected at most 4 tiles for a short polyline, got %d", len(tiles))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestTilesForPolyline -v .`
Expected: FAIL — `tilesForPolyline` not defined.

- [ ] **Step 3: Implement `tilesForPolyline`**

Add to `route_build.go`:

```go
func tilesForPolyline(points [][2]float64, tileSizeDeg float64) []quality.Tile {
	seen := make(map[[2]int]bool)
	var tiles []quality.Tile

	for _, p := range points {
		lat, lon := p[0], p[1]
		latIdx := int(math.Floor(lat / tileSizeDeg))
		lonIdx := int(math.Floor(lon / tileSizeDeg))
		key := [2]int{latIdx, lonIdx}
		if !seen[key] {
			seen[key] = true
			tiles = append(tiles, quality.Tile{
				South: float64(latIdx) * tileSizeDeg,
				West:  float64(lonIdx) * tileSizeDeg,
				North: float64(latIdx+1) * tileSizeDeg,
				East:  float64(lonIdx+1) * tileSizeDeg,
			})
		}
	}
	return tiles
}
```

Add `"math"` to imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestTilesForPolyline -v .`
Expected: PASS

- [ ] **Step 5: Write the failing test for the proximity filter**

```go
func TestIsNearPolyline(t *testing.T) {
	polyline := [][2]float64{
		{40.0, -75.5},
		{40.01, -75.5},
	}

	// Point on the polyline — should be near
	if !isNearPolyline(geo.Coord{Lat: 40.005, Lon: -75.5}, polyline, 50) {
		t.Fatal("expected point on polyline to be near")
	}

	// Point far away — should not be near
	if isNearPolyline(geo.Coord{Lat: 41.0, Lon: -74.0}, polyline, 50) {
		t.Fatal("expected distant point to not be near")
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test -run TestIsNearPolyline -v .`
Expected: FAIL — `isNearPolyline` not defined.

- [ ] **Step 7: Implement `isNearPolyline`**

Add to `route_build.go`:

```go
func isNearPolyline(point geo.Coord, polyline [][2]float64, thresholdM float64) bool {
	for _, p := range polyline {
		d := geo.Haversine(point, geo.Coord{Lat: p[0], Lon: p[1]})
		if d <= thresholdM {
			return true
		}
	}
	return false
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test -run TestIsNearPolyline -v .`
Expected: PASS

- [ ] **Step 9: Write the failing test for the score handler**

```go
func TestHandleScore_emptyPoints(t *testing.T) {
	srv := &buildServer{tileSize: 0.1}
	body := `{"points":[]}`
	req := httptest.NewRequest("POST", "/api/score", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleScore(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp scoreResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Score != 0 {
		t.Fatalf("expected score=0 for empty points, got %f", resp.Score)
	}
	if resp.PendingTiles != 0 {
		t.Fatalf("expected pending_tiles=0 for empty points, got %d", resp.PendingTiles)
	}
}
```

- [ ] **Step 10: Run test to verify it fails**

Run: `go test -run TestHandleScore_emptyPoints -v .`
Expected: FAIL — `handleScore` not defined.

- [ ] **Step 11: Implement the score handler**

Add to `route_build.go`:

```go
type scoreRequest struct {
	Points [][2]float64 `json:"points"`
}

type scoreResponse struct {
	Score        float64 `json:"score"`
	PendingTiles int     `json:"pending_tiles"`
	FailedTiles  int     `json:"failed_tiles"`
}

func (s *buildServer) handleScore(w http.ResponseWriter, r *http.Request) {
	var req scoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.Points) < 2 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scoreResponse{})
		return
	}

	tiles := tilesForPolyline(req.Points, s.tileSize)

	cacheDir := s.cacheDir
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}

	var cachedTiles []quality.Tile
	var missingTiles []quality.Tile
	for _, t := range tiles {
		if cache.Has(t) {
			cachedTiles = append(cachedTiles, t)
		} else {
			missingTiles = append(missingTiles, t)
		}
	}

	// Kick off background fetches for missing tiles
	if len(missingTiles) > 0 {
		go s.fetchMissingTiles(missingTiles)
	}

	// Score cached tiles
	score := s.scoreFromCachedTiles(cachedTiles, cache, req.Points)

	resp := scoreResponse{
		Score:        score,
		PendingTiles: len(missingTiles),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *buildServer) fetchMissingTiles(tiles []quality.Tile) {
	var fetchDelay time.Duration
	if s.fetchDelay != "" {
		d, err := time.ParseDuration(s.fetchDelay)
		if err == nil {
			fetchDelay = d
		}
	}
	if fetchDelay == 0 {
		fetchDelay = time.Second
	}

	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}
	cache.EnsureDir()

	cfg := quality.TileFetchConfig{
		Endpoint:       s.overpassURL,
		TileSize:       s.tileSize,
		Cache:          cache,
		RateLimitDelay: fetchDelay,
	}

	ctx := context.Background()
	quality.FetchTiledWaysForTiles(ctx, tiles, cfg)
}

func (s *buildServer) scoreFromCachedTiles(tiles []quality.Tile, cache *quality.TileCache, routePoints [][2]float64) float64 {
	var totalScore float64

	for _, t := range tiles {
		data, err := cache.Read(t)
		if err != nil {
			continue
		}

		ways, err := quality.ParseOverpassJSON(data)
		if err != nil {
			continue
		}

		result := quality.RunScorePipeline(ways)

		for _, sw := range result.ScoredWays {
			for _, seg := range sw.Segments {
				mid := geo.Coord{
					Lat: (seg.Start.Lat + seg.End.Lat) / 2,
					Lon: (seg.Start.Lon + seg.End.Lon) / 2,
				}
				if isNearPolyline(mid, routePoints, 50) {
					totalScore += seg.Score
				}
			}
		}
	}

	return totalScore
}
```

Add `"context"` to imports.

- [ ] **Step 12: Register the endpoint in `execBuild`**

Add to the mux setup in `execBuild`:

```go
mux.HandleFunc("/api/score", srv.handleScore)
```

- [ ] **Step 13: Run tests to verify they pass**

Run: `go test -run "TestHandleScore|TestTilesForPolyline|TestIsNearPolyline" -v .`
Expected: PASS

- [ ] **Step 14: Commit**

```
git add route_build.go route_build_test.go
git commit -m "feat(build): add /api/score endpoint with tile-based async scoring"
```

---

### Task 5: POST `/api/export` endpoint

**Files:**
- Modify: `route_build.go`
- Test: `route_build_test.go`

- [ ] **Step 1: Write the failing test for GPX export**

```go
func TestHandleExport_gpx(t *testing.T) {
	srv := &buildServer{}
	body := `{
		"format": "gpx",
		"waypoints": [{"lat":40.0,"lon":-75.5},{"lat":40.01,"lon":-75.49}],
		"legs": [{"points":[[40.0,-75.5],[40.005,-75.495],[40.01,-75.49]]}]
	}`
	req := httptest.NewRequest("POST", "/api/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/gpx+xml" {
		t.Fatalf("expected gpx content type, got %q", ct)
	}

	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Fatalf("expected attachment disposition, got %q", cd)
	}

	if !strings.Contains(w.Body.String(), "<gpx") {
		t.Fatal("expected GPX content in body")
	}
}

func TestHandleExport_kml(t *testing.T) {
	srv := &buildServer{}
	body := `{
		"format": "kml",
		"waypoints": [{"lat":40.0,"lon":-75.5},{"lat":40.01,"lon":-75.49}],
		"legs": [{"points":[[40.0,-75.5],[40.005,-75.495],[40.01,-75.49]]}]
	}`
	req := httptest.NewRequest("POST", "/api/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/vnd.google-earth.kml+xml" {
		t.Fatalf("expected kml content type, got %q", ct)
	}

	if !strings.Contains(w.Body.String(), "<kml") {
		t.Fatal("expected KML content in body")
	}
}

func TestHandleExport_badFormat(t *testing.T) {
	srv := &buildServer{}
	body := `{"format":"csv","waypoints":[],"legs":[]}`
	req := httptest.NewRequest("POST", "/api/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleExport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestHandleExport" -v .`
Expected: FAIL — `handleExport` not defined.

- [ ] **Step 3: Implement the export handler**

Add to `route_build.go`:

```go
type exportRequest struct {
	Format    string      `json:"format"`
	Waypoints []coordJSON `json:"waypoints"`
	Legs      []legJSON   `json:"legs"`
}

type legJSON struct {
	Points [][2]float64 `json:"points"`
}

func (s *buildServer) handleExport(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Format != "gpx" && req.Format != "kml" {
		http.Error(w, "format must be 'gpx' or 'kml'", http.StatusBadRequest)
		return
	}

	// Concatenate all leg points into a single polyline, deduplicating boundary points
	var allPoints []geo.Coord
	for _, leg := range req.Legs {
		for i, p := range leg.Points {
			if i == 0 && len(allPoints) > 0 {
				last := allPoints[len(allPoints)-1]
				if last.Lat == p[0] && last.Lon == p[1] {
					continue
				}
			}
			allPoints = append(allPoints, geo.Coord{Lat: p[0], Lon: p[1]})
		}
	}

	// Build waypoint labels
	var gpxWaypoints []gpxPkg.Waypoint
	for i, wp := range req.Waypoints {
		gpxWaypoints = append(gpxWaypoints, gpxPkg.Waypoint{
			Lat:  wp.Lat,
			Lon:  wp.Lon,
			Name: fmt.Sprintf("Waypoint %d", i+1),
		})
	}

	switch req.Format {
	case "gpx":
		w.Header().Set("Content-Type", "application/gpx+xml")
		w.Header().Set("Content-Disposition", `attachment; filename="twisty-route.gpx"`)
		writeGPXToWriter(w, allPoints, gpxWaypoints)
	case "kml":
		w.Header().Set("Content-Type", "application/vnd.google-earth.kml+xml")
		w.Header().Set("Content-Disposition", `attachment; filename="twisty-route.kml"`)
		writeRouteKML(w, allPoints)
	}
}

func writeGPXToWriter(w io.Writer, points []geo.Coord, waypoints []gpxPkg.Waypoint) {
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"))

	type trackPoint struct {
		Lat float64 `xml:"lat,attr"`
		Lon float64 `xml:"lon,attr"`
	}
	type trackSeg struct {
		Points []trackPoint `xml:"trkpt"`
	}
	type track struct {
		Name   string   `xml:"name"`
		TrkSeg trackSeg `xml:"trkseg"`
	}
	type gpxDoc struct {
		XMLName xml.Name          `xml:"gpx"`
		Version string            `xml:"version,attr"`
		Creator string            `xml:"creator,attr"`
		Xmlns   string            `xml:"xmlns,attr"`
		Wpts    []gpxPkg.Waypoint `xml:"wpt"`
		Trk     track             `xml:"trk"`
	}

	pts := make([]trackPoint, len(points))
	for i, p := range points {
		pts[i] = trackPoint{Lat: p.Lat, Lon: p.Lon}
	}

	doc := gpxDoc{
		Version: "1.1",
		Creator: "twisty-build",
		Xmlns:   "http://www.topografix.com/GPX/1/1",
		Wpts:    waypoints,
		Trk: track{
			Name:   "Twisty Route",
			TrkSeg: trackSeg{Points: pts},
		},
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(doc)
	enc.Flush()
}

func writeRouteKML(w io.Writer, points []geo.Coord) {
	var sb strings.Builder
	for i, p := range points {
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "%f,%f,0", p.Lon, p.Lat)
	}

	type lineString struct {
		Coordinates string `xml:"coordinates"`
	}
	type placemark struct {
		Name       string     `xml:"name"`
		LineString lineString `xml:"LineString"`
	}
	type document struct {
		Name       string     `xml:"name"`
		Placemarks []placemark `xml:"Placemark"`
	}
	type kmlDoc struct {
		XMLName xml.Name `xml:"kml"`
		XMLNS   string   `xml:"xmlns,attr"`
		Doc     document `xml:"Document"`
	}

	doc := kmlDoc{
		XMLNS: "http://www.opengis.net/kml/2.2",
		Doc: document{
			Name: "Twisty Route",
			Placemarks: []placemark{{
				Name:       "Route",
				LineString: lineString{Coordinates: sb.String()},
			}},
		},
	}

	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(doc)
	enc.Flush()
}
```

Add `"encoding/xml"`, `"io"`, `"strings"` to imports. Add a package alias for the gpx import:
```go
gpxPkg "github.com/yardbirdsax/twisty/gpx"
```

- [ ] **Step 4: Register the endpoint in `execBuild`**

Add to the mux setup:

```go
mux.HandleFunc("/api/export", srv.handleExport)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -run "TestHandleExport" -v .`
Expected: PASS

- [ ] **Step 6: Commit**

```
git add route_build.go route_build_test.go
git commit -m "feat(build): add /api/export endpoint for GPX and KML download"
```

---

### Task 6: Full interactive frontend

**Files:**
- Modify: `route_build.go` (replace the `buildHTML` const)

This task replaces the minimal map placeholder with the full interactive UI. Since this is a large HTML/JS const, no unit test for the HTML itself — testing happens via the existing API handler tests plus manual verification in the browser.

- [ ] **Step 1: Replace `buildHTML` with the full interactive UI**

Replace the `buildHTML` const in `route_build.go` with the full implementation. The HTML includes:

1. Leaflet map with click handler
2. Waypoint markers (green start, blue intermediate, red last)
3. Polyline rendering per leg
4. Stats overlay (top-right) with score, distance, time, and fetch status
5. Export buttons (bottom-right)
6. Instruction hint (bottom-left)
7. Toast notification for errors

```go
const buildHTML = `<!DOCTYPE html>
<html>
<head>
<title>twisty build</title>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
<style>
  body { margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; }
  #map { position: absolute; top: 0; bottom: 0; width: 100%%; }

  #stats {
    position: absolute; top: 12px; right: 12px; z-index: 1000;
    background: rgba(255,255,255,0.95); border-radius: 8px;
    padding: 14px 18px; box-shadow: 0 2px 8px rgba(0,0,0,0.15);
    min-width: 180px; font-size: 13px;
  }
  #stats .label { font-size: 11px; text-transform: uppercase; color: #666; letter-spacing: 0.5px; margin-bottom: 8px; }
  #stats .row { display: flex; justify-content: space-between; margin-bottom: 6px; }
  #stats .row .key { color: #555; }
  #stats .row .val { font-weight: bold; }
  #stats .score-val { color: #2563eb; }
  #stats .fetch-status { border-top: 1px solid #eee; padding-top: 8px; margin-top: 4px; font-size: 11px; color: #f59e0b; }
  #stats .fetch-status.done { color: #16a34a; }

  #export-buttons {
    position: absolute; bottom: 12px; right: 12px; z-index: 1000;
    display: flex; gap: 8px;
  }
  #export-buttons button {
    background: #1d4ed8; color: white; border: none; border-radius: 6px;
    padding: 8px 14px; font-size: 12px; font-weight: 500; cursor: pointer;
  }
  #export-buttons button:disabled { background: #9ca3af; cursor: not-allowed; }
  #export-buttons button:hover:not(:disabled) { background: #1e40af; }

  #hint {
    position: absolute; bottom: 12px; left: 12px; z-index: 1000;
    background: rgba(0,0,0,0.7); color: white; border-radius: 6px;
    padding: 8px 12px; font-size: 11px;
  }

  #toast {
    position: absolute; top: 60px; left: 50%%; transform: translateX(-50%%);
    z-index: 1001; background: #dc2626; color: white; border-radius: 6px;
    padding: 10px 16px; font-size: 13px; display: none;
    box-shadow: 0 2px 8px rgba(0,0,0,0.2);
  }
</style>
</head>
<body>
<div id="map"></div>

<div id="stats">
  <div class="label">Route Stats</div>
  <div class="row"><span class="key">Twist Score</span><span class="val score-val" id="score-val">—</span></div>
  <div class="row"><span class="key">Distance</span><span class="val" id="dist-val">—</span></div>
  <div class="row"><span class="key">Time</span><span class="val" id="time-val">—</span></div>
  <div class="fetch-status done" id="fetch-status">Click map to start</div>
</div>

<div id="export-buttons">
  <button id="btn-gpx" disabled onclick="exportRoute('gpx')">Export GPX</button>
  <button id="btn-kml" disabled onclick="exportRoute('kml')">Export KML</button>
</div>

<div id="hint">Click map to add waypoint · Click last marker to undo</div>

<div id="toast" id="toast"></div>

<script>
var map = L.map('map').setView([%f, %f], 13);
L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 19,
    attribution: '&copy; OpenStreetMap contributors'
}).addTo(map);

var waypoints = [];
var markers = [];
var legPolylines = [];
var legs = [];
var scorePollTimer = null;

function markerColor(index, total) {
  if (index === 0) return '#16a34a';
  if (index === total - 1) return '#dc2626';
  return '#2563eb';
}

function createMarkerIcon(index, total) {
  var color = markerColor(index, total);
  var isLast = (index === total - 1 && total > 1);
  var size = isLast ? 28 : 24;
  var border = isLast ? '3px solid #fca5a5' : '2px solid white';
  var shadow = isLast ? 'box-shadow:0 0 8px rgba(220,38,38,0.5);' : '';
  return L.divIcon({
    className: '',
    iconSize: [size, size],
    iconAnchor: [size/2, size/2],
    html: '<div style="width:'+size+'px;height:'+size+'px;background:'+color+
          ';border-radius:50%%;border:'+border+';display:flex;align-items:center;'+
          'justify-content:center;color:white;font-size:11px;font-weight:bold;'+
          shadow+'">'+(index+1)+'</div>'
  });
}

function refreshMarkers() {
  markers.forEach(function(m) { map.removeLayer(m); });
  markers = [];
  for (var i = 0; i < waypoints.length; i++) {
    var m = L.marker(waypoints[i], { icon: createMarkerIcon(i, waypoints.length) }).addTo(map);
    (function(idx) {
      m.on('click', function() {
        if (idx === waypoints.length - 1 && waypoints.length > 0) {
          removeLastWaypoint();
        }
      });
    })(i);
    markers.push(m);
  }
}

function updateStats() {
  var totalDist = 0, totalTime = 0;
  legs.forEach(function(leg) {
    totalDist += leg.distance;
    totalTime += leg.duration;
  });

  document.getElementById('dist-val').textContent = totalDist > 0 ? (totalDist / 1000).toFixed(1) + ' km' : '—';
  document.getElementById('time-val').textContent = totalTime > 0 ? Math.round(totalTime / 60) + ' min' : '—';

  var hasRoute = waypoints.length >= 2;
  document.getElementById('btn-gpx').disabled = !hasRoute;
  document.getElementById('btn-kml').disabled = !hasRoute;
}

function requestScore() {
  if (scorePollTimer) { clearTimeout(scorePollTimer); scorePollTimer = null; }

  var allPoints = [];
  legs.forEach(function(leg) {
    leg.points.forEach(function(p, i) {
      if (i === 0 && allPoints.length > 0) {
        var last = allPoints[allPoints.length - 1];
        if (last[0] === p[0] && last[1] === p[1]) return;
      }
      allPoints.push(p);
    });
  });

  if (allPoints.length < 2) {
    document.getElementById('score-val').textContent = '—';
    document.getElementById('fetch-status').textContent = 'Click map to start';
    document.getElementById('fetch-status').className = 'fetch-status done';
    return;
  }

  fetch('/api/score', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ points: allPoints })
  })
  .then(function(r) { return r.json(); })
  .then(function(data) {
    document.getElementById('score-val').textContent = Math.round(data.score).toLocaleString();

    if (data.pending_tiles > 0) {
      document.getElementById('fetch-status').textContent = '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...';
      document.getElementById('fetch-status').className = 'fetch-status';
      scorePollTimer = setTimeout(requestScore, 2000);
    } else if (data.failed_tiles > 0) {
      document.getElementById('fetch-status').textContent = '⚠ ' + data.failed_tiles + ' tile(s) failed';
      document.getElementById('fetch-status').className = 'fetch-status';
    } else {
      document.getElementById('fetch-status').textContent = '✅ Score complete';
      document.getElementById('fetch-status').className = 'fetch-status done';
    }
  })
  .catch(function(err) {
    document.getElementById('fetch-status').textContent = 'Score error';
    document.getElementById('fetch-status').className = 'fetch-status';
  });
}

function showToast(msg) {
  var el = document.getElementById('toast');
  el.textContent = msg;
  el.style.display = 'block';
  setTimeout(function() { el.style.display = 'none'; }, 4000);
}

function addWaypoint(latlng) {
  var prev = waypoints.length > 0 ? waypoints[waypoints.length - 1] : null;
  waypoints.push([latlng.lat, latlng.lng]);
  refreshMarkers();

  if (prev) {
    document.getElementById('fetch-status').textContent = 'Routing...';
    document.getElementById('fetch-status').className = 'fetch-status';

    fetch('/api/route-leg', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        from: { lat: prev[0], lon: prev[1] },
        to: { lat: latlng.lat, lon: latlng.lng }
      })
    })
    .then(function(r) {
      if (!r.ok) throw new Error('Routing failed');
      return r.json();
    })
    .then(function(data) {
      var latLngs = data.points.map(function(p) { return [p[0], p[1]]; });
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 4 }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      updateStats();
      requestScore();
    })
    .catch(function(err) {
      waypoints.pop();
      refreshMarkers();
      showToast('Could not route between these points — try a different location');
    });
  }
}

function removeLastWaypoint() {
  if (waypoints.length === 0) return;
  waypoints.pop();

  if (legPolylines.length > 0) {
    map.removeLayer(legPolylines.pop());
    legs.pop();
  }

  refreshMarkers();
  updateStats();
  requestScore();
}

map.on('click', function(e) {
  addWaypoint(e.latlng);
});

function exportRoute(format) {
  var exportLegs = legs.map(function(leg) {
    return { points: leg.points };
  });
  var exportWaypoints = waypoints.map(function(wp) {
    return { lat: wp[0], lon: wp[1] };
  });

  fetch('/api/export', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ format: format, waypoints: exportWaypoints, legs: exportLegs })
  })
  .then(function(r) {
    if (!r.ok) throw new Error('Export failed');
    return r.blob();
  })
  .then(function(blob) {
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = 'twisty-route.' + format;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  })
  .catch(function(err) {
    showToast('Export failed: ' + err.message);
  });
}
</script>
</body>
</html>`
```

- [ ] **Step 2: Build and verify it compiles**

Run: `go build -o /dev/null .`
Expected: Compiles with no errors.

- [ ] **Step 3: Manual test in browser**

Run: `go run . build --address "Royersford, PA"`

Open `http://localhost:8080` in browser and verify:
1. Map loads centered on the address
2. Clicking adds a green start marker
3. Second click triggers routing and draws a blue polyline
4. Stats overlay shows distance and time
5. Score updates (may show "Scoring N tiles..." if cache is empty)
6. Clicking the last (red) marker removes it and its leg
7. Export buttons download GPX/KML files

- [ ] **Step 4: Commit**

```
git add route_build.go
git commit -m "feat(build): add full interactive route builder frontend"
```

---

### Task 7: Integration test — full round-trip

**Files:**
- Modify: `route_build_test.go`

This test verifies the full server works end-to-end: start the server, hit the endpoints, and confirm the responses.

- [ ] **Step 1: Write the integration test**

```go
func TestBuildServer_integration(t *testing.T) {
	srv := &buildServer{
		overpassURL: quality.OverpassBaseURL,
		cacheDir:    t.TempDir(),
		tileSize:    0.1,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/api/route-leg", srv.handleRouteLeg)
	mux.HandleFunc("/api/score", srv.handleScore)
	mux.HandleFunc("/api/export", srv.handleExport)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// GET / — should return HTML
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("GET / status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// POST /api/score with empty points — should return zero score
	scoreBody := strings.NewReader(`{"points":[]}`)
	resp, err = http.Post(ts.URL+"/api/score", "application/json", scoreBody)
	if err != nil {
		t.Fatalf("POST /api/score: %v", err)
	}
	var scoreResp scoreResponse
	json.NewDecoder(resp.Body).Decode(&scoreResp)
	resp.Body.Close()
	if scoreResp.Score != 0 {
		t.Fatalf("expected score=0, got %f", scoreResp.Score)
	}

	// POST /api/export — should return GPX
	exportBody := strings.NewReader(`{
		"format":"gpx",
		"waypoints":[{"lat":40.0,"lon":-75.5}],
		"legs":[{"points":[[40.0,-75.5],[40.01,-75.49]]}]
	}`)
	resp, err = http.Post(ts.URL+"/api/export", "application/json", exportBody)
	if err != nil {
		t.Fatalf("POST /api/export: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("POST /api/export status: %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "application/gpx+xml" {
		t.Fatalf("expected gpx content type, got %q", resp.Header.Get("Content-Type"))
	}
	resp.Body.Close()
}
```

- [ ] **Step 2: Run the integration test**

Run: `go test -run TestBuildServer_integration -v .`
Expected: PASS

- [ ] **Step 3: Run all build-related tests together**

Run: `go test -run "TestNewBuildCmd|TestExecBuild|TestHandleRouteLeg|TestTilesForPolyline|TestIsNearPolyline|TestHandleScore|TestHandleExport|TestBuildServer" -v .`
Expected: All PASS

- [ ] **Step 4: Commit**

```
git add route_build_test.go
git commit -m "test(build): add integration test for build server round-trip"
```

---

### Task 8: Final cleanup and manual verification

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: All tests pass, no regressions.

- [ ] **Step 2: Build the binary**

Run: `make build`
Expected: Compiles successfully.

- [ ] **Step 3: Full manual test**

Run: `./bin/twisty build --address "Royersford, PA"`

Verify in browser:
1. Map loads centered on Royersford
2. Click to place start point (green marker)
3. Click second point — blue polyline appears, stats show distance and time
4. Click third point — new leg added, totals update
5. Score loads (may poll for tiles)
6. Click last marker — leg removed, totals recalculate
7. Export GPX — file downloads and opens in Google Earth or similar
8. Export KML — file downloads and opens in Google Earth

- [ ] **Step 4: Test with local Overpass**

Run: `./bin/twisty build --address "Royersford, PA" --overpass-url http://localhost:12345/api/interpreter`

Verify that the `--overpass-url` flag is respected for tile fetching.

- [ ] **Step 5: Commit any final fixes**

If any issues were found during manual testing, fix and commit them.

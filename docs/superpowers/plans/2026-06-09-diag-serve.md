# diag serve — Visual Segment Debugger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `twisty diag serve` subcommand that reads the overpass cache, runs the scoring pipeline, and serves an interactive Leaflet map where hovering any road segment shows its name, tier, and score.

**Architecture:** A new `diag_serve.go` file at the package root contains the `diagServe()` function and an embedded HTML page constant. On startup it reads all cached tiles via `diag.AllCachedTiles`, runs `diag.SimulatePipelineFull`, and serializes the resulting segments to GeoJSON held in memory. A minimal `net/http` server serves the HTML page at `GET /` and the GeoJSON at `GET /api/segments`.

**Tech Stack:** Go stdlib `net/http`, `encoding/json`; Leaflet.js 1.9 via CDN in the embedded HTML; existing `diag` and `quality` packages.

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `diag_serve.go` | Create | `diagServe()`, GeoJSON types, HTML constant |
| `diag_serve_test.go` | Create | Unit test for GeoJSON serialization + httptest integration tests |
| `main.go` | Modify | Wire `newDiagCmd()` into `newRootCmd()` |

---

### Task 1: GeoJSON serialization types and function

**Files:**
- Create: `diag_serve.go`

- [ ] **Step 1: Write the failing test**

Create `diag_serve_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)

func TestCollectionsToGeoJSON(t *testing.T) {
	collections := []quality.RoadCollection{
		{
			Name: "PA 120",
			Segments: []quality.ScoredSegment{
				{
					WayID:  111,
					Start:  geo.Coord{Lat: 41.1, Lon: -78.1},
					End:    geo.Coord{Lat: 41.2, Lon: -78.2},
					Tier:   3,
					Score:  142.5,
				},
			},
		},
	}

	fc := collectionsToGeoJSON(collections)

	if fc.Type != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %q", fc.Type)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(fc.Features))
	}
	f := fc.Features[0]
	if f.Geometry.Type != "LineString" {
		t.Fatalf("expected LineString, got %q", f.Geometry.Type)
	}
	if len(f.Geometry.Coordinates) != 2 {
		t.Fatalf("expected 2 coordinates, got %d", len(f.Geometry.Coordinates))
	}
	// GeoJSON coordinate order is [lon, lat]
	if f.Geometry.Coordinates[0][0] != -78.1 || f.Geometry.Coordinates[0][1] != 41.1 {
		t.Fatalf("unexpected start coord: %v", f.Geometry.Coordinates[0])
	}
	if f.Properties.RoadName != "PA 120" {
		t.Fatalf("expected road_name PA 120, got %q", f.Properties.RoadName)
	}
	if f.Properties.Tier != 3 {
		t.Fatalf("expected tier 3, got %d", f.Properties.Tier)
	}
	if f.Properties.Score != 142.5 {
		t.Fatalf("expected score 142.5, got %f", f.Properties.Score)
	}
	if f.Properties.WayID != 111 {
		t.Fatalf("expected way_id 111, got %d", f.Properties.WayID)
	}

	// Verify it round-trips through JSON cleanly
	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var roundtrip geoJSONFeatureCollection
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if roundtrip.Features[0].Properties.RoadName != "PA 120" {
		t.Fatalf("round-trip road_name mismatch")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestCollectionsToGeoJSON -v ./...
```

Expected: compile error — `collectionsToGeoJSON`, `geoJSONFeatureCollection` undefined.

- [ ] **Step 3: Implement GeoJSON types and serialization**

Create `diag_serve.go`:

```go
package main

import "github.com/yardbirdsax/twisty/quality"

type geoJSONSegmentProps struct {
	RoadName string  `json:"road_name"`
	Tier     int     `json:"tier"`
	Score    float64 `json:"score"`
	WayID    int64   `json:"way_id"`
}

type geoJSONGeometry struct {
	Type        string      `json:"type"`
	Coordinates [][2]float64 `json:"coordinates"`
}

type geoJSONFeature struct {
	Type       string              `json:"type"`
	Geometry   geoJSONGeometry     `json:"geometry"`
	Properties geoJSONSegmentProps `json:"properties"`
}

type geoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

func collectionsToGeoJSON(collections []quality.RoadCollection) geoJSONFeatureCollection {
	fc := geoJSONFeatureCollection{Type: "FeatureCollection"}
	for _, c := range collections {
		for _, seg := range c.Segments {
			fc.Features = append(fc.Features, geoJSONFeature{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type: "LineString",
					Coordinates: [][2]float64{
						{seg.Start.Lon, seg.Start.Lat},
						{seg.End.Lon, seg.End.Lat},
					},
				},
				Properties: geoJSONSegmentProps{
					RoadName: c.Name,
					Tier:     seg.Tier,
					Score:    seg.Score,
					WayID:    seg.WayID,
				},
			})
		}
	}
	if fc.Features == nil {
		fc.Features = []geoJSONFeature{}
	}
	return fc
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test -run TestCollectionsToGeoJSON -v ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add diag_serve.go diag_serve_test.go
git commit -m "feat(diag): add GeoJSON serialization for scored segments"
```

---

### Task 2: HTTP handlers and server startup

**Files:**
- Modify: `diag_serve.go` — add `diagServe()`, HTTP handlers, HTML constant

- [ ] **Step 1: Write the failing integration tests**

Add to `diag_serve_test.go`. First, update the import block at the top of the file to include the new imports (merge with existing imports — do not add a second `import` block):

```go
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/quality"
)
```

Then add the new test function:

```go
func TestDiagServeHandlers(t *testing.T) {
	collections := []quality.RoadCollection{
		{
			Name: "Test Road",
			Segments: []quality.ScoredSegment{
				{
					WayID: 1,
					Start: geo.Coord{Lat: 40.0, Lon: -75.0},
					End:   geo.Coord{Lat: 40.1, Lon: -75.1},
					Tier:  2,
					Score: 50.0,
				},
			},
		},
	}

	mux := buildDiagMux(collections)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("GET / returns HTML", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("expected text/html content-type, got %q", ct)
		}
	})

	t.Run("GET /api/segments returns GeoJSON", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/segments")
		if err != nil {
			t.Fatalf("GET /api/segments: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Fatalf("expected application/json, got %q", ct)
		}
		var fc geoJSONFeatureCollection
		if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
			t.Fatalf("decode GeoJSON: %v", err)
		}
		if fc.Type != "FeatureCollection" {
			t.Fatalf("expected FeatureCollection, got %q", fc.Type)
		}
		if len(fc.Features) != 1 {
			t.Fatalf("expected 1 feature, got %d", len(fc.Features))
		}
		if fc.Features[0].Properties.RoadName != "Test Road" {
			t.Fatalf("expected Test Road, got %q", fc.Features[0].Properties.RoadName)
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestDiagServeHandlers -v ./...
```

Expected: compile error — `buildDiagMux` undefined.

- [ ] **Step 3: Implement `buildDiagMux` and the HTML constant**

Add to `diag_serve.go`:

```go
import (
	"encoding/json"
	"net/http"
)

const diagHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>twisty diag</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css"/>
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
<style>
  html, body, #map { height: 100%; margin: 0; padding: 0; }
  #tooltip {
    position: fixed;
    background: rgba(15,17,23,0.92);
    color: #e2e8f0;
    border: 1px solid #334155;
    border-radius: 6px;
    padding: 8px 12px;
    font-family: system-ui, sans-serif;
    font-size: 13px;
    pointer-events: none;
    display: none;
    z-index: 9999;
    line-height: 1.6;
  }
  #tooltip .road { font-weight: 600; color: #7dd3fc; }
</style>
</head>
<body>
<div id="map"></div>
<div id="tooltip"><div class="road"></div><div class="detail"></div></div>
<script>
const TIER_COLORS = ['#475569','#4ade80','#facc15','#fb923c','#f87171'];
const map = L.map('map').setView([39, -77], 7);
L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
  attribution: '© OpenStreetMap contributors', maxZoom: 19
}).addTo(map);

const tooltip = document.getElementById('tooltip');
let allPolylines = [];

fetch('/api/segments').then(r => r.json()).then(fc => {
  // Group polylines by road_name for highlight-on-hover
  const byRoad = {};
  fc.features.forEach(f => {
    const p = f.properties;
    const coords = f.geometry.coordinates.map(c => [c[1], c[0]]);
    const color = TIER_COLORS[Math.min(p.tier, 4)] || TIER_COLORS[0];
    const line = L.polyline(coords, { color, weight: 3, opacity: 0.8 });
    line._diagProps = p;
    allPolylines.push(line);
    if (!byRoad[p.road_name]) byRoad[p.road_name] = [];
    byRoad[p.road_name].push(line);
    line.addTo(map);
  });

  allPolylines.forEach(line => {
    line.on('mouseover', function(e) {
      const name = this._diagProps.road_name;
      const group = byRoad[name] || [];
      allPolylines.forEach(l => l.setStyle({ weight: 2, opacity: 0.2 }));
      group.forEach(l => l.setStyle({ weight: 5, opacity: 1.0 }));
      tooltip.querySelector('.road').textContent = name;
      tooltip.querySelector('.detail').textContent =
        'Tier ' + this._diagProps.tier + ' · Score ' + this._diagProps.score.toFixed(1);
      tooltip.style.display = 'block';
    });
    line.on('mousemove', function(e) {
      tooltip.style.left = (e.originalEvent.clientX + 14) + 'px';
      tooltip.style.top  = (e.originalEvent.clientY - 10) + 'px';
    });
    line.on('mouseout', function() {
      allPolylines.forEach(l => l.setStyle({ weight: 3, opacity: 0.8 }));
      tooltip.style.display = 'none';
    });
  });

  // Auto-fit map to data bounds if any features
  if (allPolylines.length > 0) {
    const group = L.featureGroup(allPolylines);
    map.fitBounds(group.getBounds(), { padding: [20, 20] });
  }
});
</script>
</body>
</html>`

func buildDiagMux(collections []quality.RoadCollection) *http.ServeMux {
	fc := collectionsToGeoJSON(collections)
	fcJSON, _ := json.Marshal(fc)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(diagHTML))
	})
	mux.HandleFunc("/api/segments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(fcJSON)
	})
	return mux
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test -run "TestCollectionsToGeoJSON|TestDiagServeHandlers" -v ./...
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add diag_serve.go diag_serve_test.go
git commit -m "feat(diag): add HTTP handlers and embedded HTML page"
```

---

### Task 3: `diagServe()` entry point with cache loading

**Files:**
- Modify: `diag_serve.go` — add `diagServe()` with cache loading and server startup

- [ ] **Step 1: Add `diagServe()` to `diag_serve.go`**

Add these imports to `diag_serve.go` (merge with existing imports):

```go
import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/yardbirdsax/twisty/diag"
	"github.com/yardbirdsax/twisty/quality"
)
```

Add the function:

```go
func diagServe(cacheDir string, port int) error {
	cfg := diag.DefaultCacheConfig()
	if cacheDir != "" {
		cfg.OverpassDir = cacheDir
	}

	if _, err := os.Stat(cfg.OverpassDir); os.IsNotExist(err) {
		return fmt.Errorf("cache directory does not exist: %s", cfg.OverpassDir)
	}

	tiles, err := diag.AllCachedTiles(cfg)
	if err != nil {
		return fmt.Errorf("reading cache: %w", err)
	}
	if len(tiles) == 0 {
		return fmt.Errorf("no tiles found in cache directory: %s\nRun 'twisty score' first to populate the cache", cfg.OverpassDir)
	}

	fmt.Fprintf(os.Stderr, "Loading %d tiles from cache...\n", len(tiles))
	result, err := diag.SimulatePipelineFull(tiles, cfg, diag.SimulatePipelineOptions{})
	if err != nil {
		return fmt.Errorf("running pipeline: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Loaded %d road collections\n", len(result.Collections))

	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", port, err)
	}

	mux := buildDiagMux(result.Collections)
	fmt.Fprintf(os.Stderr, "Listening on http://localhost:%d\n", port)
	return http.Serve(ln, mux)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add diag_serve.go
git commit -m "feat(diag): add diagServe entry point with cache loading"
```

---

### Task 4: Wire `diag serve` into the CLI

**Files:**
- Modify: `main.go` — add `newDiagCmd()` and register it

- [ ] **Step 1: Add `newDiagCmd()` to `main.go`**

Add this function to `main.go` (near the other `new*Cmd` functions):

```go
func newDiagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diag",
		Short: "Diagnostic tools",
	}
	cmd.AddCommand(newDiagServeCmd())
	return cmd
}

func newDiagServeCmd() *cobra.Command {
	var cacheDir string
	var port int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve an interactive map of scored segments from the cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return diagServe(cacheDir, port)
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Overpass cache directory (default ~/.twisty/cache/overpass/)")
	cmd.Flags().IntVar(&port, "port", 7777, "Port to listen on")
	return cmd
}
```

- [ ] **Step 2: Register the command in `newRootCmd()`**

In `main.go`, find `cmd.AddCommand(` and add `newDiagCmd()`:

```go
cmd.AddCommand(
    newRouteCmd(),
    newFetchCmd(),
    newScoreCmd(),
    newRandomCmd(),
    newOverpassCmd(),
    newGpxCmd(),
    newDiagCmd(),
)
```

- [ ] **Step 3: Verify it compiles and the command is visible**

```bash
go build -o bin/twisty . && bin/twisty diag serve --help
```

Expected output includes:
```
Serve an interactive map of scored segments from the cache

Usage:
  twisty diag serve [flags]

Flags:
      --cache-dir string   Overpass cache directory (default ~/.twisty/cache/overpass/)
  -h, --help               help for serve
      --port int           Port to listen on (default 7777)
```

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat(diag): wire diag serve subcommand into CLI"
```

---

### Task 5: Manual smoke test

- [ ] **Step 1: Build and run against real cache**

```bash
go build -o bin/twisty . && bin/twisty diag serve
```

Expected stderr:
```
Loading N tiles from cache...
Loaded M road collections
Listening on http://localhost:7777
```

- [ ] **Step 2: Open the browser and verify the map loads**

Open `http://localhost:7777` in a browser. Verify:
- Map renders with OpenStreetMap tiles as background
- Road segments appear as colored polylines
- Map auto-fits to the data bounds

- [ ] **Step 3: Verify hover behavior**

Move the mouse over a road segment. Verify:
- A floating tooltip appears near the cursor showing road name, tier, and score
- All segments of that road brighten (weight 5, opacity 1.0)
- All other segments dim (weight 2, opacity 0.2)
- Moving off the segment hides the tooltip and restores all segments

- [ ] **Step 4: Verify error cases**

```bash
bin/twisty diag serve --cache-dir /nonexistent
```

Expected: exits with `cache directory does not exist: /nonexistent`

- [ ] **Step 5: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 6: Commit if any fixes were needed**

```bash
git add -p
git commit -m "fix(diag): smoke test fixes"
```

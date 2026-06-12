# Build Overlay Toggle (Ways / Roads) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a toggle to the `twisty build` map that switches the road overlay between per-segment tier coloring (existing) and per-road aggregate gradient coloring (matching `twisty score` KML output).

**Architecture:** A new `GET /api/road-segments` endpoint reads cached tiles in the viewport bbox, runs aggregation, and returns one GeoJSON feature per `RoadCollection` colored by `CurvatureColorLevel`/`GradientColor` converted to CSS hex. The frontend gains a `overlayMode` state variable and a toggle button in the `#stats` panel; `loadVisibleSegments` and `segmentStyle` are updated to branch on the mode.

**Tech Stack:** Go (net/http, encoding/json), Leaflet.js, existing `quality` package (`Aggregate`, `CurvatureColorLevel`, `GradientColor`, `DefaultMinCurvature`, `DefaultMaxCurvature`).

---

## File Map

| File | Change |
|------|--------|
| `quality/kml.go` | Add exported `GradientColorCSS(level int) string` helper |
| `quality/kml_test.go` | Add test for `GradientColorCSS` |
| `geojson.go` | Add `geoJSONRoadProps` struct and `collectionsToRoadGeoJSON` function |
| `geojson_test.go` | Add test for `collectionsToRoadGeoJSON` |
| `route_build.go` | Register `/api/road-segments`; add `handleRoadSegments`; update frontend HTML/JS |
| `route_build_test.go` | Add `TestHandleRoadSegments_emptyCache` and `TestHandleRoadSegments_returnsFeaturesWithColor` |

---

## Task 1: Add `GradientColorCSS` to `quality/kml.go`

`GradientColor` returns KML `AABBGGRR` format (e.g. `"FF00FFFF"`). We need a CSS `#RRGGBB` version for GeoJSON properties.

**Files:**
- Modify: `quality/kml.go` (after the `GradientColor` function, ~line 258)
- Modify: `quality/kml_test.go`

- [ ] **Step 1: Write the failing test**

Add to `quality/kml_test.go` (at the end of the file):

```go
func TestGradientColorCSS(t *testing.T) {
    tests := []struct {
        level int
        want  string
    }{
        {0, "#10e000"},   // level 0 → TierColors[0] KML "F000E010" → CSS #10e000
        {1, "#ffff00"},   // level 1 → yellow: KML "FF00FFFF" → CSS #ffff00
        {256, "#ff0000"}, // level 256 → red: KML "FF0000FF" → CSS #ff0000
        {511, "#ff00ff"}, // level 511 → magenta: KML "FFFF00FF" → CSS #ff00ff
    }
    for _, tc := range tests {
        got := GradientColorCSS(tc.level)
        if got != tc.want {
            t.Errorf("GradientColorCSS(%d) = %q, want %q", tc.level, got, tc.want)
        }
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./quality/ -run TestGradientColorCSS -v
```

Expected: `FAIL` — `GradientColorCSS` undefined.

- [ ] **Step 3: Add `GradientColorCSS` to `quality/kml.go`**

Add immediately after the closing brace of `GradientColor` (~line 258):

```go
// GradientColorCSS returns a CSS #RRGGBB color string for the given level (0-511).
// Converts the KML AABBGGRR output of GradientColor to CSS format.
func GradientColorCSS(level int) string {
	kml := GradientColor(level) // AABBGGRR, e.g. "FF00FFFF"
	return "#" + strings.ToLower(kml[6:8]+kml[4:6]+kml[2:4])
}
```

Note: `strings` is already imported in `quality/kml.go`.

- [ ] **Step 4: Run test to confirm it passes**

```bash
go test ./quality/ -run TestGradientColorCSS -v
```

Expected: `PASS`.

- [ ] **Step 5: Verify level-0 output**

The level-0 case uses `TierColors[0]` which is `"F000E010"` (KML AABBGGRR). The CSS conversion of bytes `[6:8]="10"`, `[4:6]="E0"`, `[2:4]="00"` gives `#10e000`. Confirm the test expectation matches. If `TierColors[0]` differs from `"F000E010"`, update the test's level-0 `want` to match `GradientColor(0)` converted manually.

- [ ] **Step 6: Commit**

```bash
git add quality/kml.go quality/kml_test.go
git commit -m "feat: add GradientColorCSS helper to quality/kml.go"
```

---

## Task 2: Add road GeoJSON types and `collectionsToRoadGeoJSON` to `geojson.go`

The existing `collectionsToGeoJSON` produces one feature per `ScoredSegment` with `way_id`/`tier` properties. Road mode needs one feature per `RoadCollection` — a MultiLineString of all its segments, with `road_name`, `score`, and CSS `color` properties.

**Files:**
- Modify: `geojson.go`
- Modify: `geojson_test.go`

- [ ] **Step 1: Write the failing test**

Add to `geojson_test.go` (at the end of the file):

```go
func TestCollectionsToRoadGeoJSON_emptyInput(t *testing.T) {
    fc := collectionsToRoadGeoJSON(nil)
    if fc.Type != "FeatureCollection" {
        t.Fatalf("expected FeatureCollection, got %q", fc.Type)
    }
    if len(fc.RoadFeatures) != 0 {
        t.Fatalf("expected 0 features, got %d", len(fc.RoadFeatures))
    }
}

func TestCollectionsToRoadGeoJSON_colorAndCoordinates(t *testing.T) {
    col := quality.RoadCollection{
        Name:       "PA 100",
        TotalScore: 300,
        Segments: []quality.ScoredSegment{
            {
                Start: geo.Coord{Lat: 40.1, Lon: -75.1},
                End:   geo.Coord{Lat: 40.2, Lon: -75.2},
            },
            {
                Start: geo.Coord{Lat: 40.2, Lon: -75.2},
                End:   geo.Coord{Lat: 40.3, Lon: -75.3},
            },
        },
    }
    fc := collectionsToRoadGeoJSON([]quality.RoadCollection{col})
    if len(fc.RoadFeatures) != 1 {
        t.Fatalf("expected 1 feature, got %d", len(fc.RoadFeatures))
    }
    f := fc.RoadFeatures[0]
    if f.Properties.RoadName != "PA 100" {
        t.Errorf("road_name = %q, want %q", f.Properties.RoadName, "PA 100")
    }
    if f.Properties.Score != 300 {
        t.Errorf("score = %v, want 300", f.Properties.Score)
    }
    if f.Properties.Color == "" {
        t.Error("color should not be empty")
    }
    if !strings.HasPrefix(f.Properties.Color, "#") {
        t.Errorf("color %q should start with #", f.Properties.Color)
    }
    if f.Geometry.Type != "MultiLineString" {
        t.Errorf("geometry type = %q, want MultiLineString", f.Geometry.Type)
    }
    if len(f.Geometry.Lines) != 2 {
        t.Errorf("expected 2 lines, got %d", len(f.Geometry.Lines))
    }
}
```

Check current imports in `geojson_test.go` and add any missing ones (`strings`, `geo`, `quality`) if not already present.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test . -run "TestCollectionsToRoadGeoJSON" -v
```

Expected: `FAIL` — `collectionsToRoadGeoJSON` undefined.

- [ ] **Step 3: Add types and function to `geojson.go`**

Add at the end of `geojson.go`:

```go
type geoJSONRoadProps struct {
	RoadName string  `json:"road_name"`
	Score    float64 `json:"score"`
	Color    string  `json:"color"`
}

type geoJSONMultiLineGeometry struct {
	Type  string         `json:"type"`
	Lines [][][2]float64 `json:"coordinates"`
}

type geoJSONRoadFeature struct {
	Type       string                   `json:"type"`
	Geometry   geoJSONMultiLineGeometry `json:"geometry"`
	Properties geoJSONRoadProps         `json:"properties"`
}

type geoJSONRoadFeatureCollection struct {
	Type         string               `json:"type"`
	RoadFeatures []geoJSONRoadFeature `json:"features"`
}

func collectionsToRoadGeoJSON(collections []quality.RoadCollection) geoJSONRoadFeatureCollection {
	fc := geoJSONRoadFeatureCollection{
		Type:         "FeatureCollection",
		RoadFeatures: make([]geoJSONRoadFeature, 0, len(collections)),
	}
	for _, c := range collections {
		level := quality.CurvatureColorLevel(c.TotalScore, quality.DefaultMinCurvature, quality.DefaultMaxCurvature)
		color := quality.GradientColorCSS(level)

		lines := make([][][2]float64, 0, len(c.Segments))
		for _, seg := range c.Segments {
			lines = append(lines, [][2]float64{
				{seg.Start.Lon, seg.Start.Lat},
				{seg.End.Lon, seg.End.Lat},
			})
		}

		fc.RoadFeatures = append(fc.RoadFeatures, geoJSONRoadFeature{
			Type: "Feature",
			Geometry: geoJSONMultiLineGeometry{
				Type:  "MultiLineString",
				Lines: lines,
			},
			Properties: geoJSONRoadProps{
				RoadName: c.DisplayName(),
				Score:    c.TotalScore,
				Color:    color,
			},
		})
	}
	return fc
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test . -run "TestCollectionsToRoadGeoJSON" -v
```

Expected: `PASS`.

- [ ] **Step 5: Run full test suite to check for regressions**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add geojson.go geojson_test.go
git commit -m "feat: add collectionsToRoadGeoJSON for road-level overlay"
```

---

## Task 3: Add `handleRoadSegments` and register the route

**Files:**
- Modify: `route_build.go` (handler registration ~line 72, handler implementation alongside `handleSegments` ~line 1036)
- Modify: `route_build_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `route_build_test.go` (after `TestHandleSegments_emptyCache`):

```go
func TestHandleRoadSegments_emptyCache(t *testing.T) {
	srv := &buildServer{
		cacheDir: t.TempDir(),
		tileSize: 0.1,
	}
	req := httptest.NewRequest("GET", "/api/road-segments?bbox=-76,40,-75,41", nil)
	w := httptest.NewRecorder()
	srv.handleRoadSegments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var fc geoJSONRoadFeatureCollection
	if err := json.NewDecoder(w.Body).Decode(&fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Type != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %q", fc.Type)
	}
	if len(fc.RoadFeatures) != 0 {
		t.Fatalf("expected 0 features for empty cache, got %d", len(fc.RoadFeatures))
	}
}

func TestHandleRoadSegments_invalidBbox(t *testing.T) {
	srv := &buildServer{
		cacheDir: t.TempDir(),
		tileSize: 0.1,
	}
	req := httptest.NewRequest("GET", "/api/road-segments?bbox=notvalid", nil)
	w := httptest.NewRecorder()
	srv.handleRoadSegments(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test . -run "TestHandleRoadSegments" -v
```

Expected: `FAIL` — `handleRoadSegments` undefined.

- [ ] **Step 3: Add handler to `route_build.go`**

Add after `handleSegments` (~line 1067):

```go
func (s *buildServer) handleRoadSegments(w http.ResponseWriter, r *http.Request) {
	west, south, east, north, err := parseBBox(r.URL.Query().Get("bbox"))
	if err != nil {
		http.Error(w, "invalid bbox", http.StatusBadRequest)
		return
	}

	tiles := s.tilesInBBox(west, south, east, north)
	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}

	var allCollections []quality.RoadCollection
	for _, t := range tiles {
		if !cache.Has(t) {
			continue
		}
		data, err := cache.Read(t)
		if err != nil {
			continue
		}
		ways, err := quality.ParseTileData(data)
		if err != nil {
			continue
		}
		result := quality.RunScorePipeline(ways)
		allCollections = append(allCollections, quality.Aggregate(result.ScoredWays)...)
	}

	fc := collectionsToRoadGeoJSON(allCollections)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fc)
}
```

- [ ] **Step 4: Register the route**

In `execBuild` (~line 72), add after the existing `/api/segments/stream` registration:

```go
mux.HandleFunc("/api/road-segments", srv.handleRoadSegments)
```

- [ ] **Step 5: Run tests to confirm they pass**

```bash
go test . -run "TestHandleRoadSegments" -v
```

Expected: `PASS`.

- [ ] **Step 6: Run full test suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat: add /api/road-segments endpoint for road-level overlay"
```

---

## Task 4: Update frontend — `segmentStyle`, `mergeFeatures`, SSE management, toggle button

This task touches only the inline HTML/JS string inside `route_build.go`. The changes are:
1. Make `evtSource` a mutable `var` and extract `connectSSE()`.
2. Add `overlayMode` state var.
3. Update `segmentStyle` to branch on mode.
4. Update `mergeFeatures` to use mode-appropriate dedup key.
5. Update `loadVisibleSegments` to fetch the right endpoint.
6. Add toggle button to `#stats` HTML and `toggleOverlayMode()` JS function.
7. Disable toggle button below zoom 12.

**Files:**
- Modify: `route_build.go` (the large HTML/JS template string)

- [ ] **Step 1: Add `overlayMode` state variable**

Find the JS variable declarations block (~line 235, after `var scorePollTimer = null;`). Add:

```js
var overlayMode = 'ways'; // 'ways' | 'roads'
```

- [ ] **Step 2: Update `segmentStyle`**

Replace the existing `segmentStyle` function (~line 245):

```js
function segmentStyle(feature) {
  if (overlayMode === 'roads') {
    var color = feature && feature.properties ? feature.properties.color : '#475569';
    return { color: color || '#475569', weight: 3, opacity: 0.8 };
  }
  var tier = feature ? feature.properties.tier : 0;
  return { color: TIER_COLORS[Math.max(0, Math.min(tier, 4))], weight: 3, opacity: 0.8 };
}
```

- [ ] **Step 3: Update `mergeFeatures` to use mode-appropriate dedup key**

Replace the existing `mergeFeatures` function (~line 259):

```js
function mergeFeatures(features) {
  if (!features) return;
  features.forEach(function(f) {
    if (!f.geometry || !f.geometry.coordinates || f.geometry.coordinates.length < 1) return;
    var key;
    if (overlayMode === 'roads') {
      key = f.properties.road_name || '';
    } else {
      var firstCoord = f.geometry.coordinates[0];
      key = f.properties.way_id + ':' + firstCoord[0] + ':' + firstCoord[1];
    }
    if (renderedSegments.has(key)) return;
    renderedSegments.add(key);
    roadLayer.addData(f);
  });
}
```

- [ ] **Step 4: Update `loadVisibleSegments` to branch on mode and disable toggle at low zoom**

Replace the existing `loadVisibleSegments` function (~line 270):

```js
function loadVisibleSegments() {
  var zoom = map.getZoom();
  var toggleBtn = document.getElementById('btn-overlay-toggle');
  if (zoom < MIN_ZOOM) {
    map.removeLayer(roadLayer);
    roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);
    renderedSegments.clear();
    if (toggleBtn) toggleBtn.disabled = true;
    return;
  }
  if (toggleBtn) toggleBtn.disabled = false;
  var bbox = getBboxString();
  if (overlayMode === 'roads') {
    fetch('/api/road-segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(fc) { mergeFeatures(fc.features); })
      .catch(function() {});
  } else {
    fetch('/api/tiles?bbox=' + bbox);
    fetch('/api/segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(fc) { mergeFeatures(fc.features); })
      .catch(function() {});
  }
}
```

- [ ] **Step 5: Convert `evtSource` to mutable var and extract `connectSSE()`**

Find the current SSE setup (~line 303):

```js
var evtSource = new EventSource('/api/segments/stream');
evtSource.onmessage = function(e) {
  try {
    mergeFeatures(JSON.parse(e.data).features);
  } catch(err) {}
};
```

Replace with:

```js
var evtSource = null;

function connectSSE() {
  if (evtSource) { evtSource.close(); }
  evtSource = new EventSource('/api/segments/stream');
  evtSource.onmessage = function(e) {
    try {
      mergeFeatures(JSON.parse(e.data).features);
    } catch(err) {}
  };
}

connectSSE();
```

- [ ] **Step 6: Add `toggleOverlayMode()` function**

Add after `connectSSE()`:

```js
function toggleOverlayMode() {
  overlayMode = overlayMode === 'ways' ? 'roads' : 'ways';
  var btn = document.getElementById('btn-overlay-toggle');
  btn.textContent = overlayMode === 'ways' ? 'Switch to Road view' : 'Switch to Way view';
  map.removeLayer(roadLayer);
  roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);
  renderedSegments.clear();
  if (overlayMode === 'roads') {
    if (evtSource) { evtSource.close(); evtSource = null; }
  } else {
    connectSSE();
  }
  loadVisibleSegments();
}
```

- [ ] **Step 7: Add toggle button to `#stats` HTML**

Find the `#stats` div (~line 210). Add a new row after the last `<div class="row">` entry (after the Time row, before the `fetch-status` div):

```html
<div class="row" style="margin-top:8px;">
  <button id="btn-overlay-toggle" onclick="toggleOverlayMode()" style="width:100%;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Switch to Road view</button>
</div>
```

- [ ] **Step 8: Build and verify no compile errors**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 9: Run full test suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add route_build.go
git commit -m "feat: add Ways/Roads overlay toggle to build map"
```

---

## Task 5: Manual smoke test

- [ ] **Step 1: Build and start the server**

```bash
go build -o bin/twisty . && bin/twisty build --address "Royersford, PA"
```

Open the printed URL in a browser.

- [ ] **Step 2: Verify ways mode (default)**

Pan/zoom to zoom 12+. Confirm the road overlay loads with tier colors (green/yellow/orange/red segments). Confirm the "Switch to Road view" button is present in the stats panel and enabled at zoom 12+, disabled below zoom 12.

- [ ] **Step 3: Toggle to road mode**

Click "Switch to Road view". Confirm:
- Button label changes to "Switch to Way view"
- Overlay clears and reloads with uniform per-road colors (yellow→red→magenta gradient)
- Panning reloads road-mode features

- [ ] **Step 4: Toggle back to ways mode**

Click "Switch to Way view". Confirm:
- Button label changes back to "Switch to Road view"
- Overlay reloads with per-segment tier colors
- SSE stream resumes (new tiles arriving should trigger overlay updates)

- [ ] **Step 5: Zoom below 12**

Zoom out below zoom 12. Confirm overlay clears and toggle button becomes disabled in both modes.

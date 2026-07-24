# OSM Hover Tooltip Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show `highway` and `maxspeed` OSM tags in a floating tooltip when hovering over a road segment in Way view mode.

**Architecture:** Enrich `geoJSONSegmentProps` with two new pointer fields (`Highway`, `Maxspeed`) populated from `ScoredWay.Tags` at GeoJSON build time. The frontend attaches Leaflet `mouseover`/`mouseout` handlers to `roadLayer` (re-attached after each rebuild) and positions a fixed `<div>` tooltip near the cursor via a `mousemove` handler on the map.

**Tech Stack:** Go (server-side GeoJSON), Leaflet.js (map events), vanilla JS + HTML/CSS (tooltip div embedded in `buildHTML` string in `route_build.go`).

## Global Constraints

- All HTML/JS/CSS lives inside the `buildHTML` const string in `route_build.go` — no separate template files.
- Tooltip only appears in Way view (`overlayMode === OVERLAY_WAYS`). It does nothing in Road view.
- No new API endpoints. Data flows through existing GeoJSON responses.
- Run tests with: `go test ./...` from the repo root.

---

### Task 1: Add `highway` and `maxspeed` to `geoJSONSegmentProps` and `collectionsToGeoJSON`

**Files:**
- Modify: `geojson.go`
- Modify: `geojson_test.go`

**Interfaces:**
- Produces: `geoJSONSegmentProps.Highway *string` and `geoJSONSegmentProps.Maxspeed *string` (pointer, `omitempty`)
- Produces: `collectionsToGeoJSON(collections []quality.RoadCollection, scoredWays quality.ScoredWays) geoJSONFeatureCollection` — new second parameter

**Context:**
- `geoJSONSegmentProps` is in `geojson.go`. It currently has `RoadName`, `Tier`, `Score`, `WayID`.
- `collectionsToGeoJSON` iterates `collections`, and for each `collection.Segments[i]` emits a feature. Each `ScoredSegment` has `WayID int64`. Each `ScoredWay` (in `quality.ScoredWays`, which is `[]quality.ScoredWay`) has `WayID int64` and `Tags map[string]string`.
- Both callers of `collectionsToGeoJSON` in `route_build.go` have `result.ScoredWays` in scope already (`result := quality.RunScorePipeline(ways)`).
- Use pointer fields with `omitempty` so ways without `maxspeed` don't emit a `null` JSON field.

- [ ] **Step 1: Write the failing test**

Add to `geojson_test.go`:

```go
func TestCollectionsToGeoJSON_osmTags(t *testing.T) {
	hw := "secondary"
	ms := "45 mph"
	scoredWays := quality.ScoredWays{
		{
			WayID: 42,
			Tags:  map[string]string{"highway": hw, "maxspeed": ms},
			Segments: []quality.ScoredSegment{
				{
					WayID: 42,
					Start: geo.Coord{Lat: 40.0, Lon: -75.0},
					End:   geo.Coord{Lat: 40.1, Lon: -75.1},
				},
			},
		},
	}
	collections := []quality.RoadCollection{
		{
			Name: "Test Road",
			Segments: []quality.ScoredSegment{
				{
					WayID: 42,
					Start: geo.Coord{Lat: 40.0, Lon: -75.0},
					End:   geo.Coord{Lat: 40.1, Lon: -75.1},
				},
			},
		},
	}
	fc := collectionsToGeoJSON(collections, scoredWays)
	if len(fc.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(fc.Features))
	}
	p := fc.Features[0].Properties
	if p.Highway == nil || *p.Highway != hw {
		t.Errorf("Highway = %v, want %q", p.Highway, hw)
	}
	if p.Maxspeed == nil || *p.Maxspeed != ms {
		t.Errorf("Maxspeed = %v, want %q", p.Maxspeed, ms)
	}
}

func TestCollectionsToGeoJSON_missingMaxspeed(t *testing.T) {
	scoredWays := quality.ScoredWays{
		{
			WayID: 7,
			Tags:  map[string]string{"highway": "residential"},
			Segments: []quality.ScoredSegment{
				{WayID: 7, Start: geo.Coord{Lat: 40.0, Lon: -75.0}, End: geo.Coord{Lat: 40.1, Lon: -75.1}},
			},
		},
	}
	collections := []quality.RoadCollection{
		{
			Name: "Side St",
			Segments: []quality.ScoredSegment{
				{WayID: 7, Start: geo.Coord{Lat: 40.0, Lon: -75.0}, End: geo.Coord{Lat: 40.1, Lon: -75.1}},
			},
		},
	}
	fc := collectionsToGeoJSON(collections, scoredWays)
	if len(fc.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(fc.Features))
	}
	p := fc.Features[0].Properties
	if p.Highway == nil || *p.Highway != "residential" {
		t.Errorf("Highway = %v, want \"residential\"", p.Highway)
	}
	if p.Maxspeed != nil {
		t.Errorf("Maxspeed should be nil when tag absent, got %q", *p.Maxspeed)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./... -run TestCollectionsToGeoJSON_osmTags -v
go test ./... -run TestCollectionsToGeoJSON_missingMaxspeed -v
```

Expected: compile error (wrong number of arguments to `collectionsToGeoJSON`) or `FAIL`.

- [ ] **Step 3: Add `Highway` and `Maxspeed` fields to `geoJSONSegmentProps`**

In `geojson.go`, replace the `geoJSONSegmentProps` struct:

```go
type geoJSONSegmentProps struct {
	RoadName string  `json:"road_name"`
	Tier     int     `json:"tier"`
	Score    float64 `json:"score"`
	WayID    int64   `json:"way_id"`
	Highway  *string `json:"highway,omitempty"`
	Maxspeed *string `json:"maxspeed,omitempty"`
}
```

- [ ] **Step 4: Update `collectionsToGeoJSON` signature and implementation**

In `geojson.go`, replace the `collectionsToGeoJSON` function:

```go
func collectionsToGeoJSON(collections []quality.RoadCollection, scoredWays quality.ScoredWays) geoJSONFeatureCollection {
	tagsByWayID := make(map[int64]map[string]string, len(scoredWays))
	for _, sw := range scoredWays {
		tagsByWayID[sw.WayID] = sw.Tags
	}

	total := 0
	for _, c := range collections {
		total += len(c.Segments)
	}
	fc := geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]geoJSONFeature, 0, total),
	}
	for _, c := range collections {
		for _, seg := range c.Segments {
			props := geoJSONSegmentProps{
				RoadName: c.DisplayName(),
				Tier:     seg.Tier,
				Score:    seg.Score,
				WayID:    seg.WayID,
			}
			if tags, ok := tagsByWayID[seg.WayID]; ok {
				if hw := tags["highway"]; hw != "" {
					props.Highway = &hw
				}
				if ms := tags["maxspeed"]; ms != "" {
					props.Maxspeed = &ms
				}
			}
			fc.Features = append(fc.Features, geoJSONFeature{
				Type: "Feature",
				Geometry: geoJSONGeometry{
					Type: "LineString",
					Coordinates: [][2]float64{
						{seg.Start.Lon, seg.Start.Lat},
						{seg.End.Lon, seg.End.Lat},
					},
				},
				Properties: props,
			})
		}
	}
	return fc
}
```

- [ ] **Step 5: Fix callers of `collectionsToGeoJSON` in `route_build.go`**

There are two callers. Both have `result.ScoredWays` in scope.

**Caller 1** — `handleSegments` (around line 2243). The loop accumulates `allCollections` across tiles but `result` is loop-scoped. We need to accumulate `allScoredWays` too. Replace the loop and call:

```go
// Before the tile loop (near the `var allCollections` declaration):
var allCollections []quality.RoadCollection
var allScoredWays quality.ScoredWays

// Inside the loop, after `result := quality.RunScorePipeline(ways)`:
allCollections = append(allCollections, quality.Aggregate(result.ScoredWays)...)
allScoredWays = append(allScoredWays, result.ScoredWays...)

// The call site (was `collectionsToGeoJSON(allCollections)`):
fc := collectionsToGeoJSON(allCollections, allScoredWays)
```

Full updated loop block (replace lines ~2213–2243):

```go
var missing []quality.Tile
var allCollections []quality.RoadCollection
var allScoredWays quality.ScoredWays
for _, t := range tiles {
    if !cache.Has(t) {
        missing = append(missing, t)
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
    allScoredWays = append(allScoredWays, result.ScoredWays...)
}
```

And update the call:
```go
fc := collectionsToGeoJSON(allCollections, allScoredWays)
```

**Caller 2** — `runTileSSEBroadcaster` (around line 2353). `result.ScoredWays` is already in scope as a single tile:

```go
result := quality.RunScorePipeline(ways)
collections := quality.Aggregate(result.ScoredWays)
fc := collectionsToGeoJSON(collections, result.ScoredWays)
```

- [ ] **Step 6: Run all tests**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add geojson.go geojson_test.go route_build.go
git commit -m "feat(build): add highway and maxspeed to Way view GeoJSON features"
```

---

### Task 2: Add tooltip div and JS to the build UI

**Files:**
- Modify: `route_build.go` (the `buildHTML` const string)

**Interfaces:**
- Consumes: `feature.properties.highway` and `feature.properties.maxspeed` from Task 1's GeoJSON
- Consumes: `overlayMode`, `OVERLAY_WAYS`, `roadLayer`, `rebuildSegmentLayer()`, `toggleOverlayMode()`, `toggleOverlayVisibility()` — all already defined in `buildHTML`

**Context:**
- The `buildHTML` const is a Go raw string in `route_build.go`. It uses `%%` for literal `%` (Go's `fmt.Sprintf` format string escaping).
- `roadLayer` is created with `var roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);` and is recreated inside `rebuildSegmentLayer()` — the tooltip handlers must be re-attached each rebuild.
- `rebuildSegmentLayer()` creates a new layer: `roadLayer = L.geoJSON(null, { style: segmentStyle });`. Handlers are attached immediately after.
- `toggleOverlayVisibility()` hides the overlay when `overlayVisible` is true — add tooltip hide there too.
- `toggleOverlayMode()` calls `rebuildSegmentLayer()` which reattaches handlers, but hide the tooltip immediately on toggle.
- The `map.on('mousemove', ...)` handler already exists as `map.on('moveend zoomend', ...)` in the file — add a new separate `map.on('mousemove', ...)` block.

- [ ] **Step 1: Add the tooltip `<div>` after `#toast`**

In `route_build.go`, find the line:

```html
<div id="toast"></div>
```

Add immediately after it:

```html
<div id="osm-tooltip" style="display:none;position:fixed;z-index:2000;background:rgba(255,255,255,0.95);border-radius:8px;padding:8px 12px;box-shadow:0 2px 8px rgba(0,0,0,0.15);font-size:12px;pointer-events:none;min-width:120px;"></div>
```

- [ ] **Step 2: Define `attachTooltipHandlers` and wire it to initial `roadLayer` creation**

In `route_build.go`, find this line in the JS section:

```js
var roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);
```

Replace it with:

```js
function attachTooltipHandlers(layer) {
  layer.on('mouseover', function(e) {
    if (overlayMode !== OVERLAY_WAYS) return;
    var p = e.layer.feature && e.layer.feature.properties;
    var hw = (p && p.highway) || '—';
    var ms = (p && p.maxspeed) || '—';
    var el = document.getElementById('osm-tooltip');
    el.innerHTML = '<b>' + hw + '</b><br>Max speed: ' + ms;
    el.style.display = 'block';
  });
  layer.on('mouseout', function() {
    document.getElementById('osm-tooltip').style.display = 'none';
  });
}
var roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);
attachTooltipHandlers(roadLayer);
```

Note: use `'—'` (em-dash) rather than the `—` HTML entity since this is a JS string.

- [ ] **Step 3: Re-attach handlers inside `rebuildSegmentLayer()`**

In `route_build.go`, find the `rebuildSegmentLayer` function. It ends with:

```js
  renderedSegments.clear();
  return segmentGeneration;
}
```

Find the line inside it that creates the new layer:

```js
  roadLayer = L.geoJSON(null, { style: segmentStyle });
```

Add `attachTooltipHandlers(roadLayer);` immediately after:

```js
  roadLayer = L.geoJSON(null, { style: segmentStyle });
  attachTooltipHandlers(roadLayer);
```

- [ ] **Step 4: Add `mousemove` handler on the map to track cursor position**

In `route_build.go`, find the existing map event binding:

```js
map.on('moveend zoomend', function() {
```

Add a new block immediately before it:

```js
map.on('mousemove', function(e) {
  var el = document.getElementById('osm-tooltip');
  if (el.style.display === 'none') return;
  var offset = 14;
  el.style.left = (e.originalEvent.clientX + offset) + 'px';
  el.style.top  = (e.originalEvent.clientY + offset) + 'px';
});
```

- [ ] **Step 5: Hide tooltip on overlay mode toggle**

In `route_build.go`, find `function toggleOverlayMode()`. It starts with:

```js
function toggleOverlayMode() {
  overlayMode = overlayMode === OVERLAY_WAYS ? OVERLAY_ROADS : OVERLAY_WAYS;
```

Add the tooltip hide as the first statement inside the function body:

```js
function toggleOverlayMode() {
  document.getElementById('osm-tooltip').style.display = 'none';
  overlayMode = overlayMode === OVERLAY_WAYS ? OVERLAY_ROADS : OVERLAY_WAYS;
```

- [ ] **Step 6: Hide tooltip when overlay is hidden**

In `route_build.go`, find `function toggleOverlayVisibility()`. The hide branch looks like:

```js
  if (overlayVisible) {
    map.removeLayer(roadLayer);
    overlayVisible = false;
    btn.textContent = 'Show overlay';
```

Add the tooltip hide inside that branch:

```js
  if (overlayVisible) {
    map.removeLayer(roadLayer);
    overlayVisible = false;
    btn.textContent = 'Show overlay';
    document.getElementById('osm-tooltip').style.display = 'none';
```

- [ ] **Step 7: Build to verify no compile errors**

```bash
go build ./...
```

Expected: exits 0 with no output.

- [ ] **Step 8: Manual verification**

Run the build server:
```bash
go run . build --address "your test address"
```

1. Open the UI in a browser.
2. Confirm the overlay loads in Way view (default).
3. Hover over a road segment — tooltip should appear near the cursor showing highway type (e.g., `secondary`) and max speed (e.g., `45 mph`) or `—` if absent.
4. Move the mouse — tooltip should follow.
5. Move off the road — tooltip should disappear.
6. Click "Switch to Road view" — tooltip should immediately disappear and not reappear on hover.
7. Switch back to Way view — tooltip should work again.
8. Click "Hide overlay" — tooltip should disappear.

- [ ] **Step 9: Commit**

```bash
git add route_build.go
git commit -m "feat(build): show OSM highway and maxspeed on hover in Way view"
```

# Build Tier Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Embed a tier-colored road overlay into `twisty build` so all cached roads in the viewport are colored by curvature tier (green→yellow→orange→dark orange→red) at zoom 12+, with lazy tile fetching as the user pans.

**Architecture:** Move shared GeoJSON types and helpers from `diag_serve.go` into a new `geojson.go` file. Add `/api/segments`, `/api/tiles`, and `/api/segments/stream` (SSE) handlers to `route_build.go`. The frontend replaces the solid blue route line with a tier-colored road overlay drawn from GeoJSON, using the same approach as `diag serve`.

**Tech Stack:** Go (net/http, SSE via `text/event-stream`), Leaflet 1.9.4 (L.geoJSON), existing `quality` package pipeline.

---

### Task 1: Move shared GeoJSON code to `geojson.go`

`diag_serve.go` defines types and helpers used by both the diag server and the build server. Moving them to a shared file avoids duplication and keeps both files compilable.

**Files:**
- Create: `geojson.go`
- Modify: `diag_serve.go` (remove moved declarations)
- Modify: `diag_serve_test.go` (no changes needed — same package)

- [ ] **Step 1: Verify tests pass before touching anything**

```bash
go test ./... -count=1
```
Expected: all tests PASS.

- [ ] **Step 2: Create `geojson.go` with the moved types and functions**

Create `/Users/joshuafeierman/repos/yardbirdsax/twisty/geojson.go`:

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
	Type        string       `json:"type"`
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
					RoadName: c.DisplayName(),
					Tier:     seg.Tier,
					Score:    seg.Score,
					WayID:    seg.WayID,
				},
			})
		}
	}
	return fc
}

func filterFeaturesByBBox(fc geoJSONFeatureCollection, west, south, east, north float64) geoJSONFeatureCollection {
	out := geoJSONFeatureCollection{Type: "FeatureCollection", Features: []geoJSONFeature{}}
	for _, f := range fc.Features {
		if len(f.Geometry.Coordinates) == 0 {
			continue
		}
		lon, lat := f.Geometry.Coordinates[0][0], f.Geometry.Coordinates[0][1]
		if lon >= west && lon <= east && lat >= south && lat <= north {
			out.Features = append(out.Features, f)
		}
	}
	return out
}

func parseBBox(s string) (west, south, east, north float64, err error) {
	if s == "" {
		return -180, -90, 180, 90, nil
	}
	_, err = fmt.Sscanf(s, "%f,%f,%f,%f", &west, &south, &east, &north)
	return
}
```

Add `"fmt"` to the import block.

- [ ] **Step 3: Remove the moved declarations from `diag_serve.go`**

Delete these from `diag_serve.go` (they now live in `geojson.go`):
- The `geoJSONSegmentProps` struct
- The `geoJSONGeometry` struct
- The `geoJSONFeature` struct
- The `geoJSONFeatureCollection` struct
- The `collectionsToGeoJSON` function
- The `filterFeaturesByBBox` function
- The `parseBBox` function

Keep all other `diag_serve.go` content unchanged.

- [ ] **Step 4: Run tests to confirm nothing broke**

```bash
go test ./... -count=1
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add geojson.go diag_serve.go
git commit -m "refactor: move shared GeoJSON types and helpers to geojson.go"
```

---

### Task 2: Add `sseBroker` and wire `tileReady` into `buildServer`

The SSE broadcaster is a lightweight goroutine that fans out `[]byte` payloads to all currently connected clients.

**Files:**
- Modify: `route_build.go`
- Modify: `route_build_test.go`

- [ ] **Step 1: Write a failing test for sseBroker**

Add to `route_build_test.go`:

```go
func TestSSEBroker_fanOut(t *testing.T) {
	b := newSSEBroker()
	go b.run()

	ch1 := b.subscribe()
	ch2 := b.subscribe()

	msg := []byte("data: hello\n\n")
	b.broadcast(msg)

	timeout := time.After(time.Second)
	for _, ch := range []chan []byte{ch1, ch2} {
		select {
		case got := <-ch:
			if string(got) != string(msg) {
				t.Fatalf("expected %q, got %q", msg, got)
			}
		case <-timeout:
			t.Fatal("timed out waiting for SSE message")
		}
	}

	b.unsubscribe(ch1)
	b.unsubscribe(ch2)
}
```

Add `"time"` to the import block in `route_build_test.go`.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -run TestSSEBroker_fanOut -v ./...
```
Expected: FAIL — `newSSEBroker` undefined.

- [ ] **Step 3: Implement `sseBroker` in `route_build.go`**

Add after the `buildServer` struct definition (before `execBuild`):

```go
type sseBroker struct {
	subscribeCh   chan chan []byte
	unsubscribeCh chan chan []byte
	broadcastCh   chan []byte
}

func newSSEBroker() *sseBroker {
	return &sseBroker{
		subscribeCh:   make(chan chan []byte, 8),
		unsubscribeCh: make(chan chan []byte, 8),
		broadcastCh:   make(chan []byte, 64),
	}
}

func (b *sseBroker) run() {
	clients := make(map[chan []byte]struct{})
	for {
		select {
		case ch := <-b.subscribeCh:
			clients[ch] = struct{}{}
		case ch := <-b.unsubscribeCh:
			delete(clients, ch)
		case msg := <-b.broadcastCh:
			for ch := range clients {
				select {
				case ch <- msg:
				default: // slow client; drop
				}
			}
		}
	}
}

func (b *sseBroker) subscribe() chan []byte {
	ch := make(chan []byte, 8)
	b.subscribeCh <- ch
	return ch
}

func (b *sseBroker) unsubscribe(ch chan []byte) {
	b.unsubscribeCh <- ch
}

func (b *sseBroker) broadcast(msg []byte) {
	select {
	case b.broadcastCh <- msg:
	default:
	}
}
```

- [ ] **Step 4: Add `tileReady` and `sseBroker` to `buildServer`**

Update the `buildServer` struct in `route_build.go`:

```go
type buildServer struct {
	center      geocode.Result
	overpassURL string
	cacheDir    string
	tileSize    float64
	fetchDelay  string
	failedTiles sync.Map
	tileReady   chan quality.Tile
	broker      *sseBroker
}
```

- [ ] **Step 5: Initialize broker in `execBuild`**

In `execBuild`, update the `buildServer` initialisation:

```go
broker := newSSEBroker()
go broker.run()

srv := &buildServer{
	center:      center,
	overpassURL: p.overpassURL,
	cacheDir:    cacheDir,
	tileSize:    p.tileSize,
	fetchDelay:  p.fetchDelay,
	tileReady:   make(chan quality.Tile, 64),
	broker:      broker,
}
```

- [ ] **Step 6: Signal `tileReady` in `fetchMissingTiles` after successful cache write**

In `fetchMissingTiles`, after `quality.FetchTiledWaysForTiles` returns and before the failed-tile loop, add:

```go
// Notify SSE broker for each tile that landed in cache.
for _, t := range tiles {
	if cache.Has(t) {
		select {
		case s.tileReady <- t:
		default:
		}
	}
}
```

- [ ] **Step 7: Run tests**

```bash
go test ./... -count=1
```
Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): add SSE broker and tileReady channel to buildServer"
```

---

### Task 3: Add `/api/segments` and `/api/tiles` handlers

**Files:**
- Modify: `route_build.go`
- Modify: `route_build_test.go`

- [ ] **Step 1: Write failing tests**

Add to `route_build_test.go`:

```go
func TestHandleSegments_emptyCache(t *testing.T) {
	srv := &buildServer{
		cacheDir: t.TempDir(),
		tileSize: 0.1,
	}
	req := httptest.NewRequest("GET", "/api/segments?bbox=-76,40,-75,41", nil)
	w := httptest.NewRecorder()
	srv.handleSegments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var fc geoJSONFeatureCollection
	if err := json.NewDecoder(w.Body).Decode(&fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Type != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %q", fc.Type)
	}
	if len(fc.Features) != 0 {
		t.Fatalf("expected 0 features for empty cache, got %d", len(fc.Features))
	}
}

func TestHandleTiles_returns204(t *testing.T) {
	srv := &buildServer{
		cacheDir:    t.TempDir(),
		tileSize:    0.1,
		overpassURL: "http://127.0.0.1:1", // unreachable — fetch will fail silently
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()
	req := httptest.NewRequest("GET", "/api/tiles?bbox=-76,40,-75,41", nil)
	w := httptest.NewRecorder()
	srv.handleTiles(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test -run "TestHandleSegments_emptyCache|TestHandleTiles_returns204" -v ./...
```
Expected: FAIL — `handleSegments` and `handleTiles` undefined.

- [ ] **Step 3: Implement `handleSegments`**

Add to `route_build.go`:

```go
func (s *buildServer) handleSegments(w http.ResponseWriter, r *http.Request) {
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

	fc := collectionsToGeoJSON(allCollections)
	filtered := filterFeaturesByBBox(fc, west, south, east, north)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filtered)
}
```

- [ ] **Step 4: Implement `tilesInBBox`**

Add to `route_build.go`:

```go
func (s *buildServer) tilesInBBox(west, south, east, north float64) []quality.Tile {
	size := s.tileSize
	if size <= 0 {
		size = 0.1
	}
	seen := make(map[[2]int]bool)
	var tiles []quality.Tile
	latSteps := int(math.Ceil((north-south)/size)) + 1
	lonSteps := int(math.Ceil((east-west)/size)) + 1
	for i := 0; i <= latSteps; i++ {
		for j := 0; j <= lonSteps; j++ {
			lat := south + float64(i)*size
			lon := west + float64(j)*size
			latIdx := int(math.Floor(lat / size))
			lonIdx := int(math.Floor(lon / size))
			key := [2]int{latIdx, lonIdx}
			if seen[key] {
				continue
			}
			seen[key] = true
			tiles = append(tiles, quality.Tile{
				South: float64(latIdx) * size,
				West:  float64(lonIdx) * size,
				North: float64(latIdx+1) * size,
				East:  float64(lonIdx+1) * size,
			})
		}
	}
	return tiles
}
```

- [ ] **Step 5: Implement `handleTiles`**

Add to `route_build.go`:

```go
func (s *buildServer) handleTiles(w http.ResponseWriter, r *http.Request) {
	west, south, east, north, err := parseBBox(r.URL.Query().Get("bbox"))
	if err != nil {
		http.Error(w, "invalid bbox", http.StatusBadRequest)
		return
	}

	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}
	tiles := s.tilesInBBox(west, south, east, north)

	var missing []quality.Tile
	for _, t := range tiles {
		if !cache.Has(t) {
			missing = append(missing, t)
		}
	}

	if len(missing) > 0 {
		go s.fetchMissingTiles(missing)
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 6: Register new handlers in `execBuild`**

In `execBuild`, add to the mux registrations:

```go
mux.HandleFunc("/api/segments", srv.handleSegments)
mux.HandleFunc("/api/tiles", srv.handleTiles)
```

- [ ] **Step 7: Run tests**

```bash
go test ./... -count=1
```
Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): add /api/segments and /api/tiles handlers"
```

---

### Task 4: Add `/api/segments/stream` SSE handler

**Files:**
- Modify: `route_build.go`
- Modify: `route_build_test.go`

- [ ] **Step 1: Write a failing test**

Add to `route_build_test.go`:

```go
func TestHandleSegmentsStream_receivesEvent(t *testing.T) {
	broker := newSSEBroker()
	go broker.run()

	srv := &buildServer{
		cacheDir:    t.TempDir(),
		tileSize:    0.1,
		overpassURL: "http://127.0.0.1:1",
		tileReady:   make(chan quality.Tile, 64),
		broker:      broker,
	}

	req := httptest.NewRequest("GET", "/api/segments/stream", nil)
	// Use a context with cancel so we can disconnect the client.
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.handleSegmentsStream(w, req)
		close(done)
	}()

	// Broadcast a message and then cancel the context.
	broker.broadcast([]byte("data: {}\n\n"))
	cancel()
	<-done

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}
}
```

Add `"context"` to the import block in `route_build_test.go`.

- [ ] **Step 2: Run to verify it fails**

```bash
go test -run TestHandleSegmentsStream_receivesEvent -v ./...
```
Expected: FAIL — `handleSegmentsStream` undefined.

- [ ] **Step 3: Implement `handleSegmentsStream`**

Add to `route_build.go`:

```go
func (s *buildServer) handleSegmentsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ch := s.broker.subscribe()
	defer s.broker.unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "%s", msg)
			flusher.Flush()
		}
	}
}
```

- [ ] **Step 4: Add SSE broadcaster goroutine that reads `tileReady`**

Add a method to `buildServer` that runs the tile→GeoJSON→SSE fan-out:

```go
func (s *buildServer) runTileSSEBroadcaster() {
	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}
	for t := range s.tileReady {
		data, err := cache.Read(t)
		if err != nil {
			continue
		}
		ways, err := quality.ParseTileData(data)
		if err != nil {
			continue
		}
		result := quality.RunScorePipeline(ways)
		collections := quality.Aggregate(result.ScoredWays)
		fc := collectionsToGeoJSON(collections)
		payload, err := json.Marshal(fc)
		if err != nil {
			continue
		}
		s.broker.broadcast([]byte("data: " + string(payload) + "\n\n"))
	}
}
```

- [ ] **Step 5: Start broadcaster in `execBuild`**

In `execBuild`, after `go broker.run()`:

```go
go srv.runTileSSEBroadcaster()
```

- [ ] **Step 6: Register the SSE handler in `execBuild`**

Add to the mux registrations in `execBuild`:

```go
mux.HandleFunc("/api/segments/stream", srv.handleSegmentsStream)
```

- [ ] **Step 7: Run tests**

```bash
go test ./... -count=1
```
Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): add /api/segments/stream SSE endpoint"
```

---

### Task 5: Update the frontend HTML

**Files:**
- Modify: `route_build.go` (the `buildHTML` constant)

No server-side tests for HTML; verify by running the server and loading the page.

- [ ] **Step 1: Add `roadLayer`, `renderedSegments`, `segmentStyle`, `MIN_ZOOM`, `getBboxString`, and `mergeFeatures` to the JS**

In `buildHTML`, add to the `<script>` block, after `var scorePollTimer = null;`:

```js
var MIN_ZOOM = 12;
var TIER_COLORS = ['#475569', '#4ade80', '#facc15', '#fb923c', '#f87171'];

function segmentStyle(feature) {
  var tier = feature ? feature.properties.tier : 0;
  return { color: TIER_COLORS[Math.max(0, Math.min(tier, 4))], weight: 3, opacity: 0.8 };
}

var roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);
var renderedSegments = new Set();

function getBboxString() {
  var b = map.getBounds();
  return b.getWest().toFixed(6) + ',' + b.getSouth().toFixed(6) + ',' +
         b.getEast().toFixed(6) + ',' + b.getNorth().toFixed(6);
}

function mergeFeatures(features) {
  if (!features) return;
  features.forEach(function(f) {
    if (!f.geometry || !f.geometry.coordinates || f.geometry.coordinates.length < 1) return;
    var key = f.properties.way_id + ':' + f.geometry.coordinates[0][0] + ':' + f.geometry.coordinates[0][1];
    if (renderedSegments.has(key)) return;
    renderedSegments.add(key);
    roadLayer.addData(f);
  });
}

function loadVisibleSegments() {
  if (map.getZoom() < MIN_ZOOM) {
    roadLayer.clearLayers();
    renderedSegments.clear();
    return;
  }
  var bbox = getBboxString();
  fetch('/api/tiles?bbox=' + bbox);
  fetch('/api/segments?bbox=' + bbox)
    .then(function(r) { return r.json(); })
    .then(function(fc) { mergeFeatures(fc.features); })
    .catch(function() {});
}

map.on('moveend zoomend', loadVisibleSegments);
loadVisibleSegments();

var evtSource = new EventSource('/api/segments/stream');
evtSource.onmessage = function(e) {
  try {
    mergeFeatures(JSON.parse(e.data).features);
  } catch(err) {}
};
```

Note: `roadLayer` must be declared before it is used by `mergeFeatures`. Since `L.geoJSON` accepts `null` as initial data, this is safe to call at init time.

- [ ] **Step 2: Change the route leg polyline style to a thin blue placeholder**

In `buildHTML`, in the `addWaypoint` function's `.then` callback, change the polyline creation from:

```js
var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 4 }).addTo(map);
```

to:

```js
var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5 }).addTo(map);
```

- [ ] **Step 3: Run the server and verify visually**

```bash
go build -o bin/twisty . && ./bin/twisty build --address "Mulholland Drive, Los Angeles, CA" --port 8080
```

Open `http://127.0.0.1:8080` in a browser. Zoom in to level 12+. Verify:
- Tier-colored road segments appear for any cached tiles in the area.
- Adding waypoints draws a thin blue placeholder line on top of the road overlay.
- Panning triggers tile fetches (watch stderr for fetch log output).
- SSE connection is open (Network tab → `/api/segments/stream` shows `EventStream`).

- [ ] **Step 4: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add tier-colored road overlay to build frontend"
```

---

### Task 6: Update integration test to register new handlers

The existing `TestBuildServer_integration` test builds a `mux` manually. It needs the new handlers registered so the test server matches what `execBuild` wires up.

**Files:**
- Modify: `route_build_test.go`

- [ ] **Step 1: Update `TestBuildServer_integration`**

In `route_build_test.go`, update the mux setup in `TestBuildServer_integration`:

```go
broker := newSSEBroker()
go broker.run()
srv := &buildServer{
	overpassURL: quality.OverpassBaseURL,
	cacheDir:    t.TempDir(),
	tileSize:    0.1,
	tileReady:   make(chan quality.Tile, 64),
	broker:      broker,
}
go srv.runTileSSEBroadcaster()

mux := http.NewServeMux()
mux.HandleFunc("/", srv.handleIndex)
mux.HandleFunc("/api/route-leg", srv.handleRouteLeg)
mux.HandleFunc("/api/score", srv.handleScore)
mux.HandleFunc("/api/export", srv.handleExport)
mux.HandleFunc("/api/segments", srv.handleSegments)
mux.HandleFunc("/api/tiles", srv.handleTiles)
mux.HandleFunc("/api/segments/stream", srv.handleSegmentsStream)
```

Add a check that `/api/segments` returns a valid (possibly empty) FeatureCollection:

```go
resp, err = http.Get(ts.URL + "/api/segments?bbox=-76,40,-75,41")
if err != nil {
	t.Fatalf("GET /api/segments: %v", err)
}
var segFC geoJSONFeatureCollection
if err := json.NewDecoder(resp.Body).Decode(&segFC); err != nil {
	t.Fatalf("decode /api/segments: %v", err)
}
resp.Body.Close()
if segFC.Type != "FeatureCollection" {
	t.Fatalf("expected FeatureCollection from /api/segments, got %q", segFC.Type)
}

resp, err = http.Get(ts.URL + "/api/tiles?bbox=-76,40,-75,41")
if err != nil {
	t.Fatalf("GET /api/tiles: %v", err)
}
resp.Body.Close()
if resp.StatusCode != http.StatusNoContent {
	t.Fatalf("expected 204 from /api/tiles, got %d", resp.StatusCode)
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./... -count=1
```
Expected: all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add route_build_test.go
git commit -m "test(build): update integration test to include new overlay handlers"
```

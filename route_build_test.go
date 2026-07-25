// route_build_test.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/geocode"
	"github.com/yardbirdsax/twisty/quality"
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

func TestExecBuild_missingAddress(t *testing.T) {
	err := execBuild(buildParams{address: ""})
	if err == nil {
		t.Fatal("expected error for missing address")
	}
	if !strings.Contains(err.Error(), "--address is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleRouteLeg_success(t *testing.T) {
	// Start a stub Valhalla server so the test does not hit the live network.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// "??" is a valid polyline6 encoding of a single point (0,0).
		fmt.Fprint(w, `{"trip":{"summary":{"time":60,"length":1.0},"legs":[{"shape":"??","summary":{"time":60,"length":1.0},"maneuvers":[]}]},"alternates":[]}`)
	}))
	defer stub.Close()

	srv := &buildServer{valhallaURL: stub.URL}

	body := `{"from":{"lat":40.0,"lon":-75.5},"to":{"lat":40.01,"lon":-75.49}}`
	req := httptest.NewRequest("POST", "/api/route-leg", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleRouteLeg(w, req)

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

func TestHandleRouteLeg_UsesConfiguredValhallaURL(t *testing.T) {
	// Start a stub Valhalla server.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Minimal valid Valhalla response.
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"trip":{"summary":{"time":60,"length":1.0},"legs":[{"shape":"??","summary":{"time":60,"length":1.0},"maneuvers":[]}]},"alternates":[]}`)
	}))
	defer stub.Close()

	srv := &buildServer{
		valhallaURL: stub.URL,
	}

	body := `{"from":{"lat":36.1,"lon":-86.7},"to":{"lat":36.2,"lon":-86.8}}`
	req := httptest.NewRequest(http.MethodPost, "/api/route-leg", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRouteLeg(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
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
	if len(tiles) > 4 {
		t.Fatalf("expected at most 4 tiles for a short polyline, got %d", len(tiles))
	}
}

func TestIsNearPolyline(t *testing.T) {
	polyline := [][2]float64{
		{40.0, -75.5},
		{40.01, -75.5},
	}

	if !isNearPolyline(geo.Coord{Lat: 40.005, Lon: -75.5}, polyline, 50) {
		t.Fatal("expected point on polyline to be near")
	}

	if isNearPolyline(geo.Coord{Lat: 41.0, Lon: -74.0}, polyline, 50) {
		t.Fatal("expected distant point to not be near")
	}
}

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

func TestHandleScore_returnsScorePerKm(t *testing.T) {
	cacheDir := t.TempDir()
	tile := quality.Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := cache.Write(tile, tileData); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	srv := &buildServer{tileSize: 0.1, cacheDir: cacheDir}
	// Route points inside the cached tile so they match segments.
	body := `{"points":[[40.010,-75.990],[40.012,-75.988]]}`
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
	if resp.ScorePerKm == 0 {
		t.Fatalf("expected ScorePerKm > 0 for cached tile with road data, got %f", resp.ScorePerKm)
	}
}

func TestScoreFromCachedTiles_returnsScorePerKm(t *testing.T) {
	cacheDir := t.TempDir()
	tile := quality.Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	// Minimal Overpass JSON: a hairpin curve (circumradius ~70m) that scores in tier 3 (r < 100m).
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := cache.Write(tile, tileData); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	srv := &buildServer{tileSize: 0.1, cacheDir: cacheDir}
	result := srv.scoreFromCachedTiles([]quality.Tile{tile}, cache, nil)

	if result.Score == 0 {
		t.Error("expected Score > 0")
	}
	if result.ScorePerKm == 0 {
		t.Error("expected ScorePerKm > 0")
	}
}

func TestSSEBroker_fanOut(t *testing.T) {
	b := newSSEBroker()
	go b.run()

	ch1 := b.subscribe()
	ch2 := b.subscribe()

	time.Sleep(10 * time.Millisecond) // Allow broker to process subscriptions

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

func TestHandleSegments_reportsPendingTilesWhenCacheEmpty(t *testing.T) {
	srv := &buildServer{
		cacheDir:    t.TempDir(),
		tileSize:    0.1,
		overpassURL: "http://127.0.0.1:1", // unreachable — fetch will fail silently
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()
	req := httptest.NewRequest("GET", "/api/segments?bbox=-76,40,-75,41", nil)
	w := httptest.NewRecorder()
	srv.handleSegments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Type         string `json:"type"`
		PendingTiles int    `json:"pending_tiles"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PendingTiles == 0 {
		t.Fatal("expected pending_tiles>0 when cache is empty and tiles need fetching")
	}
}

func TestHandleSegments_reportsFailedTiles(t *testing.T) {
	srv := &buildServer{
		cacheDir:    t.TempDir(),
		tileSize:    0.1,
		overpassURL: "http://127.0.0.1:1",
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()

	tile := quality.Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	srv.failedTiles.Store(tile, struct{}{})

	req := httptest.NewRequest("GET", "/api/segments?bbox=-75.8,39.9,-75.7,40.0", nil)
	w := httptest.NewRecorder()
	srv.handleSegments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		FailedTiles int `json:"failed_tiles"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.FailedTiles == 0 {
		t.Fatal("expected failed_tiles>0 when a tile is pre-marked failed")
	}
}

func TestHandleRoadSegments_emptyCache(t *testing.T) {
	srv := &buildServer{
		cacheDir:    t.TempDir(),
		tileSize:    0.1,
		overpassURL: "http://127.0.0.1:1", // unreachable — fetch will fail silently
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()
	req := httptest.NewRequest("GET", "/api/road-segments?bbox=-76,40,-75,41", nil)
	w := httptest.NewRecorder()
	srv.handleRoadSegments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp roadSegmentsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %q", resp.Type)
	}
	if len(resp.Features) != 0 {
		t.Fatalf("expected 0 features for empty cache, got %d", len(resp.Features))
	}
	if resp.PendingTiles == 0 {
		t.Fatal("expected pending_tiles>0 when cache is empty and tiles need fetching")
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

func TestHandleRoadSegments_failedTilesReported(t *testing.T) {
	srv := &buildServer{
		cacheDir:    t.TempDir(),
		tileSize:    0.1,
		overpassURL: "http://127.0.0.1:1", // unreachable
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()

	// Pre-mark a tile as failed. Use exact grid-aligned tile.
	// For bbox=-75.8,39.9,-75.7,40.0 with tileSize=0.1:
	// tilesInBBox generates tiles at (South=40.0, West=-75.8, North=40.1, East=-75.7)
	tile := quality.Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	srv.failedTiles.Store(tile, struct{}{})

	req := httptest.NewRequest("GET", "/api/road-segments?bbox=-75.8,39.9,-75.7,40.0", nil)
	w := httptest.NewRecorder()
	srv.handleRoadSegments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp roadSegmentsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.FailedTiles == 0 {
		t.Fatal("expected failed_tiles > 0 when a tile is pre-marked failed")
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
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.handleSegmentsStream(w, req)
		close(done)
	}()

	broker.broadcast([]byte("data: {}\n\n"))
	cancel()
	<-done

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}
}

func TestBuildServer_integration(t *testing.T) {
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
}

func TestHandleIndex_rendersSaveLoadButtons(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`id="btn-save" onclick="saveRoute()"`,
		`id="btn-load" onclick="loadRoute()"`,
		`type="file" id="file-input"`,
		`onchange="onFileSelected(event)"`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}

func TestHandleIndex_saveRouteUsesFilePicker(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`window.showSaveFilePicker`,
		`suggestedName: 'route.twisty.json'`,
		`application/json`,
		`.json`,
		`AbortError`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}

func TestHandleViewportScore_badJSON(t *testing.T) {
	srv := &buildServer{tileSize: 0.1}
	req := httptest.NewRequest("POST", "/api/score/viewport", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleViewportScore(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleViewportScore_emptyCache(t *testing.T) {
	srv := &buildServer{
		tileSize: 0.1,
		cacheDir: t.TempDir(),
	}
	body := `{"west":-76.0,"south":40.0,"east":-75.0,"north":41.0}`
	req := httptest.NewRequest("POST", "/api/score/viewport", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleViewportScore(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp scoreResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Score != 0 {
		t.Fatalf("expected score=0 for empty cache, got %f", resp.Score)
	}
	if resp.PendingTiles == 0 {
		t.Fatalf("expected pending_tiles>0 for uncached tiles, got 0")
	}
}

func TestHandleViewportScore_withCachedTile(t *testing.T) {
	cacheDir := t.TempDir()
	tile := quality.Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	// Minimal Overpass JSON: a hairpin curve (circumradius ~70m at the apex) within the tile.
	// Points at ~0.001-degree steps form a tight turn that scores in tier 3 (r < 100m).
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := cache.Write(tile, tileData); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	srv := &buildServer{
		tileSize: 0.1,
		cacheDir: cacheDir,
	}
	// Use a very small bbox that falls entirely within the cached tile so tilesInBBox
	// returns exactly one tile (Tile{South:40.0, West:-76.0, North:40.1, East:-75.9}).
	body := `{"west":-75.99,"south":40.05,"east":-75.98,"north":40.06}`
	req := httptest.NewRequest("POST", "/api/score/viewport", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleViewportScore(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp scoreResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Score == 0 {
		t.Fatal("expected Score > 0 for cached tile with road data, got 0")
	}
	// Note: pending_tiles may be > 0 because tilesInBBox intentionally expands the
	// bbox by one tile-step in each direction. We only assert on Score here.
}

func TestHandleIndex_rendersRefreshScoreButton(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`id="btn-refresh-score"`,
		`onclick="requestViewportScore()"`,
		`requestViewportScore`,
		`pollViewportScore`,
		`/api/score/viewport`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}

func TestHandleGeocode_success(t *testing.T) {
	// Spin up a fake Nominatim server
	nominatimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"lat":"40.1234","lon":"-76.5678","display_name":"123 Main St, Pottsville, PA","importance":0.9}]`)
	}))
	defer nominatimSrv.Close()

	srv := &buildServer{nominatimBase: nominatimSrv.URL}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/geocode?q=123+Main+St", nil)
	srv.handleGeocode(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
		DisplayName string  `json:"display_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Lat != 40.1234 || resp.Lon != -76.5678 {
		t.Errorf("lat/lon = %.4f, %.4f; want 40.1234, -76.5678", resp.Lat, resp.Lon)
	}
	if resp.DisplayName != "123 Main St, Pottsville, PA" {
		t.Errorf("DisplayName = %q", resp.DisplayName)
	}
}

func TestHandleGeocode_noResults(t *testing.T) {
	nominatimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer nominatimSrv.Close()

	srv := &buildServer{nominatimBase: nominatimSrv.URL}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/geocode?q=nowhere", nil)
	srv.handleGeocode(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleGeocode_missingQuery(t *testing.T) {
	srv := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/geocode", nil)
	srv.handleGeocode(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleIndex_renderstwistinessLabel(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`Twistiness`,
		`score_per_km`,
		`scorePerKmMax`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}

func TestHandleReverseGeocode_success(t *testing.T) {
	nominatimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"display_name":"123 Main St, Pottsville, PA"}`)
	}))
	defer nominatimSrv.Close()

	srv := &buildServer{nominatimBase: nominatimSrv.URL}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/reverse-geocode?lat=40.1234&lon=-76.5678", nil)
	srv.handleReverseGeocode(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DisplayName != "123 Main St, Pottsville, PA" {
		t.Errorf("DisplayName = %q", resp.DisplayName)
	}
}

func TestHandleReverseGeocode_noResult(t *testing.T) {
	nominatimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer nominatimSrv.Close()

	srv := &buildServer{nominatimBase: nominatimSrv.URL}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/reverse-geocode?lat=0&lon=0", nil)
	srv.handleReverseGeocode(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (graceful empty), got %d", w.Code)
	}
	var resp struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DisplayName != "" {
		t.Errorf("expected empty DisplayName, got %q", resp.DisplayName)
	}
}

func TestHandleReverseGeocode_missingParams(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"missing lon", "/api/reverse-geocode?lat=40.1"},
		{"missing lat", "/api/reverse-geocode?lon=-76.5"},
		{"malformed lat", "/api/reverse-geocode?lat=abc&lon=-76.5"},
		{"malformed lon", "/api/reverse-geocode?lat=40.1&lon=xyz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &buildServer{}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", tc.url, nil)
			srv.handleReverseGeocode(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestHandleIndex_rendersWaypointPanel(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`id="waypoints-section"`,
		`id="waypoints-header"`,
		`id="waypoints-list"`,
		`id="waypoints-add"`,
		`id="waypoints-count"`,
		`id="wp-error"`,
		`+ Add waypoint`,
		`wp-handle`,
		`wp-drop-indicator`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}

func TestHandleIndex_debugButtonPresentWhenDebugParam(t *testing.T) {
	srv := &buildServer{
		center:   geocode.Result{Lat: 38.4, Lon: -79.4},
		tileSize: 0.1,
	}
	req := httptest.NewRequest("GET", "/?debug=1", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "btn-debug-tiles") {
		t.Errorf("expected btn-debug-tiles button in debug mode output")
	}
}

func TestHandleRefreshViewport_clearsCacheAndFailed(t *testing.T) {
	cacheDir := t.TempDir()
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	// Use exact-multiple coordinates to avoid floating-point floor issues.
	// For bbox west:-75.8, south:40.0, east:-75.7, north:40.1, tilesInBBox generates
	// Tile{South:40.0, West:-75.8, North:40.1, East:-75.7} as the first tile.
	tile := quality.Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	// Write a stale/bad tile file.
	if err := cache.Write(tile, []byte(`{"elements":[]}`)); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	srv := &buildServer{
		cacheDir: cacheDir,
		tileSize: 0.1,
	}
	// Pre-mark the same tile as failed.
	srv.failedTiles.Store(tile, struct{}{})

	body := `{"west":-75.8,"south":40.0,"east":-75.7,"north":40.1}`
	req := httptest.NewRequest("POST", "/api/refresh-viewport", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRefreshViewport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Cache file should be gone.
	if cache.Has(tile) {
		t.Error("expected cache file to be deleted after refresh")
	}

	// failedTiles entry should be gone.
	if _, still := srv.failedTiles.Load(tile); still {
		t.Error("expected failedTiles entry to be cleared after refresh")
	}

	var resp struct {
		Cleared int `json:"cleared"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Cleared != 1 {
		t.Errorf("expected cleared == 1 (set-union: one tile cleared), got %d", resp.Cleared)
	}
}

func TestFetchMissingTiles_deduplicatesInFlightTiles(t *testing.T) {
	// Gate lets us hold the first goroutine inside the Overpass mock so a second
	// goroutine can race against it with the same tile.
	firstRequestStarted := make(chan struct{})
	firstRequestUnblock := make(chan struct{})
	fetchCount := 0

	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		close(firstRequestStarted)
		<-firstRequestUnblock
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()

	cacheDir := t.TempDir()
	tile := quality.Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}

	srv := &buildServer{
		overpassURL: overpassSrv.URL,
		cacheDir:    cacheDir,
		tileSize:    0.1,
		fetchDelay:  "1ms",
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()

	// First goroutine: will block inside the Overpass mock.
	go srv.fetchMissingTiles([]quality.Tile{tile}, false)

	// Wait until the first goroutine is inside the mock handler, then fire a second.
	select {
	case <-firstRequestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first goroutine never reached the Overpass mock")
	}

	// Second call with the same tile — should be a no-op due to in-flight tracking.
	srv.fetchMissingTiles([]quality.Tile{tile}, false)

	// Unblock the first goroutine.
	close(firstRequestUnblock)

	// Give the first goroutine time to finish.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
		if cache.Has(tile) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if fetchCount != 1 {
		t.Errorf("expected exactly 1 Overpass fetch, got %d", fetchCount)
	}
}

func TestFetchMissingTiles_clearsFailedOnSuccess(t *testing.T) {
	// Spin up a mock Overpass server that returns a valid (non-remark) response
	// for any request. This simulates a tile that previously failed but now succeeds.
	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()

	cacheDir := t.TempDir()
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	tile := quality.Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}

	srv := &buildServer{
		overpassURL: overpassSrv.URL,
		cacheDir:    cacheDir,
		tileSize:    0.1,
		fetchDelay:  "1ms", // avoid 1-second rate-limit delay in tests
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()

	// Pre-mark the tile as failed (simulates a prior failed fetch).
	srv.failedTiles.Store(tile, struct{}{})

	// Call fetchMissingTiles synchronously (not as a goroutine).
	srv.fetchMissingTiles([]quality.Tile{tile}, false)

	// The tile should now be in cache (mock server returned valid data).
	if !cache.Has(tile) {
		t.Fatal("expected tile to be in cache after successful fetch")
	}

	// The failed entry should have been cleared.
	if _, still := srv.failedTiles.Load(tile); still {
		t.Error("expected failedTiles entry to be cleared after successful fetch")
	}
}

func TestFetchMissingTiles_overlapTileContinuesOnStaleCancel(t *testing.T) {
	// tileA is in both viewport calls; tileB is only in the first.
	// After the second call (cancelStale=true), tileB's fetch should be cancelled
	// but tileA's in-flight HTTP request must NOT be cancelled.
	tileA := quality.Tile{South: 40.0, West: -75.8, North: 40.1, East: -75.7}
	tileB := quality.Tile{South: 41.0, West: -75.8, North: 41.1, East: -75.7}

	// Channel that unblocks tileA's mock response.
	unblockA := make(chan struct{})

	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The fetch uses a POST with form-encoded body containing "data".
		// Distinguish tileA vs tileB by the south coordinate in the bbox.
		if err := r.ParseForm(); err == nil {
			if strings.Contains(r.FormValue("data"), "40.000000") {
				// tileA request — block until unblocked.
				<-unblockA
			}
		}
		// Both tiles return valid empty JSON.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()

	cacheDir := t.TempDir()
	srv := &buildServer{
		overpassURL: overpassSrv.URL,
		cacheDir:    cacheDir,
		tileSize:    0.1,
		fetchDelay:  "1ms",
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()

	// First call: claim both tiles (cancelStale=false).
	go srv.fetchMissingTiles([]quality.Tile{tileA, tileB}, false)

	// Wait until both tiles appear in inFlight.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for both tiles to appear in inFlight")
		}
		_, aOk := srv.inFlight.Load(tileA)
		_, bOk := srv.inFlight.Load(tileB)
		if aOk && bOk {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second call: only tileA in viewport, cancelStale=true — should cancel tileB.
	srv.fetchMissingTiles([]quality.Tile{tileA}, true)

	// Unblock tileA's mock response so it can complete.
	close(unblockA)

	// Assert tileA lands in cache within 5 seconds.
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("tileA did not land in cache — its context was likely cancelled")
		}
		if cache.Has(tileA) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// tileB should NOT be in cache (was cancelled or never completed).
	if cache.Has(tileB) {
		t.Error("tileB should not be in cache — it was supposed to be cancelled")
	}
}

func TestHandleRefreshViewport_methodNotAllowed(t *testing.T) {
	srv := &buildServer{cacheDir: t.TempDir(), tileSize: 0.1}
	req := httptest.NewRequest("GET", "/api/refresh-viewport", nil)
	w := httptest.NewRecorder()
	srv.handleRefreshViewport(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleIndex_addRowHasDragHandle(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	// The add row must contain a wp-handle span so it can be dragged to set insert position.
	// Find the waypoints-add div and check it contains the handle markup.
	addIdx := strings.Index(body, `id="waypoints-add"`)
	if addIdx < 0 {
		t.Fatal("waypoints-add element not found in HTML")
	}
	// The handle is rendered as a child span inside #waypoints-add.
	// We check the static HTML has the handle span inline (not JS-rendered).
	if !strings.Contains(body[addIdx:addIdx+200], `wp-handle`) {
		t.Errorf("expected waypoints-add to contain a wp-handle span in static HTML")
	}
}

func TestHandleIndex_debugButtonAbsentByDefault(t *testing.T) {
	srv := &buildServer{
		center:   geocode.Result{Lat: 38.4, Lon: -79.4},
		tileSize: 0.1,
	}
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "btn-debug-tiles") {
		t.Errorf("expected btn-debug-tiles to be absent without ?debug=1")
	}
}

func TestHandleDebugTiles_returnsGrid(t *testing.T) {
	dir := t.TempDir()
	srv := &buildServer{cacheDir: dir, tileSize: 0.1}

	req := httptest.NewRequest("GET", "/api/debug/tiles?bbox=-79.5,38.3,-79.3,38.5", nil)
	w := httptest.NewRecorder()
	srv.handleDebugTiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Type     string `json:"type"`
			Geometry struct {
				Type        string         `json:"type"`
				Coordinates [][][2]float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				Tile   string `json:"tile"`
				Cached bool   `json:"cached"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fc.Type != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %q", fc.Type)
	}
	// bbox spans 0.2° lat × 0.2° lon with 0.1° tiles → expect 4 tiles (2×2)
	if len(fc.Features) < 4 {
		t.Fatalf("expected >=4 features, got %d", len(fc.Features))
	}
	for _, f := range fc.Features {
		if f.Geometry.Type != "Polygon" {
			t.Errorf("expected Polygon, got %q", f.Geometry.Type)
		}
		if f.Properties.Tile == "" {
			t.Errorf("tile property empty")
		}
		// ring has 5 points (closed)
		if len(f.Geometry.Coordinates) != 1 || len(f.Geometry.Coordinates[0]) != 5 {
			t.Errorf("expected 1 ring of 5 coords, got %v", f.Geometry.Coordinates)
		}
	}
}

func TestLoggingMiddleware(t *testing.T) {
	// Arrange: create a simple handler that records whether it was called.
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := loggingMiddleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/some/path", nil)
	w := httptest.NewRecorder()

	// Act
	wrapped.ServeHTTP(w, req)

	// Assert
	if !called {
		t.Error("expected inner handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestFetchMissingTiles_cancelStale(t *testing.T) {
	release := make(chan struct{})
	overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
	}))
	defer overpassSrv.Close()
	defer close(release)

	cacheDir := t.TempDir()
	tileA := quality.Tile{South: 40.0, West: -75.0, North: 40.1, East: -74.9}
	tileB := quality.Tile{South: 40.1, West: -75.0, North: 40.2, East: -74.9}
	tileC := quality.Tile{South: 40.2, West: -75.0, North: 40.3, East: -74.9}

	srv := &buildServer{
		overpassURL: overpassSrv.URL,
		cacheDir:    cacheDir,
		tileSize:    0.1,
		fetchDelay:  "1ms",
		tileReady:   make(chan quality.Tile, 64),
		broker:      newSSEBroker(),
	}
	go srv.broker.run()

	// First call: claim tileA and tileB; both will block at the Overpass server.
	go srv.fetchMissingTiles([]quality.Tile{tileA, tileB}, true)

	// Wait until both tiles are claimed by the first goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, aOk := srv.inFlight.Load(tileA)
		_, bOk := srv.inFlight.Load(tileB)
		if aOk && bOk {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second call: tileA and tileC. tileB is stale — the stale sweep must cancel
	// and remove it. Run in a goroutine because tileC will block at the server.
	// The stale-sweep portion runs synchronously before the goroutine can proceed,
	// so we use a channel to signal when the sweep is done.
	secondCallStarted := make(chan struct{})
	go func() {
		close(secondCallStarted)
		srv.fetchMissingTiles([]quality.Tile{tileA, tileC}, true)
	}()
	<-secondCallStarted

	// Give the second goroutine time to execute the stale sweep and claim tileC.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, bOk := srv.inFlight.Load(tileB)
		_, cOk := srv.inFlight.Load(tileC)
		if !bOk && cOk {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// tileB must have been cancelled and removed from inFlight.
	if _, tileBInFlight := srv.inFlight.Load(tileB); tileBInFlight {
		t.Error("expected tileB to have been cancelled and removed from inFlight")
	}
	// tileC must have been claimed by the second call.
	if _, tileCInFlight := srv.inFlight.Load(tileC); !tileCInFlight {
		t.Error("expected tileC to be in-flight after second call")
	}
}

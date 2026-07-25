// route_build.go
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/yardbirdsax/twisty/geo"
	"github.com/yardbirdsax/twisty/geocode"
	"github.com/yardbirdsax/twisty/quality"
	"github.com/yardbirdsax/twisty/route"
)

//go:embed static/statusManager.js
var statusManagerJS string

//go:embed static/build.html
var buildHTML string

//go:embed static/build.js
var buildJS string

const defaultNominatimBase = "https://nominatim.openstreetmap.org"

type buildParams struct {
	address     string
	port        int
	overpassURL string
	cacheDir    string
	tileSize    float64
	fetchDelay  string
	verbose     bool
	valhallaURL string // base URL for the Valhalla routing API
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("http request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func execBuild(p buildParams) error {
	if p.address == "" {
		return fmt.Errorf("--address is required")
	}

	if p.verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
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

	broker := newSSEBroker()
	go broker.run()

	srv := &buildServer{
		center:      center,
		overpassURL: p.overpassURL,
		valhallaURL: p.valhallaURL,
		cacheDir:    cacheDir,
		tileSize:    p.tileSize,
		fetchDelay:  p.fetchDelay,
		tileReady:   make(chan quality.Tile, 64),
		broker:      broker,
	}
	go srv.runTileSSEBroadcaster()

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/api/route-leg", srv.handleRouteLeg)
	mux.HandleFunc("/api/score", srv.handleScore)
	mux.HandleFunc("/api/score/viewport", srv.handleViewportScore)
	mux.HandleFunc("/api/export", srv.handleExport)
	mux.HandleFunc("/api/segments", srv.handleSegments)
	mux.HandleFunc("/api/segments/stream", srv.handleSegmentsStream)
	mux.HandleFunc("/api/road-segments", srv.handleRoadSegments)
	mux.HandleFunc("/api/tiles", srv.handleTiles)
	mux.HandleFunc("/api/debug/tiles", srv.handleDebugTiles)
	mux.HandleFunc("/api/refresh-viewport", srv.handleRefreshViewport)
	mux.HandleFunc("/api/geocode", srv.handleGeocode)
	mux.HandleFunc("/api/reverse-geocode", srv.handleReverseGeocode)

	addr := fmt.Sprintf("127.0.0.1:%d", p.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", p.port, err)
	}

	fmt.Fprintf(os.Stderr, "Map centered on %s (%.5f, %.5f)\n", center.DisplayName, center.Lat, center.Lon)
	fmt.Fprintf(os.Stderr, "Listening on http://127.0.0.1:%d\n", p.port)
	return http.Serve(ln, loggingMiddleware(mux))
}

type buildServer struct {
	center        geocode.Result
	overpassURL   string
	valhallaURL   string
	cacheDir      string
	tileSize      float64
	fetchDelay    string
	nominatimBase string   // override for tests; empty means use production Nominatim
	failedTiles   sync.Map // key: quality.Tile, value: struct{}
	inFlight      sync.Map // key: quality.Tile, value: context.CancelFunc; cancels in-flight fetch for that tile
	tileReady     chan quality.Tile
	broker        *sseBroker
}

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

func (s *buildServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	debugMode := r.URL.Query().Get("debug") == "1"
	var debugSnippet string
	if debugMode {
		debugSnippet = buildHTMLDebugSnippet
	}
	configScript := fmt.Sprintf(
		"<script>window.TWISTY_CONFIG={lat:%.6f,lon:%.6f,scorePerKmMax:%g,scoreMax:%g};</script>",
		s.center.Lat, s.center.Lon, quality.DefaultMaxCurvaturePerKm, quality.DefaultMaxCurvature,
	)
	html := fmt.Sprintf(buildHTML, debugSnippet, configScript, statusManagerJS, buildJS)
	w.Write([]byte(html))
}

// buildHTMLDebugSnippet is passed as an argument to fmt.Sprintf, not as a
// format string, so literal '%' must NOT be doubled (write '%', not '%%').
const buildHTMLDebugSnippet = `
<div class="row" style="margin-top:4px;">
  <button id="btn-debug-tiles" onclick="toggleDebugTiles()" style="width:100%;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Show tile grid</button>
</div>
<script>
var debugTileLayer = null;
var debugTilesVisible = false;

function loadDebugTiles() {
  var bbox = getBboxString();
  fetch('/api/debug/tiles?bbox=' + bbox)
    .then(function(r) { return r.json(); })
    .then(function(fc) {
      if (debugTileLayer) { map.removeLayer(debugTileLayer); }
      debugTileLayer = L.geoJSON(fc, {
        style: function(feature) {
          return {
            color: '#1d4ed8',
            weight: 1,
            dashArray: '4,4',
            fillColor: feature.properties.cached ? '#16a34a' : '#9ca3af',
            fillOpacity: 0.15
          };
        },
        onEachFeature: function(feature, layer) {
          layer.bindTooltip(feature.properties.tile, {
            permanent: true,
            direction: 'center',
            className: 'tile-debug-label'
          });
        }
      }).addTo(map);
    })
    .catch(function(err) { console.error('debug tiles:', err); });
}

function toggleDebugTiles() {
  var btn = document.getElementById('btn-debug-tiles');
  debugTilesVisible = !debugTilesVisible;
  if (debugTilesVisible) {
    btn.textContent = 'Hide tile grid';
    loadDebugTiles();
    map.on('moveend zoomend', loadDebugTiles);
  } else {
    btn.textContent = 'Show tile grid';
    map.off('moveend zoomend', loadDebugTiles);
    if (debugTileLayer) { map.removeLayer(debugTileLayer); debugTileLayer = null; }
  }
}
</script>
`

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

	valhallaURL := s.valhallaURL
	if valhallaURL == "" {
		valhallaURL = route.ValhallaBaseURL
	}
	routes, err := route.FetchRoutesFromURL(valhallaURL, origin, dest)
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

func isNearPolyline(point geo.Coord, polyline [][2]float64, thresholdM float64) bool {
	if len(polyline) == 0 {
		return false
	}
	// Check distance to first vertex.
	if geo.Haversine(point, geo.Coord{Lat: polyline[0][0], Lon: polyline[0][1]}) <= thresholdM {
		return true
	}
	// Check each segment.
	for i := 1; i < len(polyline); i++ {
		a := geo.Coord{Lat: polyline[i-1][0], Lon: polyline[i-1][1]}
		b := geo.Coord{Lat: polyline[i][0], Lon: polyline[i][1]}
		if distPointToSegment(point, a, b) <= thresholdM {
			return true
		}
	}
	return false
}

// distPointToSegment returns the minimum distance in meters from point p to segment ab.
func distPointToSegment(p, a, b geo.Coord) float64 {
	// Project p onto segment ab using a flat-earth approximation (valid for short segments).
	dLat := b.Lat - a.Lat
	dLon := b.Lon - a.Lon
	lenSq := dLat*dLat + dLon*dLon
	if lenSq == 0 {
		return geo.Haversine(p, a)
	}
	t := ((p.Lat-a.Lat)*dLat + (p.Lon-a.Lon)*dLon) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	closest := geo.Coord{
		Lat: a.Lat + t*dLat,
		Lon: a.Lon + t*dLon,
	}
	return geo.Haversine(p, closest)
}

type scoreRequest struct {
	Points [][2]float64 `json:"points"`
}

type scoreResult struct {
	Score      float64
	ScorePerKm float64
}

type scoreResponse struct {
	Score        float64 `json:"score"`
	ScorePerKm   float64 `json:"score_per_km"`
	PendingTiles int     `json:"pending_tiles"`
	FailedTiles  int     `json:"failed_tiles"`
}

type viewportScoreRequest struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

const maxViewportTiles = 1024

func (s *buildServer) handleViewportScore(w http.ResponseWriter, r *http.Request) {
	var req viewportScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	tiles := s.tilesInBBox(req.West, req.South, req.East, req.North)
	if len(tiles) > maxViewportTiles {
		http.Error(w, "bbox too large", http.StatusBadRequest)
		return
	}

	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}

	var cachedTiles []quality.Tile
	var missingTiles []quality.Tile
	for _, t := range tiles {
		if cache.Has(t) {
			cachedTiles = append(cachedTiles, t)
		} else {
			missingTiles = append(missingTiles, t)
		}
	}

	if len(missingTiles) > 0 {
		go s.fetchMissingTiles(missingTiles, false)
	}

	sr := s.scoreFromCachedTiles(cachedTiles, cache, nil)

	var failedCount int
	for _, t := range tiles {
		if _, failed := s.failedTiles.Load(t); failed {
			failedCount++
		}
	}

	resp := scoreResponse{
		Score:        sr.Score,
		ScorePerKm:   sr.ScorePerKm,
		PendingTiles: len(missingTiles),
		FailedTiles:  failedCount,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *buildServer) handleRefreshViewport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req viewportScoreRequest // reuses {West, South, East, North float64}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	tiles := s.tilesInBBox(req.West, req.South, req.East, req.North)
	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}

	cleared := 0
	for _, t := range tiles {
		tileCleared := false
		if cache.Has(t) {
			if err := os.Remove(cache.Path(t)); err == nil {
				tileCleared = true
			}
		}
		if _, wasFailed := s.failedTiles.LoadAndDelete(t); wasFailed {
			tileCleared = true
		}
		if tileCleared {
			cleared++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Cleared int `json:"cleared"`
	}{cleared})
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

	if len(missingTiles) > 0 {
		go s.fetchMissingTiles(missingTiles, false)
	}

	sr := s.scoreFromCachedTiles(cachedTiles, cache, req.Points)

	// Count failed tiles relevant to this request.
	var failedCount int
	for _, t := range tiles {
		if _, failed := s.failedTiles.Load(t); failed {
			failedCount++
		}
	}

	resp := scoreResponse{
		Score:        sr.Score,
		ScorePerKm:   sr.ScorePerKm,
		PendingTiles: len(missingTiles),
		FailedTiles:  failedCount,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *buildServer) fetchMissingTiles(tiles []quality.Tile, cancelStale bool) {
	if cancelStale {
		incoming := make(map[quality.Tile]struct{}, len(tiles))
		for _, t := range tiles {
			incoming[t] = struct{}{}
		}
		s.inFlight.Range(func(key, val any) bool {
			t := key.(quality.Tile)
			if _, keep := incoming[t]; !keep {
				fn := val.(context.CancelFunc)
				slog.Debug("cancelling stale tile fetch", "south", t.South, "west", t.West)
				fn()
				s.inFlight.Delete(t)
			}
			return true
		})
	}

	type claimedTile struct {
		tile   quality.Tile
		ctx    context.Context
		cancel context.CancelFunc
	}

	// Phase 1: claim all tiles upfront so inFlight is populated before any HTTP
	// request starts. Each tile gets its own independent context so that the stale
	// sweep can cancel individual tiles without affecting siblings.
	var claimed []claimedTile
	for _, t := range tiles {
		tileCtx, tileCancel := context.WithCancel(context.Background())
		if _, loaded := s.inFlight.LoadOrStore(t, tileCancel); loaded {
			tileCancel() // immediately release, won't be used
			slog.Debug("fetchMissingTiles: tile already in-flight, skipping", "south", t.South, "west", t.West)
		} else {
			claimed = append(claimed, claimedTile{tile: t, ctx: tileCtx, cancel: tileCancel})
			slog.Debug("fetchMissingTiles: goroutine enqueuing tile", "south", t.South, "west", t.West)
		}
	}
	if len(claimed) == 0 {
		return
	}
	defer func() {
		for _, c := range claimed {
			if fn, ok := s.inFlight.LoadAndDelete(c.tile); ok {
				fn.(context.CancelFunc)()
			}
		}
	}()

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

	// Phase 2: fetch each claimed tile using its own context so a stale-sweep
	// cancel on one tile does not affect the others.
	for _, c := range claimed {
		quality.FetchTiledWaysForTiles(c.ctx, []quality.Tile{c.tile}, cfg)
		if cache.Has(c.tile) {
			s.failedTiles.Delete(c.tile)
			select {
			case s.tileReady <- c.tile:
			default:
				slog.Warn("tileReady channel full, SSE push dropped", "south", c.tile.South, "west", c.tile.West)
			}
		} else if c.ctx.Err() == nil {
			s.failedTiles.Store(c.tile, struct{}{})
		}
	}
}

func (s *buildServer) scoreFromCachedTiles(tiles []quality.Tile, cache *quality.TileCache, routePoints [][2]float64) scoreResult {
	var totalScore float64
	var totalLength float64

	for _, t := range tiles {
		data, err := cache.Read(t)
		if err != nil {
			continue
		}

		ways, err := quality.ParseTileData(data)
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
				if len(routePoints) == 0 || isNearPolyline(mid, routePoints, 50) {
					totalScore += seg.Score
					totalLength += seg.Length
				}
			}
		}
	}

	var scorePerKm float64
	if totalLength > 0 {
		scorePerKm = totalScore / (totalLength / 1000.0)
	}
	return scoreResult{Score: totalScore, ScorePerKm: scorePerKm}
}

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
		http.Error(w, "unsupported format: "+req.Format, http.StatusBadRequest)
		return
	}

	// Concatenate leg points, deduplicating boundary points between legs.
	var points []geo.Coord
	for i, leg := range req.Legs {
		for j, p := range leg.Points {
			if i > 0 && j == 0 {
				// Skip the first point of subsequent legs — it's the same as
				// the last point of the previous leg.
				continue
			}
			points = append(points, geo.Coord{Lat: p[0], Lon: p[1]})
		}
	}

	switch req.Format {
	case "gpx":
		w.Header().Set("Content-Type", "application/gpx+xml")
		w.Header().Set("Content-Disposition", `attachment; filename="twisty-route.gpx"`)
		if err := writeGPXToWriter(w, points, req.Waypoints); err != nil {
			// Headers already sent; log error and return.
			fmt.Fprintf(os.Stderr, "gpx write error: %v\n", err)
		}
	case "kml":
		w.Header().Set("Content-Type", "application/vnd.google-earth.kml+xml")
		w.Header().Set("Content-Disposition", `attachment; filename="twisty-route.kml"`)
		if err := writeRouteKML(w, points); err != nil {
			fmt.Fprintf(os.Stderr, "kml write error: %v\n", err)
		}
	}
}

// gpxWaypoint is a local struct for GPX <wpt> elements.
type gpxWaypoint struct {
	Lat  float64 `xml:"lat,attr"`
	Lon  float64 `xml:"lon,attr"`
	Name string  `xml:"name,omitempty"`
}

// gpxTrackPoint is a local struct for GPX <trkpt> elements.
type gpxTrackPoint struct {
	Lat float64 `xml:"lat,attr"`
	Lon float64 `xml:"lon,attr"`
}

type gpxTrackSeg struct {
	Points []gpxTrackPoint `xml:"trkpt"`
}

type gpxTrack struct {
	Name   string      `xml:"name"`
	TrkSeg gpxTrackSeg `xml:"trkseg"`
}

type gpxDoc struct {
	XMLName xml.Name      `xml:"gpx"`
	Version string        `xml:"version,attr"`
	Creator string        `xml:"creator,attr"`
	Xmlns   string        `xml:"xmlns,attr"`
	Wpts    []gpxWaypoint `xml:"wpt"`
	Trk     gpxTrack      `xml:"trk"`
}

func writeGPXToWriter(w io.Writer, points []geo.Coord, waypoints []coordJSON) error {
	if _, err := io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"); err != nil {
		return err
	}

	wpts := make([]gpxWaypoint, len(waypoints))
	for i, wp := range waypoints {
		wpts[i] = gpxWaypoint{Lat: wp.Lat, Lon: wp.Lon}
	}

	trkPts := make([]gpxTrackPoint, len(points))
	for i, p := range points {
		trkPts[i] = gpxTrackPoint{Lat: p.Lat, Lon: p.Lon}
	}

	doc := gpxDoc{
		Version: "1.1",
		Creator: "twisty",
		Xmlns:   "http://www.topografix.com/GPX/1/1",
		Wpts:    wpts,
		Trk: gpxTrack{
			Name:   "twisty-route",
			TrkSeg: gpxTrackSeg{Points: trkPts},
		},
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Flush()
}

// kmlCoordinates is a simple string element for KML coordinates.
type kmlCoordinates struct {
	Value string `xml:",chardata"`
}

type kmlLineString struct {
	Coordinates kmlCoordinates `xml:"coordinates"`
}

type kmlPlacemark struct {
	Name       string        `xml:"name"`
	LineString kmlLineString `xml:"LineString"`
}

type kmlDocument struct {
	Name      string       `xml:"name"`
	Placemark kmlPlacemark `xml:"Placemark"`
}

type kmlDoc struct {
	XMLName  xml.Name    `xml:"kml"`
	Xmlns    string      `xml:"xmlns,attr"`
	Document kmlDocument `xml:"Document"`
}

func writeRouteKML(w io.Writer, points []geo.Coord) error {
	if _, err := io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"); err != nil {
		return err
	}

	var sb strings.Builder
	for i, p := range points {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%f,%f,0", p.Lon, p.Lat)
	}

	doc := kmlDoc{
		Xmlns: "http://www.opengis.net/kml/2.2",
		Document: kmlDocument{
			Name: "twisty-route",
			Placemark: kmlPlacemark{
				Name: "twisty-route",
				LineString: kmlLineString{
					Coordinates: kmlCoordinates{Value: sb.String()},
				},
			},
		},
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Flush()
}

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

type segmentsResponse struct {
	Type         string            `json:"type"`
	Features     []geoJSONFeature  `json:"features"`
	PendingTiles int               `json:"pending_tiles"`
	FailedTiles  int               `json:"failed_tiles"`
}

func (s *buildServer) handleSegments(w http.ResponseWriter, r *http.Request) {
	west, south, east, north, err := parseBBox(r.URL.Query().Get("bbox"))
	if err != nil {
		http.Error(w, "invalid bbox", http.StatusBadRequest)
		return
	}

	tiles := s.tilesInBBox(west, south, east, north)
	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}

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

	if len(missing) > 0 {
		go s.fetchMissingTiles(missing, true)
	}

	var failedCount int
	for _, t := range tiles {
		if _, failed := s.failedTiles.Load(t); failed {
			failedCount++
		}
	}

	fc := collectionsToGeoJSON(allCollections, allScoredWays)
	filtered := filterFeaturesByBBox(fc, west, south, east, north)
	resp := segmentsResponse{
		Type:         filtered.Type,
		Features:     filtered.Features,
		PendingTiles: len(missing),
		FailedTiles:  failedCount,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type roadSegmentsResponse struct {
	Type         string               `json:"type"`
	Features     []geoJSONRoadFeature `json:"features"`
	PendingTiles int                  `json:"pending_tiles"`
	FailedTiles  int                  `json:"failed_tiles"`
}

func (s *buildServer) handleRoadSegments(w http.ResponseWriter, r *http.Request) {
	west, south, east, north, err := parseBBox(r.URL.Query().Get("bbox"))
	if err != nil {
		http.Error(w, "invalid bbox", http.StatusBadRequest)
		return
	}

	tiles := s.tilesInBBox(west, south, east, north)
	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}

	var missing []quality.Tile
	var allCollections []quality.RoadCollection
	for _, tile := range tiles {
		if !cache.Has(tile) {
			missing = append(missing, tile)
			continue
		}
		data, err := cache.Read(tile)
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

	if len(missing) > 0 {
		go s.fetchMissingTiles(missing, true)
	}

	var failedCount int
	for _, t := range tiles {
		if _, failed := s.failedTiles.Load(t); failed {
			failedCount++
		}
	}

	fc := collectionsToRoadGeoJSON(allCollections)
	filtered := filterRoadFeaturesByBBox(fc, west, south, east, north)
	resp := roadSegmentsResponse{
		Type:         filtered.Type,
		Features:     filtered.Features,
		PendingTiles: len(missing),
		FailedTiles:  failedCount,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

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
			w.Write(msg) //nolint:errcheck
			flusher.Flush()
		}
	}
}

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
		fc := collectionsToGeoJSON(collections, result.ScoredWays)
		payload, err := json.Marshal(fc)
		if err != nil {
			continue
		}
		s.broker.broadcast([]byte("data: " + string(payload) + "\n\n"))
	}
}

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
		go s.fetchMissingTiles(missing, false)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *buildServer) handleDebugTiles(w http.ResponseWriter, r *http.Request) {
	west, south, east, north, err := parseBBox(r.URL.Query().Get("bbox"))
	if err != nil {
		http.Error(w, "invalid bbox", http.StatusBadRequest)
		return
	}

	tiles := s.tilesInBBox(west, south, east, north)
	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}

	type tileProps struct {
		Tile   string `json:"tile"`
		Cached bool   `json:"cached"`
	}
	type tileGeom struct {
		Type        string         `json:"type"`
		Coordinates [][][2]float64 `json:"coordinates"`
	}
	type tileFeature struct {
		Type       string    `json:"type"`
		Geometry   tileGeom  `json:"geometry"`
		Properties tileProps `json:"properties"`
	}
	type tileFC struct {
		Type     string        `json:"type"`
		Features []tileFeature `json:"features"`
	}

	fc := tileFC{Type: "FeatureCollection", Features: make([]tileFeature, 0, len(tiles))}
	for _, t := range tiles {
		ring := [][2]float64{
			{t.West, t.South},
			{t.East, t.South},
			{t.East, t.North},
			{t.West, t.North},
			{t.West, t.South},
		}
		label := fmt.Sprintf("%.3f,%.3f", t.South, t.West)
		fc.Features = append(fc.Features, tileFeature{
			Type:     "Feature",
			Geometry: tileGeom{Type: "Polygon", Coordinates: [][][2]float64{ring}},
			Properties: tileProps{
				Tile:   label,
				Cached: cache.Has(t),
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fc)
}

func (s *buildServer) handleGeocode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing query parameter q", http.StatusBadRequest)
		return
	}

	base := s.nominatimBase
	if base == "" {
		base = defaultNominatimBase
	}

	// GeocodeWithURL prints disambiguation output to stdout (side effect of processNominatimResults).
	result, err := geocode.GeocodeWithURL(q, "Waypoint", base)
	if err != nil {
		if strings.Contains(err.Error(), "no results found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
		DisplayName string  `json:"display_name"`
	}{Lat: result.Lat, Lon: result.Lon, DisplayName: result.DisplayName})
}

func (s *buildServer) handleReverseGeocode(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	if latStr == "" || lonStr == "" {
		http.Error(w, "missing lat or lon parameter", http.StatusBadRequest)
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		http.Error(w, "invalid lat", http.StatusBadRequest)
		return
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		http.Error(w, "invalid lon", http.StatusBadRequest)
		return
	}

	base := s.nominatimBase
	if base == "" {
		base = defaultNominatimBase
	}

	result, err := geocode.ReverseGeocodeWithURL(lat, lon, base)
	if err != nil {
		// Graceful fallback — return empty display_name, not an error
		result = geocode.Result{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		DisplayName string `json:"display_name"`
	}{DisplayName: result.DisplayName})
}

func newBuildCmd() *cobra.Command {
	var p buildParams

	cmd := &cobra.Command{
		Use:           "build",
		Short:         "Interactively build a route on a map with live scoring",
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
	f.StringVar(&p.valhallaURL, "valhalla-url", route.ValhallaBaseURL, "Valhalla routing API URL")
	return cmd
}

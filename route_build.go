// route_build.go
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
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
	return http.Serve(ln, mux)
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
	html := fmt.Sprintf(buildHTML, debugSnippet, s.center.Lat, s.center.Lon, quality.DefaultMaxCurvaturePerKm, statusManagerJS)
	w.Write([]byte(html))
}

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

  #waypoints-section { border-top: 1px solid #eee; padding-top: 8px; margin-top: 8px; }
  #waypoints-header { display: flex; justify-content: space-between; align-items: center; cursor: pointer; user-select: none; margin-bottom: 0; }
  #waypoints-header .label { font-size: 11px; text-transform: uppercase; color: #666; letter-spacing: 0.5px; margin-bottom: 0; }
  #waypoints-header .toggle { color: #2563eb; font-size: 12px; }
  #waypoints-list { margin-top: 6px; display: none; }
  #waypoints-list.open { display: block; }
  .wp-row { display: flex; align-items: center; gap: 6px; background: #f9fafb; border: 1px solid #e5e7eb; border-radius: 4px; padding: 4px 6px; margin-bottom: 3px; font-size: 11px; cursor: pointer; max-width: 400px; }
  .wp-row.active-slot { border-left: 3px solid #2563eb; padding-left: 4px; }
  .wp-badge { color: #9ca3af; font-size: 10px; flex-shrink: 0; cursor: pointer; }
  .wp-label { flex: 1; color: #374151; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; text-wrap: auto; }
  .wp-label.fallback { color: #9ca3af; font-style: italic; }
  .wp-input { flex: 1; border: none; outline: none; font-size: 11px; background: transparent; }
  #waypoints-add { display: flex; align-items: center; gap: 6px; background: #f0f9ff; border: 1px dashed #93c5fd; border-radius: 4px; padding: 4px 6px; margin-top: 2px; font-size: 11px; color: #2563eb; cursor: pointer; }
  .wp-error { font-size: 10px; color: #dc2626; margin-top: 2px; display: none; }
  @keyframes wp-shake { 0%%,100%%{transform:translateX(0)} 25%%{transform:translateX(-4px)} 75%%{transform:translateX(4px)} }
  .wp-input.shake { animation: wp-shake 0.3s ease; }

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

  .tile-debug-label { background: rgba(255,255,255,0.85); border: none; font-size: 9px; color: #374151; padding: 1px 3px; }
</style>
</head>
<body>
<div id="map"></div>

<div id="stats">
  <div class="label">Route Stats</div>
  <div class="row"><span class="key">Twistiness</span><span class="val score-val" id="score-val">—</span></div>
  <div class="row"><span class="key">Distance</span><span class="val" id="dist-val">—</span></div>
  <div class="row"><span class="key">Time</span><span class="val" id="time-val">—</span></div>
  <div class="row" style="margin-top:8px;">
    <button id="btn-overlay-toggle" onclick="toggleOverlayMode()" style="width:100%%;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Switch to Road view</button>
  </div>
  <div class="row" style="margin-top:4px;gap:4px;justify-content:flex-start;">
    <button id="btn-save" onclick="saveRoute()" style="flex:1;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Save</button>
    <button id="btn-load" onclick="loadRoute()" style="flex:1;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Load</button>
  </div>
  <div class="row" style="margin-top:4px;">
    <button id="btn-refresh-score" onclick="requestViewportScore()" style="width:100%%;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Refresh score</button>
  </div>
  <input type="file" id="file-input" accept=".twisty.json,.json" style="display:none;" onchange="onFileSelected(event)">
  <div class="fetch-status done" id="fetch-status">Click map to start</div>
  <div id="waypoints-section">
    <!-- JS handlers (toggleWaypointsPanel, startAddWaypoint) defined in waypoint panel script block below -->
    <div id="waypoints-header" onclick="toggleWaypointsPanel()">
      <span class="label">Waypoints (<span id="waypoints-count">0</span>)</span>
      <span class="toggle" id="waypoints-toggle">▶</span>
    </div>
    <div id="waypoints-list">
      <div id="waypoints-add" onclick="startAddWaypoint()">+ Add waypoint</div>
    </div>
    <div class="wp-error" id="wp-error"></div>
  </div>
%s
</div>

<div id="export-buttons">
  <button id="btn-gpx" disabled onclick="exportRoute('gpx')">Export GPX</button>
  <button id="btn-kml" disabled onclick="exportRoute('kml')">Export KML</button>
  <button id="btn-clear" disabled onclick="clearRoute()" style="background:#dc2626;">Clear route</button>
</div>

<div id="hint">Click map to add waypoint · Click last marker to undo</div>

<div id="toast"></div>

<script>
var map = L.map('map').setView([%f, %f], 13);
L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 19,
    attribution: '&copy; OpenStreetMap contributors'
}).addTo(map);
map.createPane('routePane');
map.getPane('routePane').style.zIndex = 450;

var waypoints = [];
var markers = [];
var legPolylines = [];
var legs = [];
var scorePollTimer = null;
var scorePerKmMax = %g;
%s
var statusManager = createStatusManager(document.getElementById('fetch-status'));

var labelCache = {};
var activeSlotIndex = -1;
var waypointsPanelOpen = false;
var reverseGeocodeGeneration = 0;

var CIRCLED_DIGITS = ['①','②','③','④','⑤','⑥','⑦','⑧','⑨','⑩'];
function waypointBadge(i) {
  return i < CIRCLED_DIGITS.length ? CIRCLED_DIGITS[i] : '#' + (i + 1);
}

function labelKey(wp) {
  return wp[0].toFixed(6) + ',' + wp[1].toFixed(6);
}

function latLonFallback(wp) {
  return wp[0].toFixed(4) + ', ' + wp[1].toFixed(4);
}

function showWpError(msg) {
  var el = document.getElementById('wp-error');
  el.textContent = msg;
  el.style.display = msg ? 'block' : 'none';
}

function toggleWaypointsPanel() {
  waypointsPanelOpen = !waypointsPanelOpen;
  var list = document.getElementById('waypoints-list');
  var toggle = document.getElementById('waypoints-toggle');
  list.className = waypointsPanelOpen ? 'open' : '';
  toggle.textContent = waypointsPanelOpen ? '▼' : '▶';
  if (waypointsPanelOpen) {
    reverseGeocodeUnlabeled();
  }
}

function renderWaypointList() {
  document.getElementById('waypoints-count').textContent = waypoints.length;
  var list = document.getElementById('waypoints-list');

  var existing = list.querySelectorAll('.wp-row');
  existing.forEach(function(el) { el.parentNode.removeChild(el); });

  var addRow = document.getElementById('waypoints-add');

  waypoints.forEach(function(wp, i) {
    var key = labelKey(wp);
    var label = labelCache[key] || null;
    var row = document.createElement('div');
    row.className = 'wp-row' + (i === activeSlotIndex ? ' active-slot' : '');
    row.dataset.index = i;

    var badge = document.createElement('span');
    badge.className = 'wp-badge';
    badge.textContent = waypointBadge(i);
    badge.title = 'Click to set as active slot';
    badge.onclick = function(e) {
      e.stopPropagation();
      setActiveSlot(i);
    };

    var labelEl = document.createElement('span');
    labelEl.className = 'wp-label' + (label ? '' : ' fallback');
    labelEl.textContent = label || latLonFallback(wp);

    labelEl.onclick = function(e) {
      e.stopPropagation();
      startEditWaypoint(row, i, labelEl);
    };

    row.appendChild(badge);
    row.appendChild(labelEl);
    list.insertBefore(row, addRow);
  });
}

function setActiveSlot(i) {
  activeSlotIndex = (activeSlotIndex === i) ? -1 : i;
  renderWaypointList();
}

function startEditWaypoint(row, index, labelEl) {
  if (row.querySelector('.wp-input')) return;
  labelEl.style.display = 'none';

  var input = document.createElement('input');
  input.className = 'wp-input';
  input.type = 'text';
  input.placeholder = 'Enter address...';
  row.appendChild(input);
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitWaypointEdit(index, q, row, input, labelEl);
    } else if (e.key === 'Escape') {
      row.removeChild(input);
      labelEl.style.display = '';
      showWpError('');
    }
  };
}

function commitWaypointEdit(index, query, row, input, labelEl) {
  input.disabled = true;
  showWpError('');
  fetch('/api/geocode?q=' + encodeURIComponent(query))
    .then(function(r) {
      if (r.status === 404) throw new Error('Address not found');
      if (!r.ok) throw new Error('Geocoding error');
      return r.json();
    })
    .then(function(data) {
      delete labelCache[labelKey(waypoints[index])];
      waypoints[index] = [data.lat, data.lon];
      labelCache[labelKey(waypoints[index])] = data.display_name;
      row.removeChild(input);
      labelEl.style.display = '';
      saveState();
      refreshMarkers();
      renderWaypointList();
      rerouteAll();
    })
    .catch(function(err) {
      input.disabled = false;
      input.classList.add('shake');
      setTimeout(function() { input.classList.remove('shake'); }, 300);
      input.focus();
      showWpError(err.message || 'Address not found');
    });
}

function startAddWaypoint() {
  var addRow = document.getElementById('waypoints-add');
  if (addRow.querySelector('.wp-input')) return;

  var input = document.createElement('input');
  input.className = 'wp-input';
  input.type = 'text';
  input.placeholder = 'Enter address...';
  input.style.flex = '1';
  addRow.textContent = '';
  addRow.appendChild(input);
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitAddWaypoint(q, addRow, input);
    } else if (e.key === 'Escape') {
      addRow.textContent = '+ Add waypoint';
      addRow.onclick = startAddWaypoint;
      showWpError('');
    }
  };
}

function commitAddWaypoint(query, addRow, input) {
  input.disabled = true;
  showWpError('');
  fetch('/api/geocode?q=' + encodeURIComponent(query))
    .then(function(r) {
      if (r.status === 404) throw new Error('Address not found');
      if (!r.ok) throw new Error('Geocoding error');
      return r.json();
    })
    .then(function(data) {
      var latlng = L.latLng(data.lat, data.lon);
      labelCache[labelKey([data.lat, data.lon])] = data.display_name;
      addRow.textContent = '+ Add waypoint';
      addRow.onclick = startAddWaypoint;
      activeSlotIndex = -1;
      addWaypoint(latlng);
    })
    .catch(function(err) {
      input.disabled = false;
      input.classList.add('shake');
      setTimeout(function() { input.classList.remove('shake'); }, 300);
      input.focus();
      showWpError(err.message || 'Address not found');
    });
}

function reverseGeocodeUnlabeled() {
  var unlabeled = waypoints.filter(function(wp) {
    return !labelCache[labelKey(wp)];
  });
  if (unlabeled.length === 0) return;

  var gen = ++reverseGeocodeGeneration;

  function next(i) {
    if (i >= unlabeled.length) return;
    if (gen !== reverseGeocodeGeneration) return;
    var wp = unlabeled[i];
    var key = labelKey(wp);
    fetch('/api/reverse-geocode?lat=' + wp[0] + '&lon=' + wp[1])
      .then(function(r) { return r.json(); })
      .then(function(data) {
        if (gen !== reverseGeocodeGeneration) return;
        if (data.display_name) {
          labelCache[key] = data.display_name;
          renderWaypointList();
        }
        next(i + 1);
      })
      .catch(function() { next(i + 1); });
  }
  next(0);
}

function rerouteAll() {
  if (waypoints.length < 2) return;

  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  legs = [];
  updateStats();
  requestScore();
  saveState();

  var gen = ++routingGeneration;

  function routeNext(i) {
    if (i >= waypoints.length - 1) return;
    var from = waypoints[i];
    var to = waypoints[i + 1];
    fetch('/api/route-leg', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from: { lat: from[0], lon: from[1] }, to: { lat: to[0], lon: to[1] } })
    })
    .then(function(r) {
      if (!r.ok) throw new Error('Routing failed');
      return r.json();
    })
    .then(function(data) {
      if (gen !== routingGeneration) return;
      var latLngs = data.points.map(function(p) { return [p[0], p[1]]; });
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5, pane: 'routePane' }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      saveState();
      updateStats();
      requestScore();
      routeNext(i + 1);
    })
    .catch(function() {
      if (gen !== routingGeneration) return;
      showToast('Could not route leg ' + (i + 1));
    });
  }
  routeNext(0);
}

function curvatureColorLevel(scorePerKm) {
  if (scorePerKm <= 0) return 0;
  var pct = scorePerKm / scorePerKmMax;
  if (pct > 1) pct = 1;
  var colorPct = 1 - 1 / Math.pow(10, pct * 0.75);
  return Math.round(510 * colorPct) + 1;
}

function gradientColorCSS(level) {
  if (level <= 0) return '#4ade80'; // green (tier 0 color)
  if (level <= 256) {
    var green = 255 - (level - 1) * 255 / 255;
    return 'rgb(255,' + Math.round(green) + ',0)';
  }
  var blue = (level - 257) * 255 / 254;
  return 'rgb(255,0,' + Math.round(blue) + ')';
}

var routingGeneration = 0;
var OVERLAY_WAYS = 'ways';
var OVERLAY_ROADS = 'roads';
var overlayMode = OVERLAY_WAYS;
var roadPollTimer = null;

var MIN_ZOOM = 12;
var TIER_COLORS = ['#475569', '#4ade80', '#facc15', '#fb923c', '#f87171'];

// segmentGeneration is incremented whenever roadLayer/renderedSegments are
// rebuilt (mode toggle, zoom-out clear). Fetch callbacks snapshot it at
// dispatch time and drop their results if the generation has moved on.
var segmentGeneration = 0;

function segmentStyle(feature) {
  if (overlayMode === OVERLAY_ROADS) {
    var color = feature && feature.properties ? feature.properties.color : '#475569';
    return { color: color || '#475569', weight: 3, opacity: 0.8 };
  }
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

// rebuildSegmentLayer tears down the current overlay and returns a new
// segmentGeneration value. All in-flight fetch/SSE callbacks that captured
// the old generation will discard their results.
function rebuildSegmentLayer() {
  if (roadPollTimer) { clearTimeout(roadPollTimer); roadPollTimer = null; }
  segmentGeneration++;
  map.removeLayer(roadLayer);
  roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);
  renderedSegments.clear();
  return segmentGeneration;
}

// mergeFeatures adds GeoJSON features to the layer, but only if the
// snapshot values (gen, mode, layer, seen) still match the current globals.
// This prevents stale in-flight callbacks from polluting the current layer.
function mergeFeatures(features, gen, mode, layer, seen) {
  if (!features) return;
  if (gen !== segmentGeneration) return;
  features.forEach(function(f) {
    if (!f.geometry || !f.geometry.coordinates || f.geometry.coordinates.length < 1) return;
    // Re-check generation inside the loop: a concurrent rebuild between
    // forEach iterations should also abort.
    if (gen !== segmentGeneration) return;
    var key;
    if (mode === OVERLAY_ROADS) {
      // Use road_name + first coordinate as key so that distinct sub-collections
      // of the same named road (same DisplayName, different geometry) are not
      // collapsed. The coordinate tiebreaker also covers unnamed roads.
      var name = f.properties && f.properties.road_name;
      var firstPair = f.geometry.coordinates[0] && f.geometry.coordinates[0][0];
      key = 'road:' + (name != null ? name : 'unnamed') + ':' + firstPair;
    } else {
      var firstCoord = f.geometry.coordinates[0];
      key = f.properties.way_id + ':' + firstCoord[0] + ':' + firstCoord[1];
    }
    if (seen.has(key)) return;
    seen.add(key);
    layer.addData(f);
  });
}

function loadVisibleSegments() {
  var zoom = map.getZoom();
  var toggleBtn = document.getElementById('btn-overlay-toggle');
  if (zoom < MIN_ZOOM) {
    // Close SSE before rebuilding so pushed data doesn't repopulate the
    // fresh empty layer while we're zoomed out.
    if (evtSource) { evtSource.close(); evtSource = null; }
    rebuildSegmentLayer();
    if (toggleBtn) toggleBtn.disabled = true;
    return;
  }
  if (toggleBtn) toggleBtn.disabled = false;

  // Snapshot all mutable state at dispatch time. The callback closures use
  // these snapshots rather than reading globals at resolution time, so a
  // mode toggle or zoom-out that runs while a fetch is in-flight will not
  // corrupt the new layer.
  var gen = segmentGeneration;
  var mode = overlayMode;
  var layer = roadLayer;
  var seen = renderedSegments;
  var bbox = getBboxString();

  if (mode === OVERLAY_ROADS) {
    statusManager.set('roads', '⏳ Loading roads...', false);
    fetch('/api/road-segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(data) {
        mergeFeatures(data.features, gen, mode, layer, seen);
        if (gen !== segmentGeneration) return;
        if (data.pending_tiles > 0) {
          statusManager.set('roads', '⏳ Loading ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
          roadPollTimer = setTimeout(loadVisibleSegments, 2000);
        } else if (data.failed_tiles > 0) {
          statusManager.set('roads', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
        } else {
          statusManager.clear('roads');
        }
      })
      .catch(function() {
        if (gen === segmentGeneration) { statusManager.clear('roads'); }
      });
  } else {
    statusManager.set('roads', '⏳ Loading roads...', false);
    fetch('/api/tiles?bbox=' + bbox);
    fetch('/api/segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(fc) {
        mergeFeatures(fc.features, gen, mode, layer, seen);
        if (gen === segmentGeneration) { statusManager.clear('roads'); }
      })
      .catch(function() {
        if (gen === segmentGeneration) { statusManager.clear('roads'); }
      });
  }
}

map.on('moveend zoomend', function() {
  if (!applyingRouteState) loadVisibleSegments();
});
loadVisibleSegments();

function saveState() {
  try {
    var center = map.getCenter();
    var state = {
      zoom: map.getZoom(),
      center: [center.lat, center.lng],
      waypoints: waypoints,
      legs: legs
    };
    localStorage.setItem('twisty-build-state', JSON.stringify(state));
  } catch(e) {}
}

map.on('moveend zoomend', function() {
  if (!applyingRouteState) saveState();
});

var evtSource = null;

function connectSSE() {
  if (evtSource) { evtSource.close(); }
  evtSource = new EventSource('/api/segments/stream');
  // Snapshot generation/mode/layer/seen at connect time. If a rebuild
  // happens the handler's captured gen will no longer match segmentGeneration
  // and mergeFeatures will discard the message.
  var gen = segmentGeneration;
  var mode = overlayMode;
  var layer = roadLayer;
  var seen = renderedSegments;
  evtSource.onmessage = function(e) {
    try {
      mergeFeatures(JSON.parse(e.data).features, gen, mode, layer, seen);
    } catch(err) {}
  };
}

connectSSE();

function toggleOverlayMode() {
  overlayMode = overlayMode === OVERLAY_WAYS ? OVERLAY_ROADS : OVERLAY_WAYS;
  var btn = document.getElementById('btn-overlay-toggle');
  btn.textContent = overlayMode === OVERLAY_WAYS ? 'Switch to Road view' : 'Switch to Way view';
  // rebuildSegmentLayer increments segmentGeneration, which invalidates all
  // in-flight fetch callbacks and the old SSE handler's captured gen.
  rebuildSegmentLayer();
  if (overlayMode === OVERLAY_ROADS) {
    if (evtSource) { evtSource.close(); evtSource = null; }
  } else {
    connectSSE();
  }
  loadVisibleSegments();
}

function restoreState() {
  var raw = localStorage.getItem('twisty-build-state');
  if (!raw) return;
  var state;
  try {
    state = JSON.parse(raw);
  } catch(e) {
    localStorage.removeItem('twisty-build-state');
    return;
  }
  if (!state || !Array.isArray(state.waypoints) || !Array.isArray(state.legs)) {
    localStorage.removeItem('twisty-build-state');
    return;
  }
  var valid = state.legs.every(function(leg) {
    return leg && Array.isArray(leg.points);
  });
  if (!valid) {
    localStorage.removeItem('twisty-build-state');
    return;
  }
  if (state.waypoints.length > state.legs.length + 1) {
    state.waypoints = state.waypoints.slice(0, state.legs.length + 1);
  }
  applyRouteState(state);
}

// applyingRouteState suppresses the moveend/zoomend handlers while
// applyRouteState runs, so that map.setView doesn't fire loadVisibleSegments
// before the route is drawn.
var applyingRouteState = false;

function applyRouteState(state) {
  clearRoute();
  waypoints = state.waypoints;
  legs = state.legs;
  applyingRouteState = true;
  if (state.center != null && state.zoom != null) {
    map.setView(state.center, state.zoom);
  }
  applyingRouteState = false;
  legs.forEach(function(leg) {
    var latLngs = leg.points.map(function(p) { return [p[0], p[1]]; });
    var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5, pane: 'routePane' }).addTo(map);
    legPolylines.push(polyline);
  });
  refreshMarkers();
  renderWaypointList();
  updateStats();
  if (legs.length > 0) {
    requestScore();
  }
  // Now that the route is fully drawn, load segments for the restored view.
  loadVisibleSegments();
}

restoreState();

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
  document.getElementById('btn-clear').disabled = waypoints.length === 0;
  document.getElementById('btn-save').disabled = waypoints.length === 0;
}

// scoreGeneration is incremented by clearRoute so that in-flight score
// responses (including pending-tiles poll callbacks) discard their results
// rather than re-arming the timer or updating the UI for a cleared route.
var scoreGeneration = 0;

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
    document.getElementById('score-val').style.color = '';
    statusManager.clear('scoring');
    return;
  }

  var gen = scoreGeneration;

  fetch('/api/score', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ points: allPoints })
  })
  .then(function(r) { return r.json(); })
  .then(function(data) {
    if (gen !== scoreGeneration) return;
    var spk = data.score_per_km || 0;
    var scoreEl = document.getElementById('score-val');
    if (spk > 0) {
      scoreEl.textContent = Math.round(spk).toLocaleString();
      scoreEl.style.color = gradientColorCSS(curvatureColorLevel(spk));
    } else {
      scoreEl.textContent = '—';
      scoreEl.style.color = '';
    }

    if (data.pending_tiles > 0) {
      statusManager.set('scoring', '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
      scorePollTimer = setTimeout(requestScore, 2000);
    } else if (data.failed_tiles > 0) {
      statusManager.set('scoring', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
    } else {
      statusManager.set('scoring', '✅ Score complete', true);
    }
  })
  .catch(function(err) {
    if (gen !== scoreGeneration) return;
    statusManager.set('scoring', 'Score error', false);
  });
}

function pollViewportScore(payload) {
  fetch('/api/score/viewport', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  .then(function(r) { return r.json(); })
  .then(function(data) {
    document.getElementById('score-val').textContent = '—';
    document.getElementById('score-val').style.color = '';

    if (data.pending_tiles > 0) {
      statusManager.set('scoring', '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
      scorePollTimer = setTimeout(function() { pollViewportScore(payload); }, 2000);
    } else if (data.failed_tiles > 0) {
      statusManager.set('scoring', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
    } else {
      statusManager.set('scoring', '✅ Score complete', true);
    }
  })
  .catch(function(err) {
    statusManager.set('scoring', 'Score error', false);
  });
}

function requestViewportScore() {
  if (scorePollTimer) { clearTimeout(scorePollTimer); scorePollTimer = null; }

  var bounds = map.getBounds();
  var payload = {
    west:  bounds.getWest(),
    south: bounds.getSouth(),
    east:  bounds.getEast(),
    north: bounds.getNorth()
  };

  statusManager.set('scoring', '⏳ Refreshing viewport...', false);

  fetch('/api/refresh-viewport', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  .then(function() {
    rebuildSegmentLayer();
    if (overlayMode !== OVERLAY_ROADS) { connectSSE(); }
    loadVisibleSegments();

    statusManager.set('scoring', '⏳ Scoring viewport...', false);
    pollViewportScore(payload);
  })
  .catch(function(err) {
    statusManager.set('scoring', 'Score error', false);
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

  if (activeSlotIndex >= 0 && activeSlotIndex < waypoints.length) {
    waypoints[activeSlotIndex] = [latlng.lat, latlng.lng];
    var replacedIndex = activeSlotIndex;
    activeSlotIndex = -1;
    refreshMarkers();
    saveState();
    renderWaypointList();
    reverseGeocodeUnlabeled();
    rerouteAll();
    return;
  }

  waypoints.push([latlng.lat, latlng.lng]);
  refreshMarkers();
  saveState();
  renderWaypointList();
  reverseGeocodeUnlabeled();

  if (waypoints.length === 1 && !waypointsPanelOpen) {
    toggleWaypointsPanel();
  }

  if (prev) {
    var gen = ++routingGeneration;
    statusManager.set('routing', 'Routing...', false);

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
      if (gen !== routingGeneration) return;
      statusManager.clear('routing');
      var latLngs = data.points.map(function(p) { return [p[0], p[1]]; });
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5, pane: 'routePane' }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      saveState();
      updateStats();
      requestScore();
    })
    .catch(function(err) {
      if (gen !== routingGeneration) return;
      statusManager.clear('routing');
      waypoints.pop();
      refreshMarkers();
      saveState();
      renderWaypointList();
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

  if (waypoints.length === 0) {
    localStorage.removeItem('twisty-build-state');
  } else {
    saveState();
  }
  renderWaypointList();
}

function clearRoute() {
  routingGeneration++;
  reverseGeocodeGeneration++;
  scoreGeneration++;
  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  markers.forEach(function(m) { map.removeLayer(m); });
  markers = [];
  waypoints = [];
  legs = [];
  labelCache = {};
  localStorage.removeItem('twisty-build-state');
  updateStats();
  requestScore();
  renderWaypointList();
}

function saveRoute() {
  var center = map.getCenter();
  var state = {
    version: 1,
    center: [center.lat, center.lng],
    zoom: map.getZoom(),
    waypoints: waypoints,
    legs: legs
  };
  var json = JSON.stringify(state, null, 2);

  if (window.showSaveFilePicker) {
    window.showSaveFilePicker({
      suggestedName: 'route.twisty.json',
      types: [{
        description: 'Twisty route',
        accept: { 'application/json': ['.json'] }
      }]
    }).then(function(fileHandle) {
      return fileHandle.createWritable();
    }).then(function(writable) {
      return writable.write(json).then(
        function() { return writable.close(); },
        function(err) { return writable.close().then(function() { throw err; }); }
      );
    }).catch(function(e) {
      if (e && e.name === 'AbortError') return;
      showToast('Could not save route');
    });
    return;
  }

  try {
    var blob = new Blob([json], { type: 'application/json' });
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = 'route.twisty.json';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  } catch(e) {
    showToast('Could not save route');
  }
}

function loadRoute() {
  var input = document.getElementById('file-input');
  input.value = '';
  input.click();
}

function onFileSelected(event) {
  var file = event.target.files[0];
  if (!file) return;
  var reader = new FileReader();
  reader.onload = function(e) {
    var state;
    try {
      state = JSON.parse(e.target.result);
    } catch(err) {
      showToast('Invalid route file');
      return;
    }
    if (
      !state ||
      state.version !== 1 ||
      !Array.isArray(state.waypoints) ||
      !state.waypoints.every(function(wp) { return Array.isArray(wp) && wp.length >= 2; }) ||
      !Array.isArray(state.legs) ||
      !state.legs.every(function(leg) {
        return leg &&
          Array.isArray(leg.points) &&
          leg.points.every(function(p) { return Array.isArray(p) && p.length >= 2; });
      }) ||
      state.waypoints.length > state.legs.length + 1
    ) {
      showToast('Invalid route file');
      return;
    }
    applyRouteState(state);
    saveState();
  };
  reader.onerror = function() {
    showToast('Could not read route file');
  };
  reader.readAsText(file);
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

// buildHTMLDebugSnippet is injected verbatim into buildHTML via fmt.Sprintf.
// Any literal '%' in this string MUST be written as '%%'.
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
		go s.fetchMissingTiles(missingTiles)
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
		go s.fetchMissingTiles(missingTiles)
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

	// Notify SSE broker for each tile that landed in cache.
	// Also clear any prior failure record — a tile that failed once and later
	// succeeds should no longer appear as failed in the UI.
	for _, t := range tiles {
		if cache.Has(t) {
			s.failedTiles.Delete(t)
			select {
			case s.tileReady <- t:
			default:
			}
		}
	}

	// After fetching, mark any tiles still missing in cache as failed.
	for _, t := range tiles {
		if !cache.Has(t) {
			s.failedTiles.Store(t, struct{}{})
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
		go s.fetchMissingTiles(missing)
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
		fc := collectionsToGeoJSON(collections)
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
		go s.fetchMissingTiles(missing)
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

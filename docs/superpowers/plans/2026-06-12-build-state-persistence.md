# Build State Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist map viewport, waypoints, and routed leg geometry in `localStorage` so that refreshing the browser during `twisty build` restores the in-progress route instantly.

**Architecture:** All changes are purely frontend JavaScript inside the `buildHTML` constant in `route_build.go`. A `saveState()` helper serializes state to `localStorage` on every mutation. On page load, a `restoreState()` function reads the saved state and replays it before any server-provided defaults take effect. A "Clear route" button resets all state and removes the saved entry.

**Tech Stack:** Go (string constant containing HTML/JS), Leaflet.js, browser `localStorage` API.

---

### Task 1: Add `saveState()` helper and hook it into map movement

**Files:**
- Modify: `route_build.go` (inside `buildHTML` const, after the `map.on('moveend zoomend', loadVisibleSegments)` line at ~line 283)

The goal is to save the viewport whenever the user pans or zooms, even without any waypoints. This is the simplest mutation hook — no route data involved.

- [ ] **Step 1: Add `saveState()` function after `loadVisibleSegments` definition**

Find the block around line 283 that reads:
```javascript
map.on('moveend zoomend', loadVisibleSegments);
loadVisibleSegments();
```

Replace it with:
```javascript
map.on('moveend zoomend', loadVisibleSegments);
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

map.on('moveend zoomend', saveState);
```

- [ ] **Step 2: Manually verify the save works**

Run `twisty build --address "your address"`, open the browser, pan the map, then open DevTools → Application → Local Storage and confirm `twisty-build-state` appears with `zoom` and `center` fields.

- [ ] **Step 3: Commit**

```bash
git add route_build.go
git commit -m "feat(build): save map viewport to localStorage on move/zoom"
```

---

### Task 2: Hook `saveState()` into waypoint and leg mutations

**Files:**
- Modify: `route_build.go` (inside `addWaypoint` and `removeLastWaypoint` functions)

- [ ] **Step 1: Call `saveState()` after adding the first waypoint (no leg yet)**

Find `addWaypoint`. It currently ends with `refreshMarkers()` for the no-previous-waypoint case. The structure is:

```javascript
function addWaypoint(latlng) {
  var prev = waypoints.length > 0 ? waypoints[waypoints.length - 1] : null;
  waypoints.push([latlng.lat, latlng.lng]);
  refreshMarkers();

  if (prev) {
    // ... fetch /api/route-leg ...
  }
}
```

Add `saveState()` unconditionally after `refreshMarkers()` (covers the single-waypoint case), and also after `legs.push(...)` inside the `.then()` (covers the leg case). The updated function:

```javascript
function addWaypoint(latlng) {
  var prev = waypoints.length > 0 ? waypoints[waypoints.length - 1] : null;
  waypoints.push([latlng.lat, latlng.lng]);
  refreshMarkers();
  saveState();

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
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5 }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      saveState();
      updateStats();
      requestScore();
    })
    .catch(function(err) {
      waypoints.pop();
      refreshMarkers();
      saveState();
      showToast('Could not route between these points — try a different location');
    });
  }
}
```

- [ ] **Step 2: Call `saveState()` at the end of `removeLastWaypoint()`**

Find `removeLastWaypoint`. After the final `requestScore()` call, add the save-or-clear logic:

```javascript
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
}
```

- [ ] **Step 3: Manually verify**

Run `twisty build`, add two waypoints, check DevTools localStorage — confirm `waypoints` and `legs` arrays are populated. Undo back to zero waypoints — confirm the key is removed.

- [ ] **Step 4: Commit**

```bash
git add route_build.go
git commit -m "feat(build): save waypoints and legs to localStorage on mutation"
```

---

### Task 3: Restore state on page load

**Files:**
- Modify: `route_build.go` (inside `buildHTML` const, near the map init at ~line 228)

Currently the map initializes with:
```javascript
var map = L.map('map').setView([%f, %f], 13);
```

The `%f, %f` values are Go `fmt.Sprintf` placeholders filled with the geocoded center. We keep them as the fallback but override with localStorage if present.

- [ ] **Step 1: Replace the `setView` call and add `restoreState()` invocation**

Change the map initialization block. Before it currently reads:
```javascript
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
```

Replace with:
```javascript
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
```

Then, after ALL functions are defined (after `exportRoute`, before `map.on('click', ...)`) add a `restoreState()` function and call it:

```javascript
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
  if (state.center && state.zoom) {
    map.setView(state.center, state.zoom);
  }
  waypoints = state.waypoints;
  legs = state.legs;
  legs.forEach(function(leg) {
    var latLngs = leg.points.map(function(p) { return [p[0], p[1]]; });
    var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5 }).addTo(map);
    legPolylines.push(polyline);
  });
  refreshMarkers();
  updateStats();
  if (legs.length > 0) {
    requestScore();
  }
}

restoreState();
```

Place this block just before `map.on('click', function(e) {`.

- [ ] **Step 2: Manually verify restore**

Run `twisty build`, add waypoints and route legs, then refresh the browser. Confirm:
- Map returns to the same viewport
- Waypoint markers appear in the correct positions
- Blue leg polylines render
- Stats (distance, time) populate
- Score is requested and appears

- [ ] **Step 3: Verify empty-state fallback**

Open DevTools, manually remove `twisty-build-state` from localStorage, then refresh. Confirm the map starts at the server-provided center with no markers or legs.

- [ ] **Step 4: Commit**

```bash
git add route_build.go
git commit -m "feat(build): restore map state from localStorage on page load"
```

---

### Task 4: Add "Clear route" button

**Files:**
- Modify: `route_build.go` (HTML and JS sections of `buildHTML`)

- [ ] **Step 1: Add the button to the HTML**

Find the `export-buttons` div:
```html
<div id="export-buttons">
  <button id="btn-gpx" disabled onclick="exportRoute('gpx')">Export GPX</button>
  <button id="btn-kml" disabled onclick="exportRoute('kml')">Export KML</button>
</div>
```

Replace with:
```html
<div id="export-buttons">
  <button id="btn-gpx" disabled onclick="exportRoute('gpx')">Export GPX</button>
  <button id="btn-kml" disabled onclick="exportRoute('kml')">Export KML</button>
  <button id="btn-clear" disabled onclick="clearRoute()" style="background:#dc2626;">Clear route</button>
</div>
```

- [ ] **Step 2: Add `clearRoute()` function and wire up disabled state**

Add `clearRoute()` after `removeLastWaypoint()`:

```javascript
function clearRoute() {
  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  markers.forEach(function(m) { map.removeLayer(m); });
  markers = [];
  waypoints = [];
  legs = [];
  localStorage.removeItem('twisty-build-state');
  updateStats();
  requestScore();
}
```

- [ ] **Step 3: Disable the button when no waypoints exist**

Find `updateStats()`. It currently sets `btn-gpx` and `btn-kml` disabled state:

```javascript
var hasRoute = waypoints.length >= 2;
document.getElementById('btn-gpx').disabled = !hasRoute;
document.getElementById('btn-kml').disabled = !hasRoute;
```

Replace with:
```javascript
var hasRoute = waypoints.length >= 2;
document.getElementById('btn-gpx').disabled = !hasRoute;
document.getElementById('btn-kml').disabled = !hasRoute;
document.getElementById('btn-clear').disabled = waypoints.length === 0;
```

- [ ] **Step 4: Manually verify**

Run `twisty build`, add waypoints. Confirm "Clear route" button is enabled. Click it — confirm all markers and polylines disappear, stats reset to `—`, and localStorage key is removed. Confirm the button becomes disabled again.

Also verify: refresh after clearing — map starts fresh at server-provided center.

- [ ] **Step 5: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add Clear route button that resets state and localStorage"
```

---

### Task 5: Verify the integration test still passes

**Files:**
- Read: `route_build_test.go`

No test changes are needed (this is pure frontend JS). Run the existing suite to confirm nothing was broken by changes to `buildHTML`.

- [ ] **Step 1: Run the test suite**

```bash
go test ./... -run TestBuild -v -count=1
```

Expected: all `TestBuild*` tests pass.

- [ ] **Step 2: Run the full test suite**

```bash
go test ./... -count=1
```

Expected: all tests pass with no failures.

- [ ] **Step 3: Commit if any test fixes were needed**

If all tests pass with no changes, no commit needed. If any test required a fix, commit with:

```bash
git add <changed files>
git commit -m "fix(build): update tests after state persistence changes"
```

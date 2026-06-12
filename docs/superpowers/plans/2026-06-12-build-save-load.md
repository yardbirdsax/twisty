# Route Save/Load Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Save and Load buttons to the `twisty build` map UI so users can download route state to a `.twisty.json` file and restore it later.

**Architecture:** Entirely frontend-only changes inside the `buildHTML` Go string constant in `route_build.go`. Save serializes `{version, viewport, waypoints, legs}` to JSON and triggers a browser file download. Load uses a hidden `<input type="file">` + `FileReader` to read a file back and restore state via the same logic as `restoreState()`.

**Tech Stack:** Go (embedded HTML string), vanilla JS, Leaflet — no new dependencies.

---

## File Structure

| File | Change |
|---|---|
| `route_build.go` | Add Save/Load buttons to `#stats` HTML; add `saveRoute()`, `loadRoute()` JS functions; add hidden file input element |
| `route_build_test.go` | Add integration test verifying the Save and Load button HTML is present in the served page |

---

### Task 1: Add Save/Load buttons to the stats panel HTML

**Files:**
- Modify: `route_build.go` (the `buildHTML` const, around line 216–220)

The `#stats` panel currently has this structure (lines 211–220):

```html
<div id="stats">
  <div class="label">Route Stats</div>
  <div class="row">...</div>  <!-- Twist Score -->
  <div class="row">...</div>  <!-- Distance -->
  <div class="row">...</div>  <!-- Time -->
  <div class="row" style="margin-top:8px;">
    <button id="btn-overlay-toggle" ...>Switch to Road view</button>
  </div>
  <div class="fetch-status done" id="fetch-status">Click map to start</div>
</div>
```

Add a new row with Save/Load buttons and a hidden file input, between the overlay toggle row and the fetch-status div:

```html
  <div class="row" style="margin-top:4px;gap:4px;">
    <button onclick="saveRoute()" style="flex:1;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Save</button>
    <button onclick="loadRoute()" style="flex:1;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Load</button>
  </div>
  <input type="file" id="file-input" accept=".json" style="display:none;" onchange="onFileSelected(event)">
```

Note: In `route_build.go`, `%%` is used to escape literal `%` inside a Go format string. The HTML above has no `%` chars so no escaping is needed.

- [ ] **Step 1: Add the button row and hidden input to `buildHTML`**

Locate the line containing `<div class="fetch-status done" id="fetch-status">` (around line 219 in `route_build.go`). Insert the two `<div class="row">` and `<input>` elements immediately before it, inside the `#stats` div.

- [ ] **Step 2: Build and verify HTML renders**

```bash
make build
```

Open `http://localhost:8080` after running `./bin/twisty build --address "your address"` and confirm the Save and Load buttons appear in the stats panel.

- [ ] **Step 3: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add Save/Load buttons and hidden file input to stats panel"
```

---

### Task 2: Implement `saveRoute()` JS function

**Files:**
- Modify: `route_build.go` (JS section, after `clearRoute()` around line 587)

- [ ] **Step 1: Add `saveRoute()` after `clearRoute()`**

Insert the following immediately after the closing `}` of `clearRoute()` (around line 587):

```js
function saveRoute() {
  var center = map.getCenter();
  var state = {
    version: 1,
    viewport: { center: [center.lat, center.lng], zoom: map.getZoom() },
    waypoints: waypoints,
    legs: legs
  };
  var blob = new Blob([JSON.stringify(state, null, 2)], { type: 'application/json' });
  var url = URL.createObjectURL(blob);
  var a = document.createElement('a');
  a.href = url;
  a.download = 'route.twisty.json';
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
```

- [ ] **Step 2: Build and manual test**

```bash
make build
```

Run the server, add a couple of waypoints, click **Save**. Verify:
- Browser downloads a file named `route.twisty.json`.
- Opening the file shows valid JSON with `version`, `viewport`, `waypoints`, and `legs` keys.
- Clicking Save with no waypoints downloads a file with `"waypoints": []` and `"legs": []`.

- [ ] **Step 3: Commit**

```bash
git add route_build.go
git commit -m "feat(build): implement saveRoute() to download route state as JSON"
```

---

### Task 3: Implement `loadRoute()` and `onFileSelected()` JS functions

**Files:**
- Modify: `route_build.go` (JS section, after `saveRoute()`)

- [ ] **Step 1: Add `loadRoute()` and `onFileSelected()` after `saveRoute()`**

```js
function loadRoute() {
  document.getElementById('file-input').value = '';
  document.getElementById('file-input').click();
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
      !Array.isArray(state.legs) ||
      !state.legs.every(function(leg) { return leg && Array.isArray(leg.points); }) ||
      state.waypoints.length > state.legs.length + 1
    ) {
      showToast('Invalid route file');
      return;
    }
    clearRoute();
    waypoints = state.waypoints;
    legs = state.legs;
    if (state.viewport && state.viewport.center && state.viewport.zoom != null) {
      map.setView(state.viewport.center, state.viewport.zoom);
    }
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
    saveState();
  };
  reader.readAsText(file);
}
```

- [ ] **Step 2: Build and manual test — happy path**

```bash
make build
```

1. Add waypoints, click **Save** to download `route.twisty.json`.
2. Click **Clear route**.
3. Click **Load**, select the saved file.
4. Verify: waypoints and route polylines reappear, map pans to saved viewport, score polling restarts.

- [ ] **Step 3: Manual test — error paths**

Test each error case:
- Load a file containing `"not json"` → toast "Invalid route file", state unchanged.
- Load a valid JSON file missing `version` → toast "Invalid route file".
- Load a valid JSON file with `version: 2` → toast "Invalid route file".
- Load a file with `legs: [{"no_points": true}]` → toast "Invalid route file".

- [ ] **Step 4: Commit**

```bash
git add route_build.go
git commit -m "feat(build): implement loadRoute() and onFileSelected() to restore route from file"
```

---

### Task 4: Add integration test for Save/Load button presence

**Files:**
- Modify: `route_build_test.go`

The existing `TestBuildServer_integration` test (line 374) hits the root handler and checks for certain HTML fragments. We add a focused test that the Save and Load buttons and the hidden file input are present in the served HTML.

- [ ] **Step 1: Write the failing test**

Add this test to `route_build_test.go`:

```go
func TestBuildHTML_containsSaveLoadUI(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`onclick="saveRoute()"`,
		`onclick="loadRoute()"`,
		`id="file-input"`,
		`type="file"`,
		`onchange="onFileSelected(event)"`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestBuildHTML_containsSaveLoadUI -v ./...
```

Expected: FAIL — the Save/Load UI elements don't exist yet (or if Task 1 is done first, this passes immediately; that's fine — run it now to confirm the test is syntactically correct).

- [ ] **Step 3: Verify test passes after Tasks 1–3**

```bash
go test -run TestBuildHTML_containsSaveLoadUI -v ./...
```

Expected: PASS

- [ ] **Step 4: Run full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add route_build_test.go
git commit -m "test(build): verify Save/Load UI elements present in served HTML"
```

---

## Execution Order

Tasks can be done in order 1 → 2 → 3 → 4. Task 4's test can be written first (TDD) before Task 1 if preferred — it will fail until Task 1 is complete.

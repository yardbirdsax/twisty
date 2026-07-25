# Build HTML/JS Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the ~1,470-line `buildHTML` Go string constant (HTML + CSS + JavaScript) from `route_build.go` into `static/build.html` and `static/build.js`, embed them via `//go:embed`, and add a Node.js test suite for the JS.

**Architecture:** Go injects a tiny inline `<script>window.TWISTY_CONFIG={...}</script>` block at serve time; the main JS reads config from that global instead of baking in `fmt.Sprintf` format verbs. All files are embedded into the binary — no static file server is added. The pattern mirrors the existing `static/statusManager.js` + `//go:embed` approach.

**Tech Stack:** Go (`//go:embed`, `fmt.Sprintf`), JavaScript (ES5 compatible, no bundler), Node.js built-in `node:test` + `node:vm` for JS tests.

## Global Constraints

- JavaScript must remain ES5-compatible (no `const`/`let`/arrow functions/template literals) — matches the existing style throughout `buildHTML`.
- No static file server — all assets injected inline at serve time.
- `make test` must pass: `go test -short ./...` + `node --test static/statusManager.test.js` + `node --test static/build.test.js`.
- No change to user-visible behavior: rendered HTML output must be semantically identical.
- `static/build.js` must be valid, loadable JavaScript — zero `%f`/`%g`/`%s` format verbs.

---

### Task 1: Create `static/build.html` and update Go embedding

Extract the HTML shell from the `buildHTML` constant into `static/build.html`, update `handleIndex` to use a config `<script>` block, and wire up the new `//go:embed` vars. After this task `go test ./...` still passes.

**Files:**
- Create: `static/build.html`
- Modify: `route_build.go` (lines 1–192, 1590–1646)

**Interfaces:**
- Produces: `var buildHTML string` (embedded from `static/build.html`) and `var buildJS string` (embedded from `static/build.js`, stubbed as empty string for now) used by `handleIndex`
- Produces: `window.TWISTY_CONFIG` shape: `{lat: float, lon: float, scorePerKmMax: float, scoreMax: float}`

- [ ] **Step 1: Create `static/build.html`**

Copy the content of the `buildHTML` constant (lines 193–1592 in `route_build.go`, the backtick string not including the backticks themselves) into `static/build.html`. Then make exactly these edits:

  a. Before the `<script>` tag at line 336 (the one that opens with `var map = L.map...`), insert these three lines:
     ```
     %s
     <script>%s</script>
     <script>%s</script>
     ```
     These become `fmt.Sprintf` args 1, 2, 3 (configScript, statusManagerJS, buildJS).

  b. Replace `var map = L.map('map').setView([%f, %f], 13);` with:
     ```javascript
     var map = L.map('map').setView([window.TWISTY_CONFIG.lat, window.TWISTY_CONFIG.lon], 13);
     ```

  c. Replace `var scorePerKmMax = %g;` with:
     ```javascript
     var scorePerKmMax = window.TWISTY_CONFIG.scorePerKmMax;
     ```

  d. Remove the bare `%s` line that was between `scorePerKmMax` and `var statusManager = ...` (this was the old statusManagerJS injection — it's now handled by the `<script>%s</script>` block added in step a).

  e. Replace `var SLIDER_MAX_SCORE = %g;` with:
     ```javascript
     var SLIDER_MAX_SCORE = window.TWISTY_CONFIG.scoreMax;
     ```

  f. The first `%s` debug-snippet placeholder (inside `<div id="stats">`, just before the closing `</div>`) stays as-is — it becomes arg 4 to `fmt.Sprintf`.

  g. Do NOT change any `%%` sequences — `build.html` is still passed through `fmt.Sprintf`, so `%%` must remain `%%` to produce literal `%` in CSS.

  The result has exactly **four** `%s` placeholders in order: configScript, statusManagerJS, buildJS, debugSnippet. No `%f` or `%g` remain.

     The final `static/build.html` will have exactly **four** `%s` placeholders:
     1. Before `<script>` tag (near bottom of body): config `<script>` block
     2. First `<script>%s</script>`: statusManagerJS
     3. Second `<script>%s</script>`: buildJS (empty for now)
     4. Inside `<div id="stats">` after waypoints section: debug snippet

- [ ] **Step 2: Create a temporary stub `static/build.js`**

Create `static/build.js` as an empty file (one comment line so it's syntactically valid):

```javascript
// build.js — extracted from buildHTML (populated in next task)
```

- [ ] **Step 3: Update `route_build.go` embeds and `handleIndex`**

Replace the three embed vars and `handleIndex` at the top of `route_build.go`. The current code (lines 28–30) is:

```go
//go:embed static/statusManager.js
var statusManagerJS string
```

Replace with:

```go
//go:embed static/statusManager.js
var statusManagerJS string

//go:embed static/build.html
var buildHTML string

//go:embed static/build.js
var buildJS string
```

Then update `handleIndex` (lines 178–191). Replace:

```go
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
	html := fmt.Sprintf(buildHTML, debugSnippet, s.center.Lat, s.center.Lon, quality.DefaultMaxCurvaturePerKm, statusManagerJS, quality.DefaultMaxCurvature)
	w.Write([]byte(html))
}
```

With:

```go
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
	html := fmt.Sprintf(buildHTML, configScript, statusManagerJS, buildJS, debugSnippet)
	w.Write([]byte(html))
}
```

Also remove the `const buildHTML = \`...\`` constant (lines 193–1592) and the `const buildHTMLDebugSnippet` constant (lines 1594–1646) from `route_build.go`. The `buildHTMLDebugSnippet` constant must move to `static/build.html` — but it's injected as the 4th `%s` arg, and its content is now in `static/build.html`. Wait — no: `buildHTMLDebugSnippet` is a snippet of HTML+JS that is conditionally injected. It stays as a Go constant in `route_build.go`. Only `buildHTML` is removed (now embedded from `static/build.html`).

So:
- Remove `const buildHTML = \`...\`` (lines 193–1592).
- Keep `const buildHTMLDebugSnippet = \`...\`` (lines 1594–1646) — it stays in `route_build.go` unchanged.

- [ ] **Step 4: Verify it compiles and tests pass**

```bash
cd /Users/joshuafeierman/repos/yardbirdsax/twisty
go build ./...
go test -short ./...
```

Expected: compiles cleanly, all Go tests pass (including existing `TestHandleIndex_*` tests).

- [ ] **Step 5: Commit**

```bash
git add static/build.html static/build.js route_build.go
git commit -m "refactor(build): embed build.html from static/ via go:embed"
```

---

### Task 2: Extract JavaScript into `static/build.js`

Move all the JavaScript from `static/build.html` into `static/build.js`. After this task `build.html` contains no JavaScript (only HTML/CSS), and `build.js` is a standalone valid JS file.

**Files:**
- Modify: `static/build.html`
- Modify: `static/build.js`

**Interfaces:**
- Consumes: `window.TWISTY_CONFIG.lat`, `window.TWISTY_CONFIG.lon`, `window.TWISTY_CONFIG.scorePerKmMax`, `window.TWISTY_CONFIG.scoreMax` (set by Go-injected config script before `build.js` runs)
- Consumes: `createStatusManager` function (provided by `statusManagerJS`, injected before `build.js`)
- Consumes: `L` global (Leaflet, loaded via CDN `<script>` tag in `build.html` `<head>`)

- [ ] **Step 1: Extract JS from `static/build.html` into `static/build.js`**

In `static/build.html`, find the `<script>` block that starts after the two `<script>%s</script>` injection lines. It begins with:

```html
<script>
var map = L.map('map').setView([window.TWISTY_CONFIG.lat, window.TWISTY_CONFIG.lon], 13);
```

and ends just before `</script>\n</body>\n</html>`.

Cut everything between (and including) that `<script>` and `</script>` — but NOT the outer tags themselves. The content that was between the tags goes into `static/build.js`. In `static/build.html`, replace that entire `<script>...</script>` block with nothing (remove it entirely — the JS is now injected via the `<script>%s</script>` placeholder).

`static/build.js` should start with:

```javascript
'use strict';
(function(global) {
var map = L.map('map').setView([window.TWISTY_CONFIG.lat, window.TWISTY_CONFIG.lon], 13);
```

Wait — the existing JS is NOT wrapped in an IIFE. Do not add one. Keep the JS exactly as it was between the `<script>` tags, with only the `window.TWISTY_CONFIG` substitutions already made in Task 1. The file starts with:

```javascript
var map = L.map('map').setView([window.TWISTY_CONFIG.lat, window.TWISTY_CONFIG.lon], 13);
L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 19,
    attribution: '&copy; OpenStreetMap contributors'
}).addTo(map);
```

and ends with the closing brace of `exportRoute`:

```javascript
  .catch(function(err) {
    showToast('Export failed: ' + err.message);
  });
}
```

- [ ] **Step 2: Verify `static/build.html` no longer contains `<script>` (aside from injection placeholders)**

Open `static/build.html` and confirm:
- No `<script>` tag remains in the body other than the two `<script>%s</script>` placeholders.
- The CDN `<script src="...leaflet...">` in `<head>` is still present.
- The `%s` placeholders for configScript, statusManagerJS, buildJS, and debugSnippet are all present.

- [ ] **Step 3: Verify it compiles and tests pass**

```bash
cd /Users/joshuafeierman/repos/yardbirdsax/twisty
go build ./...
go test -short ./...
```

Expected: all Go tests pass.

- [ ] **Step 4: Verify `static/build.js` is valid JavaScript**

```bash
node --check /Users/joshuafeierman/repos/yardbirdsax/twisty/static/build.js
```

Expected: exits 0 with no output (syntax valid). Note: this will not catch runtime errors from missing Leaflet globals — that's expected at this stage.

- [ ] **Step 5: Commit**

```bash
git add static/build.html static/build.js
git commit -m "refactor(build): move inline JS to static/build.js"
```

---

### Task 3: Write `static/build.test.js` and add to Makefile

Write a Node.js test file that exercises all JS functions in `build.js` that have testable pure logic (i.e., logic that doesn't strictly require a real DOM or real Leaflet). Add the test run to the Makefile.

**Files:**
- Create: `static/build.test.js`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `static/build.js` (loaded via `node:vm`, same pattern as `statusManager.test.js`)
- Consumes: `window.TWISTY_CONFIG` shape: `{lat: 40.0, lon: -75.0, scorePerKmMax: 1000, scoreMax: 500}`

**Functions to test** (all pure logic, no DOM/Leaflet side effects):

| Function | What to test |
|---|---|
| `waypointBadge(i)` | Returns circled digit for i=0..9, returns `#11` for i=10 |
| `labelKey(wp)` | Returns `"lat.toFixed(6),lon.toFixed(6)"` |
| `latLonFallback(wp)` | Returns `"lat.toFixed(4), lon.toFixed(4)"` |
| `curvatureColorLevel(scorePerKm)` | Returns 0 for score=0; returns value in (0,511] for positive score; clamps at max |
| `gradientColorCSS(level)` | Returns `#4ade80` for level≤0; `rgb(255,green,0)` for 1–256; `rgb(255,0,blue)` for 257–511 |
| `getDropIndex(list, clientY)` | Returns index where drop should land based on clientY vs row midpoints |
| `clearDropIndicator(list)` | Removes `.wp-drop-indicator` element if present |
| `markerColor(index, total)` | Returns `#16a34a` for index=0, `#dc2626` for last, `#2563eb` for middle |
| `getBboxString()` | Returns comma-separated bbox from map bounds (requires map stub) |
| `restoreState()` | Ignores missing/invalid localStorage; returns without throwing |

- [ ] **Step 1: Write `static/build.test.js`**

Create `static/build.test.js` with this content:

```javascript
'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');

// Build a minimal stub environment that satisfies build.js at load time.
// Functions that require real Leaflet/DOM are not tested here — only pure logic.
function makeCtx(overrides) {
  var mapBounds = {
    getWest: function() { return -75.123456; },
    getSouth: function() { return 40.123456; },
    getEast: function() { return -74.876544; },
    getNorth: function() { return 40.876544; },
  };
  var mapStub = {
    setView: function() { return mapStub; },
    createPane: function() {},
    getPane: function() { return { style: {} }; },
    getBounds: function() { return mapBounds; },
    getCenter: function() { return { lat: 40.0, lng: -75.0 }; },
    getZoom: function() { return 13; },
    on: function() {},
    off: function() {},
    removeLayer: function() {},
    addTo: function() { return mapStub; },
  };
  var leafletStub = {
    map: function() { return mapStub; },
    tileLayer: function() { return { addTo: function() {} }; },
    polyline: function() { return { addTo: function() {} }; },
    geoJSON: function() { return { addTo: function() {}, setStyle: function() {}, addData: function() {} }; },
    marker: function() { return { addTo: function() { return { on: function() {} }; } }; },
    divIcon: function(opts) { return opts; },
    latLng: function(lat, lng) { return { lat: lat, lng: lng }; },
  };
  var localStorageStub = {
    _data: {},
    getItem: function(k) { return this._data[k] || null; },
    setItem: function(k, v) { this._data[k] = v; },
    removeItem: function(k) { delete this._data[k]; },
  };
  function makeEl() {
    return {
      textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; },
      querySelectorAll: function() { return []; },
      appendChild: function() {},
      insertBefore: function() {},
      removeChild: function() {},
      addEventListener: function() {},
      getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {},
      classList: { add: function() {}, remove: function() {}, contains: function() { return false; } },
    };
  }
  var documentStub = {
    getElementById: function() { return makeEl(); },
    createElement: function() { return makeEl(); },
    createTextNode: function(t) { return { textContent: t }; },
    body: { appendChild: function() {}, removeChild: function() {} },
  };
  var EventSourceStub = function() {
    return { close: function() {}, onmessage: null };
  };
  var ctx = {
    window: {},
    globalThis: {},
    L: leafletStub,
    document: documentStub,
    localStorage: localStorageStub,
    EventSource: EventSourceStub,
    setTimeout: function() { return 0; },
    clearTimeout: function() {},
    fetch: function() { return Promise.resolve({ ok: true, json: function() { return Promise.resolve({}); }, blob: function() { return Promise.resolve(new Blob()); } }); },
    URL: { createObjectURL: function() { return ''; }, revokeObjectURL: function() {} },
    Blob: function() {},
    console: { log: function() {}, error: function() {} },
    Set: Set,
    Promise: Promise,
    JSON: JSON,
    Math: Math,
    Array: Array,
    parseInt: parseInt,
    parseFloat: parseFloat,
    encodeURIComponent: encodeURIComponent,
    isNaN: isNaN,
  };
  ctx.window.TWISTY_CONFIG = { lat: 40.0, lon: -75.0, scorePerKmMax: 1000, scoreMax: 500 };
  ctx.window.showSaveFilePicker = undefined;
  Object.assign(ctx, overrides || {});
  return ctx;
}

function loadBuildJS(ctx) {
  var src = fs.readFileSync(path.join(__dirname, 'build.js'), 'utf8');
  vm.runInContext(src, vm.createContext(ctx));
  return ctx;
}

// --- waypointBadge ---

test('waypointBadge returns circled digit for 0', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.waypointBadge(0), '①');
});

test('waypointBadge returns circled digit for 9', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.waypointBadge(9), '⑩');
});

test('waypointBadge returns #N for index >= 10', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.waypointBadge(10), '#11');
});

// --- labelKey ---

test('labelKey returns fixed-6 lat,lon string', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.labelKey([40.123456789, -75.987654321]), '40.123457,-75.987654');
});

// --- latLonFallback ---

test('latLonFallback returns fixed-4 with space after comma', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.latLonFallback([40.12345, -75.98765]), '40.1235, -75.9877');
});

// --- curvatureColorLevel ---

test('curvatureColorLevel returns 0 for score 0', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.curvatureColorLevel(0), 0);
});

test('curvatureColorLevel returns positive value for positive score', function() {
  var ctx = loadBuildJS(makeCtx());
  var level = ctx.curvatureColorLevel(500);
  assert.ok(level > 0, 'expected level > 0, got ' + level);
  assert.ok(level <= 511, 'expected level <= 511, got ' + level);
});

test('curvatureColorLevel clamps at max (score >> scorePerKmMax)', function() {
  var ctx = loadBuildJS(makeCtx());
  var levelAtMax = ctx.curvatureColorLevel(1000);
  var levelBeyond = ctx.curvatureColorLevel(9999999);
  assert.equal(levelAtMax, levelBeyond);
});

// --- gradientColorCSS ---

test('gradientColorCSS returns green for level 0', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.gradientColorCSS(0), '#4ade80');
});

test('gradientColorCSS returns green for negative level', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.gradientColorCSS(-1), '#4ade80');
});

test('gradientColorCSS returns rgb(255,g,0) for level 1', function() {
  var ctx = loadBuildJS(makeCtx());
  var c = ctx.gradientColorCSS(1);
  assert.match(c, /^rgb\(255,\d+,0\)$/);
});

test('gradientColorCSS returns rgb(255,0,0) for level 256', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.gradientColorCSS(256), 'rgb(255,0,0)');
});

test('gradientColorCSS returns rgb(255,0,b) for level 257', function() {
  var ctx = loadBuildJS(makeCtx());
  var c = ctx.gradientColorCSS(257);
  assert.match(c, /^rgb\(255,0,\d+\)$/);
});

// --- markerColor ---

test('markerColor returns green for index 0', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.markerColor(0, 3), '#16a34a');
});

test('markerColor returns red for last index', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.markerColor(2, 3), '#dc2626');
});

test('markerColor returns blue for middle index', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.equal(ctx.markerColor(1, 3), '#2563eb');
});

// --- getBboxString ---

test('getBboxString returns comma-separated fixed-6 bbox', function() {
  var ctx = loadBuildJS(makeCtx());
  var result = ctx.getBboxString();
  assert.equal(result, '-75.123456,40.123456,-74.876544,40.876544');
});

// --- getDropIndex ---

test('getDropIndex returns 0 when clientY is above first row midpoint', function() {
  var ctx = makeCtx();
  // Stub document.getElementById to return a list with two rows
  var rows = [
    { getBoundingClientRect: function() { return { top: 10, height: 20 }; } },
    { getBoundingClientRect: function() { return { top: 40, height: 20 }; } },
  ];
  var listEl = {
    querySelectorAll: function(sel) { return sel === '.wp-row' ? rows : []; },
  };
  ctx = loadBuildJS(ctx);
  assert.equal(ctx.getDropIndex(listEl, 5), 0);
});

test('getDropIndex returns 1 when clientY is between rows', function() {
  var ctx = makeCtx();
  var rows = [
    { getBoundingClientRect: function() { return { top: 10, height: 20 }; } },
    { getBoundingClientRect: function() { return { top: 40, height: 20 }; } },
  ];
  var listEl = {
    querySelectorAll: function(sel) { return sel === '.wp-row' ? rows : []; },
  };
  ctx = loadBuildJS(ctx);
  assert.equal(ctx.getDropIndex(listEl, 35), 1);
});

test('getDropIndex returns row count when clientY is below all rows', function() {
  var ctx = makeCtx();
  var rows = [
    { getBoundingClientRect: function() { return { top: 10, height: 20 }; } },
  ];
  var listEl = {
    querySelectorAll: function(sel) { return sel === '.wp-row' ? rows : []; },
  };
  ctx = loadBuildJS(ctx);
  assert.equal(ctx.getDropIndex(listEl, 999), 1);
});

// --- clearDropIndicator ---

test('clearDropIndicator removes .wp-drop-indicator if present', function() {
  var ctx = makeCtx();
  var removed = false;
  var indicator = { parentNode: { removeChild: function() { removed = true; } } };
  var listEl = {
    querySelector: function(sel) { return sel === '.wp-drop-indicator' ? indicator : null; },
  };
  ctx = loadBuildJS(ctx);
  ctx.clearDropIndicator(listEl);
  assert.ok(removed, 'expected removeChild to be called');
});

test('clearDropIndicator does nothing if no indicator present', function() {
  var ctx = makeCtx();
  var listEl = {
    querySelector: function() { return null; },
  };
  ctx = loadBuildJS(ctx);
  assert.doesNotThrow(function() { ctx.clearDropIndicator(listEl); });
});

// --- restoreState ---

test('restoreState does nothing when localStorage is empty', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.doesNotThrow(function() { ctx.restoreState(); });
});

test('restoreState ignores invalid JSON in localStorage', function() {
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-build-state'] = 'not-json';
  ctx = loadBuildJS(ctx);
  assert.doesNotThrow(function() { ctx.restoreState(); });
});

test('restoreState ignores state with missing waypoints array', function() {
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-build-state'] = JSON.stringify({ version: 1, legs: [] });
  ctx = loadBuildJS(ctx);
  assert.doesNotThrow(function() { ctx.restoreState(); });
});
```

- [ ] **Step 2: Run the tests to verify they all pass**

```bash
node --test /Users/joshuafeierman/repos/yardbirdsax/twisty/static/build.test.js
```

Expected: all tests pass. If any test fails due to a mismatch between the stub and actual function signature, fix the stub — do not change `build.js`.

- [ ] **Step 3: Add `build.test.js` to Makefile**

In `Makefile`, find the `test` target:

```makefile
test:
	go test -short ./...
	node --test static/statusManager.test.js
```

Replace with:

```makefile
test:
	go test -short ./...
	node --test static/statusManager.test.js
	node --test static/build.test.js
```

- [ ] **Step 4: Run full test suite**

```bash
cd /Users/joshuafeierman/repos/yardbirdsax/twisty && make test
```

Expected output ends with something like:
```
ok  	github.com/yardbirdsax/twisty	...
✓ idle state when no slots set
...
✓ waypointBadge returns circled digit for 0
...
```
All tests pass, zero failures.

- [ ] **Step 5: Commit**

```bash
git add static/build.test.js Makefile
git commit -m "test(build): add Node test suite for build.js"
```

---

### Task 4: Final verification

Confirm `route_build.go` contains no HTML or inline JavaScript, and `make test` passes cleanly end-to-end.

**Files:** No changes.

- [ ] **Step 1: Verify `route_build.go` is clean**

```bash
grep -c "DOCTYPE\|</script>\|var map = L" /Users/joshuafeierman/repos/yardbirdsax/twisty/route_build.go
```

Expected: `0` (no HTML or JS artifacts remain).

- [ ] **Step 2: Verify `static/build.js` has no format verbs**

```bash
grep -c '%f\|%g\|%s\|%d' /Users/joshuafeierman/repos/yardbirdsax/twisty/static/build.js
```

Expected: `0`

- [ ] **Step 3: Run full test suite one final time**

```bash
cd /Users/joshuafeierman/repos/yardbirdsax/twisty && make test
```

Expected: all pass.

- [ ] **Step 4: Verify the binary builds**

```bash
go build -o /tmp/twisty-check /Users/joshuafeierman/repos/yardbirdsax/twisty/...
```

Expected: exits 0. (Google API key is not needed for a plain `go build` without ldflags.)

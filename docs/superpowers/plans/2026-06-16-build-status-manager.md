# Status Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract a priority-based status manager from the scattered `#fetch-status` DOM assignments in `route_build.go`, add road-loading feedback, and cover the manager with unit tests.

**Architecture:** A `createStatusManager(el)` factory in `static/statusManager.js` owns three priority slots (`routing` > `scoring` > `roads`). Each async concern calls `statusManager.set(slot, text, done)` / `statusManager.clear(slot)` instead of directly touching the DOM. The file is embedded in the Go binary via `//go:embed` and inlined into the HTML template.

**Tech Stack:** Go (embed, net/http), vanilla JS (ES5 for compatibility with the existing code style), Node 18+ `node --test` for unit tests.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `static/statusManager.js` | Create | Status manager factory — pure logic, no globals |
| `static/statusManager.test.js` | Create | Unit tests via `node --test` |
| `route_build.go` | Modify | Embed JS, thread `statusManager` through HTML, replace DOM assignments |
| `Makefile` | Modify | Add `node --test` to `test` target |

---

## Task 1: Create `static/statusManager.js` (failing test first)

**Files:**
- Create: `static/statusManager.test.js`
- Create: `static/statusManager.js`

- [ ] **Step 1: Create the test file**

Create `static/statusManager.test.js` with this content:

```js
'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');

// Load the module under test. createStatusManager is assigned to globalThis
// by the script (no ES module exports, to stay compatible with <script> embedding).
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const src = fs.readFileSync(path.join(__dirname, 'statusManager.js'), 'utf8');
const ctx = vm.createContext({ globalThis: {} });
vm.runInContext(src, ctx);
const { createStatusManager } = ctx.globalThis;

function mockEl() {
  return { textContent: '', className: '' };
}

test('idle state when no slots set', function() {
  var el = mockEl();
  createStatusManager(el);
  assert.equal(el.textContent, 'Click map to start');
  assert.equal(el.className, 'fetch-status done');
});

test('set scoring only', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  sm.set('scoring', '⏳ Scoring 3 tiles...', false);
  assert.equal(el.textContent, '⏳ Scoring 3 tiles...');
  assert.equal(el.className, 'fetch-status');
});

test('routing wins over scoring', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  sm.set('scoring', '⏳ Scoring...', false);
  sm.set('routing', 'Routing...', false);
  assert.equal(el.textContent, 'Routing...');
  assert.equal(el.className, 'fetch-status');
});

test('clear routing reveals scoring', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  sm.set('scoring', '⏳ Scoring...', false);
  sm.set('routing', 'Routing...', false);
  sm.clear('routing');
  assert.equal(el.textContent, '⏳ Scoring...');
  assert.equal(el.className, 'fetch-status');
});

test('roads does not override scoring', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  sm.set('scoring', '⏳ Scoring...', false);
  sm.set('roads', '⏳ Loading roads...', false);
  assert.equal(el.textContent, '⏳ Scoring...');
});

test('clear all returns to idle', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  sm.set('scoring', '⏳ Scoring...', false);
  sm.set('roads', '⏳ Loading roads...', false);
  sm.clear('scoring');
  sm.clear('roads');
  assert.equal(el.textContent, 'Click map to start');
  assert.equal(el.className, 'fetch-status done');
});

test('done true gives fetch-status done class', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  sm.set('scoring', '✅ Score complete', true);
  assert.equal(el.className, 'fetch-status done');
});

test('done false gives fetch-status class', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  sm.set('scoring', '⏳ Scoring...', false);
  assert.equal(el.className, 'fetch-status');
});
```

- [ ] **Step 2: Run the tests — verify they fail**

```bash
node --test static/statusManager.test.js
```

Expected: error like `Cannot find module './statusManager.js'` or similar. The tests must fail before we write the implementation.

- [ ] **Step 3: Create `static/statusManager.js`**

Create `static/statusManager.js` with this content:

```js
'use strict';
(function(global) {
  var SLOTS = ['routing', 'scoring', 'roads'];

  function createStatusManager(el) {
    var state = { routing: null, scoring: null, roads: null };

    function render() {
      for (var i = 0; i < SLOTS.length; i++) {
        var s = state[SLOTS[i]];
        if (s !== null) {
          el.textContent = s.text;
          el.className = s.done ? 'fetch-status done' : 'fetch-status';
          return;
        }
      }
      el.textContent = 'Click map to start';
      el.className = 'fetch-status done';
    }

    render();

    return {
      set: function(slot, text, done) {
        state[slot] = { text: text, done: !!done };
        render();
      },
      clear: function(slot) {
        state[slot] = null;
        render();
      }
    };
  }

  global.createStatusManager = createStatusManager;
}(globalThis));
```

- [ ] **Step 4: Run the tests — verify they pass**

```bash
node --test static/statusManager.test.js
```

Expected output: all 8 tests pass, no failures.

- [ ] **Step 5: Commit**

```bash
git add static/statusManager.js static/statusManager.test.js
git commit -m "feat(build): add statusManager with unit tests"
```

---

## Task 2: Add `node --test` to `make test`

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Update the `test` target**

In `Makefile`, replace:

```makefile
test:
	go test -short ./...
```

with:

```makefile
test:
	go test -short ./...
	node --test static/statusManager.test.js
```

- [ ] **Step 2: Verify `make test` runs both suites**

```bash
make test
```

Expected: Go tests pass, then Node tests pass. No failures.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: add node --test to make test target"
```

---

## Task 3: Embed `statusManager.js` in Go and wire into the HTML template

**Files:**
- Modify: `route_build.go` (lines 1–25 for imports/embed, line 160 for `handleIndex`, line 164 for `buildHTML`)

- [ ] **Step 1: Add the embed directive and import**

At the top of `route_build.go`, after the `package main` line, add the embed import and directive. The current imports block starts at line 4. Add `_ "embed"` to the import block and add the embed directive before `buildHTML`. The result should look like this at the top of the file:

```go
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
```

- [ ] **Step 2: Update `handleIndex` to pass the embedded JS**

Replace the current `handleIndex` body (line 160):

```go
html := fmt.Sprintf(buildHTML, s.center.Lat, s.center.Lon, quality.DefaultMaxCurvaturePerKm)
```

with:

```go
html := fmt.Sprintf(buildHTML, s.center.Lat, s.center.Lon, quality.DefaultMaxCurvaturePerKm, statusManagerJS)
```

- [ ] **Step 3: Add the `%s` placeholder to `buildHTML`**

In the `buildHTML` const, find the `<script>` opening tag (currently at line 275):

```html
<script>
var map = L.map('map').setView([%f, %f], 13);
```

Replace it with:

```html
<script>
%s
var map = L.map('map').setView([%f, %f], 13);
```

Note: `buildHTML` uses `%%` for literal `%` throughout. The new `%s` is a real format verb — do not double it.

- [ ] **Step 4: Instantiate `statusManager` in the JS globals block**

Immediately after the `%s` line you just added (and before `var map = ...`), add the instantiation. The top of the `<script>` block should now read:

```html
<script>
%s
var statusManager = createStatusManager(document.getElementById('fetch-status'));
var map = L.map('map').setView([%f, %f], 13);
```

- [ ] **Step 5: Verify the binary compiles**

```bash
go build ./...
```

Expected: no errors. If you get `embed: pattern static/statusManager.js: no matching files found`, confirm the file exists at `static/statusManager.js`.

- [ ] **Step 6: Commit**

```bash
git add route_build.go
git commit -m "feat(build): embed statusManager.js and wire into HTML template"
```

---

## Task 4: Replace routing `fetch-status` DOM assignments

**Files:**
- Modify: `route_build.go` (`addWaypoint` function, lines 984–1019)

The `addWaypoint` function currently sets `fetch-status` directly at two places:
- Line 986: `document.getElementById('fetch-status').textContent = 'Routing...';`
- Line 987: `document.getElementById('fetch-status').className = 'fetch-status';`

There is no clear call on routing completion because routing success falls through to `requestScore()` (which will own the scoring slot). On error the route leg is rolled back and a toast is shown.

- [ ] **Step 1: Replace the routing status assignments in `addWaypoint`**

Find this block (around line 984):

```js
  if (prev) {
    var gen = ++routingGeneration;
    document.getElementById('fetch-status').textContent = 'Routing...';
    document.getElementById('fetch-status').className = 'fetch-status';

    fetch('/api/route-leg', {
```

Replace with:

```js
  if (prev) {
    var gen = ++routingGeneration;
    statusManager.set('routing', 'Routing...', false);

    fetch('/api/route-leg', {
```

Then find the `.then` success callback (around line 1001):

```js
    .then(function(data) {
      if (gen !== routingGeneration) return;
      var latLngs = data.points.map(function(p) { return [p[0], p[1]]; });
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5, pane: 'routePane' }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      saveState();
      updateStats();
      requestScore();
    })
```

Replace with:

```js
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
```

Then find the `.catch` callback (around line 1011):

```js
    .catch(function(err) {
      if (gen !== routingGeneration) return;
      waypoints.pop();
      refreshMarkers();
      saveState();
      renderWaypointList();
      showToast('Could not route between these points — try a different location');
    });
```

Replace with:

```js
    .catch(function(err) {
      if (gen !== routingGeneration) return;
      statusManager.clear('routing');
      waypoints.pop();
      refreshMarkers();
      saveState();
      renderWaypointList();
      showToast('Could not route between these points — try a different location');
    });
```

- [ ] **Step 2: Verify the binary compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add route_build.go
git commit -m "refactor(build): route routing status through statusManager"
```

---

## Task 5: Replace scoring `fetch-status` DOM assignments in `requestScore`

**Files:**
- Modify: `route_build.go` (`requestScore` function, lines 849–908)

- [ ] **Step 1: Replace the idle-path assignments**

Find (around line 866):

```js
    document.getElementById('fetch-status').textContent = 'Click map to start';
    document.getElementById('fetch-status').className = 'fetch-status done';
    return;
```

Replace with:

```js
    statusManager.clear('scoring');
    return;
```

- [ ] **Step 2: Replace the pending-tiles branch**

Find (around line 891):

```js
    if (data.pending_tiles > 0) {
      document.getElementById('fetch-status').textContent = '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...';
      document.getElementById('fetch-status').className = 'fetch-status';
      scorePollTimer = setTimeout(requestScore, 2000);
    } else if (data.failed_tiles > 0) {
      document.getElementById('fetch-status').textContent = '⚠ ' + data.failed_tiles + ' tile(s) failed';
      document.getElementById('fetch-status').className = 'fetch-status';
    } else {
      document.getElementById('fetch-status').textContent = '✅ Score complete';
      document.getElementById('fetch-status').className = 'fetch-status done';
    }
```

Replace with:

```js
    if (data.pending_tiles > 0) {
      statusManager.set('scoring', '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
      scorePollTimer = setTimeout(requestScore, 2000);
    } else if (data.failed_tiles > 0) {
      statusManager.set('scoring', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
    } else {
      statusManager.set('scoring', '✅ Score complete', true);
    }
```

- [ ] **Step 3: Replace the catch branch**

Find (around line 903):

```js
  .catch(function(err) {
    if (gen !== scoreGeneration) return;
    document.getElementById('fetch-status').textContent = 'Score error';
    document.getElementById('fetch-status').className = 'fetch-status';
  });
```

Replace with:

```js
  .catch(function(err) {
    if (gen !== scoreGeneration) return;
    statusManager.set('scoring', 'Score error', false);
  });
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add route_build.go
git commit -m "refactor(build): route requestScore status through statusManager"
```

---

## Task 6: Replace scoring `fetch-status` DOM assignments in `requestViewportScore`

**Files:**
- Modify: `route_build.go` (`requestViewportScore` function, lines 910–950)

- [ ] **Step 1: Replace the initial "Scoring viewport..." assignment**

Find (around line 921):

```js
  document.getElementById('fetch-status').textContent = '⏳ Scoring viewport...';
  document.getElementById('fetch-status').className = 'fetch-status';
```

Replace with:

```js
  statusManager.set('scoring', '⏳ Scoring viewport...', false);
```

- [ ] **Step 2: Replace the response branches**

Find (around line 934):

```js
    if (data.pending_tiles > 0) {
      document.getElementById('fetch-status').textContent = '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...';
      document.getElementById('fetch-status').className = 'fetch-status';
      scorePollTimer = setTimeout(requestViewportScore, 2000);
    } else if (data.failed_tiles > 0) {
      document.getElementById('fetch-status').textContent = '⚠ ' + data.failed_tiles + ' tile(s) failed';
      document.getElementById('fetch-status').className = 'fetch-status';
    } else {
      document.getElementById('fetch-status').textContent = '✅ Score complete';
      document.getElementById('fetch-status').className = 'fetch-status done';
    }
```

Replace with:

```js
    if (data.pending_tiles > 0) {
      statusManager.set('scoring', '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
      scorePollTimer = setTimeout(requestViewportScore, 2000);
    } else if (data.failed_tiles > 0) {
      statusManager.set('scoring', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
    } else {
      statusManager.set('scoring', '✅ Score complete', true);
    }
```

- [ ] **Step 3: Replace the catch branch**

Find (around line 946):

```js
  .catch(function(err) {
    document.getElementById('fetch-status').textContent = 'Score error';
    document.getElementById('fetch-status').className = 'fetch-status';
  });
```

Replace with:

```js
  .catch(function(err) {
    statusManager.set('scoring', 'Score error', false);
  });
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add route_build.go
git commit -m "refactor(build): route requestViewportScore status through statusManager"
```

---

## Task 7: Add road-loading status to `loadVisibleSegments`

**Files:**
- Modify: `route_build.go` (`loadVisibleSegments` function, lines 635–670)

- [ ] **Step 1: Add status calls to both fetch branches**

Find the current `loadVisibleSegments` function body (the two fetch branches, around line 658):

```js
  if (mode === OVERLAY_ROADS) {
    fetch('/api/road-segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(fc) { mergeFeatures(fc.features, gen, mode, layer, seen); })
      .catch(function() {});
  } else {
    fetch('/api/tiles?bbox=' + bbox);
    fetch('/api/segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(fc) { mergeFeatures(fc.features, gen, mode, layer, seen); })
      .catch(function() {});
  }
```

Replace with:

```js
  if (mode === OVERLAY_ROADS) {
    statusManager.set('roads', '⏳ Loading roads...', false);
    fetch('/api/road-segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(fc) {
        mergeFeatures(fc.features, gen, mode, layer, seen);
        if (gen === segmentGeneration) { statusManager.clear('roads'); }
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
```

Note: the `gen === segmentGeneration` guard mirrors the existing pattern in `mergeFeatures` — if a rebuild happened while the fetch was in-flight, don't touch the status (the rebuild will have started its own fetch with its own status).

- [ ] **Step 2: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add route_build.go
git commit -m "feat(build): show loading status in loadVisibleSegments"
```

---

## Task 8: Verify end-to-end with `make test` and smoke test

- [ ] **Step 1: Run the full test suite**

```bash
make test
```

Expected: Go tests pass, then:
```
▶ idle state when no slots set
▶ set scoring only
▶ routing wins over scoring
▶ clear routing reveals scoring
▶ roads does not override scoring
▶ clear all returns to idle
▶ done true gives fetch-status done class
▶ done false gives fetch-status class
ℹ tests 8
ℹ pass 8
ℹ fail 0
```

- [ ] **Step 2: Smoke test the running server**

Start the server:
```bash
go run . build --address "Asheville, NC"
```

Open `http://127.0.0.1:8080` in a browser.

Verify:
- Initial state shows "Click map to start"
- Pan or zoom the map → status briefly shows "⏳ Loading roads..."  then clears
- Click map to add a waypoint → status shows "Routing..." while routing, then transitions to scoring status
- Click "Refresh score" → status shows "⏳ Scoring viewport..." while pending

- [ ] **Step 3: Commit if any cleanup was needed; otherwise done**

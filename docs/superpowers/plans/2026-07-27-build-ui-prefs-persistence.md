# Build UI Preferences Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist UI preferences (overlay mode, overlay visibility, min-twist slider, min-speed slider, waypoints panel state) across page reloads using `localStorage`, and persist the reverse-geocode label cache alongside route state.

**Architecture:** Add `savePrefs()` / `loadPrefs()` functions to `build.js` that read/write a `twisty-ui-prefs` key. Extend the existing `saveState()` / `restoreState()` functions to include `labelCache`. Call `savePrefs()` at the end of each mutating function. Call `loadPrefs()` at startup before `restoreState()`.

**Tech Stack:** Vanilla JS, Node.js `node:test` for unit tests, `node:vm` sandbox for testing `build.js` in isolation.

## Global Constraints

- Tests use Node.js built-in `node:test` runner — run with `node --test static/build.test.js`
- Test context is a `vm` sandbox via `makeCtx()` / `loadBuildJS()` — all stubs live there
- `localStorage` stub is `ctx.localStorage._data` (object), not the browser API
- No new dependencies — plain JS only
- `savePrefs()` must be wrapped in try/catch like `saveState()`
- `loadPrefs()` must silently skip missing/invalid prefs keys without removing them
- `loadPrefs()` must run before `restoreState()` at startup

---

### Task 1: Add `savePrefs()` and `loadPrefs()`, extend `saveState()` / `restoreState()` with `labelCache`

**Files:**
- Modify: `static/build.js`
- Modify: `static/build.test.js`

**Interfaces:**
- Produces: `savePrefs()` — writes `twisty-ui-prefs` to `localStorage`
- Produces: `loadPrefs()` — reads `twisty-ui-prefs` and applies all prefs
- Modifies: `saveState()` — now includes `labelCache` in saved object
- Modifies: `restoreState()` — now reads `labelCache` from saved state

---

- [ ] **Step 1: Write failing tests for `savePrefs`**

Add to the bottom of `static/build.test.js`:

```js
// --- savePrefs ---

test('savePrefs writes overlayMode to twisty-ui-prefs', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.overlayMode = 'roads';
  ctx.savePrefs();
  var stored = JSON.parse(ctx.localStorage._data['twisty-ui-prefs']);
  assert.equal(stored.overlayMode, 'roads');
});

test('savePrefs writes overlayVisible to twisty-ui-prefs', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.overlayVisible = false;
  ctx.savePrefs();
  var stored = JSON.parse(ctx.localStorage._data['twisty-ui-prefs']);
  assert.equal(stored.overlayVisible, false);
});

test('savePrefs writes minTwistSlider from DOM input value', function() {
  var ctx = makeCtx();
  var sliderVal = '42';
  ctx.document.getElementById = function(id) {
    if (id === 'min-twist-slider') return { value: sliderVal };
    if (id === 'min-speed-slider') return { value: '0' };
    return { textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; }, querySelectorAll: function() { return []; },
      appendChild: function() {}, insertBefore: function() {}, removeChild: function() {},
      addEventListener: function() {}, getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {}, classList: { add: function() {}, remove: function() {}, contains: function() { return false; } } };
  };
  ctx = loadBuildJS(ctx);
  ctx.savePrefs();
  var stored = JSON.parse(ctx.localStorage._data['twisty-ui-prefs']);
  assert.equal(stored.minTwistSlider, 42);
});

test('savePrefs writes minSpeedSlider from DOM input value', function() {
  var ctx = makeCtx();
  ctx.document.getElementById = function(id) {
    if (id === 'min-twist-slider') return { value: '0' };
    if (id === 'min-speed-slider') return { value: '55' };
    return { textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; }, querySelectorAll: function() { return []; },
      appendChild: function() {}, insertBefore: function() {}, removeChild: function() {},
      addEventListener: function() {}, getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {}, classList: { add: function() {}, remove: function() {}, contains: function() { return false; } } };
  };
  ctx = loadBuildJS(ctx);
  ctx.savePrefs();
  var stored = JSON.parse(ctx.localStorage._data['twisty-ui-prefs']);
  assert.equal(stored.minSpeedSlider, 55);
});

test('savePrefs writes waypointsPanelOpen to twisty-ui-prefs', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.waypointsPanelOpen = true;
  ctx.savePrefs();
  var stored = JSON.parse(ctx.localStorage._data['twisty-ui-prefs']);
  assert.equal(stored.waypointsPanelOpen, true);
});
```

- [ ] **Step 2: Run tests to verify they fail**

```
node --test static/build.test.js 2>&1 | grep -A2 "savePrefs"
```

Expected: `TypeError: ctx.savePrefs is not a function` (or similar — function doesn't exist yet)

- [ ] **Step 3: Write failing tests for `loadPrefs`**

Add to the bottom of `static/build.test.js`:

```js
// --- loadPrefs ---

test('loadPrefs sets overlayMode from stored prefs', function() {
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-ui-prefs'] = JSON.stringify({ overlayMode: 'roads', overlayVisible: true, minTwistSlider: 0, minSpeedSlider: 0, waypointsPanelOpen: false });
  ctx = loadBuildJS(ctx);
  ctx.loadPrefs();
  assert.equal(ctx.overlayMode, 'roads');
});

test('loadPrefs updates btn-overlay-toggle text for roads mode', function() {
  var btnText = null;
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-ui-prefs'] = JSON.stringify({ overlayMode: 'roads', overlayVisible: true, minTwistSlider: 0, minSpeedSlider: 0, waypointsPanelOpen: false });
  ctx.document.getElementById = function(id) {
    var el = { textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; }, querySelectorAll: function() { return []; },
      appendChild: function() {}, insertBefore: function() {}, removeChild: function() {},
      addEventListener: function() {}, getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {}, classList: { add: function() {}, remove: function() {}, contains: function() { return false; } },
      value: '0' };
    if (id === 'btn-overlay-toggle') {
      Object.defineProperty(el, 'textContent', { get: function() { return btnText; }, set: function(v) { btnText = v; }, configurable: true });
    }
    return el;
  };
  ctx = loadBuildJS(ctx);
  ctx.loadPrefs();
  assert.equal(btnText, 'Switch to Way view');
});

test('loadPrefs sets overlayVisible=false and removes roadLayer from map', function() {
  var layerRemoved = false;
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-ui-prefs'] = JSON.stringify({ overlayMode: 'ways', overlayVisible: false, minTwistSlider: 0, minSpeedSlider: 0, waypointsPanelOpen: false });
  ctx = loadBuildJS(ctx);
  var origRemove = ctx.map.removeLayer.bind ? ctx.map.removeLayer.bind(ctx.map) : ctx.map.removeLayer;
  ctx.map.removeLayer = function(layer) { layerRemoved = true; };
  ctx.loadPrefs();
  assert.equal(ctx.overlayVisible, false);
  assert.ok(layerRemoved, 'expected map.removeLayer to be called');
});

test('loadPrefs calls onMinTwistInput with stored slider value', function() {
  var twistInputVal = null;
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-ui-prefs'] = JSON.stringify({ overlayMode: 'ways', overlayVisible: true, minTwistSlider: 30, minSpeedSlider: 0, waypointsPanelOpen: false });
  ctx = loadBuildJS(ctx);
  var origOnMinTwist = ctx.onMinTwistInput;
  ctx.onMinTwistInput = function(v) { twistInputVal = v; origOnMinTwist(v); };
  ctx.loadPrefs();
  assert.equal(twistInputVal, 30);
});

test('loadPrefs calls onMinSpeedInput with stored slider value', function() {
  var speedInputVal = null;
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-ui-prefs'] = JSON.stringify({ overlayMode: 'ways', overlayVisible: true, minTwistSlider: 0, minSpeedSlider: 45, waypointsPanelOpen: false });
  ctx = loadBuildJS(ctx);
  var origOnMinSpeed = ctx.onMinSpeedInput;
  ctx.onMinSpeedInput = function(v) { speedInputVal = v; origOnMinSpeed(v); };
  ctx.loadPrefs();
  assert.equal(speedInputVal, 45);
});

test('loadPrefs sets waypointsPanelOpen and updates panel DOM', function() {
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-ui-prefs'] = JSON.stringify({ overlayMode: 'ways', overlayVisible: true, minTwistSlider: 0, minSpeedSlider: 0, waypointsPanelOpen: true });
  ctx = loadBuildJS(ctx);
  ctx.loadPrefs();
  assert.equal(ctx.waypointsPanelOpen, true);
});

test('loadPrefs does nothing when twisty-ui-prefs is absent', function() {
  var ctx = loadBuildJS(makeCtx());
  assert.doesNotThrow(function() { ctx.loadPrefs(); });
  assert.equal(ctx.overlayMode, 'ways'); // default unchanged
});

test('loadPrefs does nothing when twisty-ui-prefs is invalid JSON', function() {
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-ui-prefs'] = 'not-json';
  ctx = loadBuildJS(ctx);
  assert.doesNotThrow(function() { ctx.loadPrefs(); });
  assert.equal(ctx.overlayMode, 'ways'); // default unchanged
});
```

- [ ] **Step 4: Run tests to verify they fail**

```
node --test static/build.test.js 2>&1 | grep -A2 "loadPrefs"
```

Expected: failures because `ctx.loadPrefs is not a function`

- [ ] **Step 5: Write failing tests for `labelCache` in `saveState` / `restoreState`**

Add to the bottom of `static/build.test.js`:

```js
// --- labelCache in saveState / restoreState ---

test('saveState includes labelCache in stored JSON', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.labelCache['40.000000,-75.000000'] = 'Main St';
  ctx.saveState();
  var stored = JSON.parse(ctx.localStorage._data['twisty-build-state']);
  assert.equal(stored.labelCache['40.000000,-75.000000'], 'Main St');
});

test('restoreState populates labelCache from stored state', function() {
  var state = {
    zoom: 13, center: [40.0, -75.0],
    waypoints: [[40.0, -75.0]], legs: [],
    labelCache: { '40.000000,-75.000000': 'Main St' }
  };
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-build-state'] = JSON.stringify(state);
  ctx = loadBuildJS(ctx);
  ctx.restoreState();
  assert.equal(ctx.labelCache['40.000000,-75.000000'], 'Main St');
});

test('restoreState works when labelCache is absent from stored state', function() {
  var state = {
    zoom: 13, center: [40.0, -75.0],
    waypoints: [[40.0, -75.0]], legs: []
  };
  var ctx = makeCtx();
  ctx.localStorage._data['twisty-build-state'] = JSON.stringify(state);
  ctx = loadBuildJS(ctx);
  assert.doesNotThrow(function() { ctx.restoreState(); });
});
```

- [ ] **Step 6: Run tests to verify they fail**

```
node --test static/build.test.js 2>&1 | grep -A2 "labelCache"
```

Expected: the `saveState includes labelCache` test fails (labelCache not in stored JSON yet)

- [ ] **Step 7: Implement `savePrefs`, `loadPrefs`, extend `saveState` / `restoreState`, and call `loadPrefs` at startup**

In `static/build.js`, make these changes:

**7a.** In `saveState()`, add `labelCache` to the state object (around line 734):

```js
function saveState() {
  try {
    var center = map.getCenter();
    var state = {
      zoom: map.getZoom(),
      center: [center.lat, center.lng],
      waypoints: waypoints,
      legs: legs,
      labelCache: labelCache
    };
    localStorage.setItem('twisty-build-state', JSON.stringify(state));
  } catch(e) {}
}
```

**7b.** In `restoreState()`, after `applyRouteState(state);` (around line 829), add labelCache restore. Find this block:

```js
  applyRouteState(state);
```

And change it to:

```js
  if (state.labelCache && typeof state.labelCache === 'object') {
    labelCache = state.labelCache;
  }
  applyRouteState(state);
```

**7c.** Add `savePrefs()` and `loadPrefs()` functions. Insert them just before `restoreState()` (around line 805):

```js
function savePrefs() {
  try {
    var prefs = {
      overlayMode: overlayMode,
      overlayVisible: overlayVisible,
      minTwistSlider: parseInt(document.getElementById('min-twist-slider').value, 10),
      minSpeedSlider: parseInt(document.getElementById('min-speed-slider').value, 10),
      waypointsPanelOpen: waypointsPanelOpen
    };
    localStorage.setItem('twisty-ui-prefs', JSON.stringify(prefs));
  } catch(e) {}
}

function loadPrefs() {
  var raw = localStorage.getItem('twisty-ui-prefs');
  if (!raw) return;
  var prefs;
  try {
    prefs = JSON.parse(raw);
  } catch(e) {
    return;
  }
  if (!prefs || typeof prefs !== 'object') return;

  if (prefs.overlayMode === 'roads' || prefs.overlayMode === 'ways') {
    overlayMode = prefs.overlayMode;
    var btn = document.getElementById('btn-overlay-toggle');
    if (btn) btn.textContent = overlayMode === 'ways' ? 'Switch to Road view' : 'Switch to Way view';
    var twistSlider = document.getElementById('min-twist-slider');
    var speedSlider = document.getElementById('min-speed-slider');
    if (twistSlider) twistSlider.disabled = overlayMode === 'ways';
    if (speedSlider) speedSlider.disabled = overlayMode === 'ways';
  }

  if (prefs.overlayVisible === false) {
    map.removeLayer(roadLayer);
    overlayVisible = false;
    var visBtn = document.getElementById('btn-overlay-visibility');
    if (visBtn) visBtn.textContent = 'Show overlay';
  }

  if (typeof prefs.minTwistSlider === 'number') {
    var ts = document.getElementById('min-twist-slider');
    if (ts) ts.value = prefs.minTwistSlider;
    onMinTwistInput(prefs.minTwistSlider);
  }

  if (typeof prefs.minSpeedSlider === 'number') {
    var ss = document.getElementById('min-speed-slider');
    if (ss) ss.value = prefs.minSpeedSlider;
    onMinSpeedInput(prefs.minSpeedSlider);
  }

  if (prefs.waypointsPanelOpen === true) {
    waypointsPanelOpen = true;
    var list = document.getElementById('waypoints-list');
    var toggle = document.getElementById('waypoints-toggle');
    if (list) list.className = 'open';
    if (toggle) toggle.textContent = '▼';
  }
}
```

**7d.** At the bottom of `build.js`, replace the `restoreState()` call with:

```js
loadPrefs();
restoreState();
```

(The existing call at line 861 is just `restoreState();` — add `loadPrefs();` on the line before it.)

- [ ] **Step 8: Run all tests**

```
node --test static/build.test.js
```

Expected: all tests pass

- [ ] **Step 9: Commit**

```
git add static/build.js static/build.test.js
git commit -m "feat(build): persist UI prefs and label cache across page reloads"
```

---

### Task 2: Wire `savePrefs()` into mutating functions

**Files:**
- Modify: `static/build.js`
- Modify: `static/build.test.js`

**Interfaces:**
- Consumes: `savePrefs()` from Task 1

---

- [ ] **Step 1: Write failing tests**

Add to the bottom of `static/build.test.js`:

```js
// --- savePrefs wiring ---

test('toggleOverlayMode saves prefs', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.toggleOverlayMode();
  assert.ok(ctx.localStorage._data['twisty-ui-prefs'], 'expected twisty-ui-prefs to be written after toggleOverlayMode');
  var stored = JSON.parse(ctx.localStorage._data['twisty-ui-prefs']);
  assert.equal(stored.overlayMode, 'roads');
});

test('toggleOverlayVisibility saves prefs', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.toggleOverlayVisibility();
  assert.ok(ctx.localStorage._data['twisty-ui-prefs'], 'expected twisty-ui-prefs to be written after toggleOverlayVisibility');
  var stored = JSON.parse(ctx.localStorage._data['twisty-ui-prefs']);
  assert.equal(stored.overlayVisible, false);
});

test('onMinTwistInput saves prefs', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.onMinTwistInput(50);
  assert.ok(ctx.localStorage._data['twisty-ui-prefs'], 'expected twisty-ui-prefs to be written after onMinTwistInput');
});

test('onMinSpeedInput saves prefs', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.onMinSpeedInput(30);
  assert.ok(ctx.localStorage._data['twisty-ui-prefs'], 'expected twisty-ui-prefs to be written after onMinSpeedInput');
});

test('toggleWaypointsPanel saves prefs', function() {
  var ctx = loadBuildJS(makeCtx());
  ctx.toggleWaypointsPanel();
  assert.ok(ctx.localStorage._data['twisty-ui-prefs'], 'expected twisty-ui-prefs to be written after toggleWaypointsPanel');
  var stored = JSON.parse(ctx.localStorage._data['twisty-ui-prefs']);
  assert.equal(stored.waypointsPanelOpen, true);
});
```

- [ ] **Step 2: Run tests to verify they fail**

```
node --test static/build.test.js 2>&1 | grep -A2 "saves prefs"
```

Expected: failures — `twisty-ui-prefs` is not written by these functions yet

- [ ] **Step 3: Add `savePrefs()` call to each function**

In `static/build.js`:

**3a.** At the end of `toggleOverlayMode()` (currently ends around line 789), add `savePrefs();` as the last line before the closing `}`.

**3b.** At the end of `toggleOverlayVisibility()` (currently ends around line 803), add `savePrefs();` as the last line before the closing `}`.

**3c.** At the end of `onMinTwistInput()` (currently ends around line 569), add `savePrefs();` as the last line before the closing `}`.

**3d.** At the end of `onMinSpeedInput()` (currently ends around line 575), add `savePrefs();` as the last line before the closing `}`.

**3e.** At the end of `toggleWaypointsPanel()` (currently ends around line 53), add `savePrefs();` as the last line before the closing `}`.

- [ ] **Step 4: Run all tests**

```
node --test static/build.test.js
```

Expected: all tests pass

- [ ] **Step 5: Commit**

```
git add static/build.js static/build.test.js
git commit -m "feat(build): call savePrefs on every UI preference change"
```

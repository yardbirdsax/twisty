# Insert Waypoint via Map Click Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a drag-to-insert placeholder row is showing in the waypoint list, a map click inserts the new waypoint at that position instead of appending it.

**Architecture:** Add two module-level variables (`insertSlotIndex`, `insertPlaceholderRow`) to `build.js`. `startInsertWaypoint` sets them when the placeholder appears and clears them on Escape. `addWaypoint` checks `insertSlotIndex` first (before the existing `activeSlotIndex` branch) and performs a direct splice + reroute when set. `commitInsertWaypoint` also clears them at the start of its success handler to prevent double-insertion.

**Tech Stack:** Vanilla JS (ES5), Node.js `--test` runner, `node:vm` for test isolation.

## Global Constraints

- All JS must be ES5-compatible (no `let`, `const`, arrow functions, template literals, destructuring).
- Tests run with: `node --test static/build.test.js`
- No new files — all changes go in `static/build.js` and `static/build.test.js`.
- No geocode call on map-click insert path — latlng is already known.
- `activeSlotIndex` replace logic must remain untouched.

---

### Task 1: Add module-level insert state variables and wire `startInsertWaypoint`

**Files:**
- Modify: `static/build.js` — add two variables, update `startInsertWaypoint` to set/clear them
- Test: `static/build.test.js`

**Interfaces:**
- Produces: `insertSlotIndex` (number, `-1` when idle), `insertPlaceholderRow` (DOM element or `null`)
- Produces: `startInsertWaypoint(insertIndex)` — sets `insertSlotIndex = insertIndex` and `insertPlaceholderRow = placeholderRow` after inserting the placeholder; clears both on Escape

- [ ] **Step 1: Write the failing tests**

Add to the end of `static/build.test.js`:

```js
// --- insertSlotIndex state ---

test('startInsertWaypoint sets insertSlotIndex to the given index', function() {
  var list = {
    querySelectorAll: function(sel) { return sel === '.wp-row' ? [] : []; },
    insertBefore: function() {},
  };
  var addRow = { id: 'waypoints-add' };
  var focusedInput = null;
  var createdEl = {
    className: '', type: '', placeholder: '', style: {},
    appendChild: function() {},
    focus: function() { focusedInput = this; },
    onkeydown: null,
  };
  var ctx = makeCtx();
  ctx.document.getElementById = function(id) {
    if (id === 'waypoints-list') return list;
    if (id === 'waypoints-add') return addRow;
    return { textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; },
      querySelectorAll: function() { return []; },
      appendChild: function() {}, insertBefore: function() {}, removeChild: function() {},
      addEventListener: function() {}, getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {}, classList: { add: function() {}, remove: function() {}, contains: function() { return false; } } };
  };
  ctx.document.createElement = function(tag) { return createdEl; };
  ctx = loadBuildJS(ctx);

  ctx.startInsertWaypoint(2);
  assert.equal(ctx.insertSlotIndex, 2);
});

test('startInsertWaypoint sets insertPlaceholderRow to the placeholder element', function() {
  var list = {
    querySelectorAll: function(sel) { return []; },
    insertBefore: function() {},
  };
  var addRow = { id: 'waypoints-add' };
  var createdEl = {
    className: '', type: '', placeholder: '', style: {},
    appendChild: function() {},
    focus: function() {},
    onkeydown: null,
  };
  var ctx = makeCtx();
  ctx.document.getElementById = function(id) {
    if (id === 'waypoints-list') return list;
    if (id === 'waypoints-add') return addRow;
    return { textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; },
      querySelectorAll: function() { return []; },
      appendChild: function() {}, insertBefore: function() {}, removeChild: function() {},
      addEventListener: function() {}, getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {}, classList: { add: function() {}, remove: function() {}, contains: function() { return false; } } };
  };
  ctx.document.createElement = function(tag) { return createdEl; };
  ctx = loadBuildJS(ctx);

  ctx.startInsertWaypoint(1);
  assert.ok(ctx.insertPlaceholderRow !== null, 'expected insertPlaceholderRow to be set');
});

test('Escape in startInsertWaypoint clears insertSlotIndex and insertPlaceholderRow', function() {
  var removedChild = null;
  var list = {
    querySelectorAll: function(sel) { return []; },
    insertBefore: function() {},
    removeChild: function(el) { removedChild = el; },
  };
  var addRow = { id: 'waypoints-add' };
  var capturedKeydown = null;
  var createdEl = {
    className: '', type: '', placeholder: '', style: {},
    appendChild: function() {},
    focus: function() {},
    set onkeydown(fn) { capturedKeydown = fn; },
    get onkeydown() { return capturedKeydown; },
  };
  var ctx = makeCtx();
  ctx.document.getElementById = function(id) {
    if (id === 'waypoints-list') return list;
    if (id === 'waypoints-add') return addRow;
    return { textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; },
      querySelectorAll: function() { return []; },
      appendChild: function() {}, insertBefore: function() {}, removeChild: function() {},
      addEventListener: function() {}, getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {}, classList: { add: function() {}, remove: function() {}, contains: function() { return false; } } };
  };
  ctx.document.createElement = function(tag) { return createdEl; };
  ctx = loadBuildJS(ctx);

  ctx.startInsertWaypoint(1);
  assert.equal(ctx.insertSlotIndex, 1);

  // Simulate Escape keydown
  capturedKeydown({ key: 'Escape' });
  assert.equal(ctx.insertSlotIndex, -1);
  assert.equal(ctx.insertPlaceholderRow, null);
});
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
node --test static/build.test.js 2>&1 | tail -20
```

Expected: 3 new tests fail — `insertSlotIndex` and `insertPlaceholderRow` are not defined.

- [ ] **Step 3: Add the two module-level variables to `build.js`**

In `static/build.js`, find the block near line 18 where `activeSlotIndex` is declared:

```js
var activeSlotIndex = -1;
var waypointsPanelOpen = false;
```

Change it to:

```js
var activeSlotIndex = -1;
var insertSlotIndex = -1;
var insertPlaceholderRow = null;
var waypointsPanelOpen = false;
```

- [ ] **Step 4: Update `startInsertWaypoint` to set/clear the new variables**

Find `startInsertWaypoint` (around line 349). The current function ends with:

```js
  if (insertIndex < rows.length) {
    list.insertBefore(placeholderRow, rows[insertIndex]);
  } else {
    list.insertBefore(placeholderRow, addRow);
  }
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitInsertWaypoint(insertIndex, q, placeholderRow, input);
    } else if (e.key === 'Escape') {
      list.removeChild(placeholderRow);
      showWpError('');
    }
  };
}
```

Replace that closing block with:

```js
  if (insertIndex < rows.length) {
    list.insertBefore(placeholderRow, rows[insertIndex]);
  } else {
    list.insertBefore(placeholderRow, addRow);
  }
  insertSlotIndex = insertIndex;
  insertPlaceholderRow = placeholderRow;
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitInsertWaypoint(insertIndex, q, placeholderRow, input);
    } else if (e.key === 'Escape') {
      list.removeChild(placeholderRow);
      insertSlotIndex = -1;
      insertPlaceholderRow = null;
      showWpError('');
    }
  };
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
node --test static/build.test.js 2>&1 | tail -20
```

Expected: all tests pass including the 3 new ones.

- [ ] **Step 6: Commit**

```bash
git add static/build.js static/build.test.js
git commit -m "feat(build): add insertSlotIndex state and wire startInsertWaypoint"
```

---

### Task 2: Handle map click when insert slot is active

**Files:**
- Modify: `static/build.js` — add `insertSlotIndex` branch at top of `addWaypoint`
- Modify: `static/build.js` — clear `insertSlotIndex`/`insertPlaceholderRow` at start of `commitInsertWaypoint` success handler
- Test: `static/build.test.js`

**Interfaces:**
- Consumes: `insertSlotIndex` (number), `insertPlaceholderRow` (DOM element or null) — from Task 1
- Consumes: `addWaypoint(latlng)` — existing function, `latlng` has `.lat` and `.lng` properties

- [ ] **Step 1: Write the failing tests**

Add to the end of `static/build.test.js`:

```js
// --- addWaypoint insert slot ---

test('addWaypoint with insertSlotIndex >= 0 inserts waypoint at correct index', function(t, done) {
  var rerouteAllCalled = false;
  var removedChild = null;
  var placeholderEl = {
    parentNode: { removeChild: function(el) { removedChild = el; } },
  };
  var ctx = makeCtx({
    fetch: function(url, opts) {
      if (typeof url === 'string' && url.indexOf('route-leg') !== -1) {
        return new Promise(function() {}); // never resolves — we only test the sync path
      }
      return Promise.resolve({ ok: true, json: function() { return Promise.resolve({}); } });
    }
  });
  ctx = loadBuildJS(ctx);

  // Seed two existing waypoints
  ctx.waypoints.push([40.0, -75.0]);
  ctx.waypoints.push([40.2, -75.2]);
  // Simulate insert slot between index 0 and 1
  ctx.insertSlotIndex = 1;
  ctx.insertPlaceholderRow = placeholderEl;

  ctx.addWaypoint({ lat: 40.1, lng: -75.1 });

  // Inserted at index 1 between the two existing waypoints
  assert.equal(ctx.waypoints.length, 3);
  assert.deepEqual(ctx.waypoints[1], [40.1, -75.1]);
  assert.equal(ctx.waypoints[0][0], 40.0);
  assert.equal(ctx.waypoints[2][0], 40.2);
  done();
});

test('addWaypoint with insertSlotIndex >= 0 clears insertSlotIndex and insertPlaceholderRow', function(t, done) {
  var placeholderEl = {
    parentNode: { removeChild: function() {} },
  };
  var ctx = makeCtx({
    fetch: function(url) {
      if (typeof url === 'string' && url.indexOf('route-leg') !== -1) {
        return new Promise(function() {});
      }
      return Promise.resolve({ ok: true, json: function() { return Promise.resolve({}); } });
    }
  });
  ctx = loadBuildJS(ctx);

  ctx.waypoints.push([40.0, -75.0]);
  ctx.waypoints.push([40.2, -75.2]);
  ctx.insertSlotIndex = 1;
  ctx.insertPlaceholderRow = placeholderEl;

  ctx.addWaypoint({ lat: 40.1, lng: -75.1 });

  assert.equal(ctx.insertSlotIndex, -1);
  assert.equal(ctx.insertPlaceholderRow, null);
  done();
});

test('addWaypoint with insertSlotIndex >= 0 removes placeholder row from DOM', function(t, done) {
  var removedEl = null;
  var placeholderEl = {
    parentNode: { removeChild: function(el) { removedEl = el; } },
  };
  var ctx = makeCtx({
    fetch: function(url) {
      if (typeof url === 'string' && url.indexOf('route-leg') !== -1) {
        return new Promise(function() {});
      }
      return Promise.resolve({ ok: true, json: function() { return Promise.resolve({}); } });
    }
  });
  ctx = loadBuildJS(ctx);

  ctx.waypoints.push([40.0, -75.0]);
  ctx.waypoints.push([40.2, -75.2]);
  ctx.insertSlotIndex = 1;
  ctx.insertPlaceholderRow = placeholderEl;

  ctx.addWaypoint({ lat: 40.1, lng: -75.1 });

  assert.equal(removedEl, placeholderEl);
  done();
});

test('addWaypoint falls through to append when insertSlotIndex is -1', function(t, done) {
  var ctx = makeCtx({
    fetch: function(url) {
      if (typeof url === 'string' && url.indexOf('route-leg') !== -1) {
        return new Promise(function() {});
      }
      return Promise.resolve({ ok: true, json: function() { return Promise.resolve({}); } });
    }
  });
  ctx = loadBuildJS(ctx);

  ctx.waypoints.push([40.0, -75.0]);
  // insertSlotIndex is -1 by default — normal append
  ctx.addWaypoint({ lat: 40.1, lng: -75.1 });

  assert.equal(ctx.waypoints.length, 2);
  assert.deepEqual(ctx.waypoints[1], [40.1, -75.1]);
  done();
});
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
node --test static/build.test.js 2>&1 | tail -20
```

Expected: the 4 new tests fail — `addWaypoint` doesn't yet check `insertSlotIndex`.

- [ ] **Step 3: Add the `insertSlotIndex` branch to `addWaypoint`**

Find `addWaypoint` in `static/build.js` (around line 1033). The function currently starts:

```js
function addWaypoint(latlng) {
  var prev = waypoints.length > 0 ? waypoints[waypoints.length - 1] : null;

  if (activeSlotIndex >= 0 && activeSlotIndex < waypoints.length) {
```

Change it to:

```js
function addWaypoint(latlng) {
  if (insertSlotIndex >= 0) {
    var idx = insertSlotIndex;
    var row = insertPlaceholderRow;
    insertSlotIndex = -1;
    insertPlaceholderRow = null;
    if (row && row.parentNode) row.parentNode.removeChild(row);
    waypoints.splice(idx, 0, [latlng.lat, latlng.lng]);
    legPolylines.forEach(function(p) { map.removeLayer(p); });
    legPolylines = [];
    legs = [];
    refreshMarkers();
    renderWaypointList();
    rerouteAll();
    saveState();
    return;
  }

  var prev = waypoints.length > 0 ? waypoints[waypoints.length - 1] : null;

  if (activeSlotIndex >= 0 && activeSlotIndex < waypoints.length) {
```

- [ ] **Step 4: Clear insert state in `commitInsertWaypoint` success handler**

Find `commitInsertWaypoint` (around line 383). Its `.then()` success body currently starts:

```js
    .then(function(data) {
      var list = document.getElementById('waypoints-list');
      list.removeChild(placeholderRow);
      waypoints.splice(insertIndex, 0, [data.lat, data.lon]);
```

Change it to:

```js
    .then(function(data) {
      insertSlotIndex = -1;
      insertPlaceholderRow = null;
      var list = document.getElementById('waypoints-list');
      list.removeChild(placeholderRow);
      waypoints.splice(insertIndex, 0, [data.lat, data.lon]);
```

- [ ] **Step 5: Run all tests to verify they pass**

```bash
node --test static/build.test.js 2>&1 | tail -20
```

Expected: all tests pass including the 4 new ones.

- [ ] **Step 6: Commit**

```bash
git add static/build.js static/build.test.js
git commit -m "feat(build): handle map click when insert slot is active"
```

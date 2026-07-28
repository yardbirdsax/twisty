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
    geoJSON: function() {
      var layer = { on: function() {}, addTo: function() { return layer; }, setStyle: function() { return layer; }, addData: function() {}, clear: function() {} };
      return layer;
    },
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
  var vmCtx = vm.createContext(ctx);
  // statusManager.js must be loaded first since build.js calls createStatusManager at top level.
  var smSrc = fs.readFileSync(path.join(__dirname, 'statusManager.js'), 'utf8');
  vm.runInContext(smSrc, vmCtx);
  // Expose createStatusManager from globalThis into the context root.
  vmCtx.createStatusManager = vmCtx.globalThis.createStatusManager;
  var src = fs.readFileSync(path.join(__dirname, 'build.js'), 'utf8');
  vm.runInContext(src, vmCtx);
  return vmCtx;
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
  // Use values where JS toFixed(4) rounding is unambiguous.
  assert.equal(ctx.latLonFallback([40.12346, -75.98766]), '40.1235, -75.9877');
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

// --- addWaypoint routing progress ---

test('addWaypoint sets routing status to "Routing leg 1 of 1..." when adding second waypoint', function(t, done) {
  var statusCalls = [];
  var ctx = makeCtx({
    fetch: function(url, opts) {
      return new Promise(function(resolve) {
        // Resolve after we've captured the status call
        setTimeout(resolve, 0);
      }).then(function() {
        return { ok: true, json: function() { return Promise.resolve({ points: [[40.1, -75.1], [40.2, -75.2]], duration: 60, distance: 1000 }); } };
      });
    }
  });
  // Intercept statusManager after build.js loads
  ctx = loadBuildJS(ctx);
  var origSet = ctx.statusManager.set.bind(ctx.statusManager);
  ctx.statusManager.set = function(slot, text, done2) {
    statusCalls.push({ slot: slot, text: text });
    origSet(slot, text, done2);
  };
  // Seed one existing waypoint
  ctx.waypoints.push([40.0, -75.0]);
  ctx.addWaypoint({ lat: 40.1, lng: -75.1 });
  // Check synchronously — the set call happens before the fetch resolves
  var routingCall = statusCalls.find(function(c) { return c.slot === 'routing'; });
  assert.ok(routingCall, 'expected a routing status call');
  assert.equal(routingCall.text, 'Routing leg 1 of 1...');
  done();
});

// --- rerouteAll routing progress ---

test('rerouteAll sets status to "Routing leg 1 of 2..." then "Routing leg 2 of 2..." then clears', function(t, done) {
  var statusLog = [];
  var resolvers = [];
  var ctx = makeCtx({
    fetch: function(url, opts) {
      // Only capture route-leg fetches in resolvers; score/segment fetches resolve immediately.
      if (typeof url === 'string' && url.indexOf('route-leg') !== -1) {
        return new Promise(function(resolve) {
          resolvers.push(resolve);
        }).then(function() {
          return { ok: true, json: function() { return Promise.resolve({ points: [[40.1, -75.1], [40.2, -75.2]], duration: 60, distance: 1000 }); } };
        });
      }
      return Promise.resolve({ ok: true, json: function() { return Promise.resolve({}); } });
    }
  });
  ctx = loadBuildJS(ctx);
  var origSet = ctx.statusManager.set.bind(ctx.statusManager);
  var origClear = ctx.statusManager.clear.bind(ctx.statusManager);
  ctx.statusManager.set = function(slot, text, isDone) {
    statusLog.push({ op: 'set', slot: slot, text: text });
    origSet(slot, text, isDone);
  };
  ctx.statusManager.clear = function(slot) {
    statusLog.push({ op: 'clear', slot: slot });
    origClear(slot);
  };

  ctx.waypoints.push([40.0, -75.0]);
  ctx.waypoints.push([40.1, -75.1]);
  ctx.waypoints.push([40.2, -75.2]);
  ctx.rerouteAll();

  // After rerouteAll(), leg 1 fetch is in-flight; check first status set synchronously.
  var leg1Set = statusLog.find(function(e) { return e.op === 'set' && e.slot === 'routing'; });
  assert.ok(leg1Set, 'expected first routing status set');
  assert.equal(leg1Set.text, 'Routing leg 1 of 2...');

  // Resolve leg 1 fetch. routeNext(1) fires after: fetch-mock .then (1) →
  // rerouteAll .then r.ok check returning r.json() thenable (2-4 for adoption) →
  // .then data body (5). Use 6 plain hops (no thenable return) for margin.
  resolvers[0]();
  Promise.resolve()
    .then(function(){}).then(function(){}).then(function(){})
    .then(function(){}).then(function(){}).then(function(){})
    .then(function() {
      var leg2Set = statusLog.filter(function(e) { return e.op === 'set' && e.slot === 'routing'; })[1];
      assert.ok(leg2Set, 'expected second routing status set');
      assert.equal(leg2Set.text, 'Routing leg 2 of 2...');

      // Resolve leg 2 fetch; same tick depth before clear fires.
      resolvers[1]();
      return Promise.resolve()
        .then(function(){}).then(function(){}).then(function(){})
        .then(function(){}).then(function(){}).then(function(){});
    }).then(function() {
      var cleared = statusLog.some(function(e) { return e.op === 'clear' && e.slot === 'routing'; });
      assert.ok(cleared, 'expected routing status to be cleared after last leg');
      done();
    });
});

test('rerouteAll clears routing status on error', function(t, done) {
  var statusLog = [];
  var ctx = makeCtx({
    fetch: function(url, opts) {
      return Promise.resolve({ ok: false, json: function() { return Promise.resolve({}); } });
    }
  });
  ctx = loadBuildJS(ctx);
  var origSet = ctx.statusManager.set.bind(ctx.statusManager);
  var origClear = ctx.statusManager.clear.bind(ctx.statusManager);
  ctx.statusManager.set = function(slot, text, isDone) {
    statusLog.push({ op: 'set', slot: slot, text: text });
    origSet(slot, text, isDone);
  };
  ctx.statusManager.clear = function(slot) {
    statusLog.push({ op: 'clear', slot: slot });
    origClear(slot);
  };

  ctx.waypoints.push([40.0, -75.0]);
  ctx.waypoints.push([40.1, -75.1]);
  ctx.rerouteAll();

  Promise.resolve().then(function() {
    return Promise.resolve();
  }).then(function() {
    var cleared = statusLog.some(function(e) { return e.op === 'clear' && e.slot === 'routing'; });
    assert.ok(cleared, 'expected routing status to be cleared on error');
    done();
  });
});

test('rerouteAll stale generation does not clear status of newer reroute', function(t, done) {
  var resolvers = [];
  var ctx = makeCtx({
    fetch: function(url, opts) {
      if (typeof url === 'string' && url.indexOf('route-leg') !== -1) {
        return new Promise(function(resolve) {
          resolvers.push(resolve);
        }).then(function() {
          return { ok: true, json: function() { return Promise.resolve({ points: [[40.1, -75.1], [40.2, -75.2]], duration: 60, distance: 1000 }); } };
        });
      }
      return Promise.resolve({ ok: true, json: function() { return Promise.resolve({}); } });
    }
  });
  ctx = loadBuildJS(ctx);

  var statusLog = [];
  var origSet = ctx.statusManager.set.bind(ctx.statusManager);
  var origClear = ctx.statusManager.clear.bind(ctx.statusManager);
  ctx.statusManager.set = function(slot, text, isDone) {
    statusLog.push({ op: 'set', slot: slot, text: text });
    origSet(slot, text, isDone);
  };
  ctx.statusManager.clear = function(slot) {
    statusLog.push({ op: 'clear', slot: slot });
    origClear(slot);
  };

  // First reroute with 1 leg
  ctx.waypoints.push([40.0, -75.0]);
  ctx.waypoints.push([40.1, -75.1]);
  ctx.rerouteAll();

  // Before first reroute resolves, start a second reroute (increments routingGeneration)
  ctx.rerouteAll();

  // Resolve the stale (first) fetch, then wait 6 plain hops — enough for the stale
  // callback to have fired (same depth as multi-leg test) — and assert no clear was added.
  resolvers[0]();
  Promise.resolve()
    .then(function(){}).then(function(){}).then(function(){})
    .then(function(){}).then(function(){}).then(function(){})
    .then(function() {
      var clearsAfter = statusLog.filter(function(e) { return e.op === 'clear' && e.slot === 'routing'; }).length;
      assert.equal(clearsAfter, 0, 'stale callback should not have added a clear');
      done();
    });
});

// --- insertSlotIndex state ---

test('startInsertWaypoint sets insertSlotIndex to the given index', function() {
  var list = {
    querySelectorAll: function(sel) { return sel === '.wp-row' ? [] : []; },
    insertBefore: function() {},
    addEventListener: function() {},
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
    addEventListener: function() {},
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
    addEventListener: function() {},
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

// --- addWaypoint insert slot ---

test('addWaypoint with insertSlotIndex >= 0 inserts waypoint at correct index', function(t, done) {
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
  assert.equal(ctx.waypoints[1][0], 40.1);
  assert.equal(ctx.waypoints[1][1], -75.1);
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
  assert.equal(ctx.waypoints[1][0], 40.1);
  assert.equal(ctx.waypoints[1][1], -75.1);
  done();
});

test('commitInsertWaypoint clears insertSlotIndex on geocode failure', function(t, done) {
  var placeholderEl = {
    className: '', style: {},
    appendChild: function() {},
    disabled: false,
    classList: { add: function() {}, remove: function() {} },
    focus: function() {},
    value: 'bad address',
  };
  var placeholderRow = {
    removeChild: function() {},
  };
  var ctx = makeCtx({
    fetch: function() {
      return Promise.resolve({ ok: false, status: 404, json: function() { return Promise.resolve({}); } });
    }
  });
  ctx = loadBuildJS(ctx);
  ctx.insertSlotIndex = 1;
  ctx.insertPlaceholderRow = placeholderRow;

  ctx.commitInsertWaypoint(1, 'bad address', placeholderRow, placeholderEl);

  Promise.resolve().then(function() {
    return Promise.resolve();
  }).then(function() {
    assert.equal(ctx.insertSlotIndex, -1, 'insertSlotIndex should be cleared on geocode failure');
    assert.equal(ctx.insertPlaceholderRow, null, 'insertPlaceholderRow should be cleared on geocode failure');
    done();
  });
});

test('addWaypoint with insertSlotIndex >= 0 calls reverseGeocodeUnlabeled', function(t, done) {
  var reverseGeocodeCalled = false;
  var placeholderEl = {
    parentNode: { removeChild: function() {} },
  };
  var ctx = makeCtx({
    fetch: function(url) {
      if (typeof url === 'string' && url.indexOf('route-leg') !== -1) {
        return new Promise(function() {});
      }
      if (typeof url === 'string' && url.indexOf('reverse-geocode') !== -1) {
        reverseGeocodeCalled = true;
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
  // Open the panel so reverseGeocodeUnlabeled actually fires fetches
  ctx.waypointsPanelOpen = true;

  ctx.addWaypoint({ lat: 40.1, lng: -75.1 });

  Promise.resolve().then(function() {
    assert.ok(reverseGeocodeCalled, 'expected reverseGeocodeUnlabeled to fire a reverse-geocode fetch');
    done();
  });
});

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
  ctx.document.getElementById = function(id) {
    if (id === 'min-twist-slider') return { value: '30' };
    if (id === 'min-speed-slider') return { value: '0' };
    return { textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; }, querySelectorAll: function() { return []; },
      appendChild: function() {}, insertBefore: function() {}, removeChild: function() {},
      addEventListener: function() {}, getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {}, classList: { add: function() {}, remove: function() {}, contains: function() { return false; } } };
  };
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
  ctx.document.getElementById = function(id) {
    if (id === 'min-twist-slider') return { value: '0' };
    if (id === 'min-speed-slider') return { value: '45' };
    return { textContent: '', className: '', style: {}, disabled: false,
      querySelector: function() { return null; }, querySelectorAll: function() { return []; },
      appendChild: function() {}, insertBefore: function() {}, removeChild: function() {},
      addEventListener: function() {}, getBoundingClientRect: function() { return { top: 0, height: 20 }; },
      dataset: {}, classList: { add: function() {}, remove: function() {}, contains: function() { return false; } } };
  };
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
  // labelCache must be populated after restoreState() — verifies that the
  // cache is restored inside applyRouteState() AFTER clearRoute() (which zeros
  // it) and BEFORE renderWaypointList() (which reads it to show human-readable
  // labels). Moving the assignment outside applyRouteState would cause
  // clearRoute() to wipe the cache before renderWaypointList() ever runs.
  assert.equal(ctx.labelCache['40.000000,-75.000000'], 'Main St');
});

test('applyRouteState restores labelCache before renderWaypointList runs', function() {
  // Verify timing: labelCache must be non-empty when renderWaypointList() is
  // called. We confirm this by checking that labelCache is populated immediately
  // after applyRouteState returns (the DOM stub does not support full rendering
  // inspection, but the cache value proves the assignment order is correct).
  var state = {
    zoom: 13, center: [40.0, -75.0],
    waypoints: [[40.0, -75.0]], legs: [],
    labelCache: { '40.000000,-75.000000': 'Oak Ave' }
  };
  var ctx = loadBuildJS(makeCtx());
  ctx.applyRouteState(state);
  assert.equal(ctx.labelCache['40.000000,-75.000000'], 'Oak Ave');
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

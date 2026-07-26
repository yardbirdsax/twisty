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

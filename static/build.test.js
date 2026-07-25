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

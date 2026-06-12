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

test('set with unknown slot throws', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  assert.throws(function() { sm.set('typo', 'msg', false); }, /Unknown slot/);
});

test('clear with unknown slot throws', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  assert.throws(function() { sm.clear('typo'); }, /Unknown slot/);
});

test('clear done slot while scoring active shows scoring', function() {
  var el = mockEl();
  var sm = createStatusManager(el);
  sm.set('routing', 'Routing...', false);
  sm.set('scoring', '⏳ Scoring...', false);
  sm.clear('routing');
  assert.equal(el.textContent, '⏳ Scoring...');
  assert.equal(el.className, 'fetch-status');
});

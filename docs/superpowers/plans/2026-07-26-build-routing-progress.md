# Build: Routing Progress Indicator — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show "Routing leg N of M..." in the status bar whenever the build UI is calculating route legs, replacing the current blank/generic indicator in `rerouteAll` and updating the text in `addWaypoint`.

**Architecture:** Both routing paths (`addWaypoint` for single-leg and `rerouteAll` for multi-leg) call `statusManager.set('routing', ...)` before each fetch and `statusManager.clear('routing')` on completion or error. The existing `routingGeneration` guard in `rerouteAll` is extended to also gate the `clear` call on the last leg, so a superseded reroute cannot clear a newer one's status.

**Tech Stack:** Vanilla JS (ES5); Node.js built-in test runner (`node --test`); no build step.

## Global Constraints

- Only `static/build.js` and `static/build.test.js` are modified — no changes to `statusManager.js`, `build.html`, or any Go files.
- Message format is exactly `'Routing leg N of M...'` (1-based N, M = total legs in operation).
- All status calls are gated on `gen === routingGeneration` in callbacks (same pattern as existing polyline/leg logic).
- Test runner: `node --test static/build.test.js`

---

### Task 1: Add routing progress to `addWaypoint`

**Files:**
- Modify: `static/build.js` (around line 1053 — the `statusManager.set('routing', 'Routing...', false)` call)
- Test: `static/build.test.js`

**Interfaces:**
- Produces: `statusManager.set('routing', 'Routing leg 1 of 1...', false)` replaces `statusManager.set('routing', 'Routing...', false)` — consumed by Task 2's tests as the confirmed single-leg message format.

- [ ] **Step 1: Write the failing test**

Add to the bottom of `static/build.test.js`:

```js
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
```

- [ ] **Step 2: Run the test to verify it fails**

```
node --test static/build.test.js
```

Expected: the new test FAILs with the received text being `'Routing...'`.

- [ ] **Step 3: Update `addWaypoint` in `static/build.js`**

Find (around line 1053):
```js
    statusManager.set('routing', 'Routing...', false);
```

Replace with:
```js
    statusManager.set('routing', 'Routing leg 1 of 1...', false);
```

- [ ] **Step 4: Run tests to verify the new test passes and no existing tests regress**

```
node --test static/build.test.js
```

Expected: all tests pass, 0 failures.

- [ ] **Step 5: Commit**

```bash
git add static/build.js static/build.test.js
git commit -m "feat(build): show 'Routing leg 1 of 1...' status in addWaypoint"
```

---

### Task 2: Add routing progress to `rerouteAll`

**Files:**
- Modify: `static/build.js` (the `rerouteAll` function, lines 442–484)
- Test: `static/build.test.js`

**Interfaces:**
- Consumes: single-leg message format `'Routing leg 1 of 1...'` confirmed in Task 1.
- Produces: `rerouteAll` sets `'Routing leg N of M...'` at the start of each `routeNext(i)` call; clears on last-leg success or any error — gated on `gen === routingGeneration`.

- [ ] **Step 1: Write failing tests**

Add to the bottom of `static/build.test.js`:

```js
// --- rerouteAll routing progress ---

test('rerouteAll sets status to "Routing leg 1 of 2..." then "Routing leg 2 of 2..." then clears', function(t, done) {
  var statusLog = [];
  var fetchCount = 0;
  var resolvers = [];
  var ctx = makeCtx({
    fetch: function(url, opts) {
      fetchCount++;
      return new Promise(function(resolve) {
        resolvers.push(resolve);
      }).then(function() {
        return { ok: true, json: function() { return Promise.resolve({ points: [[40.1, -75.1], [40.2, -75.2]], duration: 60, distance: 1000 }); } };
      });
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

  // After rerouteAll(), leg 1 fetch is in-flight; check first status set
  var leg1Set = statusLog.find(function(e) { return e.op === 'set' && e.slot === 'routing'; });
  assert.ok(leg1Set, 'expected first routing status set');
  assert.equal(leg1Set.text, 'Routing leg 1 of 2...');

  // Resolve leg 1 fetch, which triggers routeNext(1)
  resolvers[0]();
  Promise.resolve().then(function() {
    return Promise.resolve();
  }).then(function() {
    var leg2Set = statusLog.filter(function(e) { return e.op === 'set' && e.slot === 'routing'; })[1];
    assert.ok(leg2Set, 'expected second routing status set');
    assert.equal(leg2Set.text, 'Routing leg 2 of 2...');

    // Resolve leg 2 fetch
    resolvers[1]();
    return Promise.resolve().then(function() { return Promise.resolve(); });
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
      return new Promise(function(resolve) {
        resolvers.push(resolve);
      }).then(function() {
        return { ok: true, json: function() { return Promise.resolve({ points: [[40.1, -75.1], [40.2, -75.2]], duration: 60, distance: 1000 }); } };
      });
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

  // Count clears before resolving
  var clearsBefore = statusLog.filter(function(e) { return e.op === 'clear' && e.slot === 'routing'; }).length;

  // Resolve the stale (first) fetch
  resolvers[0]();
  Promise.resolve().then(function() {
    return Promise.resolve();
  }).then(function() {
    var clearsAfter = statusLog.filter(function(e) { return e.op === 'clear' && e.slot === 'routing'; }).length;
    assert.equal(clearsAfter, clearsBefore, 'stale callback should not have added a clear');
    done();
  });
});
```

- [ ] **Step 2: Run tests to verify the new tests fail**

```
node --test static/build.test.js
```

Expected: the three new `rerouteAll` tests FAIL (rerouteAll currently has no statusManager calls at all).

- [ ] **Step 3: Update `rerouteAll` in `static/build.js`**

Replace the entire `rerouteAll` function (lines 442–484):

```js
function rerouteAll() {
  if (waypoints.length < 2) return;

  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  legs = [];
  updateStats();
  requestScore();
  saveState();

  var gen = ++routingGeneration;
  var total = waypoints.length - 1;

  function routeNext(i) {
    if (i >= total) return;
    statusManager.set('routing', 'Routing leg ' + (i + 1) + ' of ' + total + '...', false);
    var from = waypoints[i];
    var to = waypoints[i + 1];
    fetch('/api/route-leg', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from: { lat: from[0], lon: from[1] }, to: { lat: to[0], lon: to[1] } })
    })
    .then(function(r) {
      if (!r.ok) throw new Error('Routing failed');
      return r.json();
    })
    .then(function(data) {
      if (gen !== routingGeneration) return;
      var latLngs = data.points.map(function(p) { return [p[0], p[1]]; });
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5, pane: 'routePane' }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      saveState();
      updateStats();
      requestScore();
      if (i + 1 >= total) {
        statusManager.clear('routing');
      } else {
        routeNext(i + 1);
      }
    })
    .catch(function() {
      if (gen !== routingGeneration) return;
      statusManager.clear('routing');
      showToast('Could not route leg ' + (i + 1));
    });
  }
  routeNext(0);
}
```

Note: `routeNext(i + 1)` is now called from within the success branch (replacing the previous unconditional `routeNext(i + 1)` at the end of `.then`). The logic is equivalent — only `routeNext` no longer calls itself past the end since the `if (i >= total)` guard at the top handled that — but now we also clear status on the last leg.

- [ ] **Step 4: Run tests to verify all pass**

```
node --test static/build.test.js
```

Expected: all tests pass, 0 failures.

- [ ] **Step 5: Commit**

```bash
git add static/build.js static/build.test.js
git commit -m "feat(build): show routing leg progress in rerouteAll"
```

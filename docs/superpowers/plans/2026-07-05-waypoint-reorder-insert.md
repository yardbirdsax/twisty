# Waypoint Reorder and Insert Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add drag-to-reorder and drag-to-insert to the waypoint list panel in `twisty build`.

**Architecture:** All changes are frontend-only inside the inline HTML/JS string `buildHTML` in `route_build.go`. Task 1 adds CSS and drag handles to each waypoint row plus the add row, wires up native HTML5 DnD events, and implements reorder. Task 2 extends that to support the insert-at-position flow via the add row.

**Tech Stack:** Go (host file only), vanilla JS (HTML5 Drag and Drop API), existing `rerouteAll()` / `saveState()` / `renderWaypointList()` functions.

## Global Constraints

- No new backend endpoints — frontend only.
- No new JS libraries — vanilla JS only (Leaflet is already present; no Sortable.js or similar).
- All JS lives inside the `buildHTML` Go string constant in `route_build.go`.
- CSS `%%` escaping rule: any literal `%` inside `buildHTML` or `buildHTMLDebugSnippet` must be written as `%%` because Go's `fmt.Sprintf` formats those strings.
- Run tests with: `go test ./... -run TestHandleIndex` (HTML structure tests) and `go test ./...` for the full suite.

---

### Task 1: Drag-to-reorder waypoint rows

**Files:**
- Modify: `route_build.go` (CSS block ~line 186–219, HTML waypoints panel ~line 268–278, JS `renderWaypointList` ~line 345–383)

**Interfaces:**
- Consumes: existing `waypoints []`, `legs []`, `legPolylines []`, `rerouteAll()`, `saveState()`, `renderWaypointList()`
- Produces: `dragState` object (used in Task 2), `getDropIndex(list, y)` function (used in Task 2), `clearDropIndicator()` function (used in Task 2)

- [ ] **Step 1: Write the failing HTML structure test**

Add to `route_build_test.go` inside `TestHandleIndex_rendersWaypointPanel` (the existing test at line 887). Extend its `checks` slice to also assert the drag handle CSS class and `draggable` attribute are present in the rendered HTML:

```go
func TestHandleIndex_rendersWaypointPanel(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`id="waypoints-section"`,
		`id="waypoints-header"`,
		`id="waypoints-list"`,
		`id="waypoints-add"`,
		`id="waypoints-count"`,
		`id="wp-error"`,
		`+ Add waypoint`,
		`wp-handle`,
		`wp-drop-indicator`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}
```

Note: this replaces (overwrites) the existing `TestHandleIndex_rendersWaypointPanel` function — it is a strict extension of it.

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./... -run TestHandleIndex_rendersWaypointPanel -v
```

Expected: FAIL — `expected HTML to contain "wp-handle"` and `expected HTML to contain "wp-drop-indicator"`.

- [ ] **Step 3: Add CSS for drag handle and drop indicator**

In `route_build.go`, find the CSS block that ends with:
```
  .wp-input.shake { animation: wp-shake 0.3s ease; }
```
(around line 219). Insert the four new rules immediately after that line, before the blank line that follows:

```
  .wp-handle { color: #9ca3af; cursor: grab; font-size: 13px; flex-shrink: 0; padding: 0 2px; user-select: none; }
  .wp-handle:active { cursor: grabbing; }
  .wp-row.dragging { opacity: 0.4; }
  .wp-drop-indicator { height: 2px; background: #2563eb; margin: 1px 0; border-radius: 1px; }
```

- [ ] **Step 4: Add drag handle to `renderWaypointList`**

In `route_build.go`, find the `renderWaypointList` JS function (around line 345). Currently it builds each row with a badge and label. Add a handle span as the **first** child of each row, before the badge:

Find this block inside `renderWaypointList`:
```javascript
    var badge = document.createElement('span');
    badge.className = 'wp-badge';
    badge.textContent = waypointBadge(i);
    badge.title = 'Click to set as active slot';
    badge.onclick = function(e) {
      e.stopPropagation();
      setActiveSlot(i);
    };

    var labelEl = document.createElement('span');
    labelEl.className = 'wp-label' + (label ? '' : ' fallback');
    labelEl.textContent = label || latLonFallback(wp);

    labelEl.onclick = function(e) {
      e.stopPropagation();
      startEditWaypoint(row, i, labelEl);
    };

    row.appendChild(badge);
    row.appendChild(labelEl);
    list.insertBefore(row, addRow);
```

Replace it with:
```javascript
    var handle = document.createElement('span');
    handle.className = 'wp-handle';
    handle.textContent = '⠿';
    handle.title = 'Drag to reorder';
    (function(idx) {
      handle.addEventListener('mousedown', function() { row.draggable = true; });
      handle.addEventListener('mouseup', function() { row.draggable = false; });
    })(i);

    var badge = document.createElement('span');
    badge.className = 'wp-badge';
    badge.textContent = waypointBadge(i);
    badge.title = 'Click to set as active slot';
    badge.onclick = function(e) {
      e.stopPropagation();
      setActiveSlot(i);
    };

    var labelEl = document.createElement('span');
    labelEl.className = 'wp-label' + (label ? '' : ' fallback');
    labelEl.textContent = label || latLonFallback(wp);

    labelEl.onclick = function(e) {
      e.stopPropagation();
      startEditWaypoint(row, i, labelEl);
    };

    row.appendChild(handle);
    row.appendChild(badge);
    row.appendChild(labelEl);
    list.insertBefore(row, addRow);
```

Note: `row.draggable` is set to `true` only while the mouse is held on the handle, and back to `false` on mouseup. This prevents the whole row from being a drag source when clicking the badge or label.

- [ ] **Step 5: Add drag state variables and helper functions**

In `route_build.go`, find the JS variable declarations block near the top of the `<script>` tag (around line 300–313, where `waypoints`, `markers`, etc. are declared). After `var reverseGeocodeGeneration = 0;`, add:

```javascript
var dragState = null; // { type: 'reorder'|'insert', fromIndex: number|null }
```

Then, after `renderWaypointList` and before `setActiveSlot`, add two helper functions:

```javascript
function getDropIndex(list, clientY) {
  var rows = list.querySelectorAll('.wp-row');
  for (var i = 0; i < rows.length; i++) {
    var rect = rows[i].getBoundingClientRect();
    if (clientY < rect.top + rect.height / 2) return i;
  }
  return rows.length;
}

function clearDropIndicator(list) {
  var existing = list.querySelector('.wp-drop-indicator');
  if (existing) existing.parentNode.removeChild(existing);
}
```

- [ ] **Step 6: Wire up drag-and-drop event listeners on the waypoints list**

In `route_build.go`, find the `toggleWaypointsPanel` function (around line 334). After that function (before `renderWaypointList`), add a new function `initWaypointDnD` and a call to it:

```javascript
function initWaypointDnD() {
  var list = document.getElementById('waypoints-list');

  list.addEventListener('dragstart', function(e) {
    var row = e.target.closest('.wp-row');
    if (!row) return;
    var idx = parseInt(row.dataset.index, 10);
    dragState = { type: 'reorder', fromIndex: idx };
    row.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
  });

  list.addEventListener('dragend', function(e) {
    var row = e.target.closest('.wp-row');
    if (row) { row.classList.remove('dragging'); row.draggable = false; }
    clearDropIndicator(list);
    dragState = null;
  });

  list.addEventListener('dragover', function(e) {
    if (!dragState || dragState.type !== 'reorder') return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    clearDropIndicator(list);
    var toIndex = getDropIndex(list, e.clientY);
    var addRow = document.getElementById('waypoints-add');
    var rows = list.querySelectorAll('.wp-row');
    var indicator = document.createElement('div');
    indicator.className = 'wp-drop-indicator';
    if (toIndex < rows.length) {
      list.insertBefore(indicator, rows[toIndex]);
    } else {
      list.insertBefore(indicator, addRow);
    }
  });

  list.addEventListener('drop', function(e) {
    e.preventDefault();
    if (!dragState || dragState.type !== 'reorder') return;
    var fromIndex = dragState.fromIndex;
    var toIndex = getDropIndex(list, e.clientY);
    clearDropIndicator(list);
    dragState = null;

    // Dropping at fromIndex or fromIndex+1 is a no-op (same effective position).
    if (toIndex === fromIndex || toIndex === fromIndex + 1) return;

    var wp = waypoints.splice(fromIndex, 1)[0];
    var insertAt = toIndex > fromIndex ? toIndex - 1 : toIndex;
    waypoints.splice(insertAt, 0, wp);

    legPolylines.forEach(function(p) { map.removeLayer(p); });
    legPolylines = [];
    legs = [];
    refreshMarkers();
    renderWaypointList();
    rerouteAll();
    saveState();
  });
}

initWaypointDnD();
```

- [ ] **Step 7: Run the HTML structure test to confirm it passes**

```bash
go test ./... -run TestHandleIndex_rendersWaypointPanel -v
```

Expected: PASS — `wp-handle` and `wp-drop-indicator` now appear in the rendered HTML.

- [ ] **Step 8: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 9: Manual smoke test**

Start the server (requires a geocodeable address — use any real city):
```bash
go run . build --address "Asheville, NC"
```
Open `http://127.0.0.1:8080`, click to place 3+ waypoints, open the waypoints panel, then drag the ⠿ handle on waypoint 3 to the top of the list. Verify:
- The drop indicator (blue line) appears between rows while dragging
- On drop, waypoints reorder and the route rebuilds
- Dragging a row to its own position fires no reroute (check browser console for no unnecessary fetch calls)

- [ ] **Step 10: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): add drag-to-reorder waypoints"
```

---

### Task 2: Drag-to-insert via the `+ Add waypoint` row

**Files:**
- Modify: `route_build.go` (HTML `#waypoints-add` element ~line 275, JS `initWaypointDnD`, `startAddWaypoint`, and `commitAddWaypoint`)

**Interfaces:**
- Consumes: `dragState`, `getDropIndex(list, y)`, `clearDropIndicator(list)` from Task 1; `waypoints []`, `labelCache {}`, `rerouteAll()`, `saveState()`, `renderWaypointList()`, `refreshMarkers()`
- Produces: `commitInsertWaypoint(index, query, placeholderRow, input)` function

- [ ] **Step 1: Write the failing test**

Add a new test function to `route_build_test.go` that asserts the `#waypoints-add` row has a drag handle span with class `wp-handle` in the rendered HTML:

```go
func TestHandleIndex_addRowHasDragHandle(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	// The add row must contain a wp-handle span so it can be dragged to set insert position.
	// Find the waypoints-add div and check it contains the handle markup.
	addIdx := strings.Index(body, `id="waypoints-add"`)
	if addIdx < 0 {
		t.Fatal("waypoints-add element not found in HTML")
	}
	// The handle is rendered as a child span inside #waypoints-add.
	// We check the static HTML has the handle span inline (not JS-rendered).
	if !strings.Contains(body[addIdx:addIdx+200], `wp-handle`) {
		t.Errorf("expected waypoints-add to contain a wp-handle span in static HTML")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

```bash
go test ./... -run TestHandleIndex_addRowHasDragHandle -v
```

Expected: FAIL — `expected waypoints-add to contain a wp-handle span in static HTML`.

- [ ] **Step 3: Add the drag handle to the static `#waypoints-add` HTML**

In `route_build.go`, find the static HTML for the add row (around line 275):
```html
<div id="waypoints-add" onclick="startAddWaypoint()">+ Add waypoint</div>
```

Replace it with:
```html
<div id="waypoints-add" onclick="startAddWaypoint()"><span class="wp-handle" id="waypoints-add-handle" title="Drag to insert at position">⠿</span>+ Add waypoint</div>
```

- [ ] **Step 4: Run the test to confirm it passes**

```bash
go test ./... -run TestHandleIndex_addRowHasDragHandle -v
```

Expected: PASS.

- [ ] **Step 5: Extend `initWaypointDnD` to handle insert drags**

In `route_build.go`, find the `initWaypointDnD` function added in Task 1. Add a `mousedown` listener for the add row handle immediately before the closing `}` of `initWaypointDnD`, and extend the existing `dragstart`, `dragend`, `dragover`, and `drop` listeners to handle the `'insert'` type.

Replace the entire `initWaypointDnD` function with:

```javascript
function initWaypointDnD() {
  var list = document.getElementById('waypoints-list');

  // Wire add-row handle so dragging it sets insert mode.
  var addHandle = document.getElementById('waypoints-add-handle');
  if (addHandle) {
    var addRow = document.getElementById('waypoints-add');
    addHandle.addEventListener('mousedown', function(e) {
      e.stopPropagation();
      addRow.draggable = true;
      dragState = { type: 'insert', fromIndex: null };
    });
    addHandle.addEventListener('mouseup', function() {
      addRow.draggable = false;
    });
  }

  list.addEventListener('dragstart', function(e) {
    var row = e.target.closest('.wp-row');
    if (!row) return;
    var idx = parseInt(row.dataset.index, 10);
    dragState = { type: 'reorder', fromIndex: idx };
    row.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
  });

  list.addEventListener('dragend', function(e) {
    var row = e.target.closest('.wp-row');
    if (row) { row.classList.remove('dragging'); row.draggable = false; }
    var addRow = document.getElementById('waypoints-add');
    if (addRow) addRow.draggable = false;
    clearDropIndicator(list);
    // If insert drag ended without a drop (e.g., dropped outside), clear state.
    if (dragState && dragState.type === 'insert') { dragState = null; }
  });

  list.addEventListener('dragover', function(e) {
    if (!dragState) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    clearDropIndicator(list);
    var toIndex = getDropIndex(list, e.clientY);
    var addRow = document.getElementById('waypoints-add');
    var rows = list.querySelectorAll('.wp-row');
    var indicator = document.createElement('div');
    indicator.className = 'wp-drop-indicator';
    if (toIndex < rows.length) {
      list.insertBefore(indicator, rows[toIndex]);
    } else {
      list.insertBefore(indicator, addRow);
    }
  });

  list.addEventListener('drop', function(e) {
    e.preventDefault();
    if (!dragState) return;
    var type = dragState.type;
    var fromIndex = dragState.fromIndex;
    var toIndex = getDropIndex(list, e.clientY);
    clearDropIndicator(list);
    dragState = null;

    var addRow = document.getElementById('waypoints-add');
    if (addRow) addRow.draggable = false;

    if (type === 'reorder') {
      if (toIndex === fromIndex || toIndex === fromIndex + 1) return;
      var wp = waypoints.splice(fromIndex, 1)[0];
      var insertAt = toIndex > fromIndex ? toIndex - 1 : toIndex;
      waypoints.splice(insertAt, 0, wp);
      legPolylines.forEach(function(p) { map.removeLayer(p); });
      legPolylines = [];
      legs = [];
      refreshMarkers();
      renderWaypointList();
      rerouteAll();
      saveState();
    } else if (type === 'insert') {
      startInsertWaypoint(toIndex);
    }
  });
}

initWaypointDnD();
```

- [ ] **Step 6: Add `startInsertWaypoint` and `commitInsertWaypoint`**

In `route_build.go`, find `startAddWaypoint` (around line 443). Immediately after `commitAddWaypoint`, add:

```javascript
function startInsertWaypoint(insertIndex) {
  var list = document.getElementById('waypoints-list');
  var rows = list.querySelectorAll('.wp-row');
  var addRow = document.getElementById('waypoints-add');

  var placeholderRow = document.createElement('div');
  placeholderRow.className = 'wp-row';

  var input = document.createElement('input');
  input.className = 'wp-input';
  input.type = 'text';
  input.placeholder = 'Enter address...';
  input.style.flex = '1';
  placeholderRow.appendChild(input);

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

function commitInsertWaypoint(insertIndex, query, placeholderRow, input) {
  input.disabled = true;
  showWpError('');
  fetch('/api/geocode?q=' + encodeURIComponent(query))
    .then(function(r) {
      if (r.status === 404) throw new Error('Address not found');
      if (!r.ok) throw new Error('Geocoding error');
      return r.json();
    })
    .then(function(data) {
      var list = document.getElementById('waypoints-list');
      list.removeChild(placeholderRow);
      waypoints.splice(insertIndex, 0, [data.lat, data.lon]);
      labelCache[labelKey([data.lat, data.lon])] = data.display_name;
      legPolylines.forEach(function(p) { map.removeLayer(p); });
      legPolylines = [];
      legs = [];
      refreshMarkers();
      renderWaypointList();
      rerouteAll();
      saveState();
    })
    .catch(function(err) {
      input.disabled = false;
      input.classList.add('shake');
      setTimeout(function() { input.classList.remove('shake'); }, 300);
      input.focus();
      showWpError(err.message || 'Address not found');
    });
}
```

- [ ] **Step 7: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 8: Manual smoke test — insert**

```bash
go run . build --address "Asheville, NC"
```

Open `http://127.0.0.1:8080`. Place 3 waypoints (A → B → C). Open the waypoints panel. Drag the ⠿ handle on the `+ Add waypoint` row and drop it between waypoints 1 and 2. Verify:
- The address input appears between rows 1 and 2
- Type a valid address and press Enter; the new waypoint appears at position 2 (pushing B to 3, C to 4), and the route rebuilds
- Repeat but press Escape after drop; verify the list is unchanged

- [ ] **Step 9: Manual smoke test — insert geocode failure**

Drag `+ Add waypoint` to position 1. Type a nonsense address (e.g. `xyzzy zyzzy`). Press Enter. Verify:
- Input shakes, shows "Address not found"
- Pressing Escape after failure removes the placeholder row and shows a clean list

- [ ] **Step 10: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): add drag-to-insert waypoints at position"
```

# Waypoint Delete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `×` button to each waypoint row in the build UI so users can delete any waypoint from the list.

**Architecture:** All changes are inside the large `buildHTML` string constant in `route_build.go`. Two touch points: the CSS block (add `.wp-delete` rule) and the `renderWaypointList()` JS function (append the `×` button and define `removeWaypoint`). There are no Go-level unit tests for JS; verification is manual browser testing.

**Tech Stack:** Go (embedded HTML string), vanilla JS, Leaflet

## Global Constraints

- All JS and CSS lives inside the `buildHTML` or `buildHTMLDebugSnippet` string constants in `route_build.go` — no external files.
- Literal `%` characters inside those string constants must be written as `%%` because the string is passed through `fmt.Sprintf`.
- Follow existing `var` / `function` JS style — no `const`/`let`/arrow functions.
- Do not introduce new Go packages or files.

---

### Task 1: Add CSS and `removeWaypoint`, wire up `×` button in `renderWaypointList`

**Files:**
- Modify: `route_build.go` — CSS block (~line 223), `renderWaypointList` function (~lines 435–483)

**Interfaces:**
- Produces: `removeWaypoint(i)` — callable from `×` button `onclick`; takes a zero-based waypoint index

- [ ] **Step 1: Add `.wp-delete` CSS rule after `.wp-drop-indicator`**

In `route_build.go`, find line 223:
```
  .wp-drop-indicator { height: 2px; background: #2563eb; margin: 1px 0; border-radius: 1px; }
```
Add immediately after it:
```
  .wp-delete { color: #9ca3af; font-size: 11px; flex-shrink: 0; cursor: pointer; padding: 0 2px; }
  .wp-delete:hover { color: #dc2626; }
```

- [ ] **Step 2: Add `removeWaypoint` function after `renderWaypointList`**

In `route_build.go`, find the closing `}` of `renderWaypointList` (~line 483) and add the new function immediately after:

```javascript
function removeWaypoint(i) {
  waypoints.splice(i, 1);
  activeSlotIndex = -1;
  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  legs = [];
  refreshMarkers();
  renderWaypointList();
  if (waypoints.length === 0) {
    localStorage.removeItem('twisty-build-state');
    updateStats();
    requestScore();
  } else {
    rerouteAll();
    saveState();
  }
}
```

- [ ] **Step 3: Wire up the `×` button in `renderWaypointList`**

Find the block in `renderWaypointList` that appends child elements to the row (~lines 478–481):
```javascript
    row.appendChild(handle);
    row.appendChild(badge);
    row.appendChild(labelEl);
    list.insertBefore(row, addRow);
```
Replace it with:
```javascript
    var deleteBtn = document.createElement('span');
    deleteBtn.className = 'wp-delete';
    deleteBtn.textContent = '×';
    deleteBtn.title = 'Remove waypoint';
    (function(idx) {
      deleteBtn.onclick = function(e) {
        e.stopPropagation();
        removeWaypoint(idx);
      };
    })(i);

    row.appendChild(handle);
    row.appendChild(badge);
    row.appendChild(labelEl);
    row.appendChild(deleteBtn);
    list.insertBefore(row, addRow);
```

- [ ] **Step 4: Build the project to check for compile errors**

```bash
go build ./...
```
Expected: no output (success). Fix any `%%` escaping issues if the compiler reports a format string error.

- [ ] **Step 5: Manual browser verification**

Run the server:
```bash
go run . build --address "Asheville, NC"
```
Open `http://127.0.0.1:8080` and verify:

1. Click the map to add 3 waypoints. Open the waypoints panel. Each row shows a gray `×` at the right edge.
2. Hover the `×` — it turns red.
3. Click `×` on the middle waypoint. The waypoint disappears from the list, markers refresh, and routing re-runs connecting the remaining two waypoints.
4. Click `×` on the first waypoint of a two-waypoint route. One waypoint remains, no legs, export buttons gray out.
5. Click `×` on the only remaining waypoint. The list is empty, stats show `—`, export buttons disabled.
6. Set a waypoint as active (badge click — row gets blue left border), then click `×` on a different waypoint. Confirm the active-slot highlight is gone (activeSlotIndex reset).

- [ ] **Step 6: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add delete button to waypoint rows"
```

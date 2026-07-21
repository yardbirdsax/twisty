# Overlay Visibility Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Hide overlay / Show overlay" button to the stats panel that removes or restores the road/way overlay without recalculating or refetching data.

**Architecture:** A single `overlayVisible` boolean tracks visibility. `toggleOverlayVisibility()` calls `map.removeLayer` / `map.addLayer`. `rebuildSegmentLayer()` is patched to respect `overlayVisible` so a mode switch while hidden doesn't make the overlay reappear.

**Tech Stack:** Go (HTML/JS embedded as a template string in `route_build.go`), Leaflet.js

## Global Constraints

- All HTML and JS lives inside the Go source file `route_build.go` as a backtick template string — edit that file only, no separate HTML/JS files.
- `%%` in the template is a literal `%` (Go `fmt.Sprintf` escaping) — don't change existing `%%` occurrences.
- Button styling must match existing panel buttons: `background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;width:100%%`
- No backend changes, no new Go tests — this is purely client-side JS/HTML.
- Manual verification only (no JS unit test framework exists in this project).

---

### Task 1: Add `overlayVisible` state variable and `toggleOverlayVisibility` function

**Files:**
- Modify: `route_build.go` (JS variable block ~line 821, and JS function block near `toggleOverlayMode` ~line 1028)

**Interfaces:**
- Produces: `var overlayVisible` (boolean, default `true`), `function toggleOverlayVisibility()` used by the button in Task 2, and the patched `rebuildSegmentLayer` used by `toggleOverlayMode`.

- [ ] **Step 1: Add `overlayVisible` variable**

In `route_build.go`, find the block of `var` declarations around line 821–832:

```js
var OVERLAY_WAYS = 'ways';
var OVERLAY_ROADS = 'roads';
var overlayMode = OVERLAY_WAYS;
var minScoreFilter = 0;
```

Add `var overlayVisible = true;` on the line immediately after `var minScoreFilter = 0;`:

```js
var OVERLAY_WAYS = 'ways';
var OVERLAY_ROADS = 'roads';
var overlayMode = OVERLAY_WAYS;
var minScoreFilter = 0;
var overlayVisible = true;
```

- [ ] **Step 2: Patch `rebuildSegmentLayer` to respect `overlayVisible`**

Find `rebuildSegmentLayer` (~line 874). The current line is:

```js
  roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);
```

Replace it with:

```js
  roadLayer = L.geoJSON(null, { style: segmentStyle });
  if (overlayVisible) roadLayer.addTo(map);
```

- [ ] **Step 3: Add `toggleOverlayVisibility` function**

Add the following function immediately after the closing brace of `toggleOverlayMode` (~line 1044):

```js
function toggleOverlayVisibility() {
  var btn = document.getElementById('btn-overlay-visibility');
  if (overlayVisible) {
    map.removeLayer(roadLayer);
    overlayVisible = false;
    btn.textContent = 'Show overlay';
  } else {
    roadLayer.addTo(map);
    overlayVisible = true;
    btn.textContent = 'Hide overlay';
  }
}
```

- [ ] **Step 4: Build and verify no compile errors**

```bash
make build
```

Expected: `go build` succeeds, `bin/twisty` produced, no errors.

- [ ] **Step 5: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add overlayVisible state and toggleOverlayVisibility function"
```

---

### Task 2: Add the "Hide overlay" button to the stats panel HTML

**Files:**
- Modify: `route_build.go` (HTML template ~line 275)

**Interfaces:**
- Consumes: `toggleOverlayVisibility()` from Task 1
- Produces: `<button id="btn-overlay-visibility">` in the DOM, positioned above the existing overlay mode toggle button

- [ ] **Step 1: Insert the button row**

Find the existing overlay mode toggle row in the HTML template (~line 275):

```html
  <div class="row" style="margin-top:8px;">
    <button id="btn-overlay-toggle" onclick="toggleOverlayMode()" style="width:100%%;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Switch to Road view</button>
  </div>
```

Insert a new row **above** it:

```html
  <div class="row" style="margin-top:8px;">
    <button id="btn-overlay-visibility" onclick="toggleOverlayVisibility()" style="width:100%%;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Hide overlay</button>
  </div>
  <div class="row" style="margin-top:8px;">
    <button id="btn-overlay-toggle" onclick="toggleOverlayMode()" style="width:100%%;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Switch to Road view</button>
  </div>
```

- [ ] **Step 2: Build and verify no compile errors**

```bash
make build
```

Expected: succeeds with no errors.

- [ ] **Step 3: Manual test — basic toggle**

Run `twisty build` and open the browser. With the map zoomed in enough to show the overlay (zoom ≥ 12):

1. Confirm "Hide overlay" button appears above "Switch to Road view".
2. Click "Hide overlay" — colored road/way lines disappear from the map, button reads "Show overlay".
3. Click "Show overlay" — lines reappear instantly with no network requests (check browser DevTools Network tab), button reads "Hide overlay".

- [ ] **Step 4: Manual test — mode switch while hidden**

1. Hide the overlay (button reads "Show overlay").
2. Click "Switch to Road view" — overlay must NOT reappear. Button still reads "Show overlay".
3. Click "Show overlay" — overlay appears in Road view mode.

- [ ] **Step 5: Manual test — min twist slider while hidden**

1. Hide the overlay.
2. Move the min twist slider to a non-zero value.
3. Click "Show overlay" — only roads above the min twist threshold are visible (filter is applied on re-show).

- [ ] **Step 6: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add hide/show overlay toggle button to stats panel"
```

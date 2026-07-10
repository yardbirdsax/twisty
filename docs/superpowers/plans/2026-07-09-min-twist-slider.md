# Min Twist Slider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Min twist" slider to the stats panel that instantly hides road overlay features below a total curvature score threshold, with no effect in Ways mode.

**Architecture:** All changes are client-side JavaScript inside the `buildHTML` string constant in `route_build.go`. A new `minScoreFilter` variable controls the threshold; `segmentStyle()` checks it; `toggleOverlayMode()` enables/disables the slider element. No backend or Go changes.

**Tech Stack:** Vanilla JavaScript, HTML range input, Leaflet `setStyle`

## Global Constraints

- All JS lives inside the `buildHTML` Go string constant in `route_build.go`
- `%%` must be used instead of `%` inside `buildHTML` (Go `fmt.Sprintf` escaping)
- No backend changes; no new API endpoints
- `DefaultMaxCurvature = 8000.0` — slider max must match this value
- Slider step: 100; default value: 0

---

### Task 1: Add slider HTML and CSS to the stats panel

**Files:**
- Modify: `route_build.go` (HTML section of `buildHTML`, ~line 272; CSS section ~line 195)

**Interfaces:**
- Produces: `#min-twist-slider` (range input, id used by JS in Task 2), `#min-twist-label` (span showing current value)

- [ ] **Step 1: Add CSS for the slider row**

Inside the `<style>` block in `buildHTML` (after the `.wp-delete:hover` rule, around line 233), add:

```css
  #min-twist-row { margin-top: 4px; }
  #min-twist-row label { font-size: 11px; color: #555; display: flex; justify-content: space-between; margin-bottom: 2px; }
  #min-twist-slider { width: 100%%; cursor: pointer; }
  #min-twist-slider:disabled { opacity: 0.4; cursor: not-allowed; }
```

- [ ] **Step 2: Add slider HTML between the overlay toggle row and the Save/Load row**

In `buildHTML`, the overlay toggle div ends at line 272. Insert between it and the Save/Load row (line 273):

```html
  <div id="min-twist-row" class="row" style="margin-top:4px;display:block;">
    <label for="min-twist-slider"><span>Min twist</span><span id="min-twist-label">0</span></label>
    <input id="min-twist-slider" type="range" min="0" max="8000" step="100" value="0" disabled oninput="onMinTwistInput(this.value)">
  </div>
```

Note: the slider starts `disabled` because `overlayMode` initialises to `OVERLAY_WAYS`.

- [ ] **Step 3: Start the dev server and verify the slider renders**

```bash
go run . build --address "Asheville, NC"
```

Open `http://127.0.0.1:8080`. The stats panel should show a greyed-out "Min twist: 0" slider between the overlay toggle and Save/Load buttons. Switch to Road view — slider should remain greyed out for now (JS wiring comes in Task 2).

- [ ] **Step 4: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add min twist slider HTML and CSS"
```

---

### Task 2: Wire slider JS — filtering and mode switching

**Files:**
- Modify: `route_build.go` (JS section of `buildHTML`)

**Interfaces:**
- Consumes: `#min-twist-slider` and `#min-twist-label` from Task 1; `segmentStyle(feature)` at line 825; `toggleOverlayMode()` at line 1000; `roadLayer` (Leaflet GeoJSON layer); `overlayMode`, `OVERLAY_ROADS`, `OVERLAY_WAYS` globals
- Produces: `minScoreFilter` (number, global); `onMinTwistInput(value)` (called by slider oninput)

- [ ] **Step 1: Add `minScoreFilter` variable near the other overlay globals**

In `buildHTML`, around line 814 (after `var overlayMode = OVERLAY_WAYS;`), add:

```js
var minScoreFilter = 0;
```

- [ ] **Step 2: Update `segmentStyle` to apply the filter in Roads mode**

The current `segmentStyle` function (lines 825–832) is:

```js
function segmentStyle(feature) {
  if (overlayMode === OVERLAY_ROADS) {
    var color = feature && feature.properties ? feature.properties.color : '#475569';
    return { color: color || '#475569', weight: 3, opacity: 0.8 };
  }
  var tier = feature ? feature.properties.tier : 0;
  return { color: TIER_COLORS[Math.max(0, Math.min(tier, 4))], weight: 3, opacity: 0.8 };
}
```

Replace it with:

```js
function segmentStyle(feature) {
  if (overlayMode === OVERLAY_ROADS) {
    var score = feature && feature.properties ? (feature.properties.score || 0) : 0;
    if (score < minScoreFilter) {
      return { opacity: 0, weight: 0 };
    }
    var color = feature && feature.properties ? feature.properties.color : '#475569';
    return { color: color || '#475569', weight: 3, opacity: 0.8 };
  }
  var tier = feature ? feature.properties.tier : 0;
  return { color: TIER_COLORS[Math.max(0, Math.min(tier, 4))], weight: 3, opacity: 0.8 };
}
```

- [ ] **Step 3: Add `onMinTwistInput` handler**

After the closing brace of `segmentStyle` (line 832), add:

```js
function onMinTwistInput(value) {
  minScoreFilter = parseFloat(value) || 0;
  document.getElementById('min-twist-label').textContent = Math.round(minScoreFilter);
  roadLayer.setStyle(segmentStyle);
}
```

- [ ] **Step 4: Update `toggleOverlayMode` to enable/disable the slider**

The current `toggleOverlayMode` function (lines 1000–1013) is:

```js
function toggleOverlayMode() {
  overlayMode = overlayMode === OVERLAY_WAYS ? OVERLAY_ROADS : OVERLAY_WAYS;
  var btn = document.getElementById('btn-overlay-toggle');
  btn.textContent = overlayMode === OVERLAY_WAYS ? 'Switch to Road view' : 'Switch to Way view';
  // rebuildSegmentLayer increments segmentGeneration, which invalidates all
  // in-flight fetch callbacks and the old SSE handler's captured gen.
  rebuildSegmentLayer();
  if (overlayMode === OVERLAY_ROADS) {
    if (evtSource) { evtSource.close(); evtSource = null; }
  } else {
    connectSSE();
  }
  loadVisibleSegments();
}
```

Replace it with:

```js
function toggleOverlayMode() {
  overlayMode = overlayMode === OVERLAY_WAYS ? OVERLAY_ROADS : OVERLAY_WAYS;
  var btn = document.getElementById('btn-overlay-toggle');
  btn.textContent = overlayMode === OVERLAY_WAYS ? 'Switch to Road view' : 'Switch to Way view';
  var slider = document.getElementById('min-twist-slider');
  slider.disabled = overlayMode === OVERLAY_WAYS;
  // rebuildSegmentLayer increments segmentGeneration, which invalidates all
  // in-flight fetch callbacks and the old SSE handler's captured gen.
  rebuildSegmentLayer();
  if (overlayMode === OVERLAY_ROADS) {
    if (evtSource) { evtSource.close(); evtSource = null; }
    roadLayer.setStyle(segmentStyle);
  } else {
    connectSSE();
  }
  loadVisibleSegments();
}
```

The `roadLayer.setStyle(segmentStyle)` call when entering Roads mode re-applies the current `minScoreFilter` to whatever features are already in the layer.

- [ ] **Step 5: Verify manually**

```bash
go run . build --address "Asheville, NC"
```

Test the following:

1. Start in Ways mode — slider is greyed out, all way segments visible
2. Switch to Road view — slider becomes active
3. Move slider to 2000 — roads with score < 2000 disappear instantly, no network request (check DevTools Network tab)
4. Lower slider back to 0 — all roads reappear instantly
5. Switch back to Way view — slider greys out, all way segments still visible (filter has no effect)
6. Switch to Road view again — slider re-enables at previous value, filter re-applies immediately
7. Pan/zoom map in Road view — new roads load and filter applies automatically (below-threshold roads hidden)

- [ ] **Step 6: Commit**

```bash
git add route_build.go
git commit -m "feat(build): wire min twist slider to filter road overlay"
```

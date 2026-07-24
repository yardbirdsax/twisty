# Min Speed Slider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a 0–100 mph "Min speed" slider to the build UI road overlay that hides roads whose length-weighted average speed limit falls below the threshold.

**Architecture:** A new `WeightedAverageSpeedMPH` helper in `quality/maxspeed.go` computes a single representative speed per road collection from existing `WaySpeeds` data. That value is added as `max_speed_mph` to the `geoJSONRoadProps` struct and emitted in `/api/road-segments` responses. The frontend reads the property in `segmentStyle` and applies AND filtering alongside the existing `minScoreFilter`.

**Tech Stack:** Go (backend helper + struct field), embedded HTML/JS in `route_build.go` (frontend slider + filter logic).

## Global Constraints

- No new API endpoints; no query parameter changes to `/api/road-segments`
- `max_speed_mph` serializes as JSON `null` (not `0`) when a collection has no tagged speed ways — use `*float64`
- Slider range: 0–100, step 1, linear scale (unlike min-twist which is logarithmic)
- Filtering semantics: at slider value 0, all roads shown; above 0, roads with `null` speed are hidden
- Min-speed filter ANDs with min-twist filter — both must pass for a road to appear
- Slider is disabled when `overlayMode === OVERLAY_WAYS`, same as min-twist slider
- Run tests with: `go test ./...` from the repo root
- Test file conventions: same package as the file under test (e.g. `package quality` for `quality/maxspeed_test.go`)

---

### Task 1: Add `WeightedAverageSpeedMPH` to `quality/maxspeed.go`

**Files:**
- Modify: `quality/maxspeed.go` (add function after `SpeedPassingFraction`)
- Modify: `quality/maxspeed_test.go` (add test cases)

**Interfaces:**
- Produces: `func WeightedAverageSpeedMPH(speeds []WaySpeedInfo) (mph float64, ok bool)`
  - Returns `(0, false)` if no entries have `HasSpeed == true`
  - Returns `(weightedSum/totalTaggedLength, true)` otherwise
  - Input values are already in mph (converted by `ParseMaxspeed` before being stored in `WaySpeedInfo`)

- [ ] **Step 1: Write failing tests**

Add to `quality/maxspeed_test.go` after the existing `TestSpeedPassingFraction` function:

```go
func TestWeightedAverageSpeedMPH(t *testing.T) {
	tests := []struct {
		name    string
		speeds  []WaySpeedInfo
		wantMPH float64
		wantOK  bool
	}{
		{
			name:    "empty slice",
			speeds:  nil,
			wantMPH: 0,
			wantOK:  false,
		},
		{
			name: "all untagged",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 0, HasSpeed: false, LengthM: 1000},
				{SpeedMPH: 0, HasSpeed: false, LengthM: 2000},
			},
			wantMPH: 0,
			wantOK:  false,
		},
		{
			name: "single tagged entry",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 1000},
			},
			wantMPH: 55,
			wantOK:  true,
		},
		{
			name: "two equal-length tagged entries",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 40, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 60, HasSpeed: true, LengthM: 1000},
			},
			wantMPH: 50,
			wantOK:  true,
		},
		{
			name: "length-weighted: longer segment dominates",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 25, HasSpeed: true, LengthM: 500},
				{SpeedMPH: 55, HasSpeed: true, LengthM: 4500},
			},
			// (25*500 + 55*4500) / 5000 = (12500 + 247500) / 5000 = 260000/5000 = 52
			wantMPH: 52,
			wantOK:  true,
		},
		{
			name: "untagged entries are ignored",
			speeds: []WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 0, HasSpeed: false, LengthM: 9000},
			},
			wantMPH: 55,
			wantOK:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := WeightedAverageSpeedMPH(tt.speeds)
			if ok != tt.wantOK {
				t.Fatalf("WeightedAverageSpeedMPH() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && math.Abs(got-tt.wantMPH) > 0.01 {
				t.Fatalf("WeightedAverageSpeedMPH() = %.4f, want %.4f", got, tt.wantMPH)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./quality/... -run TestWeightedAverageSpeedMPH -v
```

Expected: FAIL with `undefined: WeightedAverageSpeedMPH`

- [ ] **Step 3: Implement `WeightedAverageSpeedMPH`**

Add after `SpeedPassingFraction` in `quality/maxspeed.go`:

```go
func WeightedAverageSpeedMPH(speeds []WaySpeedInfo) (mph float64, ok bool) {
	var weightedSum, totalLength float64
	for _, s := range speeds {
		if s.HasSpeed {
			weightedSum += s.SpeedMPH * s.LengthM
			totalLength += s.LengthM
		}
	}
	if totalLength <= 0 {
		return 0, false
	}
	return weightedSum / totalLength, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./quality/... -run TestWeightedAverageSpeedMPH -v
```

Expected: all cases PASS

- [ ] **Step 5: Run full test suite to check for regressions**

```
go test ./...
```

Expected: all tests pass

- [ ] **Step 6: Commit**

```
git add quality/maxspeed.go quality/maxspeed_test.go
git commit -m "feat(quality): add WeightedAverageSpeedMPH helper"
```

---

### Task 2: Add `max_speed_mph` to road GeoJSON and wire up `collectionsToRoadGeoJSON`

**Files:**
- Modify: `geojson.go` (add field to `geoJSONRoadProps`, update `collectionsToRoadGeoJSON`)
- Modify: `geojson_test.go` (extend existing test to assert `max_speed_mph`)

**Interfaces:**
- Consumes: `quality.WeightedAverageSpeedMPH(speeds []quality.WaySpeedInfo) (float64, bool)` from Task 1
- Consumes: `quality.RoadCollection.WaySpeeds []quality.WaySpeedInfo` (already populated by `BuildRoadCollection`)
- Produces: `geoJSONRoadProps.MaxSpeedMPH *float64` serialized as `"max_speed_mph"` in JSON — `null` when no tagged speed, numeric otherwise

- [ ] **Step 1: Write failing test**

Add to `geojson_test.go` after `TestCollectionsToRoadGeoJSON_colorAndCoordinates`:

```go
func TestCollectionsToRoadGeoJSON_maxSpeedMPH(t *testing.T) {
	t.Run("collection with tagged speed emits max_speed_mph", func(t *testing.T) {
		col := quality.RoadCollection{
			Name:       "PA 100",
			TotalScore: 300,
			WaySpeeds: []quality.WaySpeedInfo{
				{SpeedMPH: 55, HasSpeed: true, LengthM: 1000},
				{SpeedMPH: 45, HasSpeed: true, LengthM: 1000},
			},
			Segments: []quality.ScoredSegment{
				{
					Start: geo.Coord{Lat: 40.1, Lon: -75.1},
					End:   geo.Coord{Lat: 40.2, Lon: -75.2},
				},
			},
		}
		fc := collectionsToRoadGeoJSON([]quality.RoadCollection{col})
		if len(fc.Features) != 1 {
			t.Fatalf("expected 1 feature, got %d", len(fc.Features))
		}
		f := fc.Features[0]
		if f.Properties.MaxSpeedMPH == nil {
			t.Fatal("expected max_speed_mph to be non-nil")
		}
		// (55*1000 + 45*1000) / 2000 = 50
		if math.Abs(*f.Properties.MaxSpeedMPH-50.0) > 0.01 {
			t.Errorf("max_speed_mph = %.4f, want 50.0", *f.Properties.MaxSpeedMPH)
		}
	})

	t.Run("collection with no tagged speed emits null max_speed_mph", func(t *testing.T) {
		col := quality.RoadCollection{
			Name:       "PA 202",
			TotalScore: 100,
			WaySpeeds: []quality.WaySpeedInfo{
				{SpeedMPH: 0, HasSpeed: false, LengthM: 500},
			},
			Segments: []quality.ScoredSegment{
				{
					Start: geo.Coord{Lat: 40.1, Lon: -75.1},
					End:   geo.Coord{Lat: 40.2, Lon: -75.2},
				},
			},
		}
		fc := collectionsToRoadGeoJSON([]quality.RoadCollection{col})
		if len(fc.Features) != 1 {
			t.Fatalf("expected 1 feature, got %d", len(fc.Features))
		}
		f := fc.Features[0]
		if f.Properties.MaxSpeedMPH != nil {
			t.Errorf("expected max_speed_mph to be nil, got %v", *f.Properties.MaxSpeedMPH)
		}
	})
}
```

Also add `"math"` to the imports in `geojson_test.go` if not already present.

- [ ] **Step 2: Run test to verify it fails**

```
go test . -run TestCollectionsToRoadGeoJSON_maxSpeedMPH -v
```

Expected: FAIL — either `f.Properties.MaxSpeedMPH` field does not exist or is always nil

- [ ] **Step 3: Add `MaxSpeedMPH` to `geoJSONRoadProps`**

In `geojson.go`, update the `geoJSONRoadProps` struct (currently lines 89–93):

```go
type geoJSONRoadProps struct {
	RoadName    string   `json:"road_name"`
	Score       float64  `json:"score"`
	Color       string   `json:"color"`
	MaxSpeedMPH *float64 `json:"max_speed_mph"`
}
```

- [ ] **Step 4: Update `collectionsToRoadGeoJSON` to populate `MaxSpeedMPH`**

In `geojson.go`, update the loop body inside `collectionsToRoadGeoJSON`. Replace the existing feature append (currently around lines 148–159) with:

```go
var maxSpeedMPH *float64
if mph, ok := quality.WeightedAverageSpeedMPH(c.WaySpeeds); ok {
    maxSpeedMPH = &mph
}

fc.Features = append(fc.Features, geoJSONRoadFeature{
    Type: "Feature",
    Geometry: geoJSONMultiLineGeometry{
        Type:  "MultiLineString",
        Lines: lines,
    },
    Properties: geoJSONRoadProps{
        RoadName:    c.DisplayName(),
        Score:       c.TotalScore,
        Color:       color,
        MaxSpeedMPH: maxSpeedMPH,
    },
})
```

- [ ] **Step 5: Run tests to verify they pass**

```
go test . -run TestCollectionsToRoadGeoJSON -v
```

Expected: all cases PASS (including the existing `TestCollectionsToRoadGeoJSON_colorAndCoordinates`)

- [ ] **Step 6: Run full test suite**

```
go test ./...
```

Expected: all tests pass

- [ ] **Step 7: Commit**

```
git add geojson.go geojson_test.go
git commit -m "feat(build): add max_speed_mph to road GeoJSON properties"
```

---

### Task 3: Add min speed slider HTML, JS, and `segmentStyle` filter

**Files:**
- Modify: `route_build.go` (HTML slider row, JS global, input handler, `segmentStyle`, `toggleOverlayMode`)

**Interfaces:**
- Consumes: `feature.properties.max_speed_mph` — `null` or a numeric mph value from the `/api/road-segments` response (produced by Task 2)
- Consumes: existing JS globals `minScoreFilter`, `overlayMode`, `OVERLAY_WAYS`, `OVERLAY_ROADS`, `roadLayer`
- Consumes: existing functions `roadLayer.setStyle(segmentStyle)`, `toggleOverlayMode`

- [ ] **Step 1: Add the slider HTML row**

In `route_build.go`, find the `#min-twist-row` div (around line 281):

```html
  <div id="min-twist-row" class="row" style="margin-top:4px;display:block;">
    <label for="min-twist-slider"><span>Min twist</span><span id="min-twist-label">0</span></label>
    <input id="min-twist-slider" type="range" min="0" max="100" step="1" value="0" disabled oninput="onMinTwistInput(this.value)">
  </div>
```

Insert the following directly after the closing `</div>` of `#min-twist-row`:

```html
  <div id="min-speed-row" class="row" style="margin-top:4px;display:block;">
    <label for="min-speed-slider"><span>Min speed</span><span id="min-speed-label">0 mph</span></label>
    <input id="min-speed-slider" type="range" min="0" max="100" step="1" value="0" disabled oninput="onMinSpeedInput(this.value)">
  </div>
```

- [ ] **Step 2: Add the `minSpeedFilter` global and `onMinSpeedInput` handler**

In `route_build.go`, find the `minScoreFilter` global (around line 827):

```js
var minScoreFilter = 0;
```

Add `minSpeedFilter` directly below it:

```js
var minScoreFilter = 0;
var minSpeedFilter = 0;
```

Then find the existing `onMinTwistInput` function (around line 859):

```js
function onMinTwistInput(value) {
  var p = parseFloat(value) / 100;
  minScoreFilter = p <= 0 ? 0 : SLIDER_A * (Math.pow(SLIDER_B, p) - 1);
  document.getElementById('min-twist-label').textContent = Math.round(minScoreFilter);
  roadLayer.setStyle(segmentStyle);
}
```

Add `onMinSpeedInput` directly after:

```js
function onMinSpeedInput(value) {
  minSpeedFilter = parseFloat(value);
  document.getElementById('min-speed-label').textContent = Math.round(minSpeedFilter) + ' mph';
  roadLayer.setStyle(segmentStyle);
}
```

- [ ] **Step 3: Update `segmentStyle` to apply the speed filter**

In `route_build.go`, find the `OVERLAY_ROADS` branch inside `segmentStyle` (around line 847). The current code is:

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
```

Update the `OVERLAY_ROADS` branch to add the speed check after the score check:

```js
function segmentStyle(feature) {
  if (overlayMode === OVERLAY_ROADS) {
    var score = feature && feature.properties ? (feature.properties.score || 0) : 0;
    if (score < minScoreFilter) {
      return { opacity: 0, weight: 0 };
    }
    if (minSpeedFilter > 0) {
      var spd = feature && feature.properties ? feature.properties.max_speed_mph : null;
      if (spd === null || spd === undefined || spd < minSpeedFilter) {
        return { opacity: 0, weight: 0 };
      }
    }
    var color = feature && feature.properties ? feature.properties.color : '#475569';
    return { color: color || '#475569', weight: 3, opacity: 0.8 };
  }
```

- [ ] **Step 4: Update `toggleOverlayMode` to enable/disable the speed slider**

In `route_build.go`, find `toggleOverlayMode` (around line 1033). The current min-twist disable line is:

```js
  var slider = document.getElementById('min-twist-slider');
  slider.disabled = overlayMode === OVERLAY_WAYS;
```

Add the speed slider disable directly after:

```js
  var slider = document.getElementById('min-twist-slider');
  slider.disabled = overlayMode === OVERLAY_WAYS;
  document.getElementById('min-speed-slider').disabled = overlayMode === OVERLAY_WAYS;
```

- [ ] **Step 5: Build and manually verify**

```
go build -o twisty . && ./twisty build --address "Royersford, PA" --port 3000
```

Open `http://localhost:3000` in a browser. Switch to Road overlay mode ("Switch to Road view" button). Verify:
1. The "Min speed" slider appears below the "Min twist" slider and is now enabled
2. Drag the Min speed slider to 45 — roads with no speed tag or average speed < 45 mph disappear
3. Drag it back to 0 — all roads reappear
4. Set both Min twist and Min speed above 0 — only roads meeting both thresholds show
5. Switch back to Way view — both sliders become disabled again

- [ ] **Step 6: Commit**

```
git add route_build.go
git commit -m "feat(build): add min speed slider to road overlay"
```

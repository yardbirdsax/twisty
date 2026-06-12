# Twistiness Score Per Km Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the raw "Twist Score" in the build UI with a score-per-km metric, color-coded using the same yellow→red→magenta gradient as the map overlay.

**Architecture:** `scoreFromCachedTiles` is changed to return both `totalScore` and `scorePerKm` (by switching to a struct return). `scoreResponse` gains a `ScorePerKm` field. A new constant `DefaultMaxCurvaturePerKm = 2000.0` is added to `scoring_params.go`. The JS in `route_build.go` is updated to display `score_per_km`, compute a color using a ported version of `CurvatureColorLevel`/`GradientColorCSS`, and apply it to the stat value element. The label is renamed "Twistiness".

**Tech Stack:** Go, `net/http/httptest`, vanilla JavaScript embedded as a string in `route_build.go`.

---

### Task 1: Add `DefaultMaxCurvaturePerKm` constant

**Files:**
- Modify: `quality/scoring_params.go` (after line 95)

- [ ] **Step 1: Add the constant**

Open `quality/scoring_params.go`. After the `DefaultMaxCurvature` constant (line 95), add:

```go
// DefaultMaxCurvaturePerKm is the score-per-km value that maps to the maximum
// color intensity (magenta) in the build UI twistiness display.
// A typical maximally-twisty road collection (~TotalScore=8000 over ~4km) yields ~2000/km.
const DefaultMaxCurvaturePerKm = 2000.0
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add quality/scoring_params.go
git commit -m "feat(quality): add DefaultMaxCurvaturePerKm constant"
```

---

### Task 2: Change `scoreFromCachedTiles` to return score and score-per-km

**Files:**
- Modify: `route_build.go` (function `scoreFromCachedTiles` at line ~1049, and its two call sites at lines ~937 and ~987)

The function currently returns `float64`. We'll switch it to return a named struct.

- [ ] **Step 1: Add the return struct type**

Find the `scoreResponse` struct in `route_build.go` (around line 893). Just before it, add:

```go
type scoreResult struct {
	Score      float64
	ScorePerKm float64
}
```

- [ ] **Step 2: Write the failing test**

In `route_build_test.go`, add a new test after `TestHandleScore_emptyPoints` (around line 229):

```go
func TestScoreFromCachedTiles_returnsScorePerKm(t *testing.T) {
	cacheDir := t.TempDir()
	tile := quality.Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	// Minimal Overpass JSON: a hairpin curve (circumradius ~70m) that scores in tier 3 (r < 100m).
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := cache.Write(tile, tileData); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	srv := &buildServer{tileSize: 0.1, cacheDir: cacheDir}
	result := srv.scoreFromCachedTiles([]quality.Tile{tile}, cache, nil)

	if result.Score == 0 {
		t.Error("expected Score > 0")
	}
	if result.ScorePerKm == 0 {
		t.Error("expected ScorePerKm > 0")
	}
}
```

- [ ] **Step 3: Run to verify it fails**

```bash
go test -run TestScoreFromCachedTiles_returnsScorePerKm -v ./...
```

Expected: compile error — `scoreFromCachedTiles` still returns `float64`, not `scoreResult`.

- [ ] **Step 4: Update `scoreFromCachedTiles` to return `scoreResult`**

Replace the entire `scoreFromCachedTiles` function (lines ~1049–1079) with:

```go
func (s *buildServer) scoreFromCachedTiles(tiles []quality.Tile, cache *quality.TileCache, routePoints [][2]float64) scoreResult {
	var totalScore float64
	var totalLength float64

	for _, t := range tiles {
		data, err := cache.Read(t)
		if err != nil {
			continue
		}

		ways, err := quality.ParseTileData(data)
		if err != nil {
			continue
		}

		result := quality.RunScorePipeline(ways)

		for _, sw := range result.ScoredWays {
			for _, seg := range sw.Segments {
				mid := geo.Coord{
					Lat: (seg.Start.Lat + seg.End.Lat) / 2,
					Lon: (seg.Start.Lon + seg.End.Lon) / 2,
				}
				if len(routePoints) == 0 || isNearPolyline(mid, routePoints, 50) {
					totalScore += seg.Score
					totalLength += seg.Length
				}
			}
		}
	}

	var scorePerKm float64
	if totalLength > 0 {
		scorePerKm = totalScore / (totalLength / 1000.0)
	}
	return scoreResult{Score: totalScore, ScorePerKm: scorePerKm}
}
```

- [ ] **Step 5: Fix the two call sites**

Both callers currently do `score := s.scoreFromCachedTiles(...)`. Update them to use the struct:

**Call site 1** (around line 937, in `handleViewportScore`):

```go
sr := s.scoreFromCachedTiles(cachedTiles, cache, nil)

var failedCount int
for _, t := range tiles {
	if _, failed := s.failedTiles.Load(t); failed {
		failedCount++
	}
}

resp := scoreResponse{
	Score:        sr.Score,
	ScorePerKm:   sr.ScorePerKm,
	PendingTiles: len(missingTiles),
	FailedTiles:  failedCount,
}
```

**Call site 2** (around line 987, in `handleScore`):

```go
sr := s.scoreFromCachedTiles(cachedTiles, cache, req.Points)

// Count failed tiles relevant to this request.
var failedCount int
for _, t := range tiles {
	if _, failed := s.failedTiles.Load(t); failed {
		failedCount++
	}
}

resp := scoreResponse{
	Score:        sr.Score,
	ScorePerKm:   sr.ScorePerKm,
	PendingTiles: len(missingTiles),
	FailedTiles:  failedCount,
}
```

- [ ] **Step 6: Add `ScorePerKm` to `scoreResponse`**

Find the `scoreResponse` struct (around line 893) and add the new field:

```go
type scoreResponse struct {
	Score        float64 `json:"score"`
	ScorePerKm   float64 `json:"score_per_km"`
	PendingTiles int     `json:"pending_tiles"`
	FailedTiles  int     `json:"failed_tiles"`
}
```

- [ ] **Step 7: Run the new test to verify it passes**

```bash
go test -run TestScoreFromCachedTiles_returnsScorePerKm -v ./...
```

Expected: PASS.

- [ ] **Step 8: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 9: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): return score_per_km from scoreFromCachedTiles and scoreResponse"
```

---

### Task 3: Add HTML-level test for the updated stat box label and JS constants

**Files:**
- Modify: `route_build_test.go`

- [ ] **Step 1: Write the failing test**

Add after `TestHandleIndex_rendersRefreshScoreButton` (around line 584):

```go
func TestHandleIndex_renderstwistinessLabel(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`Twistiness`,
		`score_per_km`,
		`scorePerKmMax`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test -run TestHandleIndex_renderstwistinessLabel -v ./...
```

Expected: FAIL — HTML does not yet contain "Twistiness" or "scorePerKmMax".

- [ ] **Step 3: Commit the failing test**

```bash
git add route_build_test.go
git commit -m "test(build): add failing test for Twistiness label and scorePerKmMax in HTML"
```

---

### Task 4: Update the build UI — label, score display, and color coding

**Files:**
- Modify: `route_build.go`

This task updates the embedded HTML/JS string in `route_build.go`.

- [ ] **Step 1: Rename the stat box label**

Find the HTML stat row (around line 214):

```html
<div class="row"><span class="key">Twist Score</span><span class="val score-val" id="score-val">—</span></div>
```

Change it to:

```html
<div class="row"><span class="key">Twistiness</span><span class="val score-val" id="score-val">—</span></div>
```

- [ ] **Step 2: Add the JS color helper functions and constant**

Find the JS section near the top of the embedded script (around line 252, near `var scorePollTimer`). Add these functions and constant immediately after the `var scorePollTimer` declaration:

```javascript
var scorePerKmMax = 2000;

function curvatureColorLevel(scorePerKm) {
  if (scorePerKm <= 0) return 0;
  var pct = scorePerKm / scorePerKmMax;
  if (pct > 1) pct = 1;
  var colorPct = 1 - 1 / Math.pow(10, pct * 0.75);
  return Math.round(510 * colorPct) + 1;
}

function gradientColorCSS(level) {
  if (level <= 0) return '#4ade80'; // green (tier 0 color)
  if (level <= 256) {
    var green = 255 - (level - 1) * 255 / 255;
    return 'rgb(255,' + Math.round(green) + ',0)';
  }
  var blue = (level - 257) * 255 / 254;
  return 'rgb(255,0,' + Math.round(blue) + ')';
}
```

- [ ] **Step 3: Update `requestScore()` to display score_per_km with color**

Find the `.then` handler inside `requestScore()` (around line 500–513). Replace the score display line:

```javascript
document.getElementById('score-val').textContent = Math.round(data.score).toLocaleString();
```

With:

```javascript
var spk = data.score_per_km || 0;
var scoreEl = document.getElementById('score-val');
if (spk > 0) {
  scoreEl.textContent = Math.round(spk).toLocaleString();
  scoreEl.style.color = gradientColorCSS(curvatureColorLevel(spk));
} else {
  scoreEl.textContent = '—';
  scoreEl.style.color = '';
}
```

- [ ] **Step 4: Update `requestViewportScore()` to clear score display**

Find the `.then` handler inside `requestViewportScore()` (around line 541–542). Replace the score display line:

```javascript
document.getElementById('score-val').textContent = Math.round(data.score).toLocaleString();
```

With:

```javascript
document.getElementById('score-val').textContent = '—';
document.getElementById('score-val').style.color = '';
```

Also reset color when score is cleared in `requestScore()` when `allPoints.length < 2` (around line 488):

```javascript
document.getElementById('score-val').textContent = '—';
document.getElementById('score-val').style.color = '';
```

- [ ] **Step 5: Run the HTML test to verify it passes**

```bash
go test -run TestHandleIndex_renderstwistinessLabel -v ./...
```

Expected: PASS.

- [ ] **Step 6: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add route_build.go
git commit -m "feat(build): show score_per_km as Twistiness with gradient color in stat box"
```

---

### Task 5: Add an integration test asserting `score_per_km` is returned by `/api/score`

**Files:**
- Modify: `route_build_test.go`

- [ ] **Step 1: Write the test**

Add after `TestHandleScore_emptyPoints`:

```go
func TestHandleScore_returnsScorePerKm(t *testing.T) {
	cacheDir := t.TempDir()
	tile := quality.Tile{South: 40.0, West: -76.0, North: 40.1, East: -75.9}
	tileData := []byte(`{"elements":[{"id":1,"tags":{"highway":"primary"},"geometry":[{"lat":40.010,"lon":-75.990},{"lat":40.011,"lon":-75.989},{"lat":40.012,"lon":-75.989},{"lat":40.011,"lon":-75.988},{"lat":40.010,"lon":-75.988}]}]}`)
	cache := &quality.TileCache{Dir: cacheDir, Precision: 3}
	if err := cache.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := cache.Write(tile, tileData); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	srv := &buildServer{tileSize: 0.1, cacheDir: cacheDir}
	// Route points inside the cached tile so they match segments.
	body := `{"points":[[40.010,-75.990],[40.012,-75.988]]}`
	req := httptest.NewRequest("POST", "/api/score", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleScore(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp scoreResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ScorePerKm == 0 {
		t.Error("expected ScorePerKm > 0 for cached tile with road data")
	}
}
```

- [ ] **Step 2: Run to verify it passes**

```bash
go test -run TestHandleScore_returnsScorePerKm -v ./...
```

Expected: PASS (implementation is already in place from Task 2).

- [ ] **Step 3: Run full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add route_build_test.go
git commit -m "test(build): assert /api/score returns score_per_km > 0 for cached tile"
```

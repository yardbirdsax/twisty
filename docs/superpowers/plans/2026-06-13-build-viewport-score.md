# Viewport Score Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Refresh score" button to the build UI that rescores all tiles visible in the current map viewport via a new `/api/score/viewport` endpoint.

**Architecture:** A new `handleViewportScore` method on `buildServer` receives a bbox JSON body, computes intersecting tiles via the existing `tilesInBBox` helper, scores cached tiles, kicks off background fetches for missing ones, and returns the same `scoreResponse` shape as `/api/score`. The frontend adds a button that calls a new `requestViewportScore()` JS function, reusing the existing `scorePollTimer` pattern.

**Tech Stack:** Go (net/http, encoding/json), vanilla JS (Leaflet `map.getBounds()`), existing `quality.TileCache`, `scoreResponse` type.

---

### Task 1: Write failing test for `handleViewportScore` — invalid JSON returns 400

**Files:**
- Modify: `route_build_test.go`

- [ ] **Step 1: Add the failing test**

Add this test to `route_build_test.go`:

```go
func TestHandleViewportScore_badJSON(t *testing.T) {
	srv := &buildServer{tileSize: 0.1}
	req := httptest.NewRequest("POST", "/api/score/viewport", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleViewportScore(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test -run TestHandleViewportScore_badJSON ./...
```

Expected: compile error — `srv.handleViewportScore undefined`

- [ ] **Step 3: Commit the failing test**

```bash
git add route_build_test.go
git commit -m "test(build): add failing test for handleViewportScore bad JSON"
```

---

### Task 2: Implement `handleViewportScore` — bad JSON path only

**Files:**
- Modify: `route_build.go`

- [ ] **Step 1: Add the request type and stub handler**

Add immediately after the `scoreResponse` type definition (around line 852 in `route_build.go`):

```go
type viewportScoreRequest struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

func (s *buildServer) handleViewportScore(w http.ResponseWriter, r *http.Request) {
	var req viewportScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
}
```

- [ ] **Step 2: Run the test to confirm it passes**

```bash
go test -run TestHandleViewportScore_badJSON ./...
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add handleViewportScore stub with bad JSON handling"
```

---

### Task 3: Write failing test for `handleViewportScore` — valid bbox returns scoreResponse

**Files:**
- Modify: `route_build_test.go`

- [ ] **Step 1: Add the test**

```go
func TestHandleViewportScore_emptyCache(t *testing.T) {
	srv := &buildServer{
		tileSize: 0.1,
		cacheDir: t.TempDir(),
	}
	body := `{"west":-76.0,"south":40.0,"east":-75.0,"north":41.0}`
	req := httptest.NewRequest("POST", "/api/score/viewport", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleViewportScore(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp scoreResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Score != 0 {
		t.Fatalf("expected score=0 for empty cache, got %f", resp.Score)
	}
	if resp.PendingTiles == 0 {
		t.Fatalf("expected pending_tiles>0 for uncached tiles, got 0")
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test -run TestHandleViewportScore_emptyCache ./...
```

Expected: FAIL — handler returns no response body (stub is incomplete)

- [ ] **Step 3: Commit the failing test**

```bash
git add route_build_test.go
git commit -m "test(build): add failing test for handleViewportScore empty cache"
```

---

### Task 4: Complete `handleViewportScore` implementation

**Files:**
- Modify: `route_build.go`

- [ ] **Step 1: Fill in the handler body**

Replace the stub body of `handleViewportScore` (everything after the JSON decode) with:

```go
	tiles := s.tilesInBBox(req.West, req.South, req.East, req.North)

	cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}

	var cachedTiles []quality.Tile
	var missingTiles []quality.Tile
	for _, t := range tiles {
		if cache.Has(t) {
			cachedTiles = append(cachedTiles, t)
		} else {
			missingTiles = append(missingTiles, t)
		}
	}

	if len(missingTiles) > 0 {
		go s.fetchMissingTiles(missingTiles)
	}

	score := s.scoreFromCachedTiles(cachedTiles, cache, nil)

	var failedCount int
	for _, t := range tiles {
		if _, failed := s.failedTiles.Load(t); failed {
			failedCount++
		}
	}

	resp := scoreResponse{
		Score:        score,
		PendingTiles: len(missingTiles),
		FailedTiles:  failedCount,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
```

Note: `scoreFromCachedTiles` takes a `routePoints [][2]float64` parameter that is used to weight tiles by how much route passes through them. Passing `nil` means all cached tiles contribute equally, which is the correct behavior for viewport scoring (no route required).

- [ ] **Step 2: Verify `scoreFromCachedTiles` handles nil routePoints**

Read `scoreFromCachedTiles` in `route_build.go` (around line 948) and confirm it does not panic on nil/empty `routePoints`. If it does, the scoring will need a guard — check the implementation before proceeding.

- [ ] **Step 3: Run all tests**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add route_build.go
git commit -m "feat(build): implement handleViewportScore"
```

---

### Task 5: Register `/api/score/viewport` route

**Files:**
- Modify: `route_build.go`

- [ ] **Step 1: Add the route registration**

In `execBuild`, after line `mux.HandleFunc("/api/score", srv.handleScore)` (around line 68), add:

```go
mux.HandleFunc("/api/score/viewport", srv.handleViewportScore)
```

- [ ] **Step 2: Run all tests**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add route_build.go
git commit -m "feat(build): register /api/score/viewport endpoint"
```

---

### Task 6: Write failing frontend test — Refresh score button present in HTML

**Files:**
- Modify: `route_build_test.go`

- [ ] **Step 1: Add the test**

```go
func TestHandleIndex_rendersRefreshScoreButton(t *testing.T) {
	bs := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	bs.handleIndex(w, r)
	body := w.Body.String()

	checks := []string{
		`id="btn-refresh-score"`,
		`onclick="requestViewportScore()"`,
		`requestViewportScore`,
		`/api/score/viewport`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test -run TestHandleIndex_rendersRefreshScoreButton ./...
```

Expected: FAIL — strings not yet present in HTML

- [ ] **Step 3: Commit the failing test**

```bash
git add route_build_test.go
git commit -m "test(build): add failing test for Refresh score button in HTML"
```

---

### Task 7: Add Refresh score button and `requestViewportScore()` to frontend

**Files:**
- Modify: `route_build.go`

- [ ] **Step 1: Add the button**

In `buildHTML`, find the Save/Load button row (around line 219):

```html
  <div class="row" style="margin-top:4px;gap:4px;justify-content:flex-start;">
    <button id="btn-save" onclick="saveRoute()" style="flex:1;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Save</button>
    <button id="btn-load" onclick="loadRoute()" style="flex:1;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Load</button>
  </div>
```

Add a new row immediately after it (before the `<input type="file"...>` line):

```html
  <div class="row" style="margin-top:4px;">
    <button id="btn-refresh-score" onclick="requestViewportScore()" style="width:100%%;background:#374151;color:white;border:none;border-radius:4px;padding:5px 8px;font-size:11px;cursor:pointer;">Refresh score</button>
  </div>
```

- [ ] **Step 2: Add `requestViewportScore()` to the JS**

In `buildHTML`, add this function immediately after the closing brace of `requestScore()` (around line 515):

```javascript
function requestViewportScore() {
  if (scorePollTimer) { clearTimeout(scorePollTimer); scorePollTimer = null; }

  var bounds = map.getBounds();
  var payload = {
    west:  bounds.getWest(),
    south: bounds.getSouth(),
    east:  bounds.getEast(),
    north: bounds.getNorth()
  };

  document.getElementById('fetch-status').textContent = '⏳ Scoring viewport...';
  document.getElementById('fetch-status').className = 'fetch-status';

  fetch('/api/score/viewport', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  .then(function(r) { return r.json(); })
  .then(function(data) {
    document.getElementById('score-val').textContent = Math.round(data.score).toLocaleString();

    if (data.pending_tiles > 0) {
      document.getElementById('fetch-status').textContent = '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...';
      document.getElementById('fetch-status').className = 'fetch-status';
      scorePollTimer = setTimeout(requestViewportScore, 2000);
    } else if (data.failed_tiles > 0) {
      document.getElementById('fetch-status').textContent = '⚠ ' + data.failed_tiles + ' tile(s) failed';
      document.getElementById('fetch-status').className = 'fetch-status';
    } else {
      document.getElementById('fetch-status').textContent = '✅ Score complete';
      document.getElementById('fetch-status').className = 'fetch-status done';
    }
  })
  .catch(function(err) {
    document.getElementById('fetch-status').textContent = 'Score error';
    document.getElementById('fetch-status').className = 'fetch-status';
  });
}
```

- [ ] **Step 3: Run all tests**

```bash
go test ./...
```

Expected: all PASS including `TestHandleIndex_rendersRefreshScoreButton`

- [ ] **Step 4: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add Refresh score button and requestViewportScore() to build UI"
```

---

### Task 8: Verify `scoreFromCachedTiles` handles nil route points

**Files:**
- Read: `route_build.go` (around line 948)

- [ ] **Step 1: Read the function**

Read `scoreFromCachedTiles` in `route_build.go`. Confirm that passing `nil` for `routePoints` does not cause a panic or incorrect zero score when there are cached tiles. The function signature is:

```go
func (s *buildServer) scoreFromCachedTiles(tiles []quality.Tile, cache *quality.TileCache, routePoints [][2]float64) float64
```

- [ ] **Step 2: If nil routePoints causes a problem, fix it**

If `scoreFromCachedTiles` uses `routePoints` in a way that panics or skips all tiles when it's nil, add a guard. For example, if it calls `isNearPolyline` and bails when `routePoints` is empty, the viewport score will always be 0. In that case, skip the proximity check when `routePoints` is nil:

```go
// If no route points provided, include all tiles (viewport scoring mode).
if len(routePoints) > 0 && !isNearPolyline(...) {
    continue
}
```

The exact fix depends on what you find in the function body.

- [ ] **Step 3: Run all tests**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 4: Commit if changed**

```bash
git add route_build.go
git commit -m "fix(build): handle nil routePoints in scoreFromCachedTiles for viewport scoring"
```

---

### Task 9: Manual smoke test

- [ ] **Step 1: Build and run**

```bash
go build -o twisty . && ./twisty build --address 127.0.0.1 --port 8080
```

Open `http://localhost:8080` in a browser.

- [ ] **Step 2: Verify button appears**

Confirm "Refresh score" button is visible in the stats panel below Save/Load.

- [ ] **Step 3: Click Refresh score**

Click the button. Confirm:
- `fetch-status` shows `⏳ Scoring viewport...` briefly, then either counts down pending tiles or shows `✅ Score complete`
- The score value updates (or stays `—` if no route and no cached tiles in the area)
- No JS errors in browser console

- [ ] **Step 4: Verify poll loop**

In an area with no cached tiles, click Refresh score and confirm the status polls every ~2 seconds until tiles arrive (or fails gracefully if Overpass is unreachable).

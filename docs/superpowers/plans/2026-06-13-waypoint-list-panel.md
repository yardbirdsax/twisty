# Waypoint List Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a collapsible waypoint list to the build UI's `#stats` panel, with lazy reverse geocoding, inline address editing for forward geocoding, and active slot selection for map-click replacement.

**Architecture:** Two new Go handler methods (`handleGeocode`, `handleReverseGeocode`) added to `buildServer` in `route_build.go`, both proxying Nominatim via the existing `geocode` package. The waypoint list UI is pure JS/HTML embedded in `buildHTML` — labels live in a JS `Map` (session cache only, never persisted). The `waypoints` array and save file format are unchanged.

**Tech Stack:** Go (net/http, existing geocode package), vanilla JS, Leaflet (already present), Nominatim geocoding API.

---

## File Map

| File | Change |
|---|---|
| `route_build.go` | Register `/api/geocode` and `/api/reverse-geocode`; add `handleGeocode` and `handleReverseGeocode` methods; add waypoint panel HTML/CSS/JS to `buildHTML` |
| `route_build_test.go` | Add handler tests for `/api/geocode` and `/api/reverse-geocode`; add HTML render test for waypoint panel presence |
| `geocode/nominatim.go` | Add `ReverseGeocode(lat, lon float64) (Result, error)` function |
| `geocode/nominatim_test.go` | Add tests for `ReverseGeocode` |

---

## Task 1: Add `ReverseGeocode` to the geocode package

**Files:**
- Modify: `geocode/nominatim.go`
- Test: `geocode/nominatim_test.go`

- [ ] **Step 1: Write the failing test**

Add to `geocode/nominatim_test.go`:

```go
func TestReverseGeocode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reverse" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		lat := r.URL.Query().Get("lat")
		lon := r.URL.Query().Get("lon")
		if lat == "" || lon == "" {
			t.Error("missing lat or lon query params")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"display_name":"123 Main St, Pottsville, PA"}`)
	}))
	defer srv.Close()

	result, err := reverseGeocodeWithURL(40.1234, -76.5678, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DisplayName != "123 Main St, Pottsville, PA" {
		t.Errorf("DisplayName = %q, want %q", result.DisplayName, "123 Main St, Pottsville, PA")
	}
}

func TestReverseGeocode_NoResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	}))
	defer srv.Close()

	result, err := reverseGeocodeWithURL(0, 0, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DisplayName != "" {
		t.Errorf("expected empty DisplayName for no result, got %q", result.DisplayName)
	}
}
```

You'll also need `reverseGeocodeWithURL` as a test helper (same file):

```go
func reverseGeocodeWithURL(lat, lon float64, baseURL string) (Result, error) {
	u := fmt.Sprintf("%s/reverse?lat=%f&lon=%f&format=jsonv2", baseURL, lat, lon)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "twisty/1.0")
	resp, err := defaultClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}
	var raw struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Result{}, err
	}
	return Result{Lat: lat, Lon: lon, DisplayName: raw.DisplayName}, nil
}
```

Also add `"fmt"` to the test file's imports if not present.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./geocode/... -run TestReverseGeocode -v
```

Expected: FAIL — `reverseGeocodeWithURL undefined`

- [ ] **Step 3: Add `ReverseGeocode` and `reverseGeocodeWithURL` to `geocode/nominatim.go`**

Add at the bottom of `geocode/nominatim.go`:

```go
// reverseNominatimResult is the JSON shape returned by the Nominatim reverse API.
type reverseNominatimResult struct {
	DisplayName string `json:"display_name"`
}

// ReverseGeocode queries Nominatim for the address at the given coordinates.
// Returns a Result with DisplayName set; returns an empty DisplayName (not an error)
// if Nominatim returns no result.
func ReverseGeocode(lat, lon float64) (Result, error) {
	return reverseGeocodeWithURL(lat, lon, "https://nominatim.openstreetmap.org")
}

func reverseGeocodeWithURL(lat, lon float64, baseURL string) (Result, error) {
	u := fmt.Sprintf("%s/reverse?lat=%f&lon=%f&format=jsonv2", baseURL, lat, lon)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return Result{}, fmt.Errorf("creating reverse geocode request: %w", err)
	}
	req.Header.Set("User-Agent", "twisty/1.0")

	resp, err := defaultClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("reverse geocoding (%.4f, %.4f): %w", lat, lon, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("reading reverse geocode response: %w", err)
	}

	var raw reverseNominatimResult
	if err := json.Unmarshal(body, &raw); err != nil {
		return Result{}, fmt.Errorf("parsing reverse geocode response: %w", err)
	}

	return Result{Lat: lat, Lon: lon, DisplayName: raw.DisplayName}, nil
}
```

Also add `"fmt"` to `geocode/nominatim.go`'s imports if not already present (it should already be there).

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./geocode/... -run TestReverseGeocode -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add geocode/nominatim.go geocode/nominatim_test.go
git commit -m "feat(geocode): add ReverseGeocode function for lat/lon to address lookup"
```

---

## Task 2: Add `/api/geocode` handler

**Files:**
- Modify: `route_build.go` (register route + add handler)
- Test: `route_build_test.go`

- [ ] **Step 1: Write the failing test**

Add to `route_build_test.go`:

```go
func TestHandleGeocode_success(t *testing.T) {
	// Spin up a fake Nominatim server
	nominatimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"lat":"40.1234","lon":"-76.5678","display_name":"123 Main St, Pottsville, PA","importance":0.9}]`)
	}))
	defer nominatimSrv.Close()

	srv := &buildServer{nominatimBase: nominatimSrv.URL}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/geocode?q=123+Main+St", nil)
	srv.handleGeocode(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
		DisplayName string  `json:"display_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Lat != 40.1234 || resp.Lon != -76.5678 {
		t.Errorf("lat/lon = %.4f, %.4f; want 40.1234, -76.5678", resp.Lat, resp.Lon)
	}
	if resp.DisplayName != "123 Main St, Pottsville, PA" {
		t.Errorf("DisplayName = %q", resp.DisplayName)
	}
}

func TestHandleGeocode_noResults(t *testing.T) {
	nominatimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[]`)
	}))
	defer nominatimSrv.Close()

	srv := &buildServer{nominatimBase: nominatimSrv.URL}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/geocode?q=nowhere", nil)
	srv.handleGeocode(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleGeocode_missingQuery(t *testing.T) {
	srv := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/geocode", nil)
	srv.handleGeocode(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./... -run TestHandleGeocode -v
```

Expected: FAIL — `buildServer has no field nominatimBase` and `handleGeocode undefined`

- [ ] **Step 3: Add `nominatimBase` field to `buildServer` and implement `handleGeocode`**

In `route_build.go`, find the `buildServer` struct definition and add the field:

```go
type buildServer struct {
	// ... existing fields ...
	nominatimBase string // override for tests; empty means use production Nominatim
}
```

Add the handler method (place it near the other handlers, e.g. after `handleViewportScore`):

```go
func (s *buildServer) handleGeocode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing query parameter q", http.StatusBadRequest)
		return
	}

	base := s.nominatimBase
	if base == "" {
		base = "https://nominatim.openstreetmap.org"
	}

	result, err := geocode.GeocodeWithURL(q, "Waypoint", base)
	if err != nil {
		if strings.Contains(err.Error(), "no results found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
		DisplayName string  `json:"display_name"`
	}{Lat: result.Lat, Lon: result.Lon, DisplayName: result.DisplayName})
}
```

This requires exposing `GeocodeWithURL` from the geocode package. Add it to `geocode/nominatim.go`:

```go
// GeocodeWithURL is like Geocode but uses a custom Nominatim base URL. Used for testing.
func GeocodeWithURL(address, label, baseURL string) (Result, error) {
	u := url.URL{
		Scheme: "https",
		Host:   strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://"),
		Path:   "/search",
	}
	// For test servers use raw URL parsing instead
	reqURL := baseURL + "/search?q=" + url.QueryEscape(address) + "&format=jsonv2&limit=5"
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "twisty/1.0")

	resp, err := defaultClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("geocoding %s %q: %w", label, address, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("reading response: %w", err)
	}

	var results []nominatimResult
	if err := json.Unmarshal(body, &results); err != nil {
		return Result{}, fmt.Errorf("parsing response: %w", err)
	}

	return processNominatimResults(results, address, label)
}
```

Also register the route in the `mux` setup in `route_build.go`:

```go
mux.HandleFunc("/api/geocode", srv.handleGeocode)
```

And add the `geocode` package import to `route_build.go` if not present:

```go
"github.com/yardbirdsax/twisty/geocode"
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./... -run TestHandleGeocode -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add route_build.go route_build_test.go geocode/nominatim.go
git commit -m "feat(build): add /api/geocode endpoint proxying Nominatim forward geocode"
```

---

## Task 3: Add `/api/reverse-geocode` handler

**Files:**
- Modify: `route_build.go`
- Test: `route_build_test.go`

- [ ] **Step 1: Write the failing test**

Add to `route_build_test.go`:

```go
func TestHandleReverseGeocode_success(t *testing.T) {
	nominatimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"display_name":"123 Main St, Pottsville, PA"}`)
	}))
	defer nominatimSrv.Close()

	srv := &buildServer{nominatimBase: nominatimSrv.URL}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/reverse-geocode?lat=40.1234&lon=-76.5678", nil)
	srv.handleReverseGeocode(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DisplayName != "123 Main St, Pottsville, PA" {
		t.Errorf("DisplayName = %q", resp.DisplayName)
	}
}

func TestHandleReverseGeocode_noResult(t *testing.T) {
	nominatimSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	}))
	defer nominatimSrv.Close()

	srv := &buildServer{nominatimBase: nominatimSrv.URL}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/reverse-geocode?lat=0&lon=0", nil)
	srv.handleReverseGeocode(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (graceful empty), got %d", w.Code)
	}
	var resp struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DisplayName != "" {
		t.Errorf("expected empty DisplayName, got %q", resp.DisplayName)
	}
}

func TestHandleReverseGeocode_missingParams(t *testing.T) {
	srv := &buildServer{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/reverse-geocode?lat=40.1", nil)
	srv.handleReverseGeocode(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./... -run TestHandleReverseGeocode -v
```

Expected: FAIL — `handleReverseGeocode undefined`

- [ ] **Step 3: Implement `handleReverseGeocode` and register route**

Add to `route_build.go`:

```go
func (s *buildServer) handleReverseGeocode(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	if latStr == "" || lonStr == "" {
		http.Error(w, "missing lat or lon parameter", http.StatusBadRequest)
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		http.Error(w, "invalid lat", http.StatusBadRequest)
		return
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		http.Error(w, "invalid lon", http.StatusBadRequest)
		return
	}

	base := s.nominatimBase
	if base == "" {
		base = "https://nominatim.openstreetmap.org"
	}

	result, err := geocode.ReverseGeocodeWithURL(lat, lon, base)
	if err != nil {
		// Graceful fallback — return empty display_name, not an error
		result = geocode.Result{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		DisplayName string `json:"display_name"`
	}{DisplayName: result.DisplayName})
}
```

Register in the mux setup:

```go
mux.HandleFunc("/api/reverse-geocode", srv.handleReverseGeocode)
```

Also add `ReverseGeocodeWithURL` to `geocode/nominatim.go` (expose the internal test helper as a public function):

```go
// ReverseGeocodeWithURL is like ReverseGeocode but uses a custom Nominatim base URL. Used for testing.
func ReverseGeocodeWithURL(lat, lon float64, baseURL string) (Result, error) {
	return reverseGeocodeWithURL(lat, lon, baseURL)
}
```

And add `"strconv"` to `route_build.go`'s imports if not already present.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./... -run TestHandleReverseGeocode -v
```

Expected: PASS

- [ ] **Step 5: Run full test suite**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add route_build.go route_build_test.go geocode/nominatim.go
git commit -m "feat(build): add /api/reverse-geocode endpoint proxying Nominatim reverse geocode"
```

---

## Task 4: Add waypoint panel HTML and CSS to `buildHTML`

**Files:**
- Modify: `route_build.go` (`buildHTML` constant)
- Test: `route_build_test.go`

- [ ] **Step 1: Write the failing test**

Add to `route_build_test.go`:

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
		`+ Add waypoint`,
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("expected HTML to contain %q", c)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./... -run TestHandleIndex_rendersWaypointPanel -v
```

Expected: FAIL

- [ ] **Step 3: Add waypoint panel HTML and CSS to `buildHTML`**

In the CSS section of `buildHTML` in `route_build.go`, add after the existing `#stats .fetch-status.done` rule:

```css
  #waypoints-section { border-top: 1px solid #eee; padding-top: 8px; margin-top: 8px; }
  #waypoints-header { display: flex; justify-content: space-between; align-items: center; cursor: pointer; user-select: none; margin-bottom: 0; }
  #waypoints-header .label { font-size: 11px; text-transform: uppercase; color: #666; letter-spacing: 0.5px; margin-bottom: 0; }
  #waypoints-header .toggle { color: #2563eb; font-size: 12px; }
  #waypoints-list { margin-top: 6px; display: none; }
  #waypoints-list.open { display: block; }
  .wp-row { display: flex; align-items: center; gap: 6px; background: #f9fafb; border: 1px solid #e5e7eb; border-radius: 4px; padding: 4px 6px; margin-bottom: 3px; font-size: 11px; cursor: pointer; }
  .wp-row.active-slot { border-left: 3px solid #2563eb; padding-left: 4px; }
  .wp-badge { color: #9ca3af; font-size: 10px; flex-shrink: 0; cursor: pointer; }
  .wp-label { flex: 1; color: #374151; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .wp-label.fallback { color: #9ca3af; font-style: italic; }
  .wp-input { flex: 1; border: none; outline: none; font-size: 11px; background: transparent; }
  #waypoints-add { display: flex; align-items: center; gap: 6px; background: #f0f9ff; border: 1px dashed #93c5fd; border-radius: 4px; padding: 4px 6px; margin-top: 2px; font-size: 11px; color: #2563eb; cursor: pointer; }
  .wp-error { font-size: 10px; color: #dc2626; margin-top: 2px; display: none; }
  @keyframes wp-shake { 0%,100%{transform:translateX(0)} 25%{transform:translateX(-4px)} 75%{transform:translateX(4px)} }
  .wp-input.shake { animation: wp-shake 0.3s ease; }
```

In the HTML body of `buildHTML`, add inside `<div id="stats">` after the `fetch-status` div:

```html
  <div id="waypoints-section">
    <div id="waypoints-header" onclick="toggleWaypointsPanel()">
      <span class="label">Waypoints (<span id="waypoints-count">0</span>)</span>
      <span class="toggle" id="waypoints-toggle">▶</span>
    </div>
    <div id="waypoints-list">
      <div id="waypoints-add" onclick="startAddWaypoint()">+ Add waypoint</div>
    </div>
    <div class="wp-error" id="wp-error"></div>
  </div>
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./... -run TestHandleIndex_rendersWaypointPanel -v
```

Expected: PASS

- [ ] **Step 5: Run full test suite**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): add waypoint panel HTML and CSS to stats box"
```

---

## Task 5: Add waypoint panel JavaScript

**Files:**
- Modify: `route_build.go` (`buildHTML` JS section)

This task adds all the JS that powers the panel. There are no Go tests for this behavior — test manually per step 4.

- [ ] **Step 1: Add JS globals and helper functions**

In the `<script>` block in `buildHTML`, after the `var scorePerKmMax = %g;` line, add:

```javascript
var labelCache = {};         // "lat,lon" -> display name string
var activeSlotIndex = -1;   // index into waypoints[], or -1 for none
var waypointsPanelOpen = false;

var CIRCLED_DIGITS = ['①','②','③','④','⑤','⑥','⑦','⑧','⑨','⑩'];
function waypointBadge(i) {
  return i < CIRCLED_DIGITS.length ? CIRCLED_DIGITS[i] : '#' + (i + 1);
}

function labelKey(wp) {
  return wp[0].toFixed(6) + ',' + wp[1].toFixed(6);
}

function latLonFallback(wp) {
  return wp[0].toFixed(4) + ', ' + wp[1].toFixed(4);
}

function showWpError(msg) {
  var el = document.getElementById('wp-error');
  el.textContent = msg;
  el.style.display = msg ? 'block' : 'none';
}
```

- [ ] **Step 2: Add panel toggle and render functions**

Still in the `<script>` block, add:

```javascript
function toggleWaypointsPanel() {
  waypointsPanelOpen = !waypointsPanelOpen;
  var list = document.getElementById('waypoints-list');
  var toggle = document.getElementById('waypoints-toggle');
  list.className = waypointsPanelOpen ? 'open' : '';
  toggle.textContent = waypointsPanelOpen ? '▼' : '▶';
  if (waypointsPanelOpen) {
    reverseGeocodeUnlabeled();
  }
}

function renderWaypointList() {
  document.getElementById('waypoints-count').textContent = waypoints.length;
  var list = document.getElementById('waypoints-list');

  // Remove existing wp-row elements (keep the add row)
  var existing = list.querySelectorAll('.wp-row');
  existing.forEach(function(el) { el.parentNode.removeChild(el); });

  var addRow = document.getElementById('waypoints-add');

  waypoints.forEach(function(wp, i) {
    var key = labelKey(wp);
    var label = labelCache[key] || null;
    var row = document.createElement('div');
    row.className = 'wp-row' + (i === activeSlotIndex ? ' active-slot' : '');
    row.dataset.index = i;

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
  });
}

function setActiveSlot(i) {
  activeSlotIndex = (activeSlotIndex === i) ? -1 : i;
  renderWaypointList();
}
```

- [ ] **Step 3: Add inline edit functions**

```javascript
function startEditWaypoint(row, index, labelEl) {
  if (row.querySelector('.wp-input')) return; // already editing
  var prev = labelEl.textContent;
  labelEl.style.display = 'none';

  var input = document.createElement('input');
  input.className = 'wp-input';
  input.type = 'text';
  input.placeholder = 'Enter address...';
  row.appendChild(input);
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitWaypointEdit(index, q, row, input, labelEl, prev);
    } else if (e.key === 'Escape') {
      row.removeChild(input);
      labelEl.style.display = '';
      showWpError('');
    }
  };
}

function commitWaypointEdit(index, query, row, input, labelEl, prevLabel) {
  input.disabled = true;
  showWpError('');
  fetch('/api/geocode?q=' + encodeURIComponent(query))
    .then(function(r) {
      if (r.status === 404) throw new Error('Address not found');
      if (!r.ok) throw new Error('Geocoding error');
      return r.json();
    })
    .then(function(data) {
      // Replace waypoint in array
      waypoints[index] = [data.lat, data.lon];
      labelCache[labelKey(waypoints[index])] = data.display_name;
      row.removeChild(input);
      labelEl.style.display = '';
      saveState();
      refreshMarkers();
      renderWaypointList();
      // Re-route affected legs
      rerouteLeg(index);
    })
    .catch(function(err) {
      input.disabled = false;
      input.classList.add('shake');
      setTimeout(function() { input.classList.remove('shake'); }, 300);
      input.focus();
      showWpError(err.message || 'Address not found');
    });
}

function startAddWaypoint() {
  var addRow = document.getElementById('waypoints-add');
  if (addRow.querySelector('.wp-input')) return;

  var input = document.createElement('input');
  input.className = 'wp-input';
  input.type = 'text';
  input.placeholder = 'Enter address...';
  input.style.flex = '1';
  addRow.textContent = '';
  addRow.appendChild(input);
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitAddWaypoint(q, addRow, input);
    } else if (e.key === 'Escape') {
      addRow.textContent = '+ Add waypoint';
      addRow.onclick = startAddWaypoint;
      showWpError('');
    }
  };
}

function commitAddWaypoint(query, addRow, input) {
  input.disabled = true;
  showWpError('');
  fetch('/api/geocode?q=' + encodeURIComponent(query))
    .then(function(r) {
      if (r.status === 404) throw new Error('Address not found');
      if (!r.ok) throw new Error('Geocoding error');
      return r.json();
    })
    .then(function(data) {
      var latlng = L.latLng(data.lat, data.lon);
      labelCache[data.lat.toFixed(6) + ',' + data.lon.toFixed(6)] = data.display_name;
      addRow.textContent = '+ Add waypoint';
      addRow.onclick = startAddWaypoint;
      addWaypoint(latlng);  // existing function — handles routing, markers, state
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

- [ ] **Step 4: Add reverse geocoding and rerouting helpers**

```javascript
function reverseGeocodeUnlabeled() {
  var unlabeled = waypoints.filter(function(wp) {
    return !labelCache[labelKey(wp)];
  });
  if (unlabeled.length === 0) return;

  // Sequential to respect Nominatim rate limits
  function next(i) {
    if (i >= unlabeled.length) return;
    var wp = unlabeled[i];
    var key = labelKey(wp);
    fetch('/api/reverse-geocode?lat=' + wp[0] + '&lon=' + wp[1])
      .then(function(r) { return r.json(); })
      .then(function(data) {
        if (data.display_name) {
          labelCache[key] = data.display_name;
          renderWaypointList();
        }
        next(i + 1);
      })
      .catch(function() { next(i + 1); });
  }
  next(0);
}

function rerouteLeg(index) {
  // Re-route the leg before this waypoint (index-1 -> index) and after (index -> index+1)
  // For simplicity, clear all legs and re-route everything sequentially
  // This reuses the existing addWaypoint routing logic by rebuilding from scratch
  if (waypoints.length < 2) return;

  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  legs = [];
  updateStats();
  requestScore();
  saveState();

  var gen = ++routingGeneration;

  function routeNext(i) {
    if (i >= waypoints.length - 1) return;
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
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5 }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      saveState();
      updateStats();
      requestScore();
      routeNext(i + 1);
    })
    .catch(function() {
      showToast('Could not route leg ' + (i + 1));
    });
  }
  routeNext(0);
}
```

- [ ] **Step 5: Hook panel updates into existing waypoint mutation functions**

Find `addWaypoint` in `buildHTML` and add `renderWaypointList()` + auto-expand logic after `refreshMarkers(); saveState();`:

```javascript
function addWaypoint(latlng) {
  var prev = waypoints.length > 0 ? waypoints[waypoints.length - 1] : null;

  // If an active slot is set, replace that waypoint instead of appending
  if (activeSlotIndex >= 0 && activeSlotIndex < waypoints.length) {
    waypoints[activeSlotIndex] = [latlng.lat, latlng.lng];
    var replacedIndex = activeSlotIndex;
    activeSlotIndex = -1;
    refreshMarkers();
    saveState();
    renderWaypointList();
    rerouteLeg(replacedIndex);
    return;
  }

  waypoints.push([latlng.lat, latlng.lng]);
  refreshMarkers();
  saveState();
  renderWaypointList();

  // Auto-expand panel on first waypoint
  if (waypoints.length === 1 && !waypointsPanelOpen) {
    toggleWaypointsPanel();
  }

  if (prev) {
    // ... rest of existing routing code unchanged ...
  }
}
```

Also add `renderWaypointList()` at the end of `removeLastWaypoint()` and `clearRoute()`.

Also deselect active slot on map click by adding `activeSlotIndex = -1;` at the start of the map's `click` handler.

- [ ] **Step 6: Manual smoke test**

Start the server:
```bash
go run . build --address "Pottsville, PA"
```

Open http://localhost:8080 and verify:
1. Waypoints section shows collapsed "Waypoints (0)" with ▶
2. Click map → panel auto-expands to "Waypoints (1)", row shows address or lat/lon
3. Click + Add waypoint, type an address, Enter → waypoint added and routed
4. Click the ① badge → row gets blue left border (active slot); next map click replaces that waypoint
5. Click address text on a row → becomes editable input; Enter geocodes and updates; Escape cancels
6. Type a bad address → "Address not found" shown, input stays open
7. Clear route → panel shows "Waypoints (0)"
8. Save/Load round-trip → labels re-resolved from reverse geocode on panel expand

- [ ] **Step 7: Run full test suite**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add waypoint list panel JS — collapsible, inline editing, geocoding"
```

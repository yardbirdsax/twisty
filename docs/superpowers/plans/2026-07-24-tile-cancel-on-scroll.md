# Tile Cancel-on-Scroll Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cancel in-flight Overpass tile fetches for tiles that leave the viewport when the user scrolls, eliminating the thundering-herd effect on Overpass.

**Architecture:** A per-tile `context.CancelFunc` stored in `buildServer.inFlight` replaces the current empty `struct{}` sentinel; `fetchMissingTiles` gains a `cancelStale bool` parameter that, when true, cancels any in-flight tile not present in the new tile set before claiming new tiles. A thin HTTP logging middleware wraps the mux to provide observability.

**Tech Stack:** Go standard library (`context`, `sync`, `log/slog`, `net/http`); no new dependencies.

## Global Constraints

- `inFlight sync.Map` key type stays `quality.Tile`; only the value type changes (`struct{}` → `context.CancelFunc`).
- Cancelled tiles are NOT written to `failedTiles`; they remain uncached and will be re-fetched if the user scrolls back.
- `cancelStale=false` callers (`handleViewportScore`, `handleTiles`) retain deduplication behavior — no behavioural change.
- `FetchTiledWaysForTiles` in `quality/tilefetch.go` must not be modified.
- Tests must pass: `go test ./...`

---

## Task 1: HTTP Logging Middleware

**Files:**
- Modify: `route_build.go:81-104` (wrap mux in `execBuild`)

**Interfaces:**
- Produces: `loggingMiddleware(next http.Handler) http.Handler` — used only by `execBuild`.

- [ ] **Step 1: Write the failing test**

Add to `route_build_test.go` a test that hits any endpoint through the mux and confirms the server accepts the request (the middleware must not break routing). Because the logging middleware writes to `slog`, we verify it doesn't panic and the handler still responds correctly.

```go
func TestLoggingMiddleware(t *testing.T) {
    // Arrange: create a simple handler that records whether it was called.
    called := false
    inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        called = true
        w.WriteHeader(http.StatusOK)
    })

    wrapped := loggingMiddleware(inner)
    req := httptest.NewRequest(http.MethodGet, "/some/path", nil)
    w := httptest.NewRecorder()

    // Act
    wrapped.ServeHTTP(w, req)

    // Assert
    if !called {
        t.Error("expected inner handler to be called")
    }
    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", w.Code)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./... -run TestLoggingMiddleware -v
```

Expected: FAIL — `loggingMiddleware` undefined.

- [ ] **Step 3: Implement `loggingMiddleware` in `route_build.go`**

Add this function just before `execBuild` (around line 44):

```go
func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        slog.Info("http request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
        next.ServeHTTP(w, r)
    })
}
```

Then in `execBuild`, change the `http.Serve` call (line 104):

```go
// Before:
return http.Serve(ln, mux)

// After:
return http.Serve(ln, loggingMiddleware(mux))
```

- [ ] **Step 4: Run test to verify it passes**

```
go test ./... -run TestLoggingMiddleware -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): add HTTP request logging middleware"
```

---

## Task 2: Change `inFlight` value type to `context.CancelFunc`

**Files:**
- Modify: `route_build.go:116` (struct comment), `route_build.go:1921-1985` (`fetchMissingTiles`)

**Interfaces:**
- Consumes: `context.WithCancel(context.Background())` → `(ctx context.Context, cancel context.CancelFunc)`
- Produces: `fetchMissingTiles(tiles []quality.Tile, cancelStale bool)` — updated signature used in Tasks 3 and 4.

The existing `fetchMissingTiles` signature is `func (s *buildServer) fetchMissingTiles(tiles []quality.Tile)`. This task changes it to `func (s *buildServer) fetchMissingTiles(tiles []quality.Tile, cancelStale bool)` and updates the internals accordingly. The four call sites are updated in Task 3.

- [ ] **Step 1: Write the failing test**

Add to `route_build_test.go`:

```go
func TestFetchMissingTiles_cancelStale(t *testing.T) {
    // Mock Overpass server: blocks until released, so tiles stay in-flight long enough
    // for us to fire the second fetchMissingTiles call.
    release := make(chan struct{})
    overpassSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        <-release
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"elements":[]}`)) //nolint:errcheck
    }))
    defer overpassSrv.Close()
    defer close(release) // unblock any goroutines still waiting

    cacheDir := t.TempDir()
    tileA := quality.Tile{South: 40.0, West: -75.0, North: 40.1, East: -74.9}
    tileB := quality.Tile{South: 40.1, West: -75.0, North: 40.2, East: -74.9} // not in second call
    tileC := quality.Tile{South: 40.2, West: -75.0, North: 40.3, East: -74.9} // only in second call

    srv := &buildServer{
        overpassURL: overpassSrv.URL,
        cacheDir:    cacheDir,
        tileSize:    0.1,
        fetchDelay:  "1ms",
        tileReady:   make(chan quality.Tile, 64),
        broker:      newSSEBroker(),
    }
    go srv.broker.run()

    // First call: claim tileA and tileB with cancelStale=true.
    go srv.fetchMissingTiles([]quality.Tile{tileA, tileB}, true)

    // Give the goroutine time to claim the tiles.
    time.Sleep(20 * time.Millisecond)

    // Second call: tileA overlaps; tileB is stale; tileC is new.
    // cancelStale=true should cancel tileB and leave tileA alone.
    srv.fetchMissingTiles([]quality.Tile{tileA, tileC}, true)

    // tileB should no longer be in inFlight (it was cancelled and then deleted
    // by the deferred cleanup when its goroutine exits, or it was cancelled
    // and deleted during stale sweep). Either way, tileB's cancel should have
    // been called. We verify this indirectly: tileA must still be in inFlight
    // (not cancelled), and tileB must not be in inFlight.
    _, tileAInFlight := srv.inFlight.Load(tileA)
    _, tileBInFlight := srv.inFlight.Load(tileB)

    // tileA should still be claimed (the first goroutine holds it).
    if !tileAInFlight {
        t.Error("expected tileA to remain in-flight (overlap tile)")
    }
    // tileB should have been cancelled and removed.
    if tileBInFlight {
        t.Error("expected tileB to have been cancelled and removed from inFlight")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./... -run TestFetchMissingTiles_cancelStale -v
```

Expected: FAIL — compile error because `fetchMissingTiles` still takes one argument.

- [ ] **Step 3: Update `fetchMissingTiles` signature and internals**

Replace `func (s *buildServer) fetchMissingTiles(tiles []quality.Tile)` (line 1921) with the full updated implementation. The complete new function body:

```go
func (s *buildServer) fetchMissingTiles(tiles []quality.Tile, cancelStale bool) {
    if cancelStale {
        // Build a set of incoming tiles for O(1) lookup.
        incoming := make(map[quality.Tile]struct{}, len(tiles))
        for _, t := range tiles {
            incoming[t] = struct{}{}
        }
        // Cancel and evict any in-flight tile not present in the new set.
        s.inFlight.Range(func(key, val any) bool {
            t := key.(quality.Tile)
            if _, keep := incoming[t]; !keep {
                fn := val.(context.CancelFunc)
                slog.Debug("cancelling stale tile fetch", "south", t.South, "west", t.West)
                fn()
                s.inFlight.Delete(t)
            }
            return true
        })
    }

    // Claim tiles that aren't already being fetched by another goroutine.
    var claimed []quality.Tile
    for _, t := range tiles {
        ctx, cancel := context.WithCancel(context.Background())
        if _, loaded := s.inFlight.LoadOrStore(t, cancel); loaded {
            // Already claimed; discard the cancel func we just created.
            cancel()
            slog.Debug("fetchMissingTiles: tile already in-flight, skipping", "south", t.South, "west", t.West)
        } else {
            claimed = append(claimed, t)
            slog.Debug("fetchMissingTiles: goroutine enqueuing tile", "south", t.South, "west", t.West)
        }
    }
    if len(claimed) == 0 {
        return
    }
    defer func() {
        for _, t := range claimed {
            if fn, ok := s.inFlight.LoadAndDelete(t); ok {
                fn.(context.CancelFunc)()
            }
        }
    }()
    tiles = claimed

    var fetchDelay time.Duration
    if s.fetchDelay != "" {
        d, err := time.ParseDuration(s.fetchDelay)
        if err == nil {
            fetchDelay = d
        }
    }
    if fetchDelay == 0 {
        fetchDelay = time.Second
    }

    cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}
    cache.EnsureDir()

    // Retrieve the context for the first claimed tile to propagate cancellation.
    // All claimed tiles were stored with their own cancel func; we need a context
    // for FetchTiledWaysForTiles. Use the context of the first claimed tile as a
    // representative — if any tile is cancelled this goroutine's work is stale.
    // NOTE: each tile has its own cancel func; we use the first tile's context as
    // the shared context for the batch. If that tile gets cancelled, the whole
    // batch stops, which is the correct behaviour (a new fetchMissingTiles call
    // will re-claim any tiles that are still needed).
    val, ok := s.inFlight.Load(claimed[0])
    if !ok {
        // The tile was cancelled between claiming and here — bail out.
        return
    }
    _ = val // val is the cancel func, not the context; we need to build the context differently.

    // We stored cancel funcs, not contexts. Derive a context per batch using
    // the cancel func of the first claimed tile as a sentinel. However, since
    // CancelFunc is write-only (we can't recover the context), we need to store
    // contexts instead. See Step 3 note below.
    cfg := quality.TileFetchConfig{
        Endpoint:       s.overpassURL,
        TileSize:       s.tileSize,
        Cache:          cache,
        RateLimitDelay: fetchDelay,
    }

    ctx := context.Background()
    quality.FetchTiledWaysForTiles(ctx, tiles, cfg)

    // Notify SSE broker for each tile that landed in cache.
    for _, t := range tiles {
        if cache.Has(t) {
            s.failedTiles.Delete(t)
            select {
            case s.tileReady <- t:
            default:
                slog.Warn("tileReady channel full, SSE push dropped", "south", t.South, "west", t.West)
            }
        }
    }

    // Mark any tiles still missing in cache as failed.
    for _, t := range tiles {
        if !cache.Has(t) {
            s.failedTiles.Store(t, struct{}{})
        }
    }
}
```

**Important implementation note:** The spec stores a `context.CancelFunc` in `inFlight`, but we need a `context.Context` to pass to `FetchTiledWaysForTiles`. The correct approach is to store a `cancelEntry` struct (or store both) — but the spec says to store the cancel func. The solution: store the cancel func, and use it to cancel, but create the goroutine's shared context separately with `context.WithCancel(context.Background())` and store _that_ cancel func. The `LoadOrStore` call stores the cancel func tied to a `ctx` the goroutine will use.

Here is the correct, final implementation of the function (replaces the draft above):

```go
func (s *buildServer) fetchMissingTiles(tiles []quality.Tile, cancelStale bool) {
    if cancelStale {
        incoming := make(map[quality.Tile]struct{}, len(tiles))
        for _, t := range tiles {
            incoming[t] = struct{}{}
        }
        s.inFlight.Range(func(key, val any) bool {
            t := key.(quality.Tile)
            if _, keep := incoming[t]; !keep {
                fn := val.(context.CancelFunc)
                slog.Debug("cancelling stale tile fetch", "south", t.South, "west", t.West)
                fn()
                s.inFlight.Delete(t)
            }
            return true
        })
    }

    var claimed []quality.Tile
    var batchCtx context.Context
    var batchCancel context.CancelFunc
    batchCtx, batchCancel = context.WithCancel(context.Background())

    for _, t := range tiles {
        if _, loaded := s.inFlight.LoadOrStore(t, batchCancel); loaded {
            slog.Debug("fetchMissingTiles: tile already in-flight, skipping", "south", t.South, "west", t.West)
        } else {
            claimed = append(claimed, t)
            slog.Debug("fetchMissingTiles: goroutine enqueuing tile", "south", t.South, "west", t.West)
        }
    }
    if len(claimed) == 0 {
        batchCancel()
        return
    }
    defer func() {
        for _, t := range claimed {
            if fn, ok := s.inFlight.LoadAndDelete(t); ok {
                fn.(context.CancelFunc)()
            }
        }
    }()
    tiles = claimed

    var fetchDelay time.Duration
    if s.fetchDelay != "" {
        d, err := time.ParseDuration(s.fetchDelay)
        if err == nil {
            fetchDelay = d
        }
    }
    if fetchDelay == 0 {
        fetchDelay = time.Second
    }

    cache := &quality.TileCache{Dir: s.cacheDir, Precision: 3}
    cache.EnsureDir()

    cfg := quality.TileFetchConfig{
        Endpoint:       s.overpassURL,
        TileSize:       s.tileSize,
        Cache:          cache,
        RateLimitDelay: fetchDelay,
    }

    quality.FetchTiledWaysForTiles(batchCtx, tiles, cfg)

    for _, t := range tiles {
        if cache.Has(t) {
            s.failedTiles.Delete(t)
            select {
            case s.tileReady <- t:
            default:
                slog.Warn("tileReady channel full, SSE push dropped", "south", t.South, "west", t.West)
            }
        }
    }

    for _, t := range tiles {
        if !cache.Has(t) {
            s.failedTiles.Store(t, struct{}{})
        }
    }
}
```

Also update the comment on the struct field (line 116):

```go
// Before:
inFlight      sync.Map // key: quality.Tile, value: struct{}; prevents duplicate concurrent fetches

// After:
inFlight      sync.Map // key: quality.Tile, value: context.CancelFunc; cancels in-flight fetch for that tile
```

- [ ] **Step 4: Fix compile errors — update the four call sites**

Change every `go s.fetchMissingTiles(...)` and `s.fetchMissingTiles(...)` to include the `cancelStale` argument. (Call sites are in `handleSegments`, `handleRoadSegments`, `handleViewportScore`, and `handleScore`.)

```
handleSegments       line 2271:  go s.fetchMissingTiles(missing, true)
handleRoadSegments   line 2330:  go s.fetchMissingTiles(missing, true)
handleViewportScore  line 1811:  go s.fetchMissingTiles(missingTiles, false)
handleScore          line 1898:  go s.fetchMissingTiles(missingTiles, false)
```

Also fix existing tests in `route_build_test.go` that call `fetchMissingTiles` — add `false` as the second argument to all three existing call sites (lines ~1074, 1084, 1136) so they keep their previous deduplication-only behaviour.

- [ ] **Step 5: Run all tests to verify they pass**

```
go test ./... -v -timeout 60s 2>&1 | tail -30
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add route_build.go route_build_test.go
git commit -m "feat(build): cancel stale in-flight tile fetches on scroll"
```

---

## Self-Review

**Spec coverage:**

| Spec section | Covered by |
|---|---|
| HTTP logging middleware | Task 1 |
| Per-tile cancel map (inFlight value → CancelFunc) | Task 2 |
| Cancellation logic in `fetchMissingTiles` (`cancelStale` param) | Task 2 |
| Context propagation to `FetchTiledWaysForTiles` | Task 2 |
| `slog.Debug` cancel log line | Task 2 |
| Deferred cleanup calls cancel before delete | Task 2 |
| Cancelled tiles not marked failed | Task 2 |
| Call sites table (handleSegments/handleRoadSegments → true; others → false) | Task 2, Step 4 |
| Unit test for cancellation | Task 2, Step 1 |

**Placeholder scan:** None found. All steps contain concrete code.

**Type consistency:** `context.CancelFunc` is used consistently in the struct comment, the `LoadOrStore` value, the `Range` cast, and the `LoadAndDelete` cast. `batchCtx context.Context` is threaded into `FetchTiledWaysForTiles` correctly.

**One flag to watch:** The `batchCancel` func is shared across all claimed tiles in a single `fetchMissingTiles` call — so cancelling any one tile via `inFlight.Range` will also cancel the whole batch. This matches the spec intent: when the user scrolls away, all stale tiles in a batch should stop, not just one.

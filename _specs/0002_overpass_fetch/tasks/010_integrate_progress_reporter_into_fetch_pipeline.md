---
# Task 010: Integrate ProgressReporter into FetchTiledWays

## Summary

Add a `Progress ProgressReporter` field to `TileFetchConfig` and wire it into `FetchTiledWays` so that `SetTotal`, `Tick`, and `Done` are called at the correct points in the tile loop. Default to `NoopProgressReporter` when the field is nil.

## Dependencies

Task 009 — `ProgressReporter` interface and `NoopProgressReporter` must exist.

## Detailed Directions

### 1. Add the field to `TileFetchConfig`

In `quality/tilefetch.go`, add a `Progress` field to the config struct:

```go
type TileFetchConfig struct {
    Endpoint       string
    TileSize       float64
    Cache          *TileCache
    NoCache        bool
    Logger         *slog.Logger
    RateLimitDelay time.Duration
    RetryDelay     time.Duration
    Progress       ProgressReporter // nil → NoopProgressReporter
}
```

### 2. Default nil to NoopProgressReporter at the top of FetchTiledWays

In the existing block that defaults nil/zero config fields, add:

```go
if cfg.Progress == nil {
    cfg.Progress = NoopProgressReporter{}
}
```

### 3. Call SetTotal after the tile list is computed

Immediately after `tiles := ComputeTiles(...)`, call:

```go
cfg.Progress.SetTotal(len(tiles))
```

### 4. Call Tick after each tile is processed

At the bottom of the tile loop, after the `if raw == nil { continue }` guard but before `allTileData = append(...)`, call:

```go
cfg.Progress.Tick(cached)
```

To support this, introduce a `cached bool` local variable at the top of each loop iteration:

```go
for _, tile := range tiles {
    var raw []byte
    cached := false

    if !cfg.NoCache && cfg.Cache.Has(tile) {
        // ...
        if err == nil {
            cacheHits++
            cached = true
        }
    }

    // ... fetch path unchanged ...

    if raw == nil {
        continue
    }

    cfg.Progress.Tick(cached)
    allTileData = append(allTileData, raw)
}
```

### 5. Call Done after the loop

Immediately after the loop and the summary log call, before calling `mergeAndDeduplicate`, add:

```go
cfg.Progress.Done()
```

## Acceptance Criteria

- [ ] `TileFetchConfig` has a `Progress ProgressReporter` field.
- [ ] A nil `Progress` value is replaced with `NoopProgressReporter{}` before the loop.
- [ ] `SetTotal` is called once with the correct tile count before the loop begins.
- [ ] `Tick(true)` is called for each tile served from cache.
- [ ] `Tick(false)` is called for each tile fetched from the API.
- [ ] `Tick` is NOT called for failed tiles (tiles where `raw == nil` after all retries).
- [ ] `Done` is called exactly once after the loop completes (including on the context-cancelled early-return path).
- [ ] All existing unit and integration tests continue to pass (`go test ./...`).

## Notes

- Failed tiles (where `raw` remains `nil`) should not call `Tick` — the total and current counts will diverge, which is acceptable and informative.
- The context-cancelled early-return path in the loop (after `sleepWithContext` returns an error) currently returns early via `mergeAndDeduplicate`. Ensure `Done()` is called on this path too — consider a `defer cfg.Progress.Done()` at the top of the function to guarantee it.

---

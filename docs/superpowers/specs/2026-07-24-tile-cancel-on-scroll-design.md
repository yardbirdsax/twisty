# Tile Cancel-on-Scroll Design

**Date:** 2026-07-24
**Status:** Approved

## Problem

When the user scrolls the map, `loadVisibleSegments()` fires on every `moveend`/`zoomend` event and kicks off a new tile fetch goroutine. Old goroutines keep running against Overpass with `context.Background()` — they are never cancelled. This creates a thundering-herd effect where many stale Overpass requests queue up even though the user has moved on.

## Goals

- Cancel in-flight Overpass tile fetches for tiles that are **not** in the new viewport when the user scrolls.
- Tiles that overlap the old and new viewport continue fetching uninterrupted.
- Add observable signal (HTTP request logging) to measure the effect before and after.

## Out of Scope

- Concurrency cap / semaphore (separate concern).
- Jitter on the rate-limit delay (separate concern).
- Cancellation for `handleViewportScore` (user-initiated, not scroll-driven).
- Cancellation for `handleTiles` (dead code — registered but never called from JS).

## Design

### 1. HTTP Logging Middleware

Wrap the mux in a thin logging handler that logs every incoming request at `Info` level:

```
method=GET path=/api/segments remote=127.0.0.1:54321
```

This is the primary observability signal: run `twisty build --verbose` while scrolling and watch the request rate in the terminal. Before the fix you see a flood of Overpass fetches; after, they stop promptly when you scroll away.

Implementation: a single `loggingMiddleware(next http.Handler) http.Handler` function wrapping the mux in `execBuild`.

### 2. Per-Tile Cancel Map

**Current state:** `buildServer.inFlight` is a `sync.Map` with `quality.Tile → struct{}`, used only to prevent duplicate concurrent fetches of the same tile.

**New state:** Change the value type to `context.CancelFunc`. Each claimed tile gets its own derived context and its cancel func stored in the map.

#### Cancellation logic in `fetchMissingTiles`

`fetchMissingTiles` gains an optional cancellation step, only executed when called from `handleSegments` or `handleRoadSegments`. A boolean parameter `cancelStale bool` controls this:

```
func (s *buildServer) fetchMissingTiles(tiles []quality.Tile, cancelStale bool)
```

When `cancelStale` is true:
1. Build a set of the incoming tiles.
2. Iterate `inFlight` with `Range`; for any tile not in the incoming set, call its `CancelFunc` and delete it from the map.
3. Proceed with the normal claim loop (skip tiles already in-flight, i.e. still in the map after step 2).

When `cancelStale` is false (viewport score, tiles endpoint): behave as today — no cancellation, just deduplication.

#### Context propagation

Each claimed tile gets:
```go
ctx, cancel := context.WithCancel(context.Background())
s.inFlight.Store(t, cancel)
```

`FetchTiledWaysForTiles` already propagates `ctx` through `sleepWithContext` and `retryWithBackoff`, so cancellation surfaces naturally mid-sleep or mid-retry. No changes needed to `tilefetch.go`.

Add a `slog.Debug` line at the point where a tile's cancel func is called:
```
slog.Debug("cancelling stale tile fetch", "south", t.South, "west", t.West)
```

#### Cleanup

The deferred cleanup in `fetchMissingTiles` currently deletes claimed tiles from `inFlight` when the goroutine exits. This stays, but now calls the cancel func before deleting. Calling `cancel()` on a context that already completed is a no-op in Go, so this is safe for tiles that fetched successfully:
```go
defer func() {
    for _, t := range claimed {
        if fn, ok := s.inFlight.LoadAndDelete(t); ok {
            fn.(context.CancelFunc)()
        }
    }
}()
```

#### Cancelled tiles are not marked failed

A tile cancelled mid-fetch is not written to `failedTiles`. It stays uncached and will be fetched again if the user scrolls back to that area.

### 3. Call Sites

| Handler | `cancelStale` |
|---|---|
| `handleSegments` | `true` |
| `handleRoadSegments` | `true` |
| `handleViewportScore` | `false` |
| `handleTiles` | `false` |

## Data Flow (scroll event)

1. User scrolls → browser fires `moveend` → `loadVisibleSegments()` → `GET /api/segments?bbox=B2`
2. `handleSegments` computes tiles for B2.
3. `go s.fetchMissingTiles(missing, true)` — cancels any in-flight tile not in B2's tile set, then claims unclaimed tiles with fresh contexts.
4. Goroutine calls `FetchTiledWaysForTiles(ctx, tiles, cfg)` — if `ctx` is cancelled during a sleep or retry, the loop exits early.
5. The log line `cancelling stale tile fetch` appears in `--verbose` output for each stopped tile.

## Testing

- **Unit test for cancellation logic:** call `fetchMissingTiles` with tile set A, then immediately with tile set B (overlapping by one tile). Assert: cancel func was called for tiles in A∖B; tile in A∩B was not cancelled and remains in-flight.
- **Existing tests:** `FetchTiledWaysForTiles` context-cancellation tests in `tilefetch_test.go` cover the fetch-side behavior — no changes needed there.
- **Manual verification:** run with `--verbose --fetch-delay 5s`, scroll rapidly, observe `cancelling stale tile fetch` lines and reduced Overpass request count in the terminal.

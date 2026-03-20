# Task 008: Concurrent Pipeline Architecture

## Summary

Replace the sequential tile processing and aggregation in `runScore()` with the concurrent two-phase pipeline described in the PRD. Phase A fans out tile processing with a worker pool and collects results at a barrier. Phase B fans out per-name-group processing (aggregation, penalties, KML fragment building) across workers.

## Dependencies

Task 007 — requires the sequential pipeline wiring to be in place first.

## Detailed Directions

### 1. Phase A: Concurrent Tile Processing

- Replace the sequential tile processing loop in `runScore()` with a worker pool pattern:
  - Create a channel of `Tile` work items
  - Launch `runtime.GOMAXPROCS(0)` worker goroutines, each reading tiles from the channel
  - Each worker: check score cache → on miss, parse tile data and run scoring pipeline → send `[]ScoredWay` to a results channel
  - A collector goroutine reads from the results channel and groups ways by `name` tag into a `map[string][]ScoredWay`
  - The collector completes when all workers are done (use `sync.WaitGroup`)

```go
func processTilesConcurrently(tiles []Tile, scoreCache *ScoreCache, ...) (map[string][]ScoredWay, Stats, error)
```

- Accumulate stats (cache hits, total ways, segments, etc.) using atomic counters or a mutex-protected struct.
- Progress reporting should still work — `ProgressReporter.Tick()` calls need synchronization.

### 2. Phase B: Concurrent Per-Name-Group Processing

- After the barrier (all tiles collected and grouped), dispatch name groups to a worker pool:
  - Each worker receives a name group (`string` key + `[]ScoredWay` value)
  - Worker runs: connected components → ordering → splitting → scoring → penalty application
  - Worker sends `[]RoadCollection` to an output channel

```go
func processNameGroupsConcurrently(groups map[string][]ScoredWay) ([]RoadCollection, error)
```

### 3. Assembly

- Collect all `RoadCollection` results from Phase B
- Sort by `PenalizedScore` descending
- Apply min-score filter
- Write KML

### 4. Error Handling

- If any worker in either phase encounters an error, propagate it back.
- Use `context.Context` for cancellation — if one worker fails, cancel remaining work.
- Return the first error encountered.

### 5. Update `runScore()`

- Replace the existing sequential logic with calls to `processTilesConcurrently` and `processNameGroupsConcurrently`.
- Keep all existing flag handling and output logic.

### 6. Write Tests

- Test concurrent tile processing produces same results as sequential (use synthetic tile data).
- Test concurrent name group processing produces same results as calling `Aggregate` sequentially.
- Test error propagation from worker failures.
- Test with varying GOMAXPROCS values (1, 2, 4).

## Acceptance Criteria

- [ ] Tile processing runs concurrently with bounded worker pool
- [ ] Name group processing runs concurrently after barrier
- [ ] Output is identical to sequential pipeline (deterministic after sorting)
- [ ] Progress reporting still works correctly
- [ ] Errors propagate cleanly from workers
- [ ] No data races (passes `go test -race`)
- [ ] All existing tests still pass

## Notes

- The barrier between Phase A and Phase B is fundamental — aggregation needs all ways for a named road, which may span multiple tiles.
- Worker pool size defaults to `runtime.GOMAXPROCS(0)` but could be made configurable via a flag in the future (not in scope for this task).
- Test with `-race` flag to catch data races: `go test -race ./...`
- Keep the concurrent code in `main.go` or extract to a new file like `pipeline.go` — whichever keeps things cleaner.

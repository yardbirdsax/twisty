# Task 007: CLI Flags and Pipeline Wiring

## Summary

Extend the `score` subcommand with `-out` and `-min-score` flags, and wire stages 5-7 into the `runScore()` function. After this task, the CLI produces KML output end-to-end.

## Dependencies

Task 001, Task 004, Task 005, Task 006 — requires tag enrichment, aggregation, penalties, and KML generation.

## Detailed Directions

### 1. Add CLI Flags

- In `main.go`, in the `runScore` function where the flag set is defined, add:

```go
outPath := scoreFlags.String("out", "", "output KML file path (required)")
minScore := scoreFlags.Float64("min-score", 0, "minimum penalized score to include in output")
```

- After flag parsing, validate that `-out` is non-empty. Return an error if missing.

### 2. Collect All Scored Ways

- After the existing tile processing loop, collect all `ScoredWay` results from all tiles into a single `[]ScoredWay` slice. The existing code already accumulates results per-tile — gather them into one slice for aggregation input.

### 3. Wire Stages 5-7

- After tile processing, add:

```go
// Stage 5: Aggregate
collections := quality.Aggregate(allScoredWays)

// Stage 6: Penalties
quality.ApplyPenalties(collections)

// Stage 7: KML Output
f, err := os.Create(*outPath)
if err != nil {
    return fmt.Errorf("creating output file: %w", err)
}
defer f.Close()

if err := quality.WriteKML(f, collections, *minScore); err != nil {
    return fmt.Errorf("writing KML: %w", err)
}
```

### 4. Print Summary to Stderr

- After KML output, print a summary to `stderr` (the `stderr` writer parameter):
  - Number of road collections produced
  - Number of collections after min-score filtering
  - Top 5 roads by penalized score (name and score)
  - Output file path

- Example output:
```
Aggregated 142 road collections (87 above min-score 300).
Top roads:
  1. Route 100         — 1523
  2. Mountain Pass Rd   — 1204
  3. Skyline Drive      — 987
Output: roads.kml
```

### 5. Handle Edge Cases

- If no roads pass the min-score filter, write a valid empty KML document and print a warning to stderr.
- If there are no scored ways at all (all tiles empty), skip aggregation and write empty KML.

### 6. Update Existing Tests

- Update `main_test.go` flag parsing tests to include `-out` and `-min-score` flags.
- Add a test that verifies `-out` is required (error when missing).
- Add a test that verifies the full flow produces a KML file (using synthetic cached tile data).

## Acceptance Criteria

- [ ] `-out` flag is required; error message if missing
- [ ] `-min-score` flag defaults to 0
- [ ] Stages 5-7 execute in order after tile processing
- [ ] KML file is written to the specified output path
- [ ] Summary printed to stderr with collection count, top roads, and output path
- [ ] Empty KML produced when no roads pass filter (with warning)
- [ ] Existing flags retain their behavior
- [ ] All tests pass

## Notes

- The `-out` flag should accept relative or absolute paths. Use `os.Create` which handles both.
- Summary output goes to the `stderr` writer parameter, not `os.Stderr` directly, so it's testable.
- This task wires everything sequentially. The concurrent pipeline optimization is a separate task.

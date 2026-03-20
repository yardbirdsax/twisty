# Task 007: CLI Flags and Orchestration Wiring

## Summary

Refactor `main.go` to use a subcommand model (`route` and `fetch`) backed by `flag.FlagSet`, then wire the `FetchTiledWays` function into the `fetch` subcommand. This eliminates the flat-flag mutual-exclusivity hack and scopes each subcommand's flags to its own usage output.

## Dependencies

Task 002 (tile computation), Task 004 (tile cache), Task 005 (fetch orchestration), Task 006 (merge/dedup).

## Detailed Directions

### 1. Add subcommand dispatch to `main.go`

- Check `os.Args[1]` for the subcommand name. If missing or unrecognized, print usage and exit.
- Supported subcommands: `route`, `fetch`.
- Each subcommand gets its own `flag.FlagSet` (named after the subcommand, using `flag.ExitOnError`).
- Call `flagSet.Parse(os.Args[2:])` in the matching branch.

```go
if len(os.Args) < 2 {
    fmt.Fprintln(os.Stderr, "Usage: twisty <route|fetch> [flags]")
    os.Exit(1)
}
switch os.Args[1] {
case "route":
    runRoute(os.Args[2:])
case "fetch":
    runFetch(os.Args[2:])
default:
    fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
    os.Exit(1)
}
```

### 2. Extract `runRoute` function

- Move all existing route pipeline logic (flags + pipeline stages) into a `runRoute(args []string)` function.
- Flags use a local `flag.FlagSet` instead of the global `flag` package:
  ```go
  fs := flag.NewFlagSet("route", flag.ExitOnError)
  origin  := fs.String("origin", "", "Origin address or lat,lon")
  dest    := fs.String("dest", "", "Destination address or lat,lon")
  twist   := fs.Float64("twist", 0.5, "0.0 = fastest, 1.0 = twistiest")
  out     := fs.String("out", "route.gpx", "Output file path")
  showAll := fs.Bool("show-all", false, "Print candidate comparison table")
  verbose := fs.Bool("v", false, "Enable verbose timing logs to stderr")
  fs.Parse(args)
  ```
- Existing pipeline logic is otherwise unchanged.

### 3. Add `runFetch` function

- Implement a `runFetch(args []string)` function with its own `flag.FlagSet`:
  ```go
  fs := flag.NewFlagSet("fetch", flag.ExitOnError)
  address        := fs.String("address", "", "Address for curvature pipeline center point")
  radius         := fs.Float64("radius", 25.0, "Search radius in km (max 50)")
  tileSize       := fs.Float64("tile-size", 0.05, "Tile size in degrees")
  cacheDir       := fs.String("cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
  noCache        := fs.Bool("no-cache", false, "Bypass cache reads (still writes)")
  clearCache     := fs.Bool("clear-cache", false, "Delete all cached tiles before fetching")
  purgeOlderThan := fs.String("purge-older-than", "", "Purge cache files older than duration (e.g., 90d, 6m)")
  verbose        := fs.Bool("v", false, "Enable verbose timing logs to stderr")
  fs.Parse(args)
  ```

### 4. Validate inputs in `runFetch`

- `--address` is required. If empty, print error and exit.
- `--radius` must be between 0 and 50 km. If > 50, print error and exit.
- `--tile-size` must be > 0.

### 5. Resolve default cache directory in `runFetch`

- If `--cache-dir` is empty, default to `~/.twisty/cache/overpass/`:
  ```go
  if *cacheDir == "" {
      home, err := os.UserHomeDir()
      if err != nil {
          log.Fatalf("cannot determine home directory: %v", err)
      }
      *cacheDir = filepath.Join(home, ".twisty", "cache", "overpass")
  }
  ```

### 6. Parse `--purge-older-than` duration

- Write a helper `parseDuration(s string) (time.Duration, error)` that supports:
  - `Nd` — N days (e.g., `90d` → 90 * 24 hours)
  - `Nm` — N months, approximate as N * 30 days (e.g., `6m` → 180 days)
  - Standard Go durations (e.g., `24h`)
- This is a simple parser, not a full-featured duration library.

### 7. Wire the tiled fetch pipeline in `runFetch`

1. Geocode the address using `geocode.Resolve(*address, "Center")`.
2. Handle `--clear-cache`: call `cache.ClearAll()` and log.
3. Handle `--purge-older-than`: parse duration, call `cache.PurgeOlderThan(dur)`, log count.
4. Call `FetchTiledWays` with the resolved coordinates, radius, and config.
5. Log the result: number of deduplicated ways.
6. For now (Phase 1), print a summary and exit. Pipeline integration with scoring stages is Phase 2.

### 8. Add verbose logging

- Use `slog.Logger` consistently with the existing pattern in `main.go`.
- Log at appropriate levels:
  - Info: pipeline start, total tiles, fetch progress, summary
  - Debug: individual cache hits, tile coordinates
  - Warn: tile fetch failures

## Acceptance Criteria

- [ ] `twisty route --origin X --dest Y` works exactly as the old `twisty --origin X --dest Y` did
- [ ] `twisty fetch --address X` triggers the tiled fetch pipeline
- [ ] `--address` is required for `fetch`; missing it prints an error and exits
- [ ] `--radius` validated with max 50 km
- [ ] `--tile-size` configurable, defaults to 0.05°
- [ ] `--cache-dir` configurable, defaults to `~/.twisty/cache/overpass/`
- [ ] `--no-cache` bypasses cache reads
- [x] `--clear-cache` deletes all cache before running
- [x] `--purge-older-than` purges old cache files (supports `Nd` and `Nm` format)
- [ ] Unknown subcommand prints usage and exits non-zero
- [ ] `go build ./...` succeeds

## Notes

- No third-party CLI libraries. Use `flag.FlagSet` from the standard library only.
- This task intentionally does not integrate with the downstream scoring pipeline. That's Phase 2. The tiled fetch output is logged/summarized but not passed to scoring stages.
- The `parseDuration` helper should be tested separately — add a few unit tests in `main_test.go` or a helper test file.
- The existing flat-flag `main.go` (produced during initial implementation) should be fully replaced by this subcommand structure.

---
# Task 007 Review: CLI Flags and Orchestration Wiring

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

This task refactored `main.go` to a subcommand model (`route` and `fetch`) backed by `flag.FlagSet`, wiring `FetchTiledWays` into the `fetch` subcommand. All acceptance criteria are met. The previously identified geocode-before-cache-maintenance ordering bug has been fixed.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `twisty route --origin X --dest Y` works exactly as the old flat-flag invocation did | PASS |
| `twisty fetch --address X` triggers the tiled fetch pipeline | PASS |
| `--address` required for `fetch`; missing prints error and exits | PASS |
| `--radius` validated with max 50 km | PASS |
| `--tile-size` configurable, defaults to 0.05 degrees | PASS |
| `--cache-dir` configurable, defaults to `~/.twisty/cache/overpass/` | PASS |
| `--no-cache` bypasses cache reads | PASS |
| `--clear-cache` deletes all cache before running | PASS |
| `--purge-older-than` purges old cache files (supports `Nd` and `Nm` format) | PASS |
| Unknown subcommand prints usage and exits non-zero | PASS |
| `go build ./...` succeeds | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
go build ./...                          # PASS — builds cleanly
go test -run TestParseDuration ./...    # PASS — all parseDuration cases pass
go vet ./...                            # PASS — no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The geocode-before-cache-maintenance ordering issue from the previous review has been corrected: `runFetch` now performs `clear-cache` and `purge-older-than` before making any network call. The `parseDuration` helper is tested with appropriate coverage including the documented `m`-means-months convention.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.

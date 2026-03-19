# Task 007: CLI Flags and Orchestration Wiring

## Summary

Add the CLI flags for the tiled fetch system (`--address`, `--radius`, `--tile-size`, `--cache-dir`, `--no-cache`, `--clear-cache`, `--purge-older-than`) and wire the `FetchTiledWays` function into the application's pipeline. This task makes the tiled fetch accessible to users.

## Dependencies

Task 002 (tile computation), Task 004 (tile cache), Task 005 (fetch orchestration), Task 006 (merge/dedup).

## Detailed Directions

### 1. Add CLI flags to `main.go`

- Add the following flags using Go's `flag` package (consistent with existing flag style):
  ```go
  address   := flag.String("address", "", "Address for curvature pipeline center point")
  radius    := flag.Float64("radius", 25.0, "Search radius in km (max 50)")
  tileSize  := flag.Float64("tile-size", 0.05, "Tile size in degrees")
  cacheDir  := flag.String("cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
  noCache   := flag.Bool("no-cache", false, "Bypass cache reads (still writes)")
  clearCache := flag.Bool("clear-cache", false, "Delete all cached tiles before fetching")
  purgeOlderThan := flag.String("purge-older-than", "", "Purge cache files older than duration (e.g., 90d, 6m)")
  ```

### 2. Validate inputs

- After `flag.Parse()`, add validation:
  - If `--address` is provided, validate:
    - `--radius` is between 0 and 50 km. If > 50, print error and exit.
    - `--tile-size` is > 0.
  - The `--address` flag triggers the tiled fetch pipeline (it's a separate mode from the existing route pipeline).

### 3. Resolve default cache directory

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

### 4. Parse `--purge-older-than` duration

- Write a helper `parseDuration(s string) (time.Duration, error)` that supports:
  - `Nd` — N days (e.g., `90d` → 90 * 24 hours)
  - `Nm` — N months, approximate as N * 30 days (e.g., `6m` → 180 days)
  - Standard Go durations (e.g., `24h`)
- This is a simple parser, not a full-featured duration library.

### 5. Wire the tiled fetch pipeline

- When `--address` is provided, execute the tiled fetch pipeline:
  1. Geocode the address using `geocode.Resolve(*address, "Center")`.
  2. Handle `--clear-cache`: call `cache.ClearAll()` and log.
  3. Handle `--purge-older-than`: parse duration, call `cache.PurgeOlderThan(dur)`, log count.
  4. Call `FetchTiledWays` with the resolved coordinates, radius, and config.
  5. Log the result: number of deduplicated ways.
  6. For now (Phase 1), print a summary and exit. The pipeline integration with scoring stages is Phase 2.

### 6. Keep existing pipeline untouched

- The existing route pipeline (triggered by `--origin` and `--dest`) must continue to work exactly as before.
- The `--address` flag is mutually exclusive with `--origin`/`--dest`. If both are provided, print an error and exit.

### 7. Add verbose logging

- Use `slog.Logger` consistently with the existing pattern in `main.go`.
- Log at appropriate levels:
  - Info: pipeline start, total tiles, fetch progress, summary
  - Debug: individual cache hits, tile coordinates
  - Warn: tile fetch failures

## Acceptance Criteria

- [ ] `--address` flag triggers the tiled fetch pipeline
- [ ] `--radius` validated with max 50 km
- [ ] `--tile-size` configurable, defaults to 0.05°
- [ ] `--cache-dir` configurable, defaults to `~/.twisty/cache/overpass/`
- [ ] `--no-cache` bypasses cache reads
- [ ] `--clear-cache` deletes all cache before running
- [ ] `--purge-older-than` purges old cache files (supports `Nd` and `Nm` format)
- [ ] `--address` is mutually exclusive with `--origin`/`--dest`
- [ ] Existing route pipeline is unaffected
- [ ] `go build ./...` succeeds

## Notes

- This task intentionally does not integrate with the downstream scoring pipeline. That's Phase 2. The tiled fetch output is logged/summarized but not passed to scoring stages.
- The `parseDuration` helper should be tested separately — add a few unit tests in `main_test.go` or a helper test file.
- Flag naming uses kebab-case with hyphens (e.g., `--cache-dir`), consistent with common Go CLI conventions.

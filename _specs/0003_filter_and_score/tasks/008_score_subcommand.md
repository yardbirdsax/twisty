# Task 008: `score` Subcommand

## Summary

Wire up the `twisty score` CLI subcommand that runs stages 2–4 over previously fetched tiles, using the score cache for efficiency, and prints a console summary.

## Dependencies

- Task 006 (score pipeline — `RunScorePipeline`, `ScorePipelineResult`)
- Task 007 (score cache — `ScoreCache`)

## Detailed Directions

### 1. Add the `score` subcommand to `main.go`

- Follow the existing pattern used by the `fetch` subcommand.
- Register a new `flag.FlagSet` named `"score"`.
- Define flags:
  - `-address` (string, required): location center.
  - `-radius` (float64, default 25.0): search radius in km, validated 0–50.
  - `-tile-size` (float64, default 0.05): tile edge in degrees.
  - `-cache-dir` (string, default `~/.twisty/cache/overpass/`): cache directory for raw tiles.
  - `-no-cache` (bool): skip score cache reads (still writes).
  - `-clear-score-cache` (bool): delete all score cache entries before running.
  - `-v` (bool): verbose logging.

### 2. Implement the score command logic

- The function should follow this flow:
  1. Parse and validate flags. If `-address` is empty, print usage and exit.
  2. Geocode the address using `geocode.Nominatim()` (same as `fetch`).
  3. Compute tiles using `quality.ComputeTiles()` (same as `fetch`).
  4. Initialize the `TileCache` (for reading raw tile data) and `ScoreCache` (for reading/writing scored data).
  5. Call `scoreCache.EnsureDir()`.
  6. If `-clear-score-cache` is set, call `scoreCache.ClearAll()`.
  7. Initialize aggregate stats: total tiles, cache hits, total ways scored, total segments, total zeroed.
  8. For each tile:
     a. Check if raw tile data exists in the `TileCache`. If not, log a warning and skip.
     b. Read the raw tile data from `TileCache`.
     c. If not `-no-cache`, check the `ScoreCache`. If hit, increment cache hits and continue.
     d. Parse ways from raw tile data using `parseTileData()` (this is unexported in `tilefetch.go` — see note below).
     e. Run `RunScorePipeline(ways)`.
     f. Write scored ways to the `ScoreCache`.
     g. Accumulate stats from `ScorePipelineResult`.
  9. Print summary to stdout:
     ```
     Score complete.
       Tiles processed: 42
       Cache hits:       38
       Ways scored:      1234
       Segments scored:  56789
       Zeroed by deflection: 4321
     ```
  10. If no tiles had raw data, print an error: `"No cached tile data found. Run 'twisty fetch -address \"...\"' first."`

### 3. Export `parseTileData` or create an accessor

- The existing `parseTileData` in `tilefetch.go` is unexported. You have two options:
  - **Option A**: Export it as `ParseTileData`. This is the simpler approach since the function is a pure parser with no side effects.
  - **Option B**: Create a `TileCache.ReadWays(t Tile) ([]Way, error)` method that reads + parses in one step.
- Option A is recommended for simplicity.

### 4. Add the subcommand to the main dispatch

- In the `main()` function, add `"score"` to the subcommand dispatch switch statement, following the pattern of `"fetch"`.

### 5. Write tests in `main_test.go`

- Test that the score flag set parses correctly with valid flags.
- Test that `-address` is required (empty address prints usage / returns error).
- Test that `-radius` validation rejects values > 50 and < 0.
- Do NOT write integration tests that require real geocoding or tile data — those are for Task 009.

## Acceptance Criteria

- [ ] `twisty score -address "..." -radius 25` runs end-to-end when tile data exists.
- [ ] The command reads from the existing tile cache and does NOT fetch from the Overpass API.
- [ ] Score cache is used: second run over the same area shows all cache hits.
- [ ] `-no-cache` skips score cache reads but still writes.
- [ ] `-clear-score-cache` deletes all score cache entries before running.
- [ ] Console summary prints accurate statistics.
- [ ] Missing tile data produces a warning per tile and an error message if no tiles have data.
- [ ] `-v` enables verbose logging.
- [ ] All existing tests continue to pass.

## Notes

- Reuse as much infrastructure as possible from the `fetch` command: geocoding, `ComputeTiles`, `TileCache`, home directory resolution, flag validation patterns.
- The score cache directory is derived from the cache dir: if cache dir is `~/.twisty/cache/overpass/`, the score cache dir is `~/.twisty/cache/scores/`. Use `filepath.Join(filepath.Dir(cacheDir), "scores")` or similar.
- Tiles are processed sequentially — no goroutines needed per the PRD's non-goals.

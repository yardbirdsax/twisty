---
# Task 002: Implement Cobra Commands (Green)

## Summary

Wire the existing `run*` business logic into a Cobra command tree so that the tests written in Task 001 compile and pass. Replace the hand-rolled `os.Args` switch in `main()` and all `flag.NewFlagSet` flag parsing with Cobra command factories. Each subcommand maps to a factory function (`newScoreCmd`, `newFetchCmd`, `newRouteCmd`, `newRandomCmd`, `newOverpassCmd`) that binds flags and calls the extracted logic.

## Dependencies

Task 001 — tests must already be updated (red) before this task begins.

## Detailed Directions

### 1. Scaffold the root command in `main.go`

Replace the `main()` switch with a Cobra root command:

```go
func main() {
    root := newRootCmd()
    if err := root.Execute(); err != nil {
        os.Exit(1)
    }
}

func newRootCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:           "twisty",
        Short:         "Twisty — motorcycle route finder",
        SilenceUsage:  true,
        SilenceErrors: true,
    }
    cmd.AddCommand(
        newRouteCmd(),
        newFetchCmd(),
        newScoreCmd(),
        newRandomCmd(),
        newOverpassCmd(),
    )
    return cmd
}
```

### 2. Implement `newScoreCmd()`

Extract flag declarations from `runScore` into the command factory. Keep the body of `runScore` as a private helper — rename it to `execScore` (or similar) — but change its signature to accept resolved values rather than `[]string`:

```go
func newScoreCmd() *cobra.Command {
    var (
        address         string
        radius          float64
        tileSize        float64
        cacheDir        string
        noCache         bool
        clearScoreCache bool
        verbose         bool
        outPath         string
        minScore        float64
        multiColor      bool
        overpassURL     string
        fetchDelay      string
    )
    cmd := &cobra.Command{
        Use:           "score",
        Short:         "Score road twistiness in a radius and output KML",
        SilenceUsage:  true,
        SilenceErrors: true,
        RunE: func(cmd *cobra.Command, args []string) error {
            return execScore(execScoreParams{
                address:         address,
                radius:          radius,
                tileSize:        tileSize,
                cacheDir:        cacheDir,
                noCache:         noCache,
                clearScoreCache: clearScoreCache,
                verbose:         verbose,
                outPath:         outPath,
                minScore:        minScore,
                multiColor:      multiColor,
                overpassURL:     overpassURL,
                fetchDelay:      fetchDelay,
                stderr:          cmd.ErrOrStderr(),
            })
        },
    }
    f := cmd.Flags()
    f.StringVar(&address, "address", "", "Location center address (required)")
    f.Float64Var(&radius, "radius", 25.0, "Search radius in km")
    f.Float64Var(&tileSize, "tile-size", 0.1, "Tile size in degrees")
    f.StringVar(&cacheDir, "cache-dir", "", "Overpass tile cache directory (default: ~/.twisty/cache/overpass/)")
    f.BoolVar(&noCache, "no-cache", false, "Skip score cache reads (still writes)")
    f.BoolVar(&clearScoreCache, "clear-score-cache", false, "Delete all score cache entries before running")
    f.BoolVar(&verbose, "v", false, "Enable verbose logging to stderr")
    f.StringVar(&outPath, "out", "", "Output KML file path (required)")
    f.Float64Var(&minScore, "min-score", 0, "Minimum penalized score to include in output")
    f.BoolVar(&multiColor, "multi-color", false, "Use per-segment tier coloring instead of single-color per-road")
    f.StringVar(&overpassURL, "overpass-url", quality.OverpassBaseURL, "Overpass API endpoint URL")
    f.StringVar(&fetchDelay, "fetch-delay", "", "delay between tile fetches in Go duration format (e.g. 500ms, 2s); default 1s")
    return cmd
}
```

Rename the existing `runScore` body to `execScore(p execScoreParams) error`. Create a simple params struct to keep the call site readable.

**Validation** — move the `-address is required` / `-out is required` / `-radius must be > 0` checks into `execScore`, same as today.

### 3. Implement `newRandomCmd()`

Same pattern as `newScoreCmd`. Move flag declarations from `runRandom` into the factory. Replace `log.Fatalf` calls inside the old `runRandom` body with `return fmt.Errorf(...)` so errors propagate through `RunE`.

Rename body to `execRandom(p execRandomParams) error`.

### 4. Implement `newFetchCmd()`

Move flag declarations from `runFetch` into the factory. Rename body to `execFetch`. Change `os.Exit` calls to `return fmt.Errorf(...)`. Include the `--fetch-delay` flag (string, default `""`, same semantics as `score`).

### 5. Implement `newRouteCmd()`

Move flag declarations from `runRoute` into the factory. Rename body to `execRoute`. Change `os.Exit` calls to `return fmt.Errorf(...)`.

### 6. Implement `newOverpassCmd()`

`overpass.go` currently has its own `runOverpass(args []string)` dispatcher that calls sub-handlers (`runOverpassStart`, `runOverpassStop`, etc.) each using `flag.NewFlagSet`. Convert these to a Cobra subcommand tree:

```
twisty overpass
  start   [--data-dir] [--port]
  stop
  status
  clean
  logs    [--lines]
  build   [--src-dir]
```

Each becomes a `*cobra.Command` added to the `overpassCmd`. No signature change for the body helpers is strictly required if the helpers use `log.Fatalf` internally (those callers exit the process), but prefer converting to `return fmt.Errorf(...)` for consistency.

### 7. Remove the old `main()` dispatcher and dead imports

After creating command factories, delete:
- The `switch os.Args[1]` block in `main()`
- Any `flag` import in `main.go` and `overpass.go` that is no longer used
- The former `runScore`, `runRandom`, `runFetch`, `runRoute`, `runOverpass` top-level functions (their bodies now live in `exec*` helpers or directly in `RunE`)

### 8. Run tests

```
go test ./... -count=1
```

All tests in Task 001 must pass. Run the short-skip integration tests separately if needed:

```
go test ./... -count=1 -short
go test -run TestRunScoreE2EWithSyntheticCache -v -count=1
go test -run TestRunRandomE2EWithSyntheticCache -v -count=1
```

## Acceptance Criteria

- [ ] `go build ./...` succeeds with no errors.
- [ ] `go test ./... -count=1 -short` passes with no failures.
- [ ] `go test -run TestRunScoreE2EWithSyntheticCache -v -count=1` passes.
- [ ] `go test -run TestRunRandomE2EWithSyntheticCache -v -count=1` passes.
- [ ] `go vet ./...` produces no warnings.
- [ ] `twisty --help` lists route, fetch, score, random, overpass subcommands.
- [ ] `twisty score --help` lists all flags previously available on the `score` subcommand.
- [ ] No `flag.NewFlagSet` calls remain in `main.go` or `overpass.go`.
- [ ] No `os.Exit` calls remain inside `exec*` helpers (only in `main()`).

## Notes

- Keep `SilenceUsage: true` and `SilenceErrors: true` on every command so test stderr buffers do not accumulate cobra's usage text.
- `cmd.ErrOrStderr()` returns the writer set via `cmd.SetErr(...)` in tests, falling back to `os.Stderr` in production — use it everywhere the old code wrote to `stderr io.Writer`.
- `parseDuration`, `stageTimer`, `formatDuration`, `termProgressBar`, `tileSetDifference`, `tileKey`, `collectAllPoints`, `printSummary`, `isTerminal`, `randomAttempt`, and `scoreAndAggregateTiles` are internal helpers — leave them in place unchanged.
- cobra/pflag flag names use the same strings as the current `flag.FlagSet` definitions (e.g., `"address"`, `"radius"`, `"tile-size"`) so existing CLI usage is preserved.
- The `overpass.go` sub-handlers (`runOverpassStart`, etc.) already receive parsed values through local vars; the refactor is mechanical — wrap each in a `*cobra.Command` and move the `flag.NewFlagSet` block into the factory.

---

---
# Task 002 Review: Implement Cobra Commands (Green)

**Reviewer:** Principal Engineer
**Date:** 2026-04-08
**Verdict:** NEEDS REVISION

---

## Summary

Replaced the hand-rolled `os.Args` switch in `main()` with a Cobra command tree. Each subcommand has a factory function (`newScoreCmd`, `newFetchCmd`, `newRouteCmd`, `newRandomCmd`, `newOverpassCmd`) that binds flags and delegates to an `exec*` helper.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go build ./...` succeeds | PASS |
| `go test ./... -count=1 -short` passes | PASS |
| `go test -run TestRunScoreE2EWithSyntheticCache` passes | PASS |
| `go test -run TestRunRandomE2EWithSyntheticCache` passes | PASS |
| `go vet ./...` produces no warnings | PASS |
| `twisty --help` lists all subcommands | PASS |
| `twisty score --help` lists all flags | PASS |
| No `flag.NewFlagSet` in `main.go` or `overpass.go` | PASS |
| No `os.Exit` in `exec*` helpers | PASS |

---

## MUST FIX

### 1. Blank import of `cobra` in test file

**File:** `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go`
**Line:** 15

**Issue:** `_ "github.com/spf13/cobra"` is a no-op blank import with no side effects and must be removed.

**Current:**
```go
_ "github.com/spf13/cobra"
```

**Required:** Remove the line entirely.

**Rationale:** Dead imports are disallowed by standard Go tooling and the project linter. This import does nothing.

---

## Good Practices Observed

None noted per reviewer instructions.

---

## Verification Commands Run

```bash
go build ./...                                          # PASS
go vet ./...                                            # PASS
go test ./... -count=1 -short                          # PASS
go test -run TestRunScoreE2EWithSyntheticCache -v      # PASS
go test -run TestRunRandomE2EWithSyntheticCache -v     # PASS (4.05s)
grep 'flag\.NewFlagSet' main.go overpass.go            # no matches
grep 'os\.Exit' main.go                                # only main.go:29 inside main()
grep '_ "github.com/spf13/cobra"' main_test.go        # main_test.go:15 — blank import found
```

---

## Final Verdict

**NEEDS REVISION**

All acceptance criteria pass. One blocking issue: the blank `_ "github.com/spf13/cobra"` import in `main_test.go:15` must be removed before this is merge-ready.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.

---

## Revision Notes

**Issue 1 fixed:** Removed blank `_ "github.com/spf13/cobra"` import from `main_test.go:15`. `go test ./... -count=1 -short` passes.

---

---
# Task 002 Review: Implement Cobra Commands (Green) — Re-review

**Reviewer:** Principal Engineer
**Date:** 2026-04-08
**Verdict:** APPROVED

---

## Summary

All acceptance criteria pass. The previously blocking blank `_ "github.com/spf13/cobra"` import has been removed from `main_test.go`.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go build ./...` succeeds | PASS |
| `go test ./... -count=1 -short` passes | PASS |
| `go test -run TestRunScoreE2EWithSyntheticCache` passes | PASS |
| `go test -run TestRunRandomE2EWithSyntheticCache` passes | PASS |
| `go vet ./...` produces no warnings | PASS |
| `twisty --help` lists all subcommands | PASS |
| `twisty score --help` lists all flags | PASS |
| No `flag.NewFlagSet` in `main.go` or `overpass.go` | PASS |
| No `os.Exit` in `exec*` helpers | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go build ./...                                          # PASS
go vet ./...                                            # PASS
go test ./... -count=1 -short                          # PASS
go test -run TestRunScoreE2EWithSyntheticCache -v      # PASS
go test -run TestRunRandomE2EWithSyntheticCache -v     # PASS (4.04s)
grep 'flag\.NewFlagSet' main.go overpass.go            # no matches
grep 'os\.Exit' main.go                                # only main.go:29 inside main()
grep 'cobra' main_test.go                              # no blank import found
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass and the previously flagged blank import has been removed. Ready to merge.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.

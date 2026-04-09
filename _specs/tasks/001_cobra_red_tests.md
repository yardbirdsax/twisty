---
# Task 001: Rewrite Tests for Cobra (Red)

## Summary

Update `main_test.go` to compile against the Cobra command API that does not exist yet, intentionally producing compile errors (red phase). Remove tests that test `flag.FlagSet` internals; replace tests that call `runScore`/`runRandom` directly with tests that use Cobra's `SetArgs`/`Execute` pattern via command factory functions (`newScoreCmd`, `newRandomCmd`). The tests must fail to compile until Task 002 is complete.

## Dependencies

None — this is the foundational task.

## Detailed Directions

### 1. Add the Cobra dependency

Add `github.com/spf13/cobra` to `go.mod` and `go.sum` before editing tests so the import resolves cleanly:

```
go get github.com/spf13/cobra@latest
```

The `flag` import will remain in `main_test.go` only if any test still needs it directly — remove it if it becomes unused.

### 2. Delete `TestScoreFlagSetParsesValidFlags`

This test exercises `flag.NewFlagSet` internals that will no longer exist after the migration. Delete it entirely.

### 3. Replace `runScore([]string, io.Writer)` call sites with Cobra command execution

All tests that currently call `runScore(args, &stderr)` must be rewritten to:

```go
cmd := newScoreCmd()
cmd.SetArgs(args)
cmd.SetErr(&stderr)
err := cmd.Execute()
```

Affected tests (update each in place, do not rename):
- `TestScoreFlagSetAddressRequired`
- `TestScoreOutFlagRequired`
- `TestScoreMinScoreDefaultsToZero` — delete this test; the default is validated by cobra's flag default mechanism and does not need an explicit test.
- `TestScoreRadiusValidation`
- `TestRunScoreE2EWithSyntheticCache`
- `TestRunScore_DefaultSingleColorOutput`
- `TestRunScore_MultiColorFlag`
- `TestScoreFetchDelayFlagAccepted`
- `TestScoreFetchDelayFlagInvalid`

For `TestScoreRadiusValidation`, the stub overpass server and test table loop stay the same; only the invocation changes.

### 4. Replace `runRandom([]string)` call sites with Cobra command execution

```go
cmd := newRandomCmd()
cmd.SetArgs(args)
err := cmd.Execute()
```

Affected test:
- `TestRunRandomE2EWithSyntheticCache`

`runRandom` currently calls `log.Fatalf` on errors; after the migration it will return an error via `RunE`, so the test should assert `err == nil` (or the relevant non-nil case) rather than relying on fatal exits.

### 5. Verify the import block

After edits, `main_test.go` should import `github.com/spf13/cobra` (for `*cobra.Command` in any helper, if needed) but the test code itself only needs the standard library plus `quality`. The `flag` import must be removed if it is no longer used.

### 6. Confirm the package still declares `package main`

No package rename. All files in the root remain `package main`.

## Acceptance Criteria

- [ ] `go build ./...` fails with "undefined: newScoreCmd" and "undefined: newRandomCmd" (compile-red).
- [ ] No test in `main_test.go` references `flag.NewFlagSet` or calls `runScore`/`runRandom` directly.
- [ ] `TestScoreFlagSetParsesValidFlags` and `TestScoreMinScoreDefaultsToZero` are deleted.
- [ ] All other test names and logic are preserved; only the invocation mechanism changes.
- [ ] `go vet ./...` produces no issues in test files beyond the expected undefined-symbol errors.

## Notes

- Do **not** touch `overpass_test.go`, `pipeline_test.go`, or any `quality/` tests — they do not call the subcommand runners.
- The `termProgressBar`, `tileSetDifference`, `parseDuration`, and `encodeP6Polyline` helpers are internal to `package main` and their tests require no changes.
- Cobra silences its own usage output on `Execute()` by default when `SilenceUsage` is set; the implementation in Task 002 should set `cmd.SilenceUsage = true` and `cmd.SilenceErrors = true` on every command so test stderr buffers stay clean.

---

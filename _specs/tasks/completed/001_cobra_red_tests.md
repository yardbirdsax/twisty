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

- [ ] `go test -run NOMATCH ./...` fails with "undefined: newScoreCmd" and "undefined: newRandomCmd" (compile-red). Note: `go build ./...` succeeds because it does not compile `_test.go` files; use `go test` or `go vet` to observe the undefined-symbol errors.
- [ ] No test in `main_test.go` references `flag.NewFlagSet` or calls `runScore`/`runRandom` directly.
- [ ] `TestScoreFlagSetParsesValidFlags` and `TestScoreMinScoreDefaultsToZero` are deleted.
- [ ] All other test names and logic are preserved; only the invocation mechanism changes.
- [ ] `go vet ./...` produces no issues in test files beyond the expected undefined-symbol errors.

## Notes

- Do **not** touch `overpass_test.go`, `pipeline_test.go`, or any `quality/` tests — they do not call the subcommand runners.
- The `termProgressBar`, `tileSetDifference`, `parseDuration`, and `encodeP6Polyline` helpers are internal to `package main` and their tests require no changes.
- Cobra silences its own usage output on `Execute()` by default when `SilenceUsage` is set; the implementation in Task 002 should set `cmd.SilenceUsage = true` and `cmd.SilenceErrors = true` on every command so test stderr buffers stay clean.

---

# Task 001 Review: Rewrite Tests for Cobra (Red)

**Reviewer:** Principal Engineer
**Date:** 2026-04-08
**Verdict:** APPROVED

---

## Summary

Task 001 rewrites `main_test.go` to call `newScoreCmd()` and `newRandomCmd()` (which do not yet exist) via the Cobra `SetArgs`/`Execute` pattern, and deletes tests that exercised `flag.FlagSet` internals directly. The intent is to produce compile-red state that Task 002 will resolve.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/go.mod` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go test -run NOMATCH ./...` fails with "undefined: newScoreCmd" and "undefined: newRandomCmd" | PASS |
| No test references `flag.NewFlagSet` or calls `runScore`/`runRandom` directly | PASS |
| `TestScoreFlagSetParsesValidFlags` and `TestScoreMinScoreDefaultsToZero` are deleted | PASS |
| All other test names and logic are preserved; only invocation mechanism changed | PASS |
| `go vet ./...` produces no issues beyond undefined-symbol errors | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test -run NOMATCH ./...
# github.com/yardbirdsax/twisty [github.com/yardbirdsax/twisty.test]
# ./main_test.go:115:9: undefined: newScoreCmd   (8 occurrences)
# ./main_test.go:701:9: undefined: newRandomCmd
# FAIL github.com/yardbirdsax/twisty [build failed]

go vet ./...
# vet: ./main_test.go:115:9: undefined: newScoreCmd
# exit status 1 (expected — only first error per file reported by go vet)

make lint   # fails due to expected undefined-symbol error only
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The compile-red state is confirmed: `go test -run NOMATCH ./...` reports both `undefined: newScoreCmd` and `undefined: newRandomCmd`. No `flag.NewFlagSet` or direct `runScore`/`runRandom` calls remain. Cobra is a direct dependency. Deleted tests are confirmed absent.

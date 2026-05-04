# Task 001: Add --cpuprofile Flag to overpass start

## Summary

Add a `--cpuprofile <path>` flag to the `twisty overpass start` command that enables Go CPU profiling scoped to the `convertPBFsToBZ2` call. This gives developers visibility into where wall-clock time is spent during conversion.

## Dependencies

None - this is the foundational task.

## Detailed Directions

### 1. Add the Flag to the Command

- In `overpass.go`, in `newOverpassStartCmd()`, add a `--cpuprofile` string flag (default empty string).
- Pass the flag value through to `runOverpassStart` (add a parameter or use a struct for options).

### 2. Implement Profiling Around convertPBFsToBZ2

- In `runOverpassStart`, just before the `convertPBFsToBZ2` call, check if `cpuprofile` is non-empty.
- If set:
  1. Create (or truncate) the file at the specified path.
  2. Call `runtime/pprof.StartCPUProfile(f)`.
  3. Use a `defer` (or explicit stop after the call) to ensure `pprof.StopCPUProfile()` and `f.Close()` run even if `convertPBFsToBZ2` returns an error.
  4. Print the profile path to stderr after stopping: `fmt.Fprintf(stderr, "CPU profile written to %s\n", cpuprofile)`.
- If not set, do nothing (existing behavior unchanged).

### 3. Handle Error Cases

- If the profile file cannot be created, return an error before starting conversion.
- If conversion errors, still flush the partial profile (the defer handles this).

### 4. Write Tests

- Write a test that invokes `runOverpassStart` (or a test helper) with `--cpuprofile` pointing to a temp file, runs a minimal conversion (1 tiny PBF or mock), and asserts the profile file exists and is non-empty.
- Write a test that confirms behavior is unchanged when `--cpuprofile` is omitted.

## Acceptance Criteria

- [ ] `twisty overpass start --cpuprofile /tmp/conv.prof --regions ...` writes a valid pprof file to the specified path
- [ ] `go tool pprof /tmp/conv.prof` can open and display the profile
- [ ] When `--cpuprofile` is omitted, no profile file is created and behavior is identical to before
- [ ] Partial profiles are flushed on conversion error
- [ ] Tests pass

## Notes

- Import `runtime/pprof` only in `overpass.go` — keep the `osmconv` package unaware of profiling.
- The profiling scope is intentionally narrow: only `convertPBFsToBZ2`, not downloads or Docker operations.
- This flag is not a committed long-term feature but should be implemented cleanly (no build tags, no hidden flags).

---

# Task 001 Review: Add --cpuprofile Flag to overpass start

**Reviewer:** Claude
**Date:** 2026-05-03
**Verdict:** APPROVED

---

## Summary

Adds a `--cpuprofile` string flag to `twisty overpass start` that enables CPU profiling scoped to the `convertPBFsToBZ2` call in `overpass.go`. Tests are in `overpass_cpuprofile_test.go`.

### Files Reviewed

| File | Status |
|------|--------|
| `overpass.go` | Reviewed |
| `overpass_cpuprofile_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `--cpuprofile` writes a valid pprof file | PASS |
| `go tool pprof` can open the profile | PASS (standard pprof format via `runtime/pprof`) |
| Omitting `--cpuprofile` leaves behavior unchanged | PASS |
| Partial profiles flushed on conversion error | PASS |
| Tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test -run TestCPUProfile -v -count=1  # 3/3 pass
make test                                 # all packages pass
make lint                                 # clean (go vet ./...)
git diff trunk -- overpass.go             # reviewed all changes
```

---

## Final Verdict

**APPROVED**

Production code correctly implements profiling with proper error handling and defer-based cleanup. Tests call `runOverpassStart` directly with staged PBF data, covering the happy path, omitted flag, and error-with-flush scenarios.

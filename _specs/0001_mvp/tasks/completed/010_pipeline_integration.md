# Task 010: Pipeline Integration and Summary Output

## Summary

Wire all pipeline stages together in main.go, implement the summary output, and implement the show-all comparison table. This task connects every prior component into a working end-to-end binary.

## Dependencies

Tasks 001-009 (all prior tasks)

## Detailed Directions

### 1. Implement the Full Pipeline in main.go

Replace the stage-comment stubs from Task 001 with real calls:

Stage 1 and 2: Resolve origin and destination using geocode package
Stage 3: Fetch routes from OSRM
Stage 4: Score all routes using route.ScoreAll
Stage 5: Check road quality using quality package
Stage 6: Select route using route.SelectRoute
Stage 7: Write GPX using gpx.WriteGPX
Stage 8: Print summary

Add a helper collectAllPoints(routes []route.Route) that returns all route point slices.

### 2. Implement printSummary

```go
func printSummary(routes []route.Route, selectedIdx int, twist float64, outPath string, showAll bool)
```

Always printed:
Selected route: distance=48.2km duration=32min angular=85.3/km score=142.1 points=512
GPX written to route.gpx

Values:
- distance = selected.Distance / 1000 (km, 1 decimal)
- duration = selected.Duration / 60 (minutes, 0 decimal)
- angular = selected.Stats.AngularDensity (1 decimal)
- score = selected.Stats.AdjustedScore (1 decimal)
- points = len selected.Points

With show-all (printed before the selection line):

Route comparison:
--------
Route    Distance     Duration   Angular/km      Twist Score
--------
1        48.2         32         85.3            142.1
2        52.7         38         124.6           210.4
3        55.1         41         98.2            178.9
--------
Distance in km, Duration in minutes

Use fmt.Printf with fixed-width format strings to align the columns. The selected route should be visually indicated.

### 3. Manual End-to-End Test

Run the following test cases and verify outputs:

Test 1: Raw coordinate input (no geocoding)
./twisty -origin "37.7749,-122.4194" -dest "37.3382,-121.8863" -twist 0.5 -out test.gpx
Should skip geocoding, print route count, produce valid GPX

Test 2: Address input with disambiguation
./twisty -origin "Springfield" -dest "Kansas City, MO"
Should print disambiguation for Springfield and confirmation for Kansas City

Test 3: Twist factor extremes
./twisty -origin "San Francisco, CA" -dest "San Jose, CA" -twist 0.0 -show-all
./twisty -origin "San Francisco, CA" -dest "San Jose, CA" -twist 1.0 -show-all
twist 0.0 should select fastest; twist 1.0 should select twistiest

Test 4: Missing required flags
./twisty -origin "San Francisco"
Should print usage and exit 1

Test 5: Invalid twist
./twisty -origin "37.7749,-122.4194" -dest "37.3382,-121.8863" -twist 1.5
Should print error and exit 1

## Acceptance Criteria

- [ ] All 5 manual test cases produce the expected behavior
- [ ] go build ./... succeeds with no errors or warnings
- [ ] The GPX file from Test 1 imports into mapping software and renders correctly
- [ ] twist 0.0 and twist 1.0 select different routes when OSRM returns multiple alternatives
- [ ] The show-all table is printed before the Selected route line
- [ ] No hardcoded coordinates or strings in main.go
- [ ] Overpass failure prints a warning but still produces GPX output

## Notes

- Import all sub-packages: geo, geocode, route, quality, gpx.
- The time.Sleep(1 * time.Second) between geocoding requests is only needed when both inputs require geocoding.
- Print all user-facing status messages to stdout; errors to stderr.
- The binary name is twisty, not twistrouter.
- **Go 1.26:** This project requires Go 1.26. Where you need to inspect a specific error type, prefer `errors.AsType[T](err)` over `errors.As()` — it is a new generic function in the `errors` package that returns the typed error value directly without a pointer-to-interface intermediate:
  ```go
  // Instead of:
  var exitErr *exec.ExitError
  if errors.As(err, &exitErr) { ... }
  // Use:
  if exitErr, ok := errors.AsType[*exec.ExitError](err); ok { ... }
  ```

---

# Task 010 Review: Pipeline Integration and Summary Output

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

This task wires all pipeline stages into main.go, implements `printSummary` with a route comparison table, and connects geocoding, OSRM routing, quality filtering, route selection, and GPX output into a working binary.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geocode/nominatim.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/overpass.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/Makefile` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| All 5 manual test cases produce the expected behavior | PARTIAL - Tests 4 and 5 verified; Tests 1-3 require live network (not verified) |
| go build ./... succeeds with no errors or warnings | PASS |
| The GPX file from Test 1 imports into mapping software and renders correctly | NOT VERIFIED (requires live network) |
| twist 0.0 and twist 1.0 select different routes when OSRM returns multiple alternatives | NOT VERIFIED (requires live network) |
| The show-all table is printed before the Selected route line | PASS |
| No hardcoded coordinates or strings in main.go | PASS |
| Overpass failure prints a warning but still produces GPX output | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make lint                      # PASS - go vet ./... clean
go build ./...                 # PASS - no errors
make test                      # PASS - all unit tests pass with -short flag
./twisty -origin "San Francisco"  # PASS - prints usage + error, exits 1
./twisty -origin "37.7749,-122.4194" -dest "37.3382,-121.8863" -twist 1.5  # PASS - exits 1 with error
```

---

## Final Verdict

**APPROVED**

All blocking issues from the prior review have been addressed: `printSummary` now accepts the `twist float64` parameter per spec, the duplicate Overpass warning has been removed from `ApplyQuality`, `make test` passes cleanly with `-short`, and the User-Agent header uses the correct `twisty/1.0` value. Build, lint, and unit tests all pass.

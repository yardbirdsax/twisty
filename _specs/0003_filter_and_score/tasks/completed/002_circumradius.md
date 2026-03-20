# Task 002: Circumradius Computation

## Summary

Implement the circumradius calculation in the `geo` package as a pure geometric function. This is the core math behind curvature scoring — given three consecutive road nodes, compute the radius of the circle passing through them.

## Dependencies

None — uses only the existing `geo.Coord` type and `geo.Haversine()` function.

## Detailed Directions

### 1. Create `geo/circumradius.go`

- Create the file `geo/circumradius.go` with package `geo`.
- Implement the function:

```go
// Circumradius returns the radius (in meters) of the unique circle passing
// through three geographic points. If the points are collinear (or nearly so),
// it returns +Inf.
func Circumradius(a, b, c Coord) float64
```

- Use the formula: `R = (a * b * c) / (4 * area)` where:
  - `a`, `b`, `c` are the Haversine distances between the three pairs of points (side lengths of the triangle in meters).
  - `area` is the triangle area computed via the cross-product method: `area = 0.5 * |ab × ac|` using the semi-perimeter approach (Heron's formula) or the direct cross-product.
  - Recommended: use Heron's formula for numerical stability: `s = (a+b+c)/2`, `area = sqrt(s*(s-a)*(s-b)*(s-c))`.
- Handle the degenerate case: if `area` is zero or very close to zero (e.g., `< 1e-10`), return `math.Inf(1)`.
- Use the existing `Haversine()` function from `geo/geo.go` to compute all three side lengths.

### 2. Write unit tests in `geo/circumradius_test.go`

- Use table-driven tests following the pattern in `geo/geo_test.go`.
- Test cases:
  - **Known equilateral triangle**: Three points forming a roughly equilateral triangle. The circumradius of an equilateral triangle with side `a` is `a / sqrt(3)`. Pick three geographic points that approximate this and verify the result within a tolerance (e.g., 1%).
  - **Known right triangle**: Three points forming a right triangle. The circumradius equals half the hypotenuse. Verify within tolerance.
  - **Collinear points**: Three points on a straight line (e.g., same longitude, increasing latitude). Must return `+Inf`.
  - **Nearly collinear points**: Three points that are almost but not quite collinear. Should return a very large radius.
  - **Tight curve**: Three points that form a tight curve (small circumradius, e.g., < 30m). Verify the result is in the expected range.
  - **Duplicate points**: Two or more identical points. Should return `+Inf` (degenerate triangle).
- Use `math.IsInf(result, 1)` to check infinite results.
- Use relative tolerance (e.g., within 2%) for non-degenerate cases since Haversine distances are approximations.

## Acceptance Criteria

- [ ] `geo/circumradius.go` exists with the `Circumradius` function.
- [ ] `geo/circumradius_test.go` passes with `go test ./geo/...`.
- [ ] Collinear and duplicate-point inputs return `+Inf`.
- [ ] Known geometric configurations produce results within 2% of expected values.
- [ ] The function uses `geo.Haversine()` for all distance calculations (not raw lat/lon math).

## Notes

- The cross-product / Heron's formula approach avoids trigonometric functions beyond what `Haversine` already uses.
- Be careful with floating-point precision: `s*(s-a)*(s-b)*(s-c)` can go slightly negative for near-degenerate triangles due to rounding. Clamp to zero before taking the square root.
- This function lives in `geo` (not `quality`) because it is pure geometry with no pipeline knowledge.

---
# Task 002 Review: Circumradius Computation

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

This task implements `Circumradius(a, b, c Coord) float64` in the `geo` package using Heron's formula and `Haversine()` for all side-length calculations, with table-driven tests covering all required cases.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geo/circumradius.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geo/circumradius_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `geo/circumradius.go` exists with the `Circumradius` function | PASS |
| `geo/circumradius_test.go` passes with `go test ./geo/...` | PASS |
| Collinear and duplicate-point inputs return `+Inf` | PASS |
| Known geometric configurations produce results within 2% of expected values | PASS |
| The function uses `geo.Haversine()` for all distance calculations | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test                                    # all packages pass
make lint                                    # go vet clean, no warnings
go test ./geo/... -v -run TestCircumradius   # all 7 sub-tests PASS
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass. Implementation is correct, tests are structurally sound (lower-bound dispatch uses a dedicated `wantLowerBound bool` field rather than string matching), and the negative clamp before `math.Sqrt` handles near-degenerate triangles safely. Ready to merge.

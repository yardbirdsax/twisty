# Task 001: Define Scoring Parameters

## Summary

Create the single source of truth for all scoring and filtering constants: tier thresholds, tier weights, and deflection filter settings. This file is referenced by every subsequent task.

## Dependencies

None — this is the foundational task.

## Detailed Directions

### 1. Create `quality/scoring_params.go`

- Create the file `quality/scoring_params.go` with package `quality`.
- Define all tier radius thresholds as named `float64` constants:
  - `TierRadius4 = 30.0` (tightest curves)
  - `TierRadius3 = 60.0`
  - `TierRadius2 = 100.0`
  - `TierRadius1 = 175.0` (gentlest curves that still score)
- Define all tier weights as named `float64` constants:
  - `TierWeight4 = 2.0`
  - `TierWeight3 = 1.6`
  - `TierWeight2 = 1.3`
  - `TierWeight1 = 1.0`
  - `TierWeight0 = 0.0` (straight segments)
- Define deflection filter constants:
  - `DeflectionLookAheadM = 2400.0` (2.4 km)
  - `DeflectionMinHeadingChange = 20.0` (degrees)
- Each constant must have a comment explaining its role.
- Add a comment block at the top of the file explaining that these are the single location for all scoring/filtering parameters.

### 2. Add a helper function for tier assignment

- Write a function `AssignTier(radius float64) (tier int, weight float64)` that maps a circumradius to its tier and weight using the constants above.
- Logic: check from tightest to widest. If `radius < TierRadius4` → tier 4, weight 2.0. If `radius < TierRadius3` → tier 3, etc. If `radius >= TierRadius1` → tier 0, weight 0.0.

### 3. Add a helper function for parameters hashing

- Write a function `ScoringParamsHash() string` that serializes all scoring constants into a deterministic string (e.g., `fmt.Sprintf` with all values in fixed order) and returns a `sha256:...` hex-encoded hash.
- Import `crypto/sha256` and `encoding/hex` from the standard library.
- This will be used by the score cache (Task 007) to detect parameter changes.

### 4. Write unit tests in `quality/scoring_params_test.go`

- Table-driven test for `AssignTier`: test each tier boundary and edge cases:
  - radius = 29.9 → tier 4
  - radius = 30.0 → tier 3 (exclusive lower bound: < 30 is tier 4)
  - radius = 59.9 → tier 3
  - radius = 60.0 → tier 2
  - radius = 99.9 → tier 2
  - radius = 100.0 → tier 1
  - radius = 174.9 → tier 1
  - radius = 175.0 → tier 0
  - radius = 1000.0 → tier 0
- Test that `ScoringParamsHash()` returns a consistent value across calls.
- Test that `ScoringParamsHash()` starts with `"sha256:"`.

## Acceptance Criteria

- [ ] `quality/scoring_params.go` exists with all constants and helper functions.
- [ ] `quality/scoring_params_test.go` passes with `go test ./quality/...`.
- [ ] Every constant has an explanatory comment.
- [ ] `AssignTier` correctly assigns tiers at all boundary values.
- [ ] `ScoringParamsHash` is deterministic.

## Notes

- Tier thresholds are exclusive lower bounds: a radius of exactly 175m is tier 0, not tier 1. The PRD states ">= 175 m" is tier 0 and "< 175 m" is tier 1.
- The deflection constants (2.4 km, 20°) come from the Curvature project — note this in comments.

---
# Task 001 Review: Define Scoring Parameters

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

This task created `quality/scoring_params.go` as the single source of truth for all tier radius thresholds, tier weights, and deflection filter constants, along with `AssignTier` and `ScoringParamsHash` helper functions and corresponding unit tests.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `quality/scoring_params.go` exists with all constants and helper functions | PASS |
| `quality/scoring_params_test.go` passes with `go test ./quality/...` | PASS |
| Every constant has an explanatory comment | PASS |
| `AssignTier` correctly assigns tiers at all boundary values | PASS |
| `ScoringParamsHash` is deterministic | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Boundary semantics documented in comments:** Each `TierRadiusN` constant comment states the inclusive/exclusive bounds of its tier band, preventing future ambiguity.

---

## Verification Commands Run

```bash
go test ./quality/...  # ok github.com/yardbirdsax/twisty/quality 8.874s
go vet ./quality/...   # no output (clean)
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met, tests pass, and `go vet` is clean.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.

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

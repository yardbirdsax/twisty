# Task 005: Highway-Type Soft Penalties

## Summary

Implement stage 6: apply highway-type penalty multipliers to road collection scores. Motorways and trunk roads get reduced scores so they don't outrank genuinely enjoyable twisty roads.

## Dependencies

Task 002, Task 004 — requires `RoadCollection` struct and aggregation producing populated collections.

## Detailed Directions

### 1. Define Penalty Constants

- In `quality/scoring_params.go`, add the penalty multiplier map:

```go
// HighwayPenalty maps highway types to score penalty multipliers.
// A multiplier of 1.0 means no penalty.
var HighwayPenalty = map[string]float64{
	"motorway":       0.3,
	"motorway_link":  0.3,
	"trunk":          0.5,
	"trunk_link":     0.5,
	"primary":        0.8,
	"primary_link":   0.8,
	"secondary":      0.9,
	"secondary_link": 0.9,
	"tertiary":       1.0,
	"tertiary_link":  1.0,
	"unclassified":   1.0,
	"residential":    1.0,
	"service":        1.0,
}

// DefaultHighwayPenalty is used for highway types not in the HighwayPenalty map.
const DefaultHighwayPenalty = 1.0
```

### 2. Update ScoringParamsHash

- Add the penalty multiplier values to the `ScoringParamsHash()` computation in `scoring_params.go`. Since penalties are applied post-cache (at the aggregation level), this is technically not needed for cache invalidation — but including them keeps the hash comprehensive. If this complicates things, skip it and add a comment explaining why penalties are excluded from the hash (they're post-cache computation).

### 3. Create `quality/penalty.go`

- Create a new file `quality/penalty.go` with:

```go
// ApplyPenalties adjusts the scores of road collections based on
// their highway types. Mutates the collections in place.
func ApplyPenalties(collections []RoadCollection)
```

- For each collection:
  1. Find the minimum penalty multiplier across all `HighwayTypes` in the collection.
  2. Set `PenaltyFactor` to that minimum.
  3. Set `PenalizedScore = TotalScore * PenaltyFactor`.
  4. Set `PenalizedPerKm = ScorePerKm * PenaltyFactor`.

- Add a helper:

```go
// penaltyForTypes returns the minimum penalty multiplier across
// the given highway types.
func penaltyForTypes(types []string) float64
```

### 4. Write Unit Tests

- Create `quality/penalty_test.go` with tests:
  - `TestApplyPenalties_Motorway`: collection with `["motorway"]` gets 0.3 multiplier
  - `TestApplyPenalties_Mixed`: collection with `["secondary", "trunk"]` gets 0.5 (minimum)
  - `TestApplyPenalties_NoPenalty`: collection with `["tertiary"]` gets 1.0
  - `TestApplyPenalties_UnknownType`: collection with unknown highway type gets `DefaultHighwayPenalty` (1.0)
  - `TestPenaltyForTypes`: direct tests of the helper function
  - Verify `PenalizedScore` and `PenalizedPerKm` are correctly computed

## Acceptance Criteria

- [ ] Penalty constants defined in `scoring_params.go` matching the PRD table
- [ ] `ApplyPenalties` correctly applies minimum penalty across highway types
- [ ] `PenaltyFactor`, `PenalizedScore`, and `PenalizedPerKm` are correctly set
- [ ] Segment-level scores and tiers are NOT modified
- [ ] Unknown highway types default to 1.0 penalty
- [ ] All unit tests pass

## Notes

- Penalties are applied to collection-level totals only. Segment scores and tiers remain unchanged — this is critical for correct per-segment KML coloring.
- The "minimum penalty wins" rule means a road that's partially motorway gets penalized for the whole collection. This is conservative and intentional per the PRD.
- The `RoadCollection` struct field was renamed from `PenaltyFactor` to `HighwayPenaltyFactor` to avoid a name collision with the package-level `PenaltyFactor` function in `overpass.go`.
- SHOULD FIX resolved: added missing tab alignment to `PenalizedPerKm` in `aggregate.go` to satisfy `gofmt`.

---
# Task 005 Review: Highway-Type Soft Penalties

**Reviewer:** Claude (claude-sonnet-4-6)
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

Implements stage 6 of the scoring pipeline: highway-type penalty multipliers applied to `RoadCollection` scores. Penalty constants are defined in `scoring_params.go`, applied via `ApplyPenalties` in `penalty.go`, and covered by unit tests in `penalty_test.go`.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/penalty.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/penalty_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/aggregate.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| Penalty constants defined in `scoring_params.go` matching the PRD table | PASS |
| `ApplyPenalties` correctly applies minimum penalty across highway types | PASS |
| `PenaltyFactor`, `PenalizedScore`, and `PenalizedPerKm` are correctly set | PASS |
| Segment-level scores and tiers are NOT modified | PASS |
| Unknown highway types default to 1.0 penalty | PASS |
| All unit tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
go test ./quality/...           # PASS — all quality package tests pass
go vet ./quality/...            # PASS — go vet clean
gofmt -l quality/               # quality/aggregate.go, penalty.go, penalty_test.go NOT listed (clean)
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. Tests pass, `go vet` is clean, and `gofmt` reports no issues with any of the files introduced or modified by this task. The previous SHOULD FIX regarding `PenalizedPerKm` tab alignment in `aggregate.go` has been resolved (confirmed in the Notes section and verified by `gofmt`).

---

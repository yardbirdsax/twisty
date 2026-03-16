# Task 008: Twist-Factor Route Selection

## Summary

Implement the SelectRoute function in route/score.go that uses min-max normalization on durations and adjusted scores to pick the best route given the user's twist factor.

## Dependencies

Task 006 (route.CurvatureStats.AdjustedScore), Task 007 (AdjustedScore populated by quality filtering)

## Detailed Directions

### 1. Implement SelectRoute

Add to route/score.go:

```go
// SelectRoute returns the index of the best route given the twist factor [0, 1].
// twist=0.0 favors the fastest route; twist=1.0 favors the twistiest.
// If there is only one route, returns 0.
func SelectRoute(routes []Route, twist float64) int
```

Algorithm:

1. If len routes equals 1, return 0.

2. Normalize durations (lower duration equals higher normalized score for speed):
   minDur equals min of all route Durations
   maxDur equals max of all route Durations
   if maxDur equals minDur: set all normalized to 1.0
   else: normalizedDuration[i] = (routes[i].Duration - minDur) / (maxDur - minDur)

3. Normalize adjusted scores (higher score equals higher normalized twist):
   minScore equals min of all route AdjustedScores
   maxScore equals max of all route AdjustedScores
   if maxScore equals minScore: set all normalized to 1.0
   else: normalizedScore[i] = (routes[i].Stats.AdjustedScore - minScore) / (maxScore - minScore)

4. Compute selection score per route:
   selectionScore[i] = twist * normalizedScore[i] + (1.0 - twist) * (1.0 - normalizedDuration[i])

5. Return the index of the route with the highest selectionScore.

### 2. Write Unit Tests

Create or extend route/score_test.go:

- Two routes, twist equals 0.0: Route A is faster, route B is twistier. Should select route A.
- Two routes, twist equals 1.0: Should select route B.
- Two routes, twist equals 0.5: Select whichever scores higher on the combined formula.
- Single route: Always returns index 0 regardless of twist.
- Identical durations: Does not divide by zero.
- Identical scores: Does not divide by zero.

## Acceptance Criteria

- [ ] go test ./route/ -run TestSelectRoute passes
- [ ] With twist=0.0, the route with the shorter duration is selected
- [ ] With twist=1.0, the route with the higher AdjustedScore is selected
- [ ] Single-route input always returns 0
- [ ] No panics when all durations are equal or all scores are equal

## Notes

- (1 - normalizedDuration) converts high duration = bad to high = good so that the weighted sum consistently means higher is better.
- The normalization is always relative to the candidate set, not any global scale.

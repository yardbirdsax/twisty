---
# Task 003: Write Failing Tests for Deterministic Way Ordering (Red)

## Summary

`OrderWays` and `FindConnectedComponents` produce non-deterministic output across
program runs because two internal functions iterate Go maps without sorting their
keys first: `findStartIndex` iterates `adj map[geo.Coord][]wayEnd` to pick a
traversal start, and `FindConnectedComponents` iterates `compMap map[int]ScoredWays`
to collect output components. Go randomises map iteration at program startup, so
which degree-1 terminus is chosen as the traversal start — and the order of output
components — changes between executions.

Because `DeflectionFilterSegments` uses a directional 2400 m look-ahead, a
different traversal direction can zero out different segments, causing `ScorePerKm`
to differ between runs with identical inputs.

This task writes tests that specify the correct deterministic behaviour. Because
Go's map seed is fixed for the lifetime of a single binary execution, these tests
may pass on some runs and fail on others; that is expected for red tests that target
map-iteration non-determinism. The fix in Task 004 makes them always pass.

## Dependencies

None — this is the foundational task.

## Detailed Directions

### 1. Add tests to `quality/aggregate_test.go`

Add the following four test functions. Do not modify any existing tests.

---

#### `TestFindStartIndex_PicksLowerLatTerminus`

Verifies that `findStartIndex` always selects the terminus with the lower latitude
when two degree-1 endpoints exist. This exercises the bug directly on the unexported
function (tests are in `package quality`).

```go
func TestFindStartIndex_PicksLowerLatTerminus(t *testing.T) {
    t.Parallel()
    south := geo.Coord{Lat: 44.000, Lon: -72.0}
    mid1  := geo.Coord{Lat: 44.001, Lon: -72.0}
    mid2  := geo.Coord{Lat: 44.002, Lon: -72.0}
    north := geo.Coord{Lat: 44.003, Lon: -72.0}

    ways := ScoredWays{
        makeWay(1, south, mid1),
        makeWay(2, mid1, mid2),
        makeWay(3, mid2, north),
    }

    adj := buildAdjacency(ways)
    idx, startFromEnd := findStartIndex(ways, adj)

    // The southern terminus is a degree-1 node at way 0's start.
    // findStartIndex must always return it (lower lat wins).
    if startFromEnd {
        t.Errorf("startFromEnd = true; expected false (forward traversal from southern terminus)")
    }
    gotStart, _, ok := wayEndpoints(ways[idx])
    if !ok {
        t.Fatal("chosen way has no segments")
    }
    if gotStart != south {
        t.Errorf("start coord = %v; want southern terminus %v", gotStart, south)
    }
}
```

---

#### `TestFindStartIndex_PicksLowerLatTerminus_ReverseInput`

Same logical road but with the ways slice provided in reverse order. The canonical
start should still be the southern terminus, demonstrating that input slice order
does not affect the choice.

```go
func TestFindStartIndex_PicksLowerLatTerminus_ReverseInput(t *testing.T) {
    t.Parallel()
    south := geo.Coord{Lat: 44.000, Lon: -72.0}
    mid1  := geo.Coord{Lat: 44.001, Lon: -72.0}
    mid2  := geo.Coord{Lat: 44.002, Lon: -72.0}
    north := geo.Coord{Lat: 44.003, Lon: -72.0}

    // Ways provided in reverse geographic order.
    ways := ScoredWays{
        makeWay(3, mid2, north),
        makeWay(2, mid1, mid2),
        makeWay(1, south, mid1),
    }

    adj := buildAdjacency(ways)
    idx, startFromEnd := findStartIndex(ways, adj)

    // Whether we start from the way's start or reverse it, the actual
    // geographic start point must still be the southern terminus.
    var actualStart geo.Coord
    if startFromEnd {
        _, actualStart, _ = wayEndpoints(ways[idx])
    } else {
        actualStart, _, _ = wayEndpoints(ways[idx])
    }
    if actualStart != south {
        t.Errorf("start coord = %v; want southern terminus %v", actualStart, south)
    }
}
```

---

#### `TestOrderWays_Deterministic`

Calls `OrderWays` 100 times with the same two-terminus chain and asserts the
result is identical every time. Because the map seed does not change within a
single binary run, this test may pass before the fix; it is included to pin the
behaviour after Task 004 so regressions are caught.

```go
func TestOrderWays_Deterministic(t *testing.T) {
    t.Parallel()
    south := geo.Coord{Lat: 44.000, Lon: -72.0}
    mid1  := geo.Coord{Lat: 44.001, Lon: -72.0}
    mid2  := geo.Coord{Lat: 44.002, Lon: -72.0}
    north := geo.Coord{Lat: 44.003, Lon: -72.0}

    ways := ScoredWays{
        makeWay(1, south, mid1),
        makeWay(2, mid1, mid2),
        makeWay(3, mid2, north),
    }

    first := OrderWays(ways)
    if len(first) != 3 {
        t.Fatalf("expected 3 ways, got %d", len(first))
    }

    for i := 0; i < 100; i++ {
        got := OrderWays(ways)
        if len(got) != len(first) {
            t.Fatalf("iteration %d: length %d, want %d", i, len(got), len(first))
        }
        for j := range first {
            if got[j].WayID != first[j].WayID {
                t.Errorf("iteration %d: ways[%d].WayID = %d, want %d",
                    i, j, got[j].WayID, first[j].WayID)
            }
        }
    }
}
```

---

#### `TestFindConnectedComponents_StableOrder`

Verifies that `FindConnectedComponents` always returns components in a consistent
order. Provides two disconnected clusters and asserts the component containing the
lower-indexed way (input index 0) is always first.

```go
func TestFindConnectedComponents_StableOrder(t *testing.T) {
    t.Parallel()
    // Cluster A — Vermont area
    a := makeWay(10, geo.Coord{Lat: 44.000, Lon: -72.0}, geo.Coord{Lat: 44.001, Lon: -72.0})
    // Cluster B — North Carolina area (far away, will not connect to A)
    b := makeWay(20, geo.Coord{Lat: 36.000, Lon: -80.0}, geo.Coord{Lat: 36.001, Lon: -80.0})

    ways := ScoredWays{a, b}
    comps := FindConnectedComponents(ways, ConnectedEndpointProximityM)

    if len(comps) != 2 {
        t.Fatalf("expected 2 components, got %d", len(comps))
    }

    // Component order must be stable: the component whose union-find root has
    // the lower index (i.e. way at input index 0) must come first.
    if comps[0][0].WayID != 10 {
        t.Errorf("first component WayID = %d; want 10 (lower input index)", comps[0][0].WayID)
    }
    if comps[1][0].WayID != 20 {
        t.Errorf("second component WayID = %d; want 20", comps[1][0].WayID)
    }
}
```

---

## Review

---
# Task 003 Review: Write Failing Tests for Deterministic Way Ordering (Red)

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-04-08
**Verdict:** APPROVED

---

## Summary

Added four test functions to `quality/aggregate_test.go` targeting map-iteration non-determinism in `findStartIndex` and `FindConnectedComponents`. These are red-phase tests that fail intermittently before the Task 004 fix.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/aggregate_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| All four test functions present in `quality/aggregate_test.go` | PASS |
| `go build ./...` succeeds | PASS |
| Tests fail on at least some runs with `-count=10` | PASS |
| No existing tests modified or deleted | PASS |
| `go vet ./...` reports no issues | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go build ./...           # no output (success)
go vet ./...             # no output (success)
go test ./quality/... -run 'TestFindStartIndex|TestOrderWays_Deterministic|TestFindConnectedComponents_StableOrder' -count=10
# Multiple FAILs across runs confirming non-determinism
```

---

## Final Verdict

**APPROVED**

All four tests are present and match the spec exactly. Build and vet pass. The `-count=10` run confirms tests fail intermittently, exercising the map-iteration non-determinism as intended.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.

---

## Acceptance Criteria

- [ ] All four test functions are present in `quality/aggregate_test.go`.
- [ ] `go build ./...` succeeds (no new symbols introduced).
- [ ] `go test ./quality/... -run 'TestFindStartIndex|TestOrderWays_Deterministic|TestFindConnectedComponents_StableOrder' -count=10` fails on at least some runs before the Task 004 fix is applied, confirming the tests exercise the non-determinism.
- [ ] No existing tests are modified or deleted.
- [ ] `go vet ./...` reports no issues.

## Notes

- All four tests are in `package quality` (same package as `aggregate.go`), which
  gives them access to unexported functions `findStartIndex` and `buildAdjacency`.
- `makeWay` already exists in `aggregate_test.go` and must be reused; do not add
  a duplicate.
- Running with `-count=10` forces the test binary to be re-executed 10 times,
  re-seeding Go's map randomisation each time and making intermittent failures
  observable.

---

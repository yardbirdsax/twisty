---
# Task 004: Fix Non-Deterministic Way Ordering (Green)

## Summary

Make `OrderWays` and `FindConnectedComponents` produce identical output across
program runs for identical inputs. Two functions in `quality/aggregate.go` iterate
Go maps without sorting their keys, causing the non-determinism:

1. `findStartIndex` iterates `adj map[geo.Coord][]wayEnd` and returns the first
   degree-1 terminus it encounters. Different traversal starts feed
   `DeflectionFilterSegments` with differently-ordered segment chains, changing
   which segments are zeroed and therefore changing `ScorePerKm` values.

2. `FindConnectedComponents` iterates `compMap map[int]ScoredWays` to build the
   output slice. The order of components is random, affecting `SubIndex` assignment
   and the fallback appending path in `OrderWays`.

Both fixes follow the same pattern: collect map keys into a slice, sort it, then
iterate the sorted slice.

## Dependencies

Task 003 — tests must exist and be failing before this task is begun.

## Detailed Directions

### 1. Add `sort` to the import block of `quality/aggregate.go`

`aggregate.go` does not currently import `sort`. Add it:

```go
import (
    "fmt"
    "sort"
    "strings"

    "github.com/yardbirdsax/twisty/geo"
)
```

---

### 2. Fix `findStartIndex` — collect and sort degree-1 candidates

Replace the early-return loop in `findStartIndex` with a collect-then-sort
approach. The canonical start is the degree-1 terminus with the lowest latitude;
longitude breaks ties.

Current code (lines ~207–227 in `aggregate.go`):

```go
func findStartIndex(ways ScoredWays, adj map[geo.Coord][]wayEnd) (index int, startFromEnd bool) {
    for _, ends := range adj {
        if len(ends) != 1 {
            continue
        }
        we := ends[0]
        w := ways[we.index]
        if we.isStart {
            return we.index, false
        }
        if !isOneway(w) {
            return we.index, true
        }
    }
    return 0, false
}
```

Replace with:

```go
func findStartIndex(ways ScoredWays, adj map[geo.Coord][]wayEnd) (index int, startFromEnd bool) {
    type candidate struct {
        coord        geo.Coord
        index        int
        startFromEnd bool
    }
    var candidates []candidate

    for coord, ends := range adj {
        if len(ends) != 1 {
            continue
        }
        we := ends[0]
        w := ways[we.index]
        if we.isStart {
            candidates = append(candidates, candidate{coord: coord, index: we.index, startFromEnd: false})
        } else if !isOneway(w) {
            candidates = append(candidates, candidate{coord: coord, index: we.index, startFromEnd: true})
        }
        // Oneway end — route terminus, not a valid start. Skip.
    }

    if len(candidates) == 0 {
        return 0, false
    }

    sort.Slice(candidates, func(i, j int) bool {
        if candidates[i].coord.Lat != candidates[j].coord.Lat {
            return candidates[i].coord.Lat < candidates[j].coord.Lat
        }
        return candidates[i].coord.Lon < candidates[j].coord.Lon
    })

    return candidates[0].index, candidates[0].startFromEnd
}
```

---

### 3. Fix `FindConnectedComponents` — sort `compMap` keys before collecting

In `FindConnectedComponents`, replace the map-range collection of components with a
sorted-key iteration.

Current code (lines ~166–170 in `aggregate.go`):

```go
components := make([]ScoredWays, 0, len(compMap))
for _, comp := range compMap {
    components = append(components, comp)
}
```

Replace with:

```go
roots := make([]int, 0, len(compMap))
for k := range compMap {
    roots = append(roots, k)
}
sort.Ints(roots)

components := make([]ScoredWays, 0, len(compMap))
for _, k := range roots {
    components = append(components, compMap[k])
}
```

---

### 4. Run the red tests to confirm they now pass

```
go test ./quality/... -run 'TestFindStartIndex|TestOrderWays_Deterministic|TestFindConnectedComponents_StableOrder' -count=10
```

All four tests from Task 003 must pass across all 10 runs.

### 5. Run the full test suite

```
go test ./...
```

No regressions. `go vet ./...` must also be clean.

---

## Acceptance Criteria

- [ ] `findStartIndex` collects all valid degree-1 candidates and sorts them by
      `(Lat ASC, Lon ASC)` before returning, with no early-return on first match.
- [ ] `FindConnectedComponents` sorts `compMap` integer keys with `sort.Ints`
      before building the `components` slice.
- [ ] `sort` is added to the import block of `quality/aggregate.go`.
- [ ] `go test ./quality/... -count=10` passes with zero failures across all runs.
- [ ] `go test ./...` reports no regressions.
- [ ] `go vet ./...` reports no issues.

## Notes

- The sort key for `findStartIndex` candidates is the **degree-1 coordinate** (the
  endpoint of the way that has only one way touching it), not the way's own start
  coordinate. For `isStart=true` candidates, the degree-1 coord is `way.start`;
  for `startFromEnd=true` candidates it is `way.end`. Both are already available
  as the map key `coord` in the iteration, so no extra lookups are needed.
- Sorting by `(Lat ASC, Lon ASC)` is a stable geographic tie-breaker with no
  special domain meaning beyond "always pick the same one". Any consistent total
  order would fix the non-determinism; lower-lat was chosen because southern
  termini are slightly more intuitive as route starts in the northeastern US region
  this tool is primarily used for.
- `sort.Ints` for `compMap` keys produces components ordered by union-find root
  index, which corresponds to input slice order (lower-indexed ways become roots
  first). This is deterministic for a given input slice.

---

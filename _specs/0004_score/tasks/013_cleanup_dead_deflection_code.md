---
# Task 013: Cleanup Dead Deflection Code and Verify

## Summary

After Task 012 moves the deflection filter post-aggregation, verify that no dead code remains referencing the old per-way deflection approach, and confirm the full pipeline produces correct output end-to-end.

## Dependencies

Task 012 — deflection filter must be moved before cleanup.

## Detailed Directions

### 1. Verify no references to removed functions

Search the entire codebase for:
- `DeflectionFilter(` — should only find `DeflectionFilterSegments`
- `ApplyDeflectionFilter` — should find zero results
- `ZeroedByDeflection` — should find zero results
- `totalZeroed` — should find zero results

If any references remain, remove or update them.

### 2. Verify score cache compatibility

- Confirm that `ScoreCache.Write` and `ScoreCache.Read` no longer accept/return a `zeroed` parameter
- Confirm that the cache format change is backwards-compatible: old cache entries (with zeroed data) should either be naturally invalidated by the `ScoringParamsHash` change or fail gracefully on read

### 3. Run the full test suite

```bash
go test -race -count=1 ./...
```

All tests must pass with no data races.

### 4. Run `go vet` and check for unused imports

```bash
go vet ./...
```

The removal of `ApplyDeflectionFilter` from `scorepipeline.go` may leave unused imports (e.g., if `geo` was only imported for the deflection filter there). Fix any issues.

### 5. Verify integration test coverage

Confirm that `quality/pipeline_integration_test.go` exercises the new deflection filter path:
- The `Aggregate` function now includes deflection filtering
- Integration tests that call `Aggregate` are implicitly testing the new deflection behavior
- Verify that the fixture roads with pre-scored segments still produce the expected number of collections and score values

## Acceptance Criteria

- [ ] Zero references to `DeflectionFilter(` (the old per-way function), `ApplyDeflectionFilter`, `ZeroedByDeflection`, or `totalZeroed` in the codebase
- [ ] `go test -race -count=1 ./...` passes
- [ ] `go vet ./...` passes
- [ ] No unused imports
- [ ] Score cache read/write signatures no longer include `zeroed`

## Notes

- This task is intentionally small — it's a verification pass after the main refactor in Task 012.
- If Task 010 was done thoroughly, this task should find nothing to fix. But it serves as a safety net.

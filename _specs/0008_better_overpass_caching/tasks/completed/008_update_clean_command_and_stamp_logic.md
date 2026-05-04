# Task 008: Update Clean Command and Stamp Logic

## Summary

Update `twisty overpass clean` to delete the new `cache/` directory alongside the existing `db/` and `merged.osm.bz2` cleanup. Ensure stamp file logic is consistent with the incremental pipeline.

## Dependencies

Task 007

## Detailed Directions

### 1. Update `overpass clean` Command

Find the existing `overpass clean` implementation and add deletion of the `cache/` directory:

```go
os.RemoveAll(filepath.Join(dataDir, "cache"))
```

This should be added alongside the existing cleanup of `db/`, `merged.osm.bz2`, and `.regions`.

### 2. Verify Stamp File Behavior

The `.regions` stamp file is written after successful conversion. Verify that:
- The stamp file is still written after `convertRegionsIncremental` completes.
- The stamp file content matches the full region set (not just new regions).
- Region comparison logic works correctly with the incremental pipeline.

### 3. Write Tests

- **Clean command test**: After running the pipeline, `overpass clean` removes `cache/`, `db/`, `merged.osm.bz2`, and `.regions`.
- **Verify no stale state**: After clean + re-run, the pipeline behaves as a fresh start (no cache reuse).

## Acceptance Criteria

- [ ] `overpass clean` deletes the `cache/` directory
- [ ] Stamp file logic is consistent with incremental pipeline
- [ ] Tests pass

## Notes

- This is a small task, but important for correctness — stale cache files from a previous run should be fully removable via `overpass clean`.

---

# Task 008 Review: Update Clean Command and Stamp Logic

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-05-03
**Verdict:** APPROVED

---

## Summary

Implements `overpass clean` deletion of the `cache/` directory (via full `dataDir` removal), verifies stamp file logic, and adds a test file covering all acceptance criteria.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass_clean_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `overpass clean` deletes the `cache/` directory | PASS |
| Stamp file logic is consistent with incremental pipeline | PASS |
| Tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
make test   # all packages pass
go vet ./.. # no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria met. Tests pass, no lint issues.

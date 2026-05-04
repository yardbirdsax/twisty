# Task 007: Update Clean Command and Stamp Logic

## Summary

Update `twisty overpass clean` to remove the new cache directory, and adjust the `.regions` stamp file logic to work correctly with the incremental pipeline.

## Dependencies

Task 006

## Detailed Directions

### 1. Update overpass clean

- In the `clean` subcommand handler, add deletion of `{dataDir}/cache/` directory (in addition to existing cleanup of `db/`, `merged.osm.bz2`, PBFs, and `.regions`).
- Ensure the order of deletion is safe (cache before merged, since cache is upstream).

### 2. Adjust Stamp File Logic

With the incremental pipeline, the stamp file behavior changes slightly:

- The `.regions` file still records the complete set of regions after a successful run.
- On startup, if regions changed (grew), only `merged.osm.bz2` and `db/` are deleted — cache files for existing regions are preserved.
- On region *removal* (new set is a subset or has different members), the old behavior is acceptable: delete everything and re-run. But stale cache files for removed regions should also be cleaned up.

### 3. Handle Stale Cache Cleanup

When regions are removed from the set:
- Identify cache files that no longer correspond to any region in the current set.
- Delete those stale cache files to avoid unbounded disk growth.
- This can be a simple "list cache dir, delete files not in current region set" pass.

### 4. Write Tests

- Test: `overpass clean` removes cache directory.
- Test: Adding a region preserves existing cache files.
- Test: Removing a region cleans up its cache file.
- Test: Stamp file is correctly written after incremental conversion.

## Acceptance Criteria

- [ ] `twisty overpass clean` removes the cache directory
- [ ] Adding regions preserves cache files for existing regions
- [ ] Removing regions cleans up stale cache files
- [ ] Stamp file accurately reflects the loaded region set
- [ ] No unbounded disk growth from orphaned cache files
- [ ] Tests pass

## Notes

- Region removal is explicitly a non-goal for optimization (PRD says users can `overpass clean` for that). The stale cache cleanup here is just hygiene, not a performance optimization.
- The cache directory might not exist on first run or after a clean — handle `os.ErrNotExist` gracefully.

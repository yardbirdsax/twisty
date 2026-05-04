# Task 008: End-to-End Validation

## Summary

Perform end-to-end validation of the incremental caching pipeline with real Geofabrik data. Verify correctness (identical merged output) and measure the speedup achieved when adding a new region to an existing set.

## Dependencies

Task 006, Task 007

## Detailed Directions

### 1. Baseline Measurement

- Pick 4 small US state regions (e.g., Delaware, Rhode Island, Connecticut, New Hampshire).
- Time a full conversion from scratch: `time twisty overpass start --regions <4 regions>`.
- Record wall-clock time for the conversion step.

### 2. Incremental Addition Measurement

- With the 4-region cache warm, add a 5th region (e.g., Vermont).
- Time: `time twisty overpass start --regions <5 regions>`.
- Record wall-clock time — this should only convert Vermont's PBF + re-merge.

### 3. Correctness Verification

- Save the `merged.osm.bz2` from the incremental run.
- Do a clean run with all 5 regions from scratch (delete cache and merged).
- Compare the two merged files:
  - Decompress both and diff, OR
  - Compare object counts (nodes, ways, relations) from both files.
- They should be identical (same objects, same order, same dedup).

### 4. Document Results

Create `_specs/0008_better_overpass_caching/validation_results.md` with:
- Baseline time (full 4-region conversion)
- Full 5-region conversion time (from scratch)
- Incremental 5th-region addition time
- Speedup percentage: `1 - (incremental_time / full_5_region_time)`
- Correctness confirmation (diff result)

### 5. Verify Success Criteria

- The PRD requires: "Adding a new region to a 4-region set takes less than 50% of the time compared to a full 5-region re-conversion."
- If the 50% target is not met, document why and whether further optimization is warranted (e.g., if BZ2 compression of the final merge dominates, the savings from skipping PBF decode may be limited).

## Acceptance Criteria

- [ ] Baseline and incremental timings are measured and documented
- [ ] Merged output from incremental run is verified correct (matches full re-conversion)
- [ ] Speedup meets or approaches the 50% target from the PRD
- [ ] Results are documented in the validation results file
- [ ] If target is not met, root cause is identified and documented

## Notes

- This is a manual validation task — the deliverable is the results document and confirmation that the implementation meets the PRD's success criteria.
- If the speedup target is not met, this task should identify what additional optimization would be needed (e.g., parallel per-region conversion, faster re-merge approach).
- The Overpass DB import time is out of scope — only measure host-side conversion time.

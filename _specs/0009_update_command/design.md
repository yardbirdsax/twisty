# Design Note: `twisty update`

Parent spec: GitHub issue #7. User-facing documentation lives in the README under "twisty update"; this note records the design decisions and the reasons behind them.

## Problem

Local Overpass and Valhalla data is built once from Geofabrik extracts. `start` skips any region whose PBF exists, so refreshing meant deleting files by hand, a long outage (the Overpass import takes hours), and the risk of destroying a working install on a failed rebuild. The live Overpass database (~68 GB) leaves too little disk for a naive second copy, and the derived caches go stale when the data changes.

## Shape

One module (`updater*.go`) owns the feature. `Update(ctx, deps, opts)` returns a per-service report (updated, unchanged, changed, failed). The cobra command (`update_cmd.go`) only maps flags to options and renders the report.

Injected dependencies (`UpdateDeps`): an upstream client (HEAD, download), a container runtime (run, stop, remove, inspect, readiness probe), a `DataRoot` (directories), a clock, a free-space function, and a PBF header timestamp reader. Production wires the Docker helpers and Geofabrik; tests use fakes and a temp directory. This is the seam that lets swap and rollback sequences be tested without Docker or the network.

## Decisions

- **Regions come from the `.regions` files.** `update` never adds a region; `start --regions` does. This keeps each command's meaning clear.
- **Staleness is `Last-Modified` against a stamp.** The stamp (`<pbf dir>/<region>-latest.osm.pbf.upstream`) is the upstream `Last-Modified` of the version held. Missing stamp means changed: the failure mode is refreshing too much. The PBF header `osmosis_replication_timestamp` is shown in `--dry-run` for information only, because Geofabrik's `Last-Modified` is the file generation time and is later than the data timestamp inside the file, so the two are not comparable.
- **Backfill from mtime.** `update backfill-stamps` seeds missing stamps from PBF modification time. A file's mtime is when its download finished, which is later than the upstream `Last-Modified` of that version, so it is a safe bound. One-off migration; never overwrites a stamp.
- **Stamps are written only after every service loading the region was refreshed.** The stamp is shared per region; stamping after one service would make the other look current and never refresh it. A failed refresh is retried next run.
- **Disk preflight.** Need = Overpass database size x 1.15 + `Content-Length` of changed downloads + intermediates (Overpass merged archive, Valhalla merged PBF and tiles). The 1.15 margin, and the assumption that an import needs no temporary space beyond the final database, are unverified and the margin is meant to cover both.
- **Reclaim only when needed, derived data only.** Order: tile and score caches, old merged archive, old Valhalla merged PBF, conversion caches of changed regions. Never the live database, live tiles or region PBFs. If reclaiming cannot make it fit, nothing is deleted and the run aborts with the exact shortfall and a `--in-place` suggestion. With `--in-place` the shortfall is a warning.
- **Staged swaps, delete after probe.** Valhalla builds into `tiles.staging`; Overpass imports into `db.staging` through a temporary container on a spare port (8081). Swap is stop, remove, rename old aside, rename staging into place, run, probe. The old data is deleted only after the probe passes; on any failure the new data is removed, the old data renamed back and the old container restarted. A crash between the two renames is repaired at the start of the next swap.
- **Overpass import marker.** The old rule "merged archive exists means import done" no longer holds, since the archive may be reclaimed. A `.twisty-imported` marker in the database directory records a completed import, and `start` only reconverts when neither the archive nor the marker exists.
- **Overpass `--in-place`.** Stop, wipe, re-import. Explicit opt-in because it means hours of downtime. Download and conversion happen first, while the old database still serves, so failures there cost nothing.
- **Order and isolation.** Valhalla first (quick), Overpass second (heavy), sequentially so they never compete for Docker. One failure does not stop the other. Exit non-zero if any service failed.
- **Cache invalidation.** After a successful Overpass swap, clear the tile and score caches entirely, since tiles are not tracked by region. Valhalla-only runs leave them alone.
- **Lock.** `<pbf dir>/update.lock` holds the owner PID, created exclusively. `start` and `clean` for both services refuse while a live process owns it; a dead PID is stale and ignored. `--dry-run` modifies nothing and takes no lock.
- **No scheduling.** The README carries a sample `launchd` plist instead.

## Testing

Primary seam is `Update` with fakes (ordering of container operations, filesystem state, outcomes): unchanged skip, `--force`, missing stamp, `--only`, `--dry-run`, preflight pass/reclaim/abort, reclaim safety, staged swap and rollback for both services, failure isolation, cache clearing, lock behaviour. The cobra command has a thin flag-mapping test. One build-tagged end-to-end test with real Docker and a tiny PBF verifies the one assumption fakes cannot: that the Overpass image restarts against an existing database without re-importing.

## Out of scope

Built-in scheduling, adding or removing regions, diff-based Overpass updates, automatic fallback to public instances, zero-downtime Valhalla swap, shrinking the Overpass database, region-scoped cache invalidation.

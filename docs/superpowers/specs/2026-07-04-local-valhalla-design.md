# Local Valhalla Routing Server

## Problem

`twisty build` routes legs via the public `valhalla1.openstreetmap.de` instance. When that
instance is down or rate-limiting, all map clicks silently fail — the route leg fetch returns
an error and the waypoint is discarded with a toast. There is no fallback and no way to run
routing locally.

## Goal

Add `twisty valhalla` — a Docker-managed local Valhalla routing server that mirrors the
lifecycle shape of `twisty overpass` — and wire `twisty build` to optionally target it via a
`--valhalla-url` flag.

## User Workflow

```
# One-time setup: download region PBF, build Valhalla tiles, start server
twisty valhalla start --regions north-america/us/tennessee

# Later sessions: just start (data already present)
twisty valhalla start

# Use local Valhalla in build
twisty build --address "Nashville, TN" --valhalla-url http://localhost:8002

# Lifecycle management
twisty valhalla status
twisty valhalla stop
twisty valhalla logs
twisty valhalla clean
```

## Architecture

### New files

- **`valhalla.go`** — all `twisty valhalla` subcommand logic (start, stop, status, logs,
  clean). Mirrors the structure of `overpass.go`.
- **`pbf.go`** — shared PBF download helpers extracted from `overpass.go`:
  `downloadPBF`, `downloadPBFsParallel`, `pbfFilename`, `mergeRegions`.

### Modified files

- **`overpass.go`** — remove the functions moved to `pbf.go`; update `pbfDir` to use the
  shared `~/.twisty/pbf/` path instead of `<dataDir>/pbf/`.
- **`main.go`** — register `newValhallaCmd()` in `newRootCmd()`; add `--valhalla-url` flag to
  `twisty build`.
- **`route/valhalla.go`** — export `fetchRoutesFromURL` → `FetchRoutesFromURL` so
  `route_build.go` can call it directly with a configurable base URL.

### Data layout

```
~/.twisty/
  pbf/            ← shared PBF cache used by both overpass and valhalla
  overpass/
    ...
  valhalla/
    tiles/        ← Valhalla tile set built from PBF
    .regions      ← stamp file: region used for last tile build
```

## `twisty valhalla start` Behaviour

1. **Resolve PBF** — download the region PBF to the shared `~/.twisty/pbf/` directory.
   Download is skipped if the file already exists.

2. **Build tiles** — run `ghcr.io/valhalla/valhalla:latest` (supports arm64 natively; no
   image patching needed) to completion before starting the server. Tiles go in
   `~/.twisty/valhalla/tiles/`. The build step is skipped if the tiles directory is already
   populated and the stored region set matches. Re-building requires `twisty valhalla clean`
   first.

   Multiple regions are supported. All PBF paths are passed to a single
   `valhalla_build_tiles` invocation; Valhalla accumulates tiles from each file into the same
   tile directory.

   The sorted, comma-separated region list used for the last tile build is recorded in
   `~/.twisty/valhalla/.regions`. On subsequent `start` invocations with no `--regions` flag,
   the stored region list is used. If regions grow, the tiles directory is wiped and rebuilt
   from the full set — the same re-import policy as `twisty overpass start`.

   Tile-build invocation (one `-v <pbf-path>:/custom_files/<filename>` mount per region,
   all PBF filenames appended to the final `valhalla_build_tiles` call):
   ```
   docker run --rm \
     -v <tiles-dir>:/custom_files \
     -v <pbf1-path>:/custom_files/<pbf1-filename> \
     [-v <pbf2-path>:/custom_files/<pbf2-filename> ...] \
     ghcr.io/valhalla/valhalla:latest \
     bash -c "valhalla_build_config \
                --mjolnir-tile-dir /custom_files/tiles \
                --mjolnir-timezone /usr/share/zoneinfo/UTC \
                --mjolnir-admin /custom_files/admins.sqlite \
                > /custom_files/valhalla.json && \
              valhalla_build_timezones /custom_files/valhalla.json && \
              valhalla_build_admins --config /custom_files/valhalla.json \
                /custom_files/<pbf1-filename> [/custom_files/<pbf2-filename> ...] && \
              valhalla_build_tiles -c /custom_files/valhalla.json \
                /custom_files/<pbf1-filename> [/custom_files/<pbf2-filename> ...]"
   ```

3. **Start server** — run the same image as a daemonised HTTP server.
   ```
   docker run -d \
     --name twisty-valhalla \
     --restart unless-stopped \
     -p <port>:8002 \
     -v <tiles-dir>:/custom_files \
     ghcr.io/valhalla/valhalla:latest \
     valhalla_service /custom_files/valhalla.json 1
   ```

4. **Readiness probe** — poll `GET http://localhost:<port>/status` every 2 s until HTTP 200.
   Timeout: 5 minutes.

5. **Re-entrant** — if `twisty-valhalla` is already running, print the URL and exit 0.

## Subcommands

| Subcommand | Behaviour |
|---|---|
| `start [--regions R] [--port N] [--data-dir DIR]` | Download PBF, build tiles, start server |
| `stop` | `docker stop twisty-valhalla` |
| `status` | Print running/stopped + URL |
| `logs [--lines N]` | Stream or tail container logs (0 = follow) |
| `clean [--data-dir DIR]` | Stop container, remove data directory |

Defaults: `--port 8002`, `--data-dir ~/.twisty/valhalla/`.

## `twisty build` Change

Add `--valhalla-url` flag (default: `"https://valhalla1.openstreetmap.de"`). Pass it through
`buildServer` and the existing call chain to `route.FetchRoutesFromURL` (the newly-exported
version of `fetchRoutesFromURL`).

## Error Handling

- No Docker: return a clear error ("docker not found in PATH").
- Tile-build failure: stream container logs to stderr, return error.
- Readiness timeout: print "Container is still running. Use 'twisty valhalla logs' to
  inspect." and return error — same pattern as overpass.

## Testing

- Unit tests for `valhallaDockerBuildArgs` and `valhallaDockerRunArgs` (pure functions,
  verify flag shapes without spawning Docker).
- Unit tests for `resolveValhallaDataDir`, `resolvePBFDir`, `probeValhalla`, and
  `valhallaDockerBuildArgs` with multiple regions.
- Tests for extracted `pbf.go` functions mirror existing overpass tests; overpass tests
  continue to pass unchanged.
- Integration / e2e tests: mirror the `overpass_e2e_*` pattern with a build tag so they
  don't run in CI by default.

## Out of Scope

- ARM64 cross-compilation patching (Valhalla's official image supports arm64 natively).
- Automatic fallback from public Valhalla to local in `twisty build`.

## Constraints

- No new external Go dependencies — stdlib only.
- All existing tests must continue to pass (`make test`).
- Use TDD: write the failing test before each piece of implementation.
- Container name: `twisty-valhalla`.
- Default port: `8002`.
- Default data dir: `~/.twisty/valhalla/`.

# Routing Diagnostics Implementation Plan

**Goal:** Make a broken or incomplete local Valhalla tile set impossible to miss, and stop a half-built tile set from being treated as complete.

**Background (incident):** A `twisty valhalla start` rebuild died after writing only level-2 tiles (levels 0 and 1 never built; intermediate `*.bin` files left behind). `.regions` is stamped *before* the build, and the next `start` only checks that the tiles directory is non-empty, so it never rebuilt. The container from before the rebuild kept running and served stale cached tiles. Result: `/api/route-leg` returned HTTP 200 with a route whose last point was ~530 m from the requested destination (Valhalla snapped to the nearest road it still knew about), and the UI showed nothing wrong.

## Amendments after final review

The whole-branch review changed these plan points (implemented in commit 4037fe4):

- **Task 1, requirement 4 (grandfathering):** a tiles dir with no `.built` marker is accepted only if all three levels have tiles **and** no intermediate `*.bin` files remain directly in `<tilesDir>/tiles/`. Completed builds leave none; a build killed late leaves them. Without this guard a late-killed build would be trusted after its marker was deleted.
- **Task 2, requirement B (health wording):** the "rebuild with `twisty valhalla clean`/`start`" advice is given only when `/locate` returns no edges and the Valhalla URL is local (localhost, 127.0.0.1, ::1). A nearest edge farther than `healthSnapMaxMeters` gets non-destructive wording ("Nearest road to <place> is <N> m away; routing near the map center may be unreliable."). A remote server's message omits the local commands.
- **Task 3, requirement 4 (toast):** a gap at the route start reads "Route starts <N> m from waypoint <k> …" instead of "Route ends …".
- **Global constraints:** `snapWarnMeters` exists only as `SNAP_WARN_M` in `static/build.js`. The server reports distances and never classifies a leg, so there is no Go constant.

## Global Constraints

- Red-green TDD: write the failing test first, watch it fail for the expected reason, then implement. Record RED and GREEN output in the report.
- Run `make test` (this is `go test -short ./...` plus `node --test` on `static/statusManager.test.js` and `static/build.test.js`) before committing. Output must be pristine.
- Do not set `GOCACHE` manually when running go commands.
- Create files with the Write tool, never `cat` heredocs or `echo >`.
- To search, use `find ... | xargs grep`, not bare `grep -r`.
- Match surrounding code style and comment density. No new dependencies.
- Do not add a max-radius validation to the `score` command, and do not add `twisty random` to README.md (both intentional).
- Thresholds are named constants, not inline numbers: `snapWarnMeters = 150` (per-leg), `healthSnapMaxMeters = 200` (startup probe).
- Distances use the existing `geo.Haversine` (meters).
- Commit with message style `fix:` / `feat:` (see `git log`), ending with the line `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.

## File Structure

- `valhalla.go`, `valhalla_test.go` — Task 1
- `route_build.go`, `route_build_test.go` — Task 2
- `static/build.js`, `static/build.html`, `static/build.test.js` — Task 3

---

### Task 1: Valhalla tile build integrity

**Files:** `valhalla.go`, `valhalla_test.go`

Valhalla's on-disk layout, relative to the tiles directory that `runValhallaStart` mounts as `/custom_files` (`<dataDir>/tiles`): finished tiles live in `<tilesDir>/tiles/{0,1,2}/**/*.gph`. A complete build has at least one `.gph` file under each of levels 0, 1 and 2. Intermediate build files (`*.bin`) sit directly in `<tilesDir>/tiles/` during a build.

**Requirements**

1. Add `tilesComplete(tilesDir string) (ok bool, problem string)` (or equivalent) that returns false, with a human-readable reason, if any of levels 0, 1, 2 has no `.gph` file under `<tilesDir>/tiles/<level>/`. A directory that has all three levels is complete.
2. Add a completion marker. After `valhalla_build_tiles` (the `dockerCmdStreaming` build call) returns success, and only then, write a marker file `<dataDir>/.built` (next to `.regions`). The marker must not be written when the build fails.
3. In `runValhallaStart`, replace the "build if `len(entries) == 0`" check: tiles are considered built only if the marker exists **and** `tilesComplete` is true. Otherwise: print which condition failed to stderr (for example `Tiles incomplete (no .gph files for level 0); rebuilding.`), remove the tiles dir contents and the marker, and run the build. Keep the existing `.regions` stamp-before-build behaviour and keep the merged PBF and downloaded PBFs (do not re-download).
4. Existing installs have complete tiles but no marker (the user's current good tiles are an example). To avoid a 15-minute needless rebuild for them: if there is no marker but `tilesComplete` is true, write the marker and proceed without rebuilding.
5. Restart after rebuild: if tiles were (re)built during this invocation and the container `twisty-valhalla` is already running, stop and remove it and start a fresh one (so it does not keep serving stale tiles). If tiles were not rebuilt and the container is running, keep the current "already running" behaviour.
6. `twisty valhalla status`: after the running/not-running line, add an on-disk check using `tilesComplete` on the default data dir, and print a clear warning line when incomplete. `newValhallaStatusCmd` currently has only `--port`; add `--data-dir` like the other subcommands for consistency.

**Tests** (in `valhalla_test.go`, using `t.TempDir()` fixtures; the docker calls are not exercised, so factor the decision logic into a small pure function that can be tested, for example `needsBuild(dataDir) (bool, reason)`):

- levels 0/1/2 all present → complete; only level 2 present (with `*.bin` beside it) → incomplete naming level 0; empty dir → incomplete.
- marker absent + complete tiles → no rebuild, marker gets written (requirement 4).
- marker present + incomplete tiles → rebuild.
- marker absent + incomplete tiles → rebuild.

**Acceptance:** `make test` passes; a tiles dir containing only level 2 causes `needsBuild` to report a rebuild.

---

### Task 2: Server-side detection (per-leg snap distance and routing health)

**Files:** `route_build.go`, `route_build_test.go`

**Requirements**

A. Per-leg snap distance. `routeLegResponse` currently has `Points`, `Duration`, `Distance`. Add `SnapFromM` (`json:"snap_from_m"`) and `SnapToM` (`json:"snap_to_m"`), float64 meters, rounded to the nearest meter: `snap_from_m` is the Haversine distance between the requested `from` and the route's first point; `snap_to_m` between the requested `to` and the route's last point. Always include both, never fail the request on a large value (the response stays HTTP 200). Compute them in `handleRouteLeg` after the best route is chosen.

B. Routing health endpoint. Add `GET /api/health` returning JSON `{"routing": {"ok": bool, "message": string, "snap_m": number}}` (register it in the mux next to the other `/api/...` routes). The probe sends a Valhalla `POST <valhallaURL>/locate` with `{"locations":[{"lat":<center.Lat>,"lon":<center.Lon>}],"verbose":true}` using the same URL resolution as `handleRouteLeg` (`s.valhallaURL`, falling back to `route.ValhallaBaseURL`) and a 3 second timeout. Valhalla's `/locate` response is an array; each entry has `edges` (each with `correlated_lat`, `correlated_lon` and `distance` in meters, where `distance` is how far the input point is from the snapped point). Rules:
  - Server unreachable, non-200, or unparsable → `ok:false`, message says the routing server is unreachable (include the error text), `snap_m: 0`.
  - Reachable but `edges` empty → `ok:false`, message `Routing server has no road data near <center.DisplayName>. Its tiles may be incomplete; rebuild with 'twisty valhalla clean' then 'twisty valhalla start'.`
  - Nearest edge `distance` greater than `healthSnapMaxMeters` → `ok:false`, same message style with the distance, `snap_m` set to it.
  - Otherwise `ok:true`, empty message, `snap_m` = nearest edge distance.
  The probe is made on each request (it is cheap); no caching.

C. Startup warning. In `execBuild`, after the center is resolved and before serving, run the same probe once; if not ok, print `warning: <message>` to stderr. Never abort startup because of it.

Put the probe in its own small function (for example `probeRouting(valhallaURL string, center geocode.Result) routingHealth`) so handler and startup share it.

**Tests** (`route_build_test.go`, using `httptest.NewServer` stubs injected via `buildServer.valhallaURL`; look at the existing `TestHandleRouteLeg_*` tests for the pattern):

- `route-leg`: stub returns a polyline whose last point is ~500 m from the requested `to` → response `snap_to_m` ≈ 500 (within a few meters) and `snap_from_m` small; status 200. A second case where both ends match → both near 0.
- `health`: stub `/locate` returning `[{"edges":[]}]` → `ok:false`, message mentions "no road data"; stub returning an edge with `distance: 12` → `ok:true`; stub returning `distance: 900` → `ok:false` with `snap_m` 900; stub that returns HTTP 502 → `ok:false`, message mentions unreachable.
- Reuse an existing helper for building a fake Valhalla `/route` JSON if one exists in `route_build_test.go`; otherwise build the polyline6 shape with the encoder used by `route/valhalla_test.go`.

**Acceptance:** `make test` passes; the new fields are present in `/api/route-leg` responses and `/api/health` exists.

---

### Task 3: Make it obvious in the UI

**Files:** `static/build.js`, `static/build.html`, `static/build.test.js`

Depends on Task 2's response shapes: `/api/route-leg` now returns `snap_from_m` and `snap_to_m`; `/api/health` returns `{routing:{ok,message,snap_m}}`.

**Requirements**

1. Constant `SNAP_WARN_M = 150` near the other constants in `build.js`. A leg "has a gap" when `snap_from_m` or `snap_to_m` exceeds it.
2. Store the two values on each entry pushed to `legs` (both code paths: the add-waypoint path near `build.js:1169` and `rerouteAll` near `build.js:471`), as `snapFromM` and `snapToM`, and make sure `saveState` and the state loader round-trip them. Older saved states without the fields must load without errors (treat as 0).
3. For a leg with a gap, draw a dashed polyline (`dashArray`, a clearly visible warning colour, same `routePane`) from the route's end point to the waypoint it missed (and from the waypoint to the route start for `snapFromM`). Remove these layers wherever `legPolylines` layers are removed so they do not leak (rerouteAll, removeLastWaypoint, clear/load paths). Keep them in a separate array if mixing them into `legPolylines` would break the index-based pops.
4. Show a toast once per leg when it is routed with a gap: `Route ends <N> m from waypoint <k> — the router may not know this road.` (`<N>` rounded to the nearest 10 m; `<k>` is the 1-based waypoint number).
5. In the waypoint list (`renderWaypointList`), add a warning badge on the row of any waypoint whose adjoining leg has a gap, with a `title` giving the distance.
6. Routing health banner: on page load, `fetch('/api/health')`. If `routing.ok` is false, show a persistent, non-modal banner at the top of the map area with `routing.message`. If the fetch itself fails, show nothing (do not block the UI). Add the banner element and its styles to `static/build.html` using the existing style conventions (including dark-mode handling if the page has it).
7. Keep functions small and in the style of the file. Put the pure logic (gap detection from a response, message text, rounding) in plain functions so `build.test.js` can test them under the existing `vm` loader.

**Tests** (`static/build.test.js`, following the existing `makeCtx`/`loadBuildJS` pattern; only pure logic is testable there):

- gap detection: `{snap_to_m: 530, snap_from_m: 10}` → gap, `{snap_to_m: 40, snap_from_m: 12}` → no gap, missing fields → no gap, boundary value exactly 150 → no gap.
- toast message formatting and rounding (530 → `530`, 534 → `530`, 536 → `540`).
- state round-trip keeps `snapFromM`/`snapToM`; a legacy saved state without them loads with 0.

Exercise the visual parts manually only if you can; say in the report what you could and could not verify.

**Acceptance:** `make test` passes (including `node --test static/build.test.js`).

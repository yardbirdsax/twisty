# Product Requirements Document: Tiled Overpass Fetch with Caching

## Overview

This system is Stage 1 of the curvature scoring pipeline for Twisty. It fetches all driveable road data from the OpenStreetMap Overpass API within a radius of a user-specified address, using a tile-based decomposition strategy that enables incremental caching and avoids Overpass query size limits. It provides the foundational dataset that all downstream pipeline stages (filtering, scoring, aggregation) operate on.

## Problem Statement

The curvature scoring pipeline needs comprehensive road data for a geographic area, but the Overpass API has practical limits on query size and rate. A single large-area query can time out or be rejected, and repeating the same query across pipeline runs wastes time and API resources.

1. **Large queries fail.** Fetching all highway ways within a 25-50 km radius in a single Overpass query can exceed the API's timeout or memory limits, especially in road-dense urban areas. The query either returns an error or incomplete results, with no way to resume.

2. **Repeated fetches are wasteful.** During iterative development and repeated pipeline runs over the same area, re-fetching identical data from Overpass is slow (seconds to minutes) and unnecessarily loads a shared public API.

3. **No incremental coverage.** A user exploring roads in an area may run the pipeline multiple times with overlapping but shifted centers. Without incremental caching, each run fetches from scratch, even for areas already retrieved.

## Users

Drivers and motorcyclists who want to discover twisty, scenic roads near a location. They interact with Twisty via the CLI, specifying an address (e.g., their home, a trailhead, a town) and a search radius. They may run the tool repeatedly as they explore different areas or refine their search, and expect subsequent runs to be faster.

## Design Principles

### 1. Tile-Based Decomposition

All Overpass fetching operates on fixed-size geographic tiles, never on arbitrary bounding boxes. This ensures every query is bounded in size (preventing API failures), every response maps to a cacheable unit, and overlapping user queries naturally share cached tiles. The tile grid is global and deterministic — the same tile boundaries apply regardless of query center.

### 2. Cache Is Additive

The cache only grows. Each fetched tile is stored permanently until the user explicitly clears it. Subsequent queries that overlap previously cached areas skip those tiles entirely. Over time, a user builds up a local mirror of road data for their region without any coordination or planning.

### 3. Raw Storage, Late Processing

Cached tile files store the raw Overpass JSON response, not processed Way objects. This preserves all OSM data (including tags and fields we may not use today) and avoids coupling the cache format to the application's data model. Parsing and deduplication happen at read time when tiles are merged for pipeline consumption.

### 4. Small Queries, Graceful Failure

Individual tile fetches are small enough to reliably succeed. If a single tile fetch fails, the system reports the error and continues with remaining tiles rather than aborting the entire operation. Partial results (cached tiles + successfully fetched tiles) are still usable by downstream stages.

## Feature 1: Address-to-Tiles Resolution

### Behavioral / Functional Goal

The user specifies a human-readable address and a search radius. The system resolves this into a concrete set of geographic tiles that cover the circular search area.

### Trigger / Entry Point

User invokes the `score` command (or future curvature pipeline entry point) with an `--address` flag and an optional `--radius` flag.

### Core Behavior

1. Geocode the address to a `(lat, lon)` center point using the existing Nominatim client in `geocode/`.
2. Compute a bounding box that encloses the circle defined by `(center, radius)`. Convert the radius in kilometers to approximate degree offsets using the Haversine-based relationship (latitude offset is straightforward; longitude offset accounts for latitude compression).
3. Snap the bounding box edges outward to the nearest tile boundaries, producing a grid of tile coordinates. Each tile is identified by its `(south, west)` corner, quantized to the tile size.
4. Return the list of tile coordinates that need to be fetched or loaded from cache.

### Rules and Constraints

- Default radius: 25 km
- Maximum radius: 50 km. Values above this are rejected with an error message.
- Default tile size: 0.05° (~5.5 km at mid-latitudes)
- Tile size is configurable via a `--tile-size` flag (in degrees)
- Tile boundaries are aligned to a global grid (i.e., `floor(coord / tileSize) * tileSize`), not relative to the query center. This ensures cache hits across different query centers.

### Data Model

Tile coordinate:

```json
{
  "south": 42.35,
  "west": -72.60,
  "north": 42.40,
  "east": -72.55
}
```

The tile is identified by its `(south, west)` pair, which is deterministic given the tile size.

## Feature 2: Tile-Based Overpass Fetching

### Behavioral / Functional Goal

Fetch road data from the Overpass API one tile at a time, skipping tiles that are already cached. The user sees progress as tiles are fetched, and the operation is resilient to individual tile failures.

### Trigger / Entry Point

Called internally after tile resolution produces the list of tiles needed for the query.

### Core Behavior

1. For each tile in the resolved set, check if a cache file exists for that tile.
2. If cached, skip the tile (log that it was loaded from cache in verbose mode).
3. If not cached, send an Overpass query for that tile's bounding box using the existing `HighwayFilter` and `[out:json]` format. The query must also request the OSM element ID (use `out geom` which includes IDs by default in the JSON output, but the response must include `"type": "way"` and `"id": <int64>`).
4. On transient failure (HTTP 429, 5xx, timeout, network error), retry with exponential backoff: initial delay 2 seconds, doubling each attempt, up to 3 retries (2s, 4s, 8s). Non-retryable errors (4xx other than 429, JSON parse errors on a 200 response) fail immediately.
5. On success, write the raw JSON response body to the cache file.
6. On permanent failure (all retries exhausted or non-retryable error), log a warning and continue to the next tile. Do not cache error responses.
7. Respect Overpass API rate limits by pausing briefly (1 second) between consecutive successful fetches.

### Rules and Constraints

- Use the existing `overpassHTTPClient` with its 30-second timeout per request.
- Reuse the existing `HighwayFilter` constant for query consistency with the route-quality pipeline.
- The Overpass query timeout (`[timeout:25]`) remains at 25 seconds per the existing implementation.
- Individual tile failures (after retries are exhausted) do not abort the overall fetch. The system proceeds with all available data.
- Retry up to 3 times with exponential backoff (2s, 4s, 8s) on transient errors (HTTP 429, 5xx, timeouts, network errors). Non-retryable errors (4xx other than 429) fail immediately.
- A 1-second delay between consecutive successful fetches avoids rate-limiting.

### Data Model

Raw Overpass JSON response per tile (stored as-is):

```json
{
  "elements": [
    {
      "type": "way",
      "id": 12345678,
      "tags": {
        "highway": "secondary",
        "name": "Mountain Road",
        "surface": "asphalt"
      },
      "geometry": [
        {"lat": 42.361, "lon": -72.591},
        {"lat": 42.362, "lon": -72.589}
      ]
    }
  ]
}
```

## Feature 3: File-Based Tile Cache

### Behavioral / Functional Goal

Persistently store fetched tile data on disk so that subsequent pipeline runs skip already-fetched tiles. The cache accumulates over time and is only cleared by explicit user action.

### Trigger / Entry Point

Read: checked before each tile fetch. Write: after each successful tile fetch.

### Core Behavior

1. Cache directory defaults to `~/.twisty/cache/overpass/`. Created automatically on first use.
2. Each tile is stored as a single JSON file named by its grid coordinates: `tile_{south}_{west}.json` (coordinates formatted to avoid floating-point ambiguity, e.g., `tile_42.350_-72.600.json`).
3. Cache lookup is a simple file existence check. If the file exists, the tile is considered cached.
4. **Access tracking via file mtime:** When a cached tile is read, its file modification time is updated via `os.Chtimes` to the current time. This means the file's mtime always reflects the last time the tile was *used*, not when it was fetched. This enables filesystem-native staleness detection without any sidecar metadata.
5. No TTL or automatic expiration. Cache is cleared manually by the user (deleting files or the directory), or by using the `--purge-older-than` flag.
6. A `--no-cache` flag bypasses cache reads (re-fetches all tiles) but still writes results to cache, refreshing stale data.
7. A `--clear-cache` flag deletes all cached tiles before running the fetch.
8. A `--purge-older-than` flag accepts a duration (e.g., `90d`, `6m`) and deletes all cache files whose mtime is older than that threshold before running the fetch. This lets users reclaim disk space from tiles they haven't queried in a long time.

### Rules and Constraints

- Cache file names must encode coordinates with sufficient precision to match the tile grid (suggest 3 decimal places for 0.05° tiles, adjustable with tile size).
- The cache directory path should be configurable via a `--cache-dir` flag.
- Cache files contain the raw Overpass JSON response, not parsed objects.
- Incomplete or corrupt cache files (e.g., from interrupted writes) should be handled gracefully. Write to a temporary file and rename atomically.

### Data Model

Cache directory layout:

```
~/.twisty/cache/overpass/
├── tile_42.350_-72.600.json
├── tile_42.350_-72.550.json
├── tile_42.400_-72.600.json
└── tile_42.400_-72.550.json
```

## Feature 4: Tile Merge and Way Deduplication

### Behavioral / Functional Goal

Combine data from all tiles (cached and freshly fetched) into a single deduplicated set of Way objects for downstream pipeline consumption.

### Trigger / Entry Point

Called after all tiles have been fetched or loaded from cache.

### Core Behavior

1. Parse each tile's cached JSON file into a list of Way objects.
2. Deduplicate ways by OSM way ID. When the same way appears in multiple tiles, keep only one instance (the first encountered is fine — the data is identical).
3. Return the deduplicated slice of `Way` objects with their full tags and geometry.

### Rules and Constraints

- The `Way` struct must be extended to include an `ID` field (`int64`) to support deduplication.
- Deduplication is by OSM way ID only. Do not attempt to merge or reconcile differing tag sets (they should be identical for the same way).
- Log the total number of ways fetched vs. deduplicated count in verbose mode, to help users understand cache efficiency.
- Ways that span tile boundaries will appear in multiple tiles. After deduplication, their full geometry is preserved (Overpass `out geom` returns complete geometry for each way, not clipped to the query bbox).

### Data Model

Extended Way struct:

```json
{
  "id": 12345678,
  "tags": {
    "highway": "secondary",
    "name": "Mountain Road",
    "surface": "asphalt"
  },
  "geometry": [
    {"lat": 42.361, "lon": -72.591},
    {"lat": 42.362, "lon": -72.589}
  ]
}
```

## Technical Direction

### Tile Grid Computation

Use `math.Floor(coord / tileSize) * tileSize` to snap coordinates to a global grid. Iterate from the southwest corner to the northeast corner of the enclosing bounding box in tile-size increments to enumerate all tiles. The radius-to-degrees conversion should use `radius_km / 111.32` for latitude and `radius_km / (111.32 * cos(lat_radians))` for longitude.

### Overpass Query Reuse

The existing `fetchWaysFromURL` function and `HighwayFilter` constant should be reused. The `overpassElement` struct needs an `ID int64 \`json:"id"\`` field added. The `fetchWaysFromURL` function (or a variant) should be updated to return the raw response bytes alongside parsed ways, or a new function should handle raw-response caching while the existing function remains unchanged for the route-quality pipeline.

### Retry Logic

Implement retry as a standalone helper function (e.g., `retryWithBackoff(fn, maxRetries, initialDelay)`) that accepts a function and returns its result or final error. This keeps retry logic decoupled from fetch logic, making it reusable and testable independently. The retry helper should accept a `context.Context` so that a future concurrency layer can cancel in-flight retries.

### Concurrency Readiness

While Phase 1 fetches tiles sequentially, the design should not preclude future parallelism:
- The per-tile fetch function should be stateless and self-contained — it takes a tile coordinate and cache directory, and returns a result. No shared mutable state between tile fetches.
- Use `context.Context` in the fetch function signature from the start, even though Phase 1 passes `context.Background()`. This avoids a signature-breaking change when adding concurrency.
- Cache writes use atomic rename, which is safe under concurrent access.
- The 1-second inter-request delay is applied by the orchestration loop, not baked into the fetch function itself.

### Cache I/O

Use `os.MkdirAll` for directory creation, `os.WriteFile` with a temp-file-and-rename pattern for atomic writes, and `os.ReadFile` for reads. No external dependencies needed — this stays pure standard library, consistent with the rest of the codebase.

### Integration with Existing System

The existing `quality/overpass.go` FetchWays function and Way struct are used by the route-quality pipeline (the `route` command). The curvature pipeline fetch is a separate use case that shares the same Overpass query format and Way model but adds tiling, caching, and ID tracking.

New code lives in:
- `quality/tilefetch.go` — tile grid computation, tiled fetch orchestration, cache read/write
- `quality/tilefetch_test.go` — unit tests using httptest mock servers
- `quality/overpass.go` — minor modifications (add ID field to Way and overpassElement structs)

## Phasing

### Phase 1: Core Tiled Fetch with Caching

**Hypothesis:** Decomposing Overpass queries into fixed tiles with file-based caching enables reliable, incremental road data fetching for areas up to 50 km radius.

**Scope:**
- Tile grid computation from center point + radius
- Per-tile Overpass fetching with 1-second inter-request delay
- File-based tile cache with atomic writes
- Way deduplication by OSM ID
- Add ID field to Way struct
- `--no-cache`, `--clear-cache`, and `--purge-older-than` flags
- Configurable tile size, radius, and cache directory
- Integration with geocode package for address resolution
- Retry with exponential backoff (3 retries, 2s/4s/8s) on transient Overpass errors
- `context.Context` threaded through fetch functions for future concurrency
- Stateless per-tile fetch function with orchestration-level delays
- Verbose logging of fetch progress (tiles cached vs. fetched, retries, way counts)

**Success measurement:**
- A 25 km radius fetch completes without Overpass timeouts or errors
- Second run of the same area completes in under 1 second (all cache hits)
- Overlapping queries (shifted center) correctly reuse cached tiles
- Way count after deduplication matches expectations (no duplicates, no missing ways)

**Exit criteria for Phase 2:** Fetch stage reliably produces a complete, deduplicated set of Ways for areas up to 50 km radius, and the cache accumulates correctly across runs.

### Phase 2: Pipeline Integration

**Hypothesis:** The tiled fetch output can directly feed the downstream curvature scoring stages (hard filter, segment scoring, aggregation) as described in the curvature pipeline spec.

**Scope:**
- Wire tiled fetch into the `score` CLI command
- Pass fetched Ways to Stage 2 (hard filter) and subsequent stages
- End-to-end pipeline run from address to scored road output

**Success measurement:**
- `score` command produces curvature-scored roads from a single address input
- Pipeline stages consume the Way data without modification to the fetch output format

**Exit criteria for Phase 2:** Full curvature pipeline runs end-to-end.

## Non-Goals

- **Real-time or streaming fetching.** All tiles are fetched before the pipeline proceeds. There is no streaming or incremental pipeline processing.
- **Cache TTL or automatic invalidation.** OSM data changes infrequently enough that manual clearing or usage-based purging is sufficient. There is no automatic background expiration.
- **Parallel tile fetching.** Overpass API rate limits make parallel requests counterproductive. Sequential fetching with a delay is the correct approach.
- **Custom Overpass queries.** The highway filter is fixed. Users cannot specify arbitrary Overpass queries.
- **Tile-level partial results.** If a tile fetch fails after retries, the entire tile is skipped. There is no partial tile caching.
- **Modifying the existing route-quality pipeline.** The `FetchWays` function used by the `route` command remains unchanged. The tiled fetch is a new code path for the curvature pipeline.

## Success Criteria

1. A 25 km radius fetch in a moderately road-dense area (e.g., rural New England) completes all tiles without Overpass errors.
2. Subsequent runs over the same area complete in under 1 second by reading entirely from cache.
3. Partially overlapping queries reuse cached tiles, fetching only new tiles.
4. The deduplicated Way set contains correct, complete geometry for all ways in the area (verified by spot-checking against OSM).
5. The Way struct's new ID field does not break existing route-quality pipeline functionality.

## Future Considerations

- **Parallel fetching with rate limiting:** If Overpass capacity improves or a private instance is used, tile fetches could be parallelized with a concurrency limiter. The tile-based design supports this without structural changes.
- **Cache sharing and portability:** Tile cache files are self-contained JSON. They could be shared between users or machines, or distributed as pre-built regional datasets.
- **OSM-aware cache invalidation:** A future enhancement could check OSM changeset timestamps to detect tiles where the underlying data has actually changed, enabling selective re-fetching rather than blanket purging.
- **Alternative data sources:** The tile/cache architecture is not Overpass-specific. A different OSM data source (e.g., direct PBF file parsing) could populate the same cache format.

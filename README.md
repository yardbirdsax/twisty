# twisty

`twisty` is a CLI tool for finding and scoring curvy roads using OpenStreetMap data. It fetches road geometry from the Overpass API, scores segments based on curvature, and outputs files you can load in Google Earth or similar tools.

Heavily influenced by the amazing [Curvature](https://roadcurvature.com/) project; please go donate and support it!`

## Subcommands

- `twisty score` — score roads in a region and write a KML file
- `twisty fetch` — pre-populate the Overpass tile cache for a region
- `twisty route` — uses the [Valhalla API](https://valhalla.openstreetmap.de/) to construct routes between two points, then picks the most twisty one
- `twisty overpass` — manage a local Overpass API instance running in Docker

## twisty score

Scores roads in a circular region around a center address and writes a KML file with color-coded results.

```
twisty score -address <address> -out <file.kml> [flags]
```

### Required flags

| Flag | Description |
|------|-------------|
| `-address string` | Center address for the search region |
| `-out string` | Output KML file path |

### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `-radius float` | `25.0` | Search radius in km |
| `-tile-size float` | `0.05` | Tile size in degrees for grid-based fetching |
| `-cache-dir string` | `~/.twisty/cache/overpass/` | Overpass tile cache directory |
| `-no-cache` | false | Skip score cache reads (cache writes still happen) |
| `-clear-score-cache` | false | Delete all score cache entries before running |
| `-min-score float` | `0` | Minimum penalized score to include in output |
| `-multi-color` | false | Color each segment by curvature tier instead of per-road |
| `-overpass-url string` | `https://overpass-api.de/api/interpreter` | Overpass API endpoint |
| `-v` | false | Verbose logging to stderr |

### Example

```bash
twisty score \
  -address "Mulholland Drive, Los Angeles, CA" \
  -radius 15 \
  -out twisty_roads.kml \
  -min-score 500
```

### How scoring works

#### 1. Hard filter

Roads are excluded if they are:
- Unpaved (gravel, dirt, mud, sand)
- Private or access-restricted
- Highway type: motorway, track, path, footway, cycleway, bridleway, steps, or residential

#### 2. Curvature scoring

Each road segment is scored using its circumradius — the radius of the circle passing through three consecutive GPS nodes. A smaller radius means a tighter curve.

Segments are assigned to tiers:

| Tier | Circumradius | Weight |
|------|-------------|--------|
| 4 | < 30 m | 2.0 |
| 3 | 30–60 m | 1.6 |
| 2 | 60–100 m | 1.3 |
| 1 | 100–175 m | 1.0 |
| 0 | ≥ 175 m | 0.0 (straight) |

Segment score = segment length (meters) × tier weight.

#### 3. Aggregation

Ways sharing a name or ref tag are grouped together into road collections. Within each group, connected ways are assembled into continuous paths using directed-graph endpoint matching. Sections are split at:
- Gaps > 100 m between way endpoints
- Straight runs ≥ 2414 m (1.5 miles) of tier-0 segments

A deflection filter then zeroes out sections where the cumulative heading change over a 2400 m window is less than 20°, eliminating false positives on gently curving roads.

#### 4. Penalties

Each road collection is penalized based on its highway type(s):

| Type | Multiplier |
|------|-----------|
| motorway / motorway_link | 0.3 |
| trunk / trunk_link | 0.5 |
| primary / primary_link | 0.8 |
| secondary / secondary_link | 0.9 |
| tertiary and below | 1.0 |

Roads with mixed types get the lowest applicable multiplier. The final `PenalizedScore` is used for filtering (`-min-score`) and KML ordering.

#### 5. Minimum road length

Road collections shorter than 1609 m (1 mile) are excluded from output.

### KML output

Roads are sorted by penalized score (highest first). Each road appears as a named folder containing its polyline(s).

**Default mode:** Each road is drawn as a single polyline colored on a green→yellow→red→magenta gradient based on its total penalized score.

**Multi-color mode** (`-multi-color`): Each segment is colored by its curvature tier:

| Tier | Color |
|------|-------|
| 0 | Green (straight) |
| 1 | Yellow |
| 2 | Orange |
| 3 | Dark orange |
| 4 | Red (tightest) |

Road descriptions in the KML include penalized score, score per km, total length, highway types, and OSM way IDs.

### Caching

Score results are cached per tile in `~/.twisty/cache/scores/`. The cache is keyed on scoring parameters, so it is automatically invalidated when the algorithm changes. Use `-no-cache` to ignore cached scores for the current run (new scores are still written), or `-clear-score-cache` to delete all cached scores before running.

## twisty fetch

Pre-populates the Overpass tile cache for a region without scoring. Useful for warming the cache before a `score` run or managing stale tiles.

```
twisty fetch -address <address> [flags]
```

### Required flags

| Flag | Description |
|------|-------------|
| `-address string` | Center address for the region to fetch |

### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `-radius float` | `25.0` | Search radius in km |
| `-tile-size float` | `0.05` | Tile size in degrees |
| `-cache-dir string` | `~/.twisty/cache/overpass/` | Overpass tile cache directory |
| `-no-cache` | false | Bypass cache reads (still writes fetched tiles) |
| `-clear-cache` | false | Delete all cached tiles before fetching |
| `-purge-older-than string` | — | Purge tiles older than a duration before fetching (e.g. `90d`, `6m`; `m` = months) |
| `-overpass-url string` | `https://overpass-api.de/api/interpreter` | Overpass API endpoint |
| `-v` | false | Verbose logging to stderr |

### Example

```bash
twisty fetch -address "Asheville, NC" -radius 30 -purge-older-than 90d
```

## twisty route

Fetches candidate routes between two addresses using the [Valhalla API](https://valhalla.openstreetmap.de/), scores each one for curvature, and writes the best match to a GPX file.

```
twisty route -origin <address> -dest <address> [flags]
```

### Required flags

| Flag | Description |
|------|-------------|
| `-origin string` | Origin address or `lat,lon` |
| `-dest string` | Destination address or `lat,lon` |

### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `-twist float` | `0.5` | Selection bias: `0.0` = fastest route, `1.0` = twistiest route |
| `-out string` | `route.gpx` | Output GPX file path |
| `-show-all` | false | Print a comparison table of all candidate routes |
| `-overpass-url string` | `https://overpass-api.de/api/interpreter` | Overpass API endpoint (used for road quality filtering) |
| `-v` | false | Verbose timing logs to stderr |

### Example

```bash
twisty route \
  -origin "Asheville, NC" \
  -dest "Deals Gap, NC" \
  -twist 0.9 \
  -out gap_run.gpx \
  -show-all
```

### How route selection works

For each candidate route, two curvature metrics are computed from the decoded polyline:

- **Angular density** — total heading change in degrees divided by route distance in km. Higher means more turns per km.
- **Indirectness** — straight-line distance divided by road distance (0–1; lower means more indirect/winding).

These are combined into a single score: `score = angularDensity × 0.7 + (1 − indirectness) × 1000 × 0.3`.

Road quality data from Overpass is used to apply penalties to routes that pass through lower-quality roads (soft failure — skipped if Overpass is unavailable).

Route selection normalizes each candidate's score and duration, then picks the route that maximizes:

```
twist × normScore + (1 − twist) × (1 − normDuration)
```

At `twist=0.0` the fastest route wins; at `twist=1.0` the twistiest wins; intermediate values blend the two.

### Output

The selected route is written as a GPX track to `-out`. A summary line is printed showing distance, duration, angular density, and twist score. With `-show-all`, a comparison table lists all candidates with the selected route marked.

## twisty overpass

Manages a local [Overpass API](https://github.com/wiktorn/Overpass-API) instance running in Docker. Useful for running `twisty score` or `twisty fetch` against a local instance instead of the public API — helpful for large regions or high-volume queries.

The local instance is backed by OSM data downloaded from [Geofabrik](https://download.geofabrik.de/) and stored in a local data directory (default: `.overpass/`).

### Subcommands

| Subcommand | Description |
|-----------|-------------|
| `start` | Download data, convert it, and start the Overpass container |
| `stop` | Stop the running container |
| `status` | Print whether the container is running |
| `logs` | Stream live logs from the container |
| `clean` | Stop the container and delete all local data |
| `build` | Build the Docker image from source (arm64-compatible) |

### twisty overpass start

Downloads PBF data for the specified regions, converts it to the OSM XML format required by Overpass, and starts the container.

```
twisty overpass start -regions <region>[,<region>...] [flags]
```

#### Required flags

| Flag | Description |
|------|-------------|
| `-regions string` | Comma-separated Geofabrik region paths (e.g. `north-america/us/new-york`) |

#### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `-port int` | `8080` | Host port to expose the Overpass API on |
| `-data-dir string` | `.overpass` | Directory for downloaded PBF files and the Overpass database |

Once the container is ready the endpoint is printed:

```
Overpass API is ready: http://localhost:8080/api/interpreter
```

Pass this URL to `twisty score` or `twisty fetch` via `-overpass-url`.

#### Example

```bash
twisty overpass start \
  -regions north-america/us/north-carolina,north-america/us/tennessee \
  -port 8080

# Then score roads against the local instance:
twisty score \
  -address "Asheville, NC" \
  -overpass-url http://localhost:8080/api/interpreter \
  -out roads.kml
```

#### Region management

Regions are cumulative. If you run `start` again with additional regions, twisty detects the change, re-downloads any missing PBF files, rebuilds the merged data file, and re-imports the database. Regions that were already downloaded are reused from the local cache.

### twisty overpass stop

Stops the running container (data is preserved).

```
twisty overpass stop
```

### twisty overpass status

Prints whether the Overpass container is currently running.

```
twisty overpass status
```

### twisty overpass logs

Streams live logs from the container (equivalent to `docker logs -f`).

```
twisty overpass logs
```

### twisty overpass clean

Stops the container and removes the entire data directory.

```
twisty overpass clean [-data-dir <dir>]
```

### twisty overpass build

Builds the Overpass Docker image from source. Run this if the pre-built image is not available for your architecture (e.g. Apple Silicon). `start` calls this automatically when needed.

```
twisty overpass build [-data-dir <dir>]
```

### Docker image

twisty uses the [`wiktorn/overpass-api`](https://github.com/wiktorn/Overpass-API) image. On arm64 hosts (Apple Silicon) the image is built locally from source because no official arm64 image is published. The build is triggered automatically by `start` if needed, or can be triggered manually with `build`.

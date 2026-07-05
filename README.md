# twisty

`twisty` is a CLI tool for finding and scoring curvy roads using OpenStreetMap data. It fetches road geometry from the Overpass API, scores segments based on curvature, and outputs files you can load in Google Earth or similar tools.

Heavily influenced by the amazing [Curvature](https://roadcurvature.com/) project; please go donate and support it!`

## Subcommands

- `twisty score` — score roads in a region and write a KML file
- `twisty fetch` — pre-populate the Overpass tile cache for a region
- `twisty route` — uses the [Valhalla API](https://valhalla.openstreetmap.de/) to construct routes between two points, then picks the most twisty one
- `twisty build` — interactively build a route on a map with live curvature scoring
- `twisty overpass` — manage a local Overpass API instance running in Docker
- `twisty valhalla` — manage a local Valhalla routing server running in Docker
- `twisty gpx` — export a Google Maps driving route as a GPX file

## Installation

### Prerequisites

- Go 1.21 or later
- macOS (the build reads the Google API key from the macOS Keychain)
- A Google API key stored in the Keychain (see [twisty gpx → Setup](#setup))

### Build and install

```bash
make install
```

This compiles `twisty` and installs it to your Go bin directory (`$GOPATH/bin` or `$GOBIN`). The Google Routes API key is injected at link time from the macOS Keychain — no runtime credentials are needed.

If `$GOPATH/bin` is already on your `PATH` (standard Go setup), the `twisty` command will be available immediately after install.

To just build a local binary without installing:

```bash
make build
# produces bin/twisty
```

## twisty score

Scores roads in a circular region around a center address and writes a KML file with color-coded results.

```
twisty score --address <address> --out <file.kml> [flags]
```

### Required flags

| Flag | Description |
|------|-------------|
| `--address string` | Center address for the search region |
| `--out string` | Output KML file path |

### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--radius float` | `25.0` | Search radius in km |
| `--tile-size float` | `0.1` | Tile size in degrees for grid-based fetching |
| `--cache-dir string` | `~/.twisty/cache/overpass/` | Overpass tile cache directory |
| `--no-cache` | false | Skip score cache reads (cache writes still happen) |
| `--fetch-delay string` | `1s` | Delay between tile fetches in Go duration format (e.g. `500ms`, `2s`) |
| `--clear-score-cache` | false | Delete all score cache entries before running |
| `--min-score float` | `0` | Minimum penalized score to include in output |
| `--multi-color` | false | Color each segment by curvature tier instead of per-road |
| `--overpass-url string` | `https://overpass-api.de/api/interpreter` | Overpass API endpoint |
| `--v` | false | Verbose logging to stderr |

### Example

```bash
twisty score \
  --address "Mulholland Drive, Los Angeles, CA" \
  --radius 15 \
  --out twisty_roads.kml \
  --min-score 500
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

Roads with mixed types get the lowest applicable multiplier. The final `PenalizedScore` is used for filtering (`--min-score`) and KML ordering.

#### 5. Minimum road length

Road collections shorter than 1609 m (1 mile) are excluded from output.

### KML output

Roads are sorted by penalized score (highest first). Each road appears as a named folder containing its polyline(s).

**Default mode:** Each road is drawn as a single polyline colored on a green→yellow→red→magenta gradient based on its total penalized score.

**Multi-color mode** (`--multi-color`): Each segment is colored by its curvature tier:

| Tier | Color |
|------|-------|
| 0 | Green (straight) |
| 1 | Yellow |
| 2 | Orange |
| 3 | Dark orange |
| 4 | Red (tightest) |

Road descriptions in the KML include penalized score, score per km, total length, highway types, and OSM way IDs.

### Caching

Score results are cached per tile in `~/.twisty/cache/scores/`. The cache is keyed on scoring parameters, so it is automatically invalidated when the algorithm changes. Use `--no-cache` to ignore cached scores for the current run (new scores are still written), or `--clear-score-cache` to delete all cached scores before running.

## twisty fetch

Pre-populates the Overpass tile cache for a region without scoring. Useful for warming the cache before a `score` run or managing stale tiles.

```
twisty fetch --address <address> [flags]
```

### Required flags

| Flag | Description |
|------|-------------|
| `--address string` | Center address for the region to fetch |

### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--radius float` | `25.0` | Search radius in km |
| `--tile-size float` | `0.1` | Tile size in degrees |
| `--cache-dir string` | `~/.twisty/cache/overpass/` | Overpass tile cache directory |
| `--no-cache` | false | Bypass cache reads (still writes fetched tiles) |
| `--clear-cache` | false | Delete all cached tiles before fetching |
| `--purge-older-than string` | — | Purge tiles older than a duration before fetching (e.g. `90d`, `6m`; `m` = months) |
| `--overpass-url string` | `https://overpass-api.de/api/interpreter` | Overpass API endpoint |
| `--fetch-delay string` | `1s` | Delay between tile fetches in Go duration format (e.g. `500ms`, `2s`) |
| `--v` | false | Verbose logging to stderr |

### Example

```bash
twisty fetch --address "Asheville, NC" --radius 30 --purge-older-than 90d
```

## twisty route

Fetches candidate routes between two addresses using the [Valhalla API](https://valhalla.openstreetmap.de/), scores each one for curvature, and writes the best match to a GPX file.

```
twisty route --origin <address> --dest <address> [flags]
```

### Required flags

| Flag | Description |
|------|-------------|
| `--origin string` | Origin address or `lat,lon` |
| `--dest string` | Destination address or `lat,lon` |

### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--twist float` | `0.5` | Selection bias: `0.0` = fastest route, `1.0` = twistiest route |
| `--out string` | `route.gpx` | Output GPX file path |
| `--show-all` | false | Print a comparison table of all candidate routes |
| `--overpass-url string` | `https://overpass-api.de/api/interpreter` | Overpass API endpoint (used for road quality filtering) |
| `--v` | false | Verbose timing logs to stderr |

### Example

```bash
twisty route \
  --origin "Asheville, NC" \
  --dest "Deals Gap, NC" \
  --twist 0.9 \
  --out gap_run.gpx \
  --show-all
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

The selected route is written as a GPX track to `--out`. A summary line is printed showing distance, duration, angular density, and twist score. With `--show-all`, a comparison table lists all candidates with the selected route marked.

## twisty build

Starts a local web server with an interactive map for building routes by clicking waypoints, with live curvature scoring as you place them.

```
twisty build --address <address> [flags]
```

### Required flags

| Flag | Description |
|------|-------------|
| `--address string` | Center address for the initial map view |

### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port int` | `8080` | Port for the local web server |
| `--overpass-url string` | `https://overpass-api.de/api/interpreter` | Overpass API endpoint |
| `--cache-dir string` | `~/.twisty/cache/overpass/` | Overpass tile cache directory |
| `--tile-size float` | `0.1` | Tile size in degrees |
| `--fetch-delay string` | `1s` | Delay between tile fetches (e.g. `500ms`, `2s`) |
| `--v` | false | Verbose logging to stderr |

### Example

```bash
twisty build --address "Asheville, NC" --port 8080
```

Then open `http://localhost:8080` in your browser. Click the map to add waypoints; each leg is routed via Valhalla and scored for curvature. Export the finished route as GPX or KML using the buttons in the UI.

### How it works

- Clicking the map adds a waypoint. Each new waypoint triggers a Valhalla routing call for the leg between it and the previous waypoint.
- Road curvature data is fetched from Overpass on demand (same tile cache as `twisty score`). Tiles are fetched in the background as you pan; the score updates automatically when they arrive.
- The "Refresh score" button clears and re-fetches all tiles in the current viewport, useful after cache staleness.
- Routes and waypoints are saved to `localStorage` automatically so your work survives a page refresh.
- The **Save / Load** buttons let you persist routes as `.twisty.json` files to share between sessions or machines.
- Toggle between **Way view** (segments colored by curvature tier) and **Road view** (segments colored by aggregate road score) using the button in the stats panel.

### Using with local instances

For the best experience, run local Overpass and Valhalla instances together. Overpass serves tile data as you pan the map; Valhalla handles routing when you click waypoints. Both use the same Geofabrik region paths so you only need to specify them once per service.

```bash
# Start a local Overpass instance for tile data
twisty overpass start \
  --regions north-america/us/north-carolina,north-america/us/tennessee \
  --port 8080

# Start a local Valhalla instance for routing
twisty valhalla start \
  --regions north-america/us/north-carolina,north-america/us/tennessee \
  --port 8002

# Launch the build UI pointed at both local instances
twisty build \
  --address "Asheville, NC" \
  --port 9090 \
  --overpass-url http://localhost:8080/api/interpreter \
  --valhalla-url http://localhost:8002
```

This is especially worthwhile when building routes across state or country boundaries, where the tile set is large and repeated routing calls would hit the public APIs. See [twisty overpass start](#twisty-overpass-start) and [twisty valhalla start](#twisty-valhalla-start) for details on region management.

## twisty overpass

Manages a local [Overpass API](https://github.com/wiktorn/Overpass-API) instance running in Docker. Useful for running `twisty score` or `twisty fetch` against a local instance instead of the public API — helpful for large regions or high-volume queries.

The local instance is backed by OSM data downloaded from [Geofabrik](https://download.geofabrik.de/) and stored in a local data directory (default: `~/.twisty/overpass/`).

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
twisty overpass start --regions <region>[,<region>...] [flags]
```

#### Required flags

| Flag | Description |
|------|-------------|
| `--regions string` | Comma-separated Geofabrik region paths (e.g. `north-america/us/new-york`) |

#### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port int` | `8080` | Host port to expose the Overpass API on |
| `--data-dir string` | `~/.twisty/overpass` | Directory for downloaded PBF files and the Overpass database |

Once the container is ready the endpoint is printed:

```
Overpass API is ready: http://localhost:8080/api/interpreter
```

Pass this URL to `twisty score` or `twisty fetch` via `--overpass-url`.

#### Example

```bash
twisty overpass start \
  --regions north-america/us/north-carolina,north-america/us/tennessee \
  --port 8080

# Then score roads against the local instance:
twisty score \
  --address "Asheville, NC" \
  --overpass-url http://localhost:8080/api/interpreter \
  --out roads.kml
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
twisty overpass logs [--lines <n>]
```

#### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--lines int` | `0` | Number of log lines to show (0 = stream all) |

### twisty overpass clean

Stops the container and removes the entire data directory.

```
twisty overpass clean [--data-dir <dir>]
```

### twisty overpass build

Builds the Overpass Docker image from source. Run this if the pre-built image is not available for your architecture (e.g. Apple Silicon). `start` calls this automatically when needed.

```
twisty overpass build [--src-dir <dir>]
```

#### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--src-dir string` | — | Source directory for the Docker build |

### Docker image

twisty uses the [`wiktorn/overpass-api`](https://github.com/wiktorn/Overpass-API) image. On arm64 hosts (Apple Silicon) the image is built locally from source because no official arm64 image is published. The build is triggered automatically by `start` if needed, or can be triggered manually with `build`.

## twisty valhalla

Manages a local [Valhalla](https://github.com/valhalla/valhalla) routing server running in Docker. Useful for running `twisty route`, `twisty build`, or `twisty random` against a local instance instead of the public API — helpful for repeated sessions, offline use, or areas where the public API is slow.

The local instance is backed by OSM data downloaded from [Geofabrik](https://download.geofabrik.de/) and stored in a local data directory (default: `~/.twisty/valhalla/`). Routing tile data is built from PBF extracts using the official Valhalla Docker image.

### Subcommands

| Subcommand | Description |
|-----------|-------------|
| `start` | Download PBF data, build routing tiles, and start the Valhalla container |
| `stop` | Stop the running container |
| `status` | Print whether the container is running |
| `logs` | Stream live logs from the container |
| `clean` | Stop the container and delete all local data |

### twisty valhalla start

Downloads PBF data for the specified regions, builds Valhalla routing tiles, and starts the container. If multiple regions are specified they are merged into a single PBF using osmium before tile-building (required to avoid corrupted output).

```
twisty valhalla start --regions <region>[,<region>...] [flags]
```

#### Required flags

| Flag | Description |
|------|-------------|
| `--regions string` | Comma-separated Geofabrik region paths (e.g. `north-america/us/tennessee`) |

#### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port int` | `8002` | Host port to expose the Valhalla API on |
| `--data-dir string` | `~/.twisty/valhalla` | Directory for downloaded PBF files and built routing tiles |

Once the container is ready the endpoint is printed:

```
Valhalla routing server is ready: http://localhost:8002
```

Pass this URL to `twisty route`, `twisty build`, or `twisty random` via `--valhalla-url`.

#### Example

```bash
twisty valhalla start \
  --regions north-america/us/north-carolina,north-america/us/tennessee \
  --port 8002

# Then find a twisty route using the local instance:
twisty route \
  --origin "Asheville, NC" \
  --dest "Deals Gap, NC" \
  --twist 0.9 \
  --valhalla-url http://localhost:8002 \
  --out gap_run.gpx
```

#### Region management

Regions are cumulative. If you run `start` again with additional regions, twisty detects the change, downloads any missing PBF files, rebuilds the merged data file, and re-imports tiles. Regions that were already downloaded are reused from the local cache. The region set is stamped to disk before tile-building so that a crash or OOM kill does not cause a full restart from scratch on the next invocation.

### twisty valhalla stop

Stops the running container (data is preserved).

```
twisty valhalla stop
```

### twisty valhalla status

Prints whether the Valhalla container is currently running and the endpoint URL.

```
twisty valhalla status [--port <n>]
```

#### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port int` | `8002` | Port the Valhalla server is running on |

### twisty valhalla logs

Streams live logs from the container (equivalent to `docker logs -f`).

```
twisty valhalla logs [--lines <n>]
```

#### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--lines int` | `0` | Number of log lines to show (0 = stream all) |

### twisty valhalla clean

Stops the container and removes the entire data directory, including all downloaded PBF files and built routing tiles.

```
twisty valhalla clean [--data-dir <dir>]
```

### Docker images

`twisty valhalla` uses two Docker images:

- [`ghcr.io/valhalla/valhalla:latest`](https://github.com/valhalla/valhalla) — the Valhalla routing engine, used for both tile-building and serving.
- [`iboates/osmium`](https://hub.docker.com/r/iboates/osmium) — used to merge multiple PBF files before tile-building when more than one region is requested.

Both images are pulled automatically on first use.

## twisty gpx

Exports a driving route from a Google Maps shared link as a GPX file for use in offline navigation applications like OSMAnd.

```
twisty gpx --maps-url <url> --out <file>
```

### Required flags

| Flag | Description |
|------|-------------|
| `--maps-url string` | Google Maps shared link URL |
| `--out string` | Output GPX file path |

### Supported URL formats

| Format | Example |
|--------|---------|
| Short link | `https://maps.app.goo.gl/abc123` |
| Full directions URL | `https://maps.google.com/maps/dir/origin/destination` |

### Setup

`twisty gpx` requires a Google Cloud project with the Routes API enabled and an API key baked into the binary at build time. All steps below use the `gcloud` CLI.

#### 1. Create and configure a Google Cloud project

```bash
PROJECT_ID=twisty-maps   # must be globally unique; adjust if taken
gcloud projects create $PROJECT_ID --name="Twisty Maps"
gcloud config set project $PROJECT_ID
```

Link a billing account (required for the Routes API):

```bash
BILLING_ACCOUNT=$(gcloud billing accounts list --format='value(name)' --filter='open=true' | head -1)
gcloud billing projects link $PROJECT_ID --billing-account=$BILLING_ACCOUNT
```

#### 2. Enable the Routes API

```bash
gcloud services enable routes.googleapis.com
```

#### 3. Create a restricted API key

```bash
API_KEY=$(gcloud alpha services api-keys create \
  --display-name="twisty-routes" \
  --api-target=service=routes.googleapis.com \
  --format='value(response.keyString)' 2>/dev/null)
echo "API key: $API_KEY"
```

This key is restricted to the Routes API only. Even if extracted from the binary, it cannot be used for any other Google service.

Set a daily quota cap to limit exposure from unauthorized use:

```bash
# Optional but recommended — adjust the limit to match your expected usage
gcloud alpha services api-keys update \
  $(gcloud alpha services api-keys list --filter='displayName=twisty-routes' --format='value(name)') \
  --api-target=service=routes.googleapis.com,methods=google.maps.routing.v2.Routes.ComputeRoutes \
  --quota-project=$PROJECT_ID
```

Also set a budget alert in the [Google Cloud Console Billing](https://console.cloud.google.com/billing) section so you are notified if usage spikes.

#### 4. Store the key in the macOS Keychain

```bash
security add-generic-password \
  -s twisty/build/google/api-key \
  -a twisty \
  -w "$API_KEY"
```

The key is read automatically by `make build` and injected into the binary at link time. It is never written to disk outside the Keychain.

#### 5. Build

```bash
make build
```

### Authentication

`twisty gpx` uses a Google API key baked into the binary at build time. No browser login is required. See [Setup](#setup) for how to provision and store the key.

### Example

```bash
twisty gpx \
  --maps-url "https://maps.app.goo.gl/abc123" \
  --out my-route.gpx
```

### Output

A GPX 1.1 file containing:
- Named `<wpt>` elements for each stop in the route
- A `<trkseg>` track segment with the full route polyline

The file can be imported directly into OSMAnd or any other application that supports GPX.

# GPX Converter — Manual Validation Procedure

Use this document to verify the `twisty gpx` feature end-to-end with real Google Maps links.

## Prerequisites

- `twisty` binary built from source: `go build -o twisty .`
- A Google account that can authorize the OAuth consent screen
- OSMAnd installed on a mobile device (iOS or Android)
- Internet access for authentication and route fetching

---

## Test 1: Authentication Flow (First Run)

**Goal:** Verify that running `twisty gpx` for the first time triggers the browser-based OAuth flow and stores credentials.

**Steps:**

1. Delete any stored credentials to force a fresh login:
   ```bash
   # macOS — remove the keychain entry if it exists
   security delete-generic-password -s twisty-maps 2>/dev/null || true
   ```
2. Run the command with a valid Google Maps URL:
   ```bash
   ./twisty gpx \
     --maps-url "https://maps.app.goo.gl/abc123" \
     --out /tmp/test-auth.gpx
   ```
3. Confirm that:
   - The terminal prints a URL and "Waiting for authorization..."
   - A browser window opens (or the URL can be pasted manually)
   - After approving the consent screen, the terminal continues and produces `test-auth.gpx`

**Expected result:** The command completes successfully. No error about authentication. The GPX file is created.

---

## Test 2: Simple 2-Point Route

**Goal:** Verify a basic origin → destination route converts correctly.

**Steps:**

1. Open Google Maps and get directions between two points (e.g. Times Square to Central Park, New York City).
2. Copy the shared link (Share → Copy Link).
3. Run:
   ```bash
   ./twisty gpx \
     --maps-url "<pasted-url>" \
     --out /tmp/simple-route.gpx
   ```
4. Open `/tmp/simple-route.gpx` in a text editor and confirm:
   - The file starts with `<?xml version="1.0" encoding="UTF-8"?>`
   - There are two `<wpt>` elements corresponding to origin and destination
   - The `<trkseg>` contains multiple `<trkpt>` elements tracing the route

**Expected result:** Valid GPX file with both waypoints and a track segment.

---

## Test 3: Multi-Stop Route (3+ Waypoints)

**Goal:** Verify that a route with multiple intermediate stops includes all waypoints in the GPX output.

**Steps:**

1. Open Google Maps and create a multi-stop route (e.g. A → B → C → D with at least 3 stops).
2. Copy the shared link.
3. Run:
   ```bash
   ./twisty gpx \
     --maps-url "<pasted-url>" \
     --out /tmp/multi-stop.gpx
   ```
4. Open `/tmp/multi-stop.gpx` and confirm:
   - There are `<wpt>` elements for each stop
   - The `<trkseg>` covers the full multi-leg route

**Expected result:** All waypoints appear in the GPX file. Track covers the complete route.

---

## Test 4: Error Handling

**Goal:** Verify that invalid inputs produce clear, actionable error messages.

### 4a. Invalid URL

```bash
./twisty gpx --maps-url "https://example.com/not-a-maps-link" --out /tmp/err.gpx
```

**Expected:** Error message indicating the URL is not a supported Google Maps link.

### 4b. Missing required flag

```bash
./twisty gpx --maps-url "https://maps.app.goo.gl/abc123"
```

**Expected:** Error indicating `--out` is required.

```bash
./twisty gpx --out /tmp/route.gpx
```

**Expected:** Error indicating `--maps-url` is required.

### 4c. Unwritable output path

```bash
./twisty gpx \
  --maps-url "https://maps.app.goo.gl/abc123" \
  --out /nonexistent/directory/route.gpx
```

**Expected:** Error indicating the output file could not be created.

---

## Test 5: Token Refresh (Re-auth Not Required Within 24 Hours)

**Goal:** Confirm that a second invocation within 24 hours does not trigger a browser login.

**Steps:**

1. Complete Test 1 or Test 2 (credentials are now stored).
2. Immediately run another conversion:
   ```bash
   ./twisty gpx \
     --maps-url "<pasted-url>" \
     --out /tmp/test-reuse.gpx
   ```
3. Confirm that:
   - No browser window opens
   - No URL is printed to the terminal
   - The command completes successfully

**Expected result:** Silent authentication. GPX file is created without any OAuth interaction.

---

## Test 6: OSMAnd Import Verification

**Goal:** Confirm the generated GPX file can be imported into OSMAnd and the route is displayed correctly offline.

**Steps:**

1. Generate a GPX file using one of the tests above.
2. Transfer the `.gpx` file to the mobile device (AirDrop, cable, or cloud storage).
3. In OSMAnd:
   - Open the menu → My Places → Tracks → Import
   - Select the transferred `.gpx` file
4. After import:
   - Open the track and verify the route polyline appears on the map
   - Confirm all named waypoints appear as pins
   - Enable offline mode on the device and verify the track is still visible

**Expected result:** The route displays correctly on the OSMAnd map. All stops are labeled. The track is navigable offline.

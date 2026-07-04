# Local Valhalla Routing Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `twisty valhalla` — a Docker-managed local Valhalla routing server — and wire `twisty build` to target it via `--valhalla-url`.

**Architecture:** Extract shared PBF download helpers from `overpass.go` into a new `pbf.go`, then build `valhalla.go` on top of them. `overpass.go` moves its PBF directory to the shared `~/.twisty/pbf/`. `route/valhalla.go` exports its internal `fetchRoutesFromURL` so `route_build.go` can use a configurable URL.

**Tech Stack:** Go stdlib only, Cobra for CLI, Docker (via `exec.Command`), `ghcr.io/valhalla/valhalla:latest`.

## Global Constraints

- No new external Go dependencies — stdlib only.
- All existing tests must continue to pass after every task (`make test`).
- Use TDD: write the failing test before each piece of implementation.
- Container name: `twisty-valhalla`.
- Default port: `8002`.
- Default data dir: `~/.twisty/valhalla/`.
- Shared PBF cache dir: `~/.twisty/pbf/`.
- Go module path: `github.com/yardbirdsax/twisty` (see `go.mod`).

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `pbf.go` | **Create** | Shared PBF download helpers: `downloadPBF`, `downloadPBFsParallel`, `pbfFilename`, `mergeRegions`, `resolvePBFDir` |
| `valhalla.go` | **Create** | `twisty valhalla` subcommand tree and all lifecycle logic |
| `overpass.go` | **Modify** | Remove functions moved to `pbf.go`; use `resolvePBFDir` for the PBF directory |
| `overpass_merge.go` | **Modify** | Use `resolvePBFDir` instead of `filepath.Join(dataDir, "pbf")` |
| `main.go` | **Modify** | Register `newValhallaCmd()` in `newRootCmd()` |
| `route/valhalla.go` | **Modify** | Export `fetchRoutesFromURL` → `FetchRoutesFromURL` |
| `route_build.go` | **Modify** | Add `valhallaURL` to `buildParams` and `buildServer`; call `route.FetchRoutesFromURL` |

---

### Task 1: Extract shared PBF helpers into `pbf.go`

Move `downloadPBF`, `downloadPBFsParallel`, `pbfFilename`, and `mergeRegions` from `overpass.go` into a new `pbf.go`, and introduce `resolvePBFDir` for the shared `~/.twisty/pbf/` path. Update `overpass.go` and `overpass_merge.go` to use it. The move is pure refactor — no behaviour changes.

**Files:**
- Create: `pbf.go`
- Modify: `overpass.go`
- Modify: `overpass_merge.go`
- Test: `overpass_test.go` (existing tests must still pass)

**Interfaces:**
- Produces:
  - `resolvePBFDir(dataDir string) string` — returns `dataDir/pbf` if non-empty, else `~/.twisty/pbf/`
  - `pbfFilename(region string) string` — unchanged signature
  - `mergeRegions(existing, add []string) []string` — unchanged signature
  - `downloadPBF(url, destPath string, fp FileDownloadProgress) error` — unchanged signature
  - `downloadPBFsParallel(urls, destPaths []string, progress DownloadProgress) error` — unchanged signature

- [ ] **Step 1: Run existing tests to establish baseline**

```bash
make test
```
Expected: all tests pass.

- [ ] **Step 2: Create `pbf.go` with the moved functions**

Create `/path/to/twisty/pbf.go`:

```go
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// resolvePBFDir returns the shared PBF cache directory. If dataDir is non-empty
// it returns dataDir/pbf (used by tests to redirect to a temp dir); otherwise
// it returns ~/.twisty/pbf/.
func resolvePBFDir(dataDir string) string {
	if dataDir != "" {
		return filepath.Join(dataDir, "pbf")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	return filepath.Join(home, ".twisty", "pbf")
}

// pbfFilename returns the local PBF filename for a Geofabrik region path.
// Slashes in the region path are replaced with underscores so the name is
// safe to use as a flat filename.
func pbfFilename(region string) string {
	return strings.ReplaceAll(region, "/", "_") + "-latest.osm.pbf"
}

// mergeRegions returns the sorted union of existing and new region lists.
func mergeRegions(existing, add []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(add))
	for _, r := range existing {
		seen[r] = struct{}{}
	}
	for _, r := range add {
		seen[r] = struct{}{}
	}
	merged := make([]string, 0, len(seen))
	for r := range seen {
		merged = append(merged, r)
	}
	sort.Strings(merged)
	return merged
}

// downloadPBFsParallel downloads all urls[i] to destPaths[i] concurrently.
// All downloads start simultaneously; the first error is returned after all
// goroutines finish.
func downloadPBFsParallel(urls, destPaths []string, progress DownloadProgress) error {
	errs := make([]error, len(urls))
	var wg sync.WaitGroup
	for i := range urls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fp := progress.StartFile(destPaths[idx], idx, len(urls), -1)
			errs[idx] = downloadPBF(urls[idx], destPaths[idx], fp)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// downloadPBF downloads the file at url to destPath atomically via a temp file,
// reporting progress via the provided FileDownloadProgress.
func downloadPBF(url, destPath string, fp FileDownloadProgress) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".download-*.osm.pbf")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := tmp.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("writing temp file: %w", writeErr)
			}
			fp.BytesDownloaded(int64(n))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("reading response: %w", readErr)
		}
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}
	fp.FileComplete()
	return nil
}
```

- [ ] **Step 3: Remove the moved functions from `overpass.go`**

In `overpass.go`, delete the bodies of `pbfFilename`, `mergeRegions`, `downloadPBFsParallel`, and `downloadPBF` (lines ~328–626 in the current file). Also remove the now-unused imports `sort`, `sync`, and `io` if they are no longer referenced by the remaining code (keep them if still used elsewhere in the file). The `strings` import is still used.

- [ ] **Step 4: Update `overpass.go` to use `resolvePBFDir`**

In `runOverpassStart`, replace:
```go
pbfDir := filepath.Join(dataDir, "pbf")
```
with:
```go
pbfDir := resolvePBFDir("")
```

Note: `dataDir` here is the overpass data dir (`~/.twisty/overpass/`). The PBF dir is now shared and independent of `dataDir`, so pass `""` to get `~/.twisty/pbf/`.

- [ ] **Step 5: Update `overpass_merge.go` to use `resolvePBFDir`**

In `convertRegionsIncremental`, replace:
```go
pbfDir := filepath.Join(dataDir, "pbf")
```
with:
```go
pbfDir := resolvePBFDir("")
```

- [ ] **Step 6: Run tests to verify the refactor compiles and passes**

```bash
make test
```
Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add pbf.go overpass.go overpass_merge.go
git commit -m "refactor: extract shared PBF helpers into pbf.go with shared cache dir"
```

---

### Task 2: Export `FetchRoutesFromURL` from `route/valhalla.go`

`route_build.go` currently calls `route.FetchRoutes` which hardcodes the public Valhalla URL. Export the internal `fetchRoutesFromURL` so callers can pass their own base URL.

**Files:**
- Modify: `route/valhalla.go`
- Test: `route/valhalla_test.go` (existing tests must still pass; no new tests needed for a rename)

**Interfaces:**
- Produces: `route.FetchRoutesFromURL(baseURL string, origin, dest geo.Coord) ([]Route, error)`

- [ ] **Step 1: Export `fetchRoutesFromURL` in `route/valhalla.go`**

In `route/valhalla.go`, rename `fetchRoutesFromURL` to `FetchRoutesFromURL` and update the internal `FetchRoutes` wrapper to call the new name:

```go
// FetchRoutesFromURL is the internal implementation of FetchRoutes, accepting a
// base URL so callers can substitute a local server.
func FetchRoutesFromURL(baseURL string, origin, dest geo.Coord) ([]Route, error) {
	// ... (body unchanged)
}

// FetchRoutes requests route alternatives from the Valhalla routing engine
// and returns decoded Route structs. Routes avoid highways (use_highways=0).
func FetchRoutes(origin, dest geo.Coord) ([]Route, error) {
	return FetchRoutesFromURL(valhallaBaseURL, origin, dest)
}
```

- [ ] **Step 2: Run tests**

```bash
make test
```
Expected: all tests pass.

- [ ] **Step 3: Commit**

```bash
git add route/valhalla.go
git commit -m "feat: export FetchRoutesFromURL from route package"
```

---

### Task 3: Wire `--valhalla-url` into `twisty build`

Add `valhallaURL` to `buildParams` and `buildServer`, thread it to `handleRouteLeg`, and register the flag in `newBuildCmd`.

**Files:**
- Modify: `route_build.go`
- Test: `route_build_test.go`

**Interfaces:**
- Consumes: `route.FetchRoutesFromURL(baseURL string, origin, dest geo.Coord) ([]Route, error)` from Task 2

- [ ] **Step 1: Write a failing test**

In `route_build_test.go`, find or add a test that verifies the build server uses the configured Valhalla URL. Look for existing tests of `handleRouteLeg` — add a case that sets a non-default `valhallaURL` on `buildServer` and confirms it reaches the stub server at that URL.

```go
func TestHandleRouteLeg_UsesConfiguredValhallaURL(t *testing.T) {
	// Start a stub Valhalla server.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Minimal valid Valhalla response.
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"trip":{"summary":{"time":60,"length":1.0},"legs":[{"shape":"??","summary":{"time":60,"length":1.0},"maneuvers":[]}]},"alternates":[]}`)
	}))
	defer stub.Close()

	srv := &buildServer{
		valhallaURL: stub.URL,
	}

	body := `{"from":{"lat":36.1,"lon":-86.7},"to":{"lat":36.2,"lon":-86.8}}`
	req := httptest.NewRequest(http.MethodPost, "/api/route-leg", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRouteLeg(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test -run TestHandleRouteLeg_UsesConfiguredValhallaURL -v .
```
Expected: compile error — `buildServer` has no `valhallaURL` field.

- [ ] **Step 3: Add `valhallaURL` to `buildParams` and `buildServer`**

In `route_build.go`, add the field to both structs:

```go
type buildParams struct {
	address     string
	port        int
	overpassURL string
	cacheDir    string
	tileSize    float64
	fetchDelay  string
	verbose     bool
	valhallaURL string // base URL for the Valhalla routing API
}
```

```go
type buildServer struct {
	center        geocode.Result
	overpassURL   string
	valhallaURL   string
	cacheDir      string
	tileSize      float64
	fetchDelay    string
	nominatimBase string
	failedTiles   sync.Map
	tileReady     chan quality.Tile
	broker        *sseBroker
}
```

- [ ] **Step 4: Thread `valhallaURL` through `execBuild` into `buildServer`**

In `execBuild`, set `valhallaURL` on the server (add it next to the other fields):

```go
srv := &buildServer{
	center:      center,
	overpassURL: p.overpassURL,
	valhallaURL: p.valhallaURL,
	cacheDir:    cacheDir,
	tileSize:    p.tileSize,
	fetchDelay:  p.fetchDelay,
	tileReady:   make(chan quality.Tile, 64),
	broker:      broker,
}
```

- [ ] **Step 5: Update `handleRouteLeg` to use `s.valhallaURL`**

In `handleRouteLeg`, replace:
```go
routes, err := route.FetchRoutes(origin, dest)
```
with:
```go
valhallaURL := s.valhallaURL
if valhallaURL == "" {
	valhallaURL = "https://valhalla1.openstreetmap.de"
}
routes, err := route.FetchRoutesFromURL(valhallaURL, origin, dest)
```

- [ ] **Step 6: Register `--valhalla-url` flag in `newBuildCmd`**

In `newBuildCmd`, add after the existing flags:
```go
f.StringVar(&p.valhallaURL, "valhalla-url", "https://valhalla1.openstreetmap.de", "Valhalla routing API URL")
```

- [ ] **Step 7: Run the test to confirm it passes**

```bash
go test -run TestHandleRouteLeg_UsesConfiguredValhallaURL -v .
```
Expected: PASS.

- [ ] **Step 8: Run all tests**

```bash
make test
```
Expected: all tests pass.

- [ ] **Step 9: Commit**

```bash
git add route_build.go
git commit -m "feat(build): add --valhalla-url flag to twisty build"
```

---

### Task 4: Implement `twisty valhalla` command

Create `valhalla.go` with the full subcommand tree. This is the largest task; it follows the exact structure of `overpass.go`.

**Files:**
- Create: `valhalla.go`
- Modify: `main.go`
- Test: `valhalla_test.go` (new)

**Interfaces:**
- Consumes: `resolvePBFDir`, `pbfFilename`, `mergeRegions`, `downloadPBFsParallel`, `downloadPBF`, `newDownloadProgress`, `isContainerRunning`, `dockerCmd`, `dockerCmdStreaming` (all from existing files)

- [ ] **Step 1: Write unit tests for pure functions**

Create `valhalla_test.go`:

```go
package main

import (
	"testing"
)

func TestValhallaDockerBuildArgs_SingleRegion(t *testing.T) {
	pbfPaths := []string{"/data/pbf/north-america_us_tennessee-latest.osm.pbf"}
	pbfFilenames := []string{"north-america_us_tennessee-latest.osm.pbf"}
	tilesDir := "/data/valhalla/tiles"

	args := valhallaDockerBuildArgs(tilesDir, pbfPaths, pbfFilenames)

	// Must contain the image name.
	if args[len(args)-1] != valhallaImage && args[len(args)-2] != valhallaImage {
		t.Errorf("expected %q in args, got %v", valhallaImage, args)
	}
	// Must mount tiles dir.
	found := false
	for _, a := range args {
		if a == tilesDir+":/custom_files" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tiles dir mount in args %v", args)
	}
	// Must mount the PBF.
	foundPBF := false
	for _, a := range args {
		if a == pbfPaths[0]+":/custom_files/"+pbfFilenames[0] {
			foundPBF = true
		}
	}
	if !foundPBF {
		t.Errorf("expected PBF mount in args %v", args)
	}
}

func TestValhallaDockerBuildArgs_MultipleRegions(t *testing.T) {
	pbfPaths := []string{
		"/data/pbf/north-america_us_tennessee-latest.osm.pbf",
		"/data/pbf/north-america_us_kentucky-latest.osm.pbf",
	}
	pbfFilenames := []string{
		"north-america_us_tennessee-latest.osm.pbf",
		"north-america_us_kentucky-latest.osm.pbf",
	}
	tilesDir := "/data/valhalla/tiles"

	args := valhallaDockerBuildArgs(tilesDir, pbfPaths, pbfFilenames)

	// Both PBF files must be mounted.
	for i, path := range pbfPaths {
		mount := path + ":/custom_files/" + pbfFilenames[i]
		found := false
		for _, a := range args {
			if a == mount {
				found = true
			}
		}
		if !found {
			t.Errorf("expected mount %q in args %v", mount, args)
		}
	}
}

func TestValhallaDockerRunArgs(t *testing.T) {
	port := 8002
	tilesDir := "/data/valhalla/tiles"

	args := valhallaDockerRunArgs(port, tilesDir)

	// Must publish port 8002.
	portFlag := "8002:8002"
	found := false
	for _, a := range args {
		if a == portFlag {
			found = true
		}
	}
	if !found {
		t.Errorf("expected port flag %q in args %v", portFlag, args)
	}
	// Must use the valhalla image.
	foundImage := false
	for _, a := range args {
		if a == valhallaImage {
			foundImage = true
		}
	}
	if !foundImage {
		t.Errorf("expected image %q in args %v", valhallaImage, args)
	}
}

func TestResolveValhallaDataDir_NonEmpty(t *testing.T) {
	got := resolveValhallaDataDir("/custom/path")
	if got != "/custom/path" {
		t.Errorf("expected /custom/path, got %q", got)
	}
}

func TestResolveValhallaDataDir_Empty(t *testing.T) {
	got := resolveValhallaDataDir("")
	// Must end with .twisty/valhalla
	if got == "" {
		t.Fatal("expected non-empty result")
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
go test -run "TestValhalla|TestResolveValhalla" -v .
```
Expected: compile error — functions not defined yet.

- [ ] **Step 3: Create `valhalla.go`**

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	valhallaContainerName = "twisty-valhalla"
	valhallaImage         = "ghcr.io/valhalla/valhalla:latest"
	defaultValhallaPort   = 8002
)

func newValhallaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "valhalla",
		Short:         "Manage a local Valhalla routing server via Docker",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		newValhallaStartCmd(),
		newValhallaStopCmd(),
		newValhallaStatusCmd(),
		newValhallaCleanCmd(),
		newValhallaLogsCmd(),
	)
	return cmd
}

func newValhallaStartCmd() *cobra.Command {
	var (
		regions string
		port    int
		dataDir string
	)
	cmd := &cobra.Command{
		Use:           "start",
		Short:         "Start the local Valhalla routing server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaStart(regions, port, dataDir, cmd.ErrOrStderr())
		},
	}
	f := cmd.Flags()
	f.StringVar(&regions, "regions", "", "Comma-separated Geofabrik region paths (e.g. north-america/us/tennessee)")
	f.IntVar(&port, "port", defaultValhallaPort, "Port to expose the Valhalla API on")
	f.StringVar(&dataDir, "data-dir", "", "Directory for Valhalla data files")
	return cmd
}

func newValhallaStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "stop",
		Short:         "Stop the local Valhalla routing server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaStop(cmd.ErrOrStderr())
		},
	}
}

func newValhallaStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "status",
		Short:         "Show status of the local Valhalla routing server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaStatus(cmd.ErrOrStderr())
		},
	}
}

func newValhallaCleanCmd() *cobra.Command {
	var dataDir string
	cmd := &cobra.Command{
		Use:           "clean",
		Short:         "Stop the container and remove all Valhalla data",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaClean(dataDir, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Directory for Valhalla data files")
	return cmd
}

func newValhallaLogsCmd() *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:           "logs",
		Short:         "Stream logs from the Valhalla routing server container",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaLogs(lines)
		},
	}
	cmd.Flags().IntVar(&lines, "lines", 0, "Number of log lines to show (0 = follow)")
	return cmd
}

func runValhallaStart(regions string, port int, dataDir string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dataDir = resolveValhallaDataDir(dataDir)

	var newRegions []string
	if regions != "" {
		for _, r := range strings.Split(regions, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				newRegions = append(newRegions, r)
			}
		}
	}

	tilesDir := filepath.Join(dataDir, "tiles")
	stampFile := filepath.Join(dataDir, ".regions")
	pbfDir := resolvePBFDir("")

	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		return fmt.Errorf("creating pbf dir: %w", err)
	}
	if err := os.MkdirAll(tilesDir, 0o755); err != nil {
		return fmt.Errorf("creating tiles dir: %w", err)
	}

	var existingRegions []string
	if stamp, err := os.ReadFile(stampFile); err == nil {
		for _, r := range strings.Split(strings.TrimSpace(string(stamp)), ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				existingRegions = append(existingRegions, r)
			}
		}
	}

	allRegions := mergeRegions(existingRegions, newRegions)
	if len(allRegions) == 0 {
		return fmt.Errorf("no regions specified and no cached regions found; use --regions to specify regions")
	}
	allRegionsKey := strings.Join(allRegions, ",")
	existingKey := strings.Join(existingRegions, ",")

	// If regions grew, wipe tiles to force a full rebuild.
	if allRegionsKey != existingKey {
		if len(existingRegions) > 0 {
			fmt.Fprintf(stderr, "Adding new regions; rebuilding tiles with full set.\n")
		}
		if err := os.RemoveAll(tilesDir); err != nil {
			return fmt.Errorf("removing tiles dir: %w", err)
		}
		if err := os.MkdirAll(tilesDir, 0o755); err != nil {
			return fmt.Errorf("recreating tiles dir: %w", err)
		}
	}

	// Download missing PBFs.
	downloadProgress := newDownloadProgress(os.Stderr)
	var missingURLs, missingPaths []string
	for _, region := range allRegions {
		filename := pbfFilename(region)
		destPath := filepath.Join(pbfDir, filename)
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			url := geofabrikBaseURL + "/" + region + "-latest.osm.pbf"
			fmt.Fprintf(stderr, "Downloading %s -> %s\n", url, destPath)
			missingURLs = append(missingURLs, url)
			missingPaths = append(missingPaths, destPath)
		} else {
			fmt.Fprintf(stderr, "PBF already exists: %s\n", destPath)
		}
	}
	if len(missingURLs) > 0 {
		if err := downloadPBFsParallel(missingURLs, missingPaths, downloadProgress); err != nil {
			return fmt.Errorf("downloading PBF: %w", err)
		}
		downloadProgress.Done()
	}

	// Build tiles if not already built.
	entries, _ := os.ReadDir(tilesDir)
	if len(entries) == 0 {
		fmt.Fprintf(stderr, "Building Valhalla tiles...\n")
		var pbfPaths, pbfNames []string
		for _, region := range allRegions {
			filename := pbfFilename(region)
			pbfPaths = append(pbfPaths, filepath.Join(pbfDir, filename))
			pbfNames = append(pbfNames, filename)
		}
		absTilesDir, err := filepath.Abs(tilesDir)
		if err != nil {
			return fmt.Errorf("resolving tiles dir: %w", err)
		}
		var absPBFPaths []string
		for _, p := range pbfPaths {
			abs, err := filepath.Abs(p)
			if err != nil {
				return fmt.Errorf("resolving pbf path: %w", err)
			}
			absPBFPaths = append(absPBFPaths, abs)
		}
		buildArgs := valhallaDockerBuildArgs(absTilesDir, absPBFPaths, pbfNames)
		if err := dockerCmdStreaming("docker", buildArgs...); err != nil {
			return fmt.Errorf("building Valhalla tiles: %w", err)
		}
	} else {
		fmt.Fprintf(stderr, "Tiles already exist: %s\n", tilesDir)
	}

	// Write stamp file.
	if err := os.WriteFile(stampFile, []byte(allRegionsKey), 0o644); err != nil {
		return fmt.Errorf("writing regions stamp: %w", err)
	}

	// If container is already running, just report the URL.
	if isContainerRunning(valhallaContainerName) {
		fmt.Fprintf(stderr, "Valhalla routing server already running: http://localhost:%d\n", port)
		return nil
	}

	// Remove any pre-existing stopped container.
	dockerCmd("rm", valhallaContainerName) //nolint:errcheck

	absTilesDir, err := filepath.Abs(tilesDir)
	if err != nil {
		return fmt.Errorf("resolving tiles dir: %w", err)
	}

	runArgs := valhallaDockerRunArgs(port, absTilesDir)
	out, err := dockerCmd(runArgs...)
	if err != nil {
		return fmt.Errorf("starting valhalla container: %v\n%s", err, out)
	}

	endpoint := fmt.Sprintf("http://localhost:%d", port)
	fmt.Fprintf(stderr, "Valhalla routing server starting. Streaming logs until ready...\n")
	fmt.Fprintf(stderr, "Endpoint: %s\n\n", endpoint)

	logCmd := exec.Command("docker", "logs", "-f", valhallaContainerName)
	logCmd.Stdout = stderr
	logCmd.Stderr = stderr
	if err := logCmd.Start(); err != nil {
		fmt.Fprintf(stderr, "warning: could not stream container logs: %v\n", err)
	}
	defer func() {
		if logCmd.Process != nil {
			logCmd.Process.Kill()
			logCmd.Wait()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := waitForValhalla(ctx, endpoint); err != nil {
		fmt.Fprintf(stderr, "\nValhalla failed to become ready: %v\n", err)
		fmt.Fprintf(stderr, "Container is still running. Use 'twisty valhalla logs' to inspect.\n")
		return fmt.Errorf("valhalla failed to become ready: %w", err)
	}

	fmt.Fprintf(stderr, "\nValhalla routing server is ready: %s\n", endpoint)
	return nil
}

func runValhallaStop(stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dockerCmd("stop", valhallaContainerName) //nolint:errcheck
	fmt.Fprintln(stderr, "Valhalla routing server stopped.")
	return nil
}

func runValhallaStatus(stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	if isContainerRunning(valhallaContainerName) {
		fmt.Fprintf(stderr, "Valhalla routing server is running: http://localhost:%d\n", defaultValhallaPort)
	} else {
		fmt.Fprintln(stderr, "Valhalla routing server is not running.")
	}
	return nil
}

func runValhallaClean(dataDir string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dataDir = resolveValhallaDataDir(dataDir)
	dockerCmd("stop", valhallaContainerName) //nolint:errcheck
	if err := os.RemoveAll(dataDir); err != nil {
		return fmt.Errorf("removing data dir %s: %w", dataDir, err)
	}
	fmt.Fprintf(stderr, "Valhalla container stopped and data directory %q removed.\n", dataDir)
	return nil
}

func runValhallaLogs(_ int) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found in PATH: %w", err)
	}
	argv := []string{"docker", "logs", "-f", valhallaContainerName}
	if err := syscall.Exec(dockerPath, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec docker logs: %w", err)
	}
	return nil
}

// valhallaDockerBuildArgs returns the `docker run --rm` arguments to build
// Valhalla tiles from one or more PBF files. absTilesDir is the absolute path
// to the tiles directory, absPBFPaths and pbfFilenames are parallel slices of
// absolute host paths and container-side filenames for each PBF.
func valhallaDockerBuildArgs(absTilesDir string, absPBFPaths, pbfFilenames []string) []string {
	args := []string{"run", "--rm", "-v", absTilesDir + ":/custom_files"}
	for i, path := range absPBFPaths {
		args = append(args, "-v", path+":/custom_files/"+pbfFilenames[i])
	}
	args = append(args, valhallaImage, "bash", "-c")

	// Build the bash command that runs all four valhalla build steps.
	pbfContainerPaths := make([]string, len(pbfFilenames))
	for i, f := range pbfFilenames {
		pbfContainerPaths[i] = "/custom_files/" + f
	}
	pbfArgs := strings.Join(pbfContainerPaths, " ")
	bashCmd := fmt.Sprintf(
		"valhalla_build_config --mjolnir-tile-dir /custom_files/tiles --mjolnir-timezone /usr/share/zoneinfo/UTC --mjolnir-admin /custom_files/admins.sqlite > /custom_files/valhalla.json && "+
			"valhalla_build_timezones /custom_files/valhalla.json && "+
			"valhalla_build_admins --config /custom_files/valhalla.json %s && "+
			"valhalla_build_tiles -c /custom_files/valhalla.json %s",
		pbfArgs, pbfArgs,
	)
	args = append(args, bashCmd)
	return args
}

// valhallaDockerRunArgs returns the `docker run -d` arguments to start the
// Valhalla routing server. absTilesDir must be an absolute path.
func valhallaDockerRunArgs(port int, absTilesDir string) []string {
	return []string{
		"run", "-d",
		"--name", valhallaContainerName,
		"--restart", "unless-stopped",
		"-p", fmt.Sprintf("%d:8002", port),
		"-v", absTilesDir + ":/custom_files",
		valhallaImage,
		"valhalla_service", "/custom_files/valhalla.json", "1",
	}
}

// probeValhalla sends a GET /status request to the Valhalla endpoint and
// returns true if it gets an HTTP 200 response.
func probeValhalla(endpoint string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(endpoint + "/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitForValhalla polls the Valhalla /status endpoint until it returns HTTP 200
// or the context is cancelled.
func waitForValhalla(ctx context.Context, endpoint string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for Valhalla to become ready")
		case <-ticker.C:
			if probeValhalla(endpoint) {
				return nil
			}
		}
	}
}

// resolveValhallaDataDir returns dir if non-empty, otherwise defaults to
// ~/.twisty/valhalla.
func resolveValhallaDataDir(dir string) string {
	if dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	return filepath.Join(home, ".twisty", "valhalla")
}
```

- [ ] **Step 4: Register `newValhallaCmd` in `main.go`**

In `newRootCmd()` in `main.go`, add `newValhallaCmd()` to the `AddCommand` call:

```go
cmd.AddCommand(
    newRouteCmd(),
    newFetchCmd(),
    newScoreCmd(),
    newRandomCmd(),
    newOverpassCmd(),
    newValhallaCmd(),
    newGpxCmd(),
    newDiagCmd(),
    newBuildCmd(),
)
```

- [ ] **Step 5: Run the unit tests to confirm they pass**

```bash
go test -run "TestValhalla|TestResolveValhalla" -v .
```
Expected: all PASS.

- [ ] **Step 6: Run all tests**

```bash
make test
```
Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add valhalla.go valhalla_test.go main.go
git commit -m "feat: add twisty valhalla command for local Valhalla routing server"
```

---

### Task 5: Verify end-to-end compile and test pass

A final integration check to confirm all four tasks compose correctly and the binary builds.

**Files:** none new

- [ ] **Step 1: Build the binary**

```bash
go build -o /dev/null .
```
Expected: exits 0 with no output.

- [ ] **Step 2: Run the full test suite**

```bash
make test
```
Expected: all tests pass.

- [ ] **Step 3: Smoke-test the CLI help output**

```bash
go run . valhalla --help
```
Expected: shows `start`, `stop`, `status`, `logs`, `clean` subcommands.

```bash
go run . valhalla start --help
```
Expected: shows `--regions`, `--port`, `--data-dir` flags.

```bash
go run . build --help
```
Expected: shows `--valhalla-url` flag with default `https://valhalla1.openstreetmap.de`.

- [ ] **Step 4: Commit if anything was fixed**

Only commit if step 1–3 required a fix. Otherwise no commit needed.

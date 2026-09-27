# Valhalla Multi-Region PBF Merge via Osmium Container

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge multiple PBF files into one using an osmium Docker container before building Valhalla tiles, eliminating the corruption Valhalla produces when given multiple PBF inputs.

**Architecture:** When `runValhallaStart` is called with multiple regions, run `iboates/osmium merge` in a Docker container to produce a single `merged.osm.pbf` in the Valhalla data dir, then pass only that single file to `valhallaDockerBuildArgs`. For a single region, skip the merge step entirely. A new function `mergePBFsWithOsmium` encapsulates the Docker call and is independently testable via its args-builder.

**Tech Stack:** Go, Docker (`iboates/osmium` image), existing `dockerCmdStreaming` helper.

## Global Constraints

- `iboates/osmium` is the osmium Docker image; no host osmium installation required.
- The merge step is skipped entirely when `len(allRegions) == 1`; no osmium container is pulled for single-region use.
- The merged PBF is written to `<dataDir>/merged.osm.pbf` and is wiped alongside the tiles dir when regions change.
- All Docker args functions must be pure (no I/O) so they are unit-testable without Docker.
- Run tests with `go test ./...` — do not set `GOCACHE`.

---

### Task 1: Add osmium merge args builder and merge function

**Files:**
- Modify: `valhalla.go`
- Modify: `valhalla_test.go`

**Interfaces:**
- Produces:
  - `osmiumMergeArgs(absMergedPBF string, absPBFPaths []string) []string` — returns `docker run --rm -v ... iboates/osmium merge -O -o /output/merged.osm.pbf /pbf/file1.osm.pbf ...` args
  - `mergePBFsWithOsmium(absMergedPBF string, absPBFPaths []string, stderr io.Writer) error` — runs osmium container, streams output to stderr

- [ ] **Step 1: Write the failing tests**

Add to `valhalla_test.go`:

```go
func TestOsmiumMergeArgs_SingleFile(t *testing.T) {
	pbfPaths := []string{"/data/pbf/north-america_us_tennessee-latest.osm.pbf"}
	mergedPath := "/data/valhalla/merged.osm.pbf"

	args := osmiumMergeArgs(mergedPath, pbfPaths)

	// Must use the osmium image.
	foundImage := false
	for _, a := range args {
		if a == osmiumImage {
			foundImage = true
		}
	}
	if !foundImage {
		t.Errorf("expected osmium image %q in args %v", osmiumImage, args)
	}

	// Must mount output dir.
	foundOutputMount := false
	for _, a := range args {
		if a == filepath.Dir(mergedPath)+":/output" {
			foundOutputMount = true
		}
	}
	if !foundOutputMount {
		t.Errorf("expected output dir mount in args %v", args)
	}

	// Must contain -O flag (overwrite).
	foundOverwrite := false
	for _, a := range args {
		if a == "-O" {
			foundOverwrite = true
		}
	}
	if !foundOverwrite {
		t.Errorf("expected -O flag in args %v", args)
	}

	// Must pass output path inside container.
	foundOutput := false
	for i, a := range args {
		if a == "-o" && i+1 < len(args) && args[i+1] == "/output/merged.osm.pbf" {
			foundOutput = true
		}
	}
	if !foundOutput {
		t.Errorf("expected '-o /output/merged.osm.pbf' in args %v", args)
	}

	// Must pass the PBF as container path.
	foundPBF := false
	for _, a := range args {
		if a == "/pbf/north-america_us_tennessee-latest.osm.pbf" {
			foundPBF = true
		}
	}
	if !foundPBF {
		t.Errorf("expected PBF container path in args %v", args)
	}
}

func TestOsmiumMergeArgs_MultipleFiles(t *testing.T) {
	pbfPaths := []string{
		"/data/pbf/north-america_us_tennessee-latest.osm.pbf",
		"/data/pbf/north-america_us_kentucky-latest.osm.pbf",
	}
	mergedPath := "/data/valhalla/merged.osm.pbf"

	args := osmiumMergeArgs(mergedPath, pbfPaths)

	// Both PBFs must be mounted under /pbf/.
	for _, p := range pbfPaths {
		filename := filepath.Base(p)
		mount := p + ":/pbf/" + filename
		found := false
		for _, a := range args {
			if a == mount {
				found = true
			}
		}
		if !found {
			t.Errorf("expected mount %q in args %v", mount, args)
		}
		// Both must appear as container-side paths in the command.
		containerPath := "/pbf/" + filename
		found = false
		for _, a := range args {
			if a == containerPath {
				found = true
			}
		}
		if !found {
			t.Errorf("expected container path %q in args %v", containerPath, args)
		}
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
go test -run "TestOsmiumMergeArgs" ./...
```

Expected: FAIL with `undefined: osmiumMergeArgs` or `undefined: osmiumImage`

- [ ] **Step 3: Add the constant and functions to `valhalla.go`**

Add the constant near the top with the other valhalla constants (after `defaultValhallaPort`):

```go
const (
	valhallaContainerName = "twisty-valhalla"
	valhallaImage         = "ghcr.io/valhalla/valhalla:latest"
	osmiumImage           = "iboates/osmium"
	defaultValhallaPort   = 8002
)
```

Add both functions near the bottom of `valhalla.go`, after `valhallaDockerRunArgs`:

```go
// osmiumMergeArgs returns the `docker run --rm` arguments to merge multiple
// PBF files into one using the osmium container. absMergedPBF is the absolute
// host path for the output file; absPBFPaths are the absolute host paths of
// the input PBF files.
func osmiumMergeArgs(absMergedPBF string, absPBFPaths []string) []string {
	outputDir := filepath.Dir(absMergedPBF)
	outputFilename := filepath.Base(absMergedPBF)

	args := []string{"run", "--rm", "-v", outputDir + ":/output"}
	for _, p := range absPBFPaths {
		filename := filepath.Base(p)
		args = append(args, "-v", p+":/pbf/"+filename)
	}
	args = append(args, osmiumImage, "merge", "-O", "-o", "/output/"+outputFilename)
	for _, p := range absPBFPaths {
		args = append(args, "/pbf/"+filepath.Base(p))
	}
	return args
}

// mergePBFsWithOsmium runs the osmium container to merge absPBFPaths into a
// single PBF at absMergedPBF, streaming output to stderr.
func mergePBFsWithOsmium(absMergedPBF string, absPBFPaths []string, stderr io.Writer) error {
	args := osmiumMergeArgs(absMergedPBF, absPBFPaths)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	return cmd.Run()
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

```bash
go test -run "TestOsmiumMergeArgs" ./...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add valhalla.go valhalla_test.go
git commit -m "feat(valhalla): add osmium merge args builder and container helper"
```

---

### Task 2: Wire merge into `runValhallaStart` and update `valhallaDockerBuildArgs`

**Files:**
- Modify: `valhalla.go`
- Modify: `valhalla_test.go`

**Interfaces:**
- Consumes:
  - `osmiumMergeArgs(absMergedPBF string, absPBFPaths []string) []string` from Task 1
  - `mergePBFsWithOsmium(absMergedPBF string, absPBFPaths []string, stderr io.Writer) error` from Task 1
  - `valhallaDockerBuildArgs(absTilesDir string, absPBFPaths []string, pbfFilenames []string) []string` — existing, unchanged signature; caller now passes a single-element slice when merging
- Produces: `runValhallaStart` calls `mergePBFsWithOsmium` when `len(allRegions) > 1`, writing `<dataDir>/merged.osm.pbf`, then passes only that merged file to `valhallaDockerBuildArgs`.

- [ ] **Step 1: Write the failing test for the multi-region merge path**

Add to `valhalla_test.go`:

```go
func TestValhallaDockerBuildArgs_UsesMergedPBFForMultipleRegions(t *testing.T) {
	// When a pre-merged PBF is passed, only one PBF mount and one container
	// path should appear — the caller is responsible for merging first.
	mergedPath := "/data/valhalla/merged.osm.pbf"
	tilesDir := "/data/valhalla/tiles"

	args := valhallaDockerBuildArgs(tilesDir, []string{mergedPath}, []string{"merged.osm.pbf"})

	// Exactly one /pbf/ mount.
	pbfMounts := 0
	for _, a := range args {
		if strings.HasPrefix(a, mergedPath+":/pbf/") {
			pbfMounts++
		}
	}
	if pbfMounts != 1 {
		t.Errorf("expected exactly 1 /pbf/ mount, got %d in %v", pbfMounts, args)
	}

	// The bash command must reference /pbf/merged.osm.pbf.
	bashIdx := -1
	for i, a := range args {
		if a == "bash" {
			bashIdx = i
		}
	}
	if bashIdx == -1 || bashIdx+2 >= len(args) {
		t.Fatalf("could not find bash -c in args %v", args)
	}
	bashCmd := args[bashIdx+2]
	if !strings.Contains(bashCmd, "/pbf/merged.osm.pbf") {
		t.Errorf("expected bash cmd to reference /pbf/merged.osm.pbf, got: %s", bashCmd)
	}
}
```

- [ ] **Step 2: Run the test to confirm it passes already** (this validates existing behavior is correct for the single-merged-pbf case before we wire the caller)

```bash
go test -run "TestValhallaDockerBuildArgs_UsesMergedPBFForMultipleRegions" ./...
```

Expected: PASS (the args builder already handles a single merged path correctly — this test documents the contract)

- [ ] **Step 3: Wire the merge step into `runValhallaStart`**

In `valhalla.go`, locate the tile-build block (around line 205). Replace:

```go
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
```

With:

```go
	// Build tiles if not already built.
	entries, _ := os.ReadDir(tilesDir)
	if len(entries) == 0 {
		fmt.Fprintf(stderr, "Building Valhalla tiles...\n")

		// Collect absolute PBF paths for all regions.
		var absPBFPaths []string
		for _, region := range allRegions {
			abs, err := filepath.Abs(filepath.Join(pbfDir, pbfFilename(region)))
			if err != nil {
				return fmt.Errorf("resolving pbf path: %w", err)
			}
			absPBFPaths = append(absPBFPaths, abs)
		}

		// When multiple regions are requested, merge them into a single PBF
		// first. Valhalla produces corrupted tiles when given multiple extracts
		// directly (https://github.com/valhalla/valhalla/issues/3925).
		buildPBFPaths := absPBFPaths
		buildPBFNames := make([]string, len(allRegions))
		for i, region := range allRegions {
			buildPBFNames[i] = pbfFilename(region)
		}
		if len(allRegions) > 1 {
			mergedPBF, err := filepath.Abs(filepath.Join(dataDir, "merged.osm.pbf"))
			if err != nil {
				return fmt.Errorf("resolving merged PBF path: %w", err)
			}
			fmt.Fprintf(stderr, "Merging %d PBF files...\n", len(absPBFPaths))
			if err := mergePBFsWithOsmium(mergedPBF, absPBFPaths, stderr); err != nil {
				return fmt.Errorf("merging PBF files: %w", err)
			}
			buildPBFPaths = []string{mergedPBF}
			buildPBFNames = []string{"merged.osm.pbf"}
		}

		absTilesDir, err := filepath.Abs(tilesDir)
		if err != nil {
			return fmt.Errorf("resolving tiles dir: %w", err)
		}
		buildArgs := valhallaDockerBuildArgs(absTilesDir, buildPBFPaths, buildPBFNames)
		if err := dockerCmdStreaming("docker", buildArgs...); err != nil {
			return fmt.Errorf("building Valhalla tiles: %w", err)
		}
	} else {
		fmt.Fprintf(stderr, "Tiles already exist: %s\n", tilesDir)
	}
```

- [ ] **Step 4: Wipe the merged PBF alongside tiles when regions change**

In `runValhallaStart`, locate the block that wipes tiles when regions change (around line 171). After the `os.MkdirAll(tilesDir, ...)` call, add removal of the stale merged PBF:

```go
	// If regions grew, wipe tiles and merged PBF to force a full rebuild.
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
		mergedPBF := filepath.Join(dataDir, "merged.osm.pbf")
		if err := os.Remove(mergedPBF); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing merged PBF: %w", err)
		}
	}
```

- [ ] **Step 5: Run the full test suite**

```bash
go test ./...
```

Expected: all non-integration tests pass (the two Valhalla integration tests require a live server and will still fail — that is expected)

- [ ] **Step 6: Commit**

```bash
git add valhalla.go valhalla_test.go
git commit -m "feat(valhalla): merge multiple PBF regions via osmium before tile build"
```

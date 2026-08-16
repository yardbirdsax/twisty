# Valhalla Stamp-Before-Build Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Write the `.regions` stamp file *before* starting the Valhalla tile build so that a failed or OOM-killed build does not cause `twisty valhalla start` to wipe and re-run from scratch on the next invocation.

**Architecture:** Currently `runValhallaStart` in `valhalla.go` writes `.regions` only after `dockerCmdStreaming` returns successfully. If the Docker container is OOM-killed (exit 137) mid-build, the stamp is never written, so the next run sees no stamp, wipes the tiles directory, and re-launches the full build — looping forever. The fix moves the stamp write to immediately before the tile build begins. The existing `len(entries) == 0` guard on the tiles directory still prevents redundant rebuilds when tiles are present; the stamp is now written speculatively and kept in sync regardless of build outcome.

**Tech Stack:** Go 1.21+, standard library only (`os`, `path/filepath`). No new dependencies.

## Global Constraints

- No new external dependencies.
- All new and changed behaviour must be covered by unit tests in `valhalla_test.go`.
- The fix must not alter the Docker arguments or container logic.
- Run `go test ./...` and confirm only the two pre-existing failures remain before claiming success.

---

### Task 1: Write stamp before tile build; update "already built" path to also stamp

**Root cause recap:**
- `stampFile` = `~/.twisty/valhalla/.regions`
- Currently written at line 259, *after* `dockerCmdStreaming` succeeds.
- On OOM kill, `dockerCmdStreaming` returns an error; the function returns early; stamp is never written.
- Next run: no stamp → `existingRegions` is empty → `allRegionsKey != existingKey` → tiles wiped → build re-launched → OOM again.

**Fix:** write the stamp (a) when we decide to build (before launching Docker) and (b) when tiles already exist and we skip the build. Both paths end with a known-good stamp.

**Files:**
- Modify: `valhalla.go:259` (stamp write at end of function)

**Interfaces:**
- Consumes: nothing new
- Produces: nothing new — pure behavioral change

- [ ] **Step 1: Write the failing test**

Add to `valhalla_test.go`. This test calls the internal helper `writeStampFile` (which does not exist yet — it will be extracted in Step 3) to verify the stamp is written before the build, by asserting that a simulated build failure does not prevent subsequent runs from detecting existing regions.

Because `runValhallaStart` calls Docker and downloads PBFs, we test the stamp-write logic through a narrower unit: a new exported-for-test helper `stampRegions(stampFile string, regions []string) error`. Add this test at the bottom of `valhalla_test.go`:

```go
func TestStampRegions_WritesFile(t *testing.T) {
	dir := t.TempDir()
	stampFile := filepath.Join(dir, ".regions")
	regions := []string{"north-america/us/tennessee", "north-america/us/kentucky"}

	if err := stampRegions(stampFile, regions); err != nil {
		t.Fatalf("stampRegions returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(stampFile)
	if err != nil {
		t.Fatalf("stamp file not written: %v", err)
	}
	got := strings.TrimSpace(string(data))
	want := "north-america/us/kentucky,north-america/us/tennessee"
	if got != want {
		t.Errorf("stamp content: got %q, want %q", got, want)
	}
}

func TestStampRegions_Overwrites(t *testing.T) {
	dir := t.TempDir()
	stampFile := filepath.Join(dir, ".regions")

	// Write initial stamp.
	if err := stampRegions(stampFile, []string{"europe/germany"}); err != nil {
		t.Fatalf("first stampRegions: %v", err)
	}
	// Overwrite with new content.
	if err := stampRegions(stampFile, []string{"north-america/us/tennessee"}); err != nil {
		t.Fatalf("second stampRegions: %v", err)
	}

	data, _ := os.ReadFile(stampFile)
	got := strings.TrimSpace(string(data))
	if got != "north-america/us/tennessee" {
		t.Errorf("expected overwrite, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -run "TestStampRegions" ./...
```

Expected: FAIL — `stampRegions` is undefined.

- [ ] **Step 3: Extract `stampRegions` helper and move call sites**

In `valhalla.go`, extract a small helper and move the stamp-write to *before* the tile build is launched. Replace the current single stamp-write at line 259 with calls at both the "build needed" and "tiles already exist" branches.

The current code around lines 210–260 looks like:

```go
	// Build tiles if not already built.
	entries, _ := os.ReadDir(tilesDir)
	if len(entries) == 0 {
		fmt.Fprintf(stderr, "Building Valhalla tiles...\n")
		// ... resolve paths, maybe merge PBFs ...
		buildArgs := valhallaDockerBuildArgs(absTilesDir, buildPBFPaths, buildPBFNames)
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
```

Replace with:

```go
	// Build tiles if not already built.
	entries, _ := os.ReadDir(tilesDir)
	if len(entries) == 0 {
		fmt.Fprintf(stderr, "Building Valhalla tiles...\n")

		// Stamp the intended regions before launching the build so that a
		// crash or OOM kill does not cause the next invocation to wipe and
		// restart from scratch when tiles are already partially built.
		if err := stampRegions(stampFile, allRegions); err != nil {
			return err
		}

		// ... resolve paths, maybe merge PBFs — unchanged ...
		buildArgs := valhallaDockerBuildArgs(absTilesDir, buildPBFPaths, buildPBFNames)
		if err := dockerCmdStreaming("docker", buildArgs...); err != nil {
			return fmt.Errorf("building Valhalla tiles: %w", err)
		}
	} else {
		fmt.Fprintf(stderr, "Tiles already exist: %s\n", tilesDir)
		if err := stampRegions(stampFile, allRegions); err != nil {
			return err
		}
	}
```

And add the helper at the bottom of `valhalla.go` (before or after `validateRegion`):

```go
// stampRegions writes the sorted region list to stampFile so subsequent
// invocations can detect which regions are already prepared.
func stampRegions(stampFile string, regions []string) error {
	sorted := make([]string, len(regions))
	copy(sorted, regions)
	sort.Strings(sorted)
	if err := os.WriteFile(stampFile, []byte(strings.Join(sorted, ",")), 0o644); err != nil {
		return fmt.Errorf("writing regions stamp: %w", err)
	}
	return nil
}
```

Add `"sort"` to the import block in `valhalla.go` (it is not yet imported there; confirm with `grep -n '"sort"' valhalla.go`).

Remove the old stamp-write block (the `// Write stamp file.` comment and the `os.WriteFile` call that followed it).

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -run "TestStampRegions" ./...
```

Expected: PASS — both `TestStampRegions_WritesFile` and `TestStampRegions_Overwrites` pass.

- [ ] **Step 5: Verify full test suite is clean**

```bash
go test ./...
```

Expected: same pass/fail count as baseline — 753 passed, 2 failed (pre-existing `TestDiagPA120` and `TestFetchRoutesValhalla_Integration`), 11 skipped. No new failures.

- [ ] **Step 6: Commit**

```bash
git add valhalla.go valhalla_test.go
git commit -m "fix(valhalla): write regions stamp before tile build to survive OOM kills"
```

---

## Self-Review

**Spec coverage:**
- Root cause 1 (OOM kill → stamp never written → perpetual wipe+rebuild loop): fixed by writing stamp before `dockerCmdStreaming` call. ✓
- Root cause 2 (OOM itself): outside the scope of a code fix — requires Docker Desktop memory increase. No code can prevent the kernel from killing the container. ✓ (out of scope, documented in diagnosis)
- "Tiles already exist" path also stamps: covered in else-branch. ✓

**Placeholder scan:** No TBDs, no vague steps, all code shown in full. ✓

**Type consistency:** `stampRegions(stampFile string, regions []string) error` — used identically in both call sites and both test cases. ✓

**Note on `sort` import:** `mergeRegions` (in `pbf.go`) already returns a sorted slice, so `allRegions` is already sorted when passed to `stampRegions`. The `sort.Strings` call inside `stampRegions` is defensive and costs nothing — it makes the helper correct in isolation regardless of call-site guarantees.

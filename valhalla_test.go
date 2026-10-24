package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValhallaDockerBuildArgs_SingleRegion(t *testing.T) {
	pbfPaths := []string{"/data/pbf/north-america_us_tennessee-latest.osm.pbf"}
	pbfFilenames := []string{"north-america_us_tennessee-latest.osm.pbf"}
	tilesDir := "/data/valhalla/tiles"

	args := valhallaDockerBuildArgs(tilesDir, pbfPaths, pbfFilenames)

	// Must contain the image name.
	foundImage := false
	for _, a := range args {
		if a == valhallaImage {
			foundImage = true
		}
	}
	if !foundImage {
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
	// Must mount the PBF under /pbf/, not inside /custom_files/ (which is
	// already a directory mount — overlapping mounts break Docker on Mac).
	foundPBF := false
	for _, a := range args {
		if a == pbfPaths[0]+":/pbf/"+pbfFilenames[0] {
			foundPBF = true
		}
	}
	if !foundPBF {
		t.Errorf("expected PBF mount in args %v", args)
	}
	// Must NOT mount PBF inside /custom_files/.
	for _, a := range args {
		if a == pbfPaths[0]+":/custom_files/"+pbfFilenames[0] {
			t.Errorf("PBF must not be mounted inside /custom_files/, got %v", args)
		}
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

	// Both PBF files must be mounted under /pbf/.
	for i, path := range pbfPaths {
		mount := path + ":/pbf/" + pbfFilenames[i]
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

func TestValidateRegion_Valid(t *testing.T) {
	cases := []string{"north-america/us/tennessee", "europe/germany", "asia/japan"}
	for _, c := range cases {
		if err := validateRegion(c); err != nil {
			t.Errorf("validateRegion(%q) unexpected error: %v", c, err)
		}
	}
}

func TestValidateRegion_Invalid(t *testing.T) {
	cases := []string{"north-america/us/tennessee;ls", "region$(cmd)", "bad region"}
	for _, c := range cases {
		if err := validateRegion(c); err == nil {
			t.Errorf("validateRegion(%q) expected error, got nil", c)
		}
	}
}

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

// writeTile creates an empty .gph file for the given level under
// <tilesDir>/tiles/<level>/000/.
func writeTile(t *testing.T, tilesDir, level string) {
	t.Helper()
	dir := filepath.Join(tilesDir, "tiles", level, "000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "123.gph"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTilesComplete_AllLevels(t *testing.T) {
	tilesDir := t.TempDir()
	for _, l := range []string{"0", "1", "2"} {
		writeTile(t, tilesDir, l)
	}
	if ok, problem := tilesComplete(tilesDir); !ok {
		t.Errorf("expected complete, got problem %q", problem)
	}
}

func TestTilesComplete_OnlyLevel2(t *testing.T) {
	tilesDir := t.TempDir()
	writeTile(t, tilesDir, "2")
	if err := os.WriteFile(filepath.Join(tilesDir, "tiles", "ways_0.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, problem := tilesComplete(tilesDir)
	if ok {
		t.Fatal("expected incomplete")
	}
	if !strings.Contains(problem, "level 0") {
		t.Errorf("expected problem to name level 0, got %q", problem)
	}
}

func TestTilesComplete_Empty(t *testing.T) {
	if ok, _ := tilesComplete(t.TempDir()); ok {
		t.Error("expected empty dir to be incomplete")
	}
}

func completeDataDir(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	tilesDir := filepath.Join(dataDir, "tiles")
	for _, l := range []string{"0", "1", "2"} {
		writeTile(t, tilesDir, l)
	}
	return dataDir
}

func TestNeedsBuild_NoMarkerCompleteTiles_WritesMarker(t *testing.T) {
	dataDir := completeDataDir(t)
	build, reason := needsBuild(dataDir)
	if build {
		t.Errorf("expected no rebuild, got reason %q", reason)
	}
	if _, err := os.Stat(filepath.Join(dataDir, builtMarkerName)); err != nil {
		t.Errorf("expected marker to be written: %v", err)
	}
}

func TestNeedsBuild_NoMarkerCompleteTilesWithLeftoverBin_Rebuilds(t *testing.T) {
	dataDir := completeDataDir(t)
	if err := os.WriteFile(filepath.Join(dataDir, "tiles", "tiles", "ways_0.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	build, reason := needsBuild(dataDir)
	if !build {
		t.Fatal("expected rebuild")
	}
	if !strings.Contains(reason, "intermediate build files") {
		t.Errorf("expected reason to mention intermediate build files, got %q", reason)
	}
	if _, err := os.Stat(filepath.Join(dataDir, builtMarkerName)); err == nil {
		t.Error("marker must not be written when intermediate files remain")
	}
}

func TestNeedsBuild_MarkerPresentIncompleteTiles(t *testing.T) {
	dataDir := t.TempDir()
	writeTile(t, filepath.Join(dataDir, "tiles"), "2")
	if err := os.WriteFile(filepath.Join(dataDir, builtMarkerName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	build, reason := needsBuild(dataDir)
	if !build {
		t.Fatal("expected rebuild")
	}
	if !strings.Contains(reason, "level 0") {
		t.Errorf("expected reason to name level 0, got %q", reason)
	}
}

func TestNeedsBuild_NoMarkerIncompleteTiles(t *testing.T) {
	dataDir := t.TempDir()
	writeTile(t, filepath.Join(dataDir, "tiles"), "2")
	build, _ := needsBuild(dataDir)
	if !build {
		t.Error("expected rebuild")
	}
	if _, err := os.Stat(filepath.Join(dataDir, builtMarkerName)); err == nil {
		t.Error("marker must not be written for incomplete tiles")
	}
}

func TestNeedsBuild_MarkerAndCompleteTiles(t *testing.T) {
	dataDir := completeDataDir(t)
	if err := os.WriteFile(filepath.Join(dataDir, builtMarkerName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if build, reason := needsBuild(dataDir); build {
		t.Errorf("expected no rebuild, got %q", reason)
	}
}

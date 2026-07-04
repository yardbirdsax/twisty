package main

import (
	"compress/bzip2"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yardbirdsax/twisty/osmconv"
)

// setupPBFRegion writes a minimal PBF file for a region into pbfDir and returns
// the PBF path.
func setupPBFRegion(t *testing.T, pbfDir, region string) string {
	t.Helper()
	pbfData := osmconv.BuildMinimalPBFBytes()
	pbfPath := filepath.Join(pbfDir, pbfFilename(region))
	if err := os.WriteFile(pbfPath, pbfData, 0o644); err != nil {
		t.Fatalf("writing PBF for %s: %v", region, err)
	}
	return pbfPath
}

// decompressBZ2 reads a BZ2 file and returns the raw bytes.
func decompressBZ2(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening bz2 file: %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(bzip2.NewReader(f))
	if err != nil {
		t.Fatalf("decompressing bz2: %v", err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Tests for cleanOrphanCacheFiles
// ---------------------------------------------------------------------------

func TestCleanOrphanCacheFiles_DeletesOrphans(t *testing.T) {
	dir := t.TempDir()

	// Create cache files for regions A, B, C.
	for _, region := range []string{"alpha", "beta", "gamma"} {
		name := cacheFilename(region)
		os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644)
		os.WriteFile(filepath.Join(dir, name+".meta.json"), []byte("{}"), 0o644)
	}

	// Active regions: only alpha and beta.
	if err := cleanOrphanCacheFiles(dir, []string{"alpha", "beta"}, io.Discard); err != nil {
		t.Fatalf("cleanOrphanCacheFiles: %v", err)
	}

	// gamma cache and meta should be gone.
	gammaCache := filepath.Join(dir, cacheFilename("gamma"))
	gammaMeta := gammaCache + ".meta.json"
	if _, err := os.Stat(gammaCache); !os.IsNotExist(err) {
		t.Error("expected gamma cache to be deleted")
	}
	if _, err := os.Stat(gammaMeta); !os.IsNotExist(err) {
		t.Error("expected gamma meta to be deleted")
	}

	// alpha and beta should still exist.
	for _, region := range []string{"alpha", "beta"} {
		cachePath := filepath.Join(dir, cacheFilename(region))
		if _, err := os.Stat(cachePath); err != nil {
			t.Errorf("expected %s cache to still exist: %v", region, err)
		}
	}
}

func TestCleanOrphanCacheFiles_NoOrphans(t *testing.T) {
	dir := t.TempDir()

	for _, region := range []string{"alpha", "beta"} {
		name := cacheFilename(region)
		os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644)
	}

	if err := cleanOrphanCacheFiles(dir, []string{"alpha", "beta"}, io.Discard); err != nil {
		t.Fatalf("cleanOrphanCacheFiles: %v", err)
	}

	for _, region := range []string{"alpha", "beta"} {
		cachePath := filepath.Join(dir, cacheFilename(region))
		if _, err := os.Stat(cachePath); err != nil {
			t.Errorf("expected %s cache to still exist: %v", region, err)
		}
	}
}

func TestCleanOrphanCacheFiles_MissingDirIsNoop(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	if err := cleanOrphanCacheFiles(dir, []string{"alpha"}, io.Discard); err != nil {
		t.Fatalf("expected no error for missing dir, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests for mergeRegionCachesToBZ2
// ---------------------------------------------------------------------------

func TestMergeRegionCachesToBZ2_ProducesValidBZ2(t *testing.T) {
	dir := t.TempDir()
	pbfDir := filepath.Join(dir, "pbf")
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(pbfDir, 0o755)
	os.MkdirAll(cacheDir, 0o755)

	regions := []string{"alpha", "beta"}
	var cachePaths []string
	for _, region := range regions {
		pbfPath := setupPBFRegion(t, pbfDir, region)
		cachePath := filepath.Join(cacheDir, cacheFilename(region))
		metaPath := cachePath + ".meta.json"
		if err := convertSingleRegionToCache(pbfPath, cachePath, metaPath, nil); err != nil {
			t.Fatalf("convertSingleRegionToCache(%s): %v", region, err)
		}
		cachePaths = append(cachePaths, cachePath)
	}

	bz2Path := filepath.Join(dir, "merged.osm.bz2")
	if err := mergeRegionCachesToBZ2(cachePaths, bz2Path, nil); err != nil {
		t.Fatalf("mergeRegionCachesToBZ2: %v", err)
	}

	// Verify file exists and decompresses to OSM XML.
	data := decompressBZ2(t, bz2Path)
	xml := string(data)
	if !strings.Contains(xml, "<osm") {
		t.Errorf("expected decompressed BZ2 to contain <osm, got prefix: %q", xml[:min(200, len(xml))])
	}
}

// ---------------------------------------------------------------------------
// Tests for convertRegionsIncremental
// ---------------------------------------------------------------------------

func TestConvertRegionsIncremental_FullPipeline(t *testing.T) {
	dataDir := t.TempDir()
	pbfDir := filepath.Join(dataDir, "pbf")
	os.MkdirAll(pbfDir, 0o755)

	regions := []string{"alpha", "beta"}
	for _, region := range regions {
		setupPBFRegion(t, pbfDir, region)
	}

	if err := convertRegionsIncremental(regions, dataDir, pbfDir, nil, nil); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}

	mergedBZ2 := filepath.Join(dataDir, "merged.osm.bz2")
	data := decompressBZ2(t, mergedBZ2)
	if !strings.Contains(string(data), "<osm") {
		t.Error("expected merged BZ2 to contain <osm")
	}
}

func TestConvertRegionsIncremental_CacheReuse(t *testing.T) {
	dataDir := t.TempDir()
	pbfDir := filepath.Join(dataDir, "pbf")
	os.MkdirAll(pbfDir, 0o755)

	regions := []string{"alpha"}
	setupPBFRegion(t, pbfDir, "alpha")

	// First run — converts and writes cache.
	if err := convertRegionsIncremental(regions, dataDir, pbfDir, nil, nil); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Record cache mtime.
	cacheDir := filepath.Join(dataDir, "cache")
	cachePath := filepath.Join(cacheDir, cacheFilename("alpha"))
	info1, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache after first run: %v", err)
	}

	// Delete merged.osm.bz2 so second run re-merges.
	os.Remove(filepath.Join(dataDir, "merged.osm.bz2"))

	// Second run — should skip conversion, only merge.
	if err := convertRegionsIncremental(regions, dataDir, pbfDir, nil, nil); err != nil {
		t.Fatalf("second run: %v", err)
	}

	info2, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache after second run: %v", err)
	}

	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("cache file was re-written on second run; expected it to be reused")
	}
}

func TestConvertRegionsIncremental_IncrementalAddition(t *testing.T) {
	dataDir := t.TempDir()
	pbfDir := filepath.Join(dataDir, "pbf")
	os.MkdirAll(pbfDir, 0o755)

	setupPBFRegion(t, pbfDir, "alpha")
	setupPBFRegion(t, pbfDir, "beta")

	// First run with alpha and beta.
	if err := convertRegionsIncremental([]string{"alpha", "beta"}, dataDir, pbfDir, nil, nil); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Record alpha cache mtime.
	cacheDir := filepath.Join(dataDir, "cache")
	alphaCache := filepath.Join(cacheDir, cacheFilename("alpha"))
	info1, _ := os.Stat(alphaCache)

	// Set up gamma PBF and delete merged so it re-runs.
	setupPBFRegion(t, pbfDir, "gamma")
	os.Remove(filepath.Join(dataDir, "merged.osm.bz2"))

	// Second run with alpha, beta, gamma.
	if err := convertRegionsIncremental([]string{"alpha", "beta", "gamma"}, dataDir, pbfDir, nil, nil); err != nil {
		t.Fatalf("second run: %v", err)
	}

	// Alpha should not have been re-converted.
	info2, _ := os.Stat(alphaCache)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("alpha cache was re-written; expected it to be reused")
	}

	// Gamma cache should now exist.
	gammaCache := filepath.Join(cacheDir, cacheFilename("gamma"))
	if _, err := os.Stat(gammaCache); err != nil {
		t.Errorf("gamma cache missing: %v", err)
	}
}

func TestConvertRegionsIncremental_OrphanCleanup(t *testing.T) {
	dataDir := t.TempDir()
	pbfDir := filepath.Join(dataDir, "pbf")
	os.MkdirAll(pbfDir, 0o755)

	setupPBFRegion(t, pbfDir, "alpha")
	setupPBFRegion(t, pbfDir, "beta")
	setupPBFRegion(t, pbfDir, "gamma")

	// First run with all three.
	if err := convertRegionsIncremental([]string{"alpha", "beta", "gamma"}, dataDir, pbfDir, nil, nil); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Delete merged and run again with only alpha and beta.
	os.Remove(filepath.Join(dataDir, "merged.osm.bz2"))
	if err := convertRegionsIncremental([]string{"alpha", "beta"}, dataDir, pbfDir, nil, nil); err != nil {
		t.Fatalf("second run: %v", err)
	}

	// Gamma cache should be cleaned up.
	cacheDir := filepath.Join(dataDir, "cache")
	gammaCache := filepath.Join(cacheDir, cacheFilename("gamma"))
	if _, err := os.Stat(gammaCache); !os.IsNotExist(err) {
		t.Error("expected gamma cache to be removed as orphan")
	}
}

// recordingConvertProgress records which methods were called and with what.
type recordingConvertProgress struct {
	setFilesCalls     int
	cachedMessages    []string
	convertingMessage []string
}

func (r *recordingConvertProgress) SetFiles(names []string, sizes []int64) { r.setFilesCalls++ }
func (r *recordingConvertProgress) BytesRead(fileIndex int, n int64)        {}
func (r *recordingConvertProgress) ObjectsWritten(n int)                    {}
func (r *recordingConvertProgress) Done()                                   {}

func TestConvertRegionsIncremental_ProgressReporting(t *testing.T) {
	dataDir := t.TempDir()
	pbfDir := filepath.Join(dataDir, "pbf")
	os.MkdirAll(pbfDir, 0o755)

	setupPBFRegion(t, pbfDir, "alpha")
	setupPBFRegion(t, pbfDir, "beta")

	rec := &recordingConvertProgress{}
	var stderrBuf strings.Builder
	if err := convertRegionsIncremental([]string{"alpha", "beta"}, dataDir, pbfDir, rec, &stderrBuf); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}

	// SetFiles is called once per region conversion plus once during the merge step.
	// With 2 regions that's 3 total calls; assert it was called at least once.
	if rec.setFilesCalls == 0 {
		t.Error("expected SetFiles to be called at least once, got 0")
	}

	output := stderrBuf.String()
	if !strings.Contains(output, "converting...") {
		t.Errorf("expected '(converting...)' in output, got: %s", output)
	}
	if !strings.Contains(output, "Region 1/2") {
		t.Errorf("expected 'Region 1/2' in output, got: %s", output)
	}
	if !strings.Contains(output, "Region 2/2") {
		t.Errorf("expected 'Region 2/2' in output, got: %s", output)
	}

	// Delete merged and run again — both should report cached.
	os.Remove(filepath.Join(dataDir, "merged.osm.bz2"))
	var stderrBuf2 strings.Builder
	if err := convertRegionsIncremental([]string{"alpha", "beta"}, dataDir, pbfDir, osmconv.NoopConvertProgress{}, &stderrBuf2); err != nil {
		t.Fatalf("second run: %v", err)
	}

	output2 := stderrBuf2.String()
	if !strings.Contains(output2, "(cached)") {
		t.Errorf("expected '(cached)' in second-run output, got: %s", output2)
	}
}

package main

import (
	"compress/bzip2"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/osmconv"
)

// e2eDataDir is the data directory for end-to-end validation.
// Override with -e2e-data-dir flag.
var e2eDataDir string

func init() {
	flag.StringVar(&e2eDataDir, "e2e-data-dir", ".tmp/e2e-validation", "data directory for e2e validation tests")
}

// Four small US states for baseline, Vermont as the incremental addition.
var (
	e2eRegions4 = []string{
		"north-america/us/connecticut",
		"north-america/us/delaware",
		"north-america/us/new-hampshire",
		"north-america/us/rhode-island",
	}
	e2eRegion5 = "north-america/us/vermont"
	e2eRegions5 = append(append([]string{}, e2eRegions4...), e2eRegion5)
)

// TestE2E_DownloadPBFs downloads all 5 PBF files into the e2eDataDir.
// Run this step before timing tests to pre-populate the pbf/ directory.
func TestE2E_DownloadPBFs(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("set RUN_E2E=1 to run end-to-end validation")
	}

	pbfDir := filepath.Join(e2eDataDir, "pbf")
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		t.Fatalf("creating pbf dir: %v", err)
	}

	progress := newDownloadProgress(os.Stderr)
	for i, region := range e2eRegions5 {
		filename := pbfFilename(region)
		destPath := filepath.Join(pbfDir, filename)
		if _, err := os.Stat(destPath); err == nil {
			t.Logf("PBF already exists: %s", destPath)
			continue
		}
		url := geofabrikBaseURL + "/" + region + "-latest.osm.pbf"
		t.Logf("Downloading %s -> %s", url, destPath)
		fp := progress.StartFile(destPath, i, len(e2eRegions5), -1)
		if err := downloadPBF(url, destPath, fp); err != nil {
			t.Fatalf("downloading %s: %v", url, err)
		}
	}
	progress.Done()
}

// TestE2E_4RegionColdCache times a full 4-region conversion from cold cache.
// PBFs must be pre-populated by TestE2E_DownloadPBFs.
func TestE2E_4RegionColdCache(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("set RUN_E2E=1 to run end-to-end validation")
	}

	// Clean cache and merged output, keep PBFs.
	cleanCacheAndMerged(t, e2eDataDir)

	start := time.Now()
	if err := convertRegionsIncremental(e2eRegions4, e2eDataDir, filepath.Join(e2eDataDir, "pbf"), osmconv.NoopConvertProgress{}, os.Stderr); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("4-REGION COLD-CACHE TIME: %s", elapsed)
	writeTimingRecord(t, e2eDataDir, "4_region_cold_cache", elapsed, len(e2eRegions4), false)
}

// TestE2E_5RegionColdCache times a full 5-region conversion from cold cache.
// This is the baseline for speedup comparison.
func TestE2E_5RegionColdCache(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("set RUN_E2E=1 to run end-to-end validation")
	}

	// Clean cache and merged output, keep PBFs.
	cleanCacheAndMerged(t, e2eDataDir)

	start := time.Now()
	if err := convertRegionsIncremental(e2eRegions5, e2eDataDir, filepath.Join(e2eDataDir, "pbf"), osmconv.NoopConvertProgress{}, os.Stderr); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("5-REGION COLD-CACHE TIME: %s", elapsed)
	writeTimingRecord(t, e2eDataDir, "5_region_cold_cache", elapsed, len(e2eRegions5), false)
}

// TestE2E_IncrementalAddition times adding Vermont (5th region) with the 4-region cache warm.
// Must be run after TestE2E_4RegionColdCache (which warms the cache for the 4 base regions).
func TestE2E_IncrementalAddition(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("set RUN_E2E=1 to run end-to-end validation")
	}

	// Ensure 4-region cache is warm.
	cacheDir := filepath.Join(e2eDataDir, "cache")
	for _, region := range e2eRegions4 {
		cachePath := filepath.Join(cacheDir, cacheFilename(region))
		metaPath := cachePath + ".meta.json"
		pbfPath := filepath.Join(e2eDataDir, "pbf", pbfFilename(region))
		if !isCacheValid(pbfPath, cachePath, metaPath) {
			t.Fatalf("4-region cache is not warm for %s; run TestE2E_4RegionColdCache first", region)
		}
	}

	// Delete only merged.osm.bz2 to simulate adding a new region.
	mergedBZ2 := filepath.Join(e2eDataDir, "merged.osm.bz2")
	if err := os.Remove(mergedBZ2); err != nil && !os.IsNotExist(err) {
		t.Fatalf("removing merged.osm.bz2: %v", err)
	}

	start := time.Now()
	if err := convertRegionsIncremental(e2eRegions5, e2eDataDir, filepath.Join(e2eDataDir, "pbf"), osmconv.NoopConvertProgress{}, os.Stderr); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("INCREMENTAL 5TH-REGION TIME: %s", elapsed)
	writeTimingRecord(t, e2eDataDir, "incremental_5th_region", elapsed, len(e2eRegions5), true)

	// Save incremental merged output for correctness verification.
	incrementalPath := filepath.Join(e2eDataDir, "merged_incremental.osm.bz2")
	if err := os.Rename(mergedBZ2, incrementalPath); err != nil {
		t.Fatalf("saving incremental output: %v", err)
	}
	t.Logf("Incremental merged output saved to %s", incrementalPath)
}

// TestE2E_CorrectnessVerification compares incremental and full 5-region merged outputs.
// Must be run after TestE2E_IncrementalAddition.
func TestE2E_CorrectnessVerification(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("set RUN_E2E=1 to run end-to-end validation")
	}

	incrementalPath := filepath.Join(e2eDataDir, "merged_incremental.osm.bz2")
	if _, err := os.Stat(incrementalPath); err != nil {
		t.Fatalf("incremental output not found at %s; run TestE2E_IncrementalAddition first", incrementalPath)
	}

	// Do a fresh full 5-region conversion.
	cleanCacheAndMerged(t, e2eDataDir)
	t.Log("Running fresh full 5-region conversion for correctness comparison...")
	if err := convertRegionsIncremental(e2eRegions5, e2eDataDir, filepath.Join(e2eDataDir, "pbf"), osmconv.NoopConvertProgress{}, os.Stderr); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	fullPath := filepath.Join(e2eDataDir, "merged.osm.bz2")

	// Count OSM objects in both outputs and compare.
	t.Log("Counting objects in incremental output...")
	incrCounts, err := countOSMObjects(incrementalPath)
	if err != nil {
		t.Fatalf("counting objects in incremental output: %v", err)
	}
	t.Log("Counting objects in full output...")
	fullCounts, err := countOSMObjects(fullPath)
	if err != nil {
		t.Fatalf("counting objects in full output: %v", err)
	}

	t.Logf("Incremental: nodes=%d ways=%d relations=%d total=%d",
		incrCounts.nodes, incrCounts.ways, incrCounts.relations, incrCounts.total())
	t.Logf("Full:        nodes=%d ways=%d relations=%d total=%d",
		fullCounts.nodes, fullCounts.ways, fullCounts.relations, fullCounts.total())

	match := incrCounts == fullCounts
	if match {
		t.Log("CORRECTNESS: PASS — object counts match exactly")
	} else {
		t.Errorf("CORRECTNESS: FAIL — object counts differ")
		t.Errorf("  nodes:     incremental=%d full=%d diff=%d",
			incrCounts.nodes, fullCounts.nodes, incrCounts.nodes-fullCounts.nodes)
		t.Errorf("  ways:      incremental=%d full=%d diff=%d",
			incrCounts.ways, fullCounts.ways, incrCounts.ways-fullCounts.ways)
		t.Errorf("  relations: incremental=%d full=%d diff=%d",
			incrCounts.relations, fullCounts.relations, incrCounts.relations-fullCounts.relations)
	}

	writeCorrectnessRecord(t, e2eDataDir, incrCounts, fullCounts, match)
}

// TestE2E_SpeedupSummary reads the recorded timing files and prints the speedup calculation.
func TestE2E_SpeedupSummary(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("set RUN_E2E=1 to run end-to-end validation")
	}

	full5, err := readTimingRecord(e2eDataDir, "5_region_cold_cache")
	if err != nil {
		t.Fatalf("reading 5-region timing: %v", err)
	}
	incr, err := readTimingRecord(e2eDataDir, "incremental_5th_region")
	if err != nil {
		t.Fatalf("reading incremental timing: %v", err)
	}

	speedup := 1.0 - (float64(incr)/float64(full5))
	t.Logf("5-region cold-cache time:           %s", time.Duration(full5))
	t.Logf("Incremental 5th-region time:         %s", time.Duration(incr))
	t.Logf("Speedup (1 - incr/full5):            %.1f%%", speedup*100)
	if speedup >= 0.50 {
		t.Logf("TARGET MET: speedup %.1f%% >= 50%%", speedup*100)
	} else {
		t.Logf("TARGET NOT MET: speedup %.1f%% < 50%%; see validation_results.md for analysis", speedup*100)
	}
}

// --- helpers ---

type objectCounts struct {
	nodes     int64
	ways      int64
	relations int64
}

func (c objectCounts) total() int64 { return c.nodes + c.ways + c.relations }

// countOSMObjects counts node/way/relation start tags in a BZ2-compressed OSM XML file.
func countOSMObjects(bz2Path string) (objectCounts, error) {
	f, err := os.Open(bz2Path)
	if err != nil {
		return objectCounts{}, err
	}
	defer f.Close()

	r := bzip2.NewReader(f)
	buf := make([]byte, 128*1024)
	var counts objectCounts
	var leftover string

	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			chunk := leftover + string(buf[:n])
			counts.nodes += int64(strings.Count(chunk, "<node "))
			counts.ways += int64(strings.Count(chunk, "<way "))
			counts.relations += int64(strings.Count(chunk, "<relation "))
			// Keep tail in case a tag spans a chunk boundary.
			if len(chunk) > 20 {
				leftover = chunk[len(chunk)-20:]
			} else {
				leftover = chunk
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return objectCounts{}, readErr
		}
	}
	return counts, nil
}

// cleanCacheAndMerged removes the cache/ directory and merged.osm.bz2 while
// leaving pbf/ intact.
func cleanCacheAndMerged(t *testing.T, dataDir string) {
	t.Helper()
	cacheDir := filepath.Join(dataDir, "cache")
	mergedBZ2 := filepath.Join(dataDir, "merged.osm.bz2")
	if err := os.RemoveAll(cacheDir); err != nil {
		t.Fatalf("removing cache dir: %v", err)
	}
	if err := os.Remove(mergedBZ2); err != nil && !os.IsNotExist(err) {
		t.Fatalf("removing merged.osm.bz2: %v", err)
	}
	t.Log("Cleaned cache and merged output (PBFs preserved).")
}

// writeTimingRecord saves a timing result to a file in dataDir for later reading.
func writeTimingRecord(t *testing.T, dataDir, name string, d time.Duration, nRegions int, incremental bool) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Logf("warning: could not create dataDir: %v", err)
		return
	}
	path := filepath.Join(dataDir, "timing_"+name+".txt")
	content := fmt.Sprintf("%d\n%d\n%v\n", d.Nanoseconds(), nRegions, incremental)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Logf("warning: could not write timing record: %v", err)
	}
}

// readTimingRecord reads a timing result saved by writeTimingRecord.
func readTimingRecord(dataDir, name string) (time.Duration, error) {
	path := filepath.Join(dataDir, "timing_"+name+".txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 1 {
		return 0, fmt.Errorf("empty timing file")
	}
	var ns int64
	_, err = fmt.Sscanf(lines[0], "%d", &ns)
	if err != nil {
		return 0, fmt.Errorf("parsing nanoseconds: %w", err)
	}
	return time.Duration(ns), nil
}

// writeCorrectnessRecord saves a correctness check result.
func writeCorrectnessRecord(t *testing.T, dataDir string, incr, full objectCounts, match bool) {
	t.Helper()
	path := filepath.Join(dataDir, "correctness_result.txt")
	result := "PASS"
	if !match {
		result = "FAIL"
	}
	content := fmt.Sprintf("result=%s\nincremental: nodes=%d ways=%d relations=%d total=%d\nfull: nodes=%d ways=%d relations=%d total=%d\n",
		result,
		incr.nodes, incr.ways, incr.relations, incr.total(),
		full.nodes, full.ways, full.relations, full.total())
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Logf("warning: could not write correctness record: %v", err)
	}
}

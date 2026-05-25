package main

// overpass_e2e_run_test.go: wires up the e2e validation for the available
// regions (Delaware as the 1-region base, Pennsylvania as the incremental
// addition).  These tests are gated by RUN_E2E_LOCAL=1 so they never run in CI.

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/osmconv"
)

var (
	localRegions1 = []string{"north-america/us/delaware"}
	localRegions2 = []string{"north-america/us/delaware", "north-america/us/pennsylvania"}
)

// localDataDir is the data directory for local e2e validation.
var localDataDir = ".tmp/e2e-validation"

// e2eLocalEnabled is set via -e2e-local flag to enable local validation tests.
var e2eLocalEnabled bool

func init() {
	flag.BoolVar(&e2eLocalEnabled, "e2e-local", false, "enable local e2e validation tests (needs PBFs in .tmp/e2e-validation/pbf/)")
}

// TestE2E_Local_1RegionColdCache times a 1-region cold-cache conversion
// (Delaware only).
func TestE2E_Local_1RegionColdCache(t *testing.T) {
	if !e2eLocalEnabled {
		t.Skip("pass -e2e-local to run local e2e validation (needs PBFs in .tmp/e2e-validation/pbf/)")
	}
	requirePBFs(t, localDataDir, localRegions1)
	cleanCacheAndMerged(t, localDataDir)

	start := time.Now()
	if err := convertRegionsIncremental(localRegions1, localDataDir, osmconv.NoopConvertProgress{}, os.Stderr); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("1-REGION (Delaware) COLD-CACHE TIME: %s", elapsed)
	writeTimingRecord(t, localDataDir, "1_region_cold_cache", elapsed, 1, false)
}

// TestE2E_Local_2RegionColdCache times a 2-region cold-cache conversion.
// This is the baseline for speedup comparison.
func TestE2E_Local_2RegionColdCache(t *testing.T) {
	if !e2eLocalEnabled {
		t.Skip("pass -e2e-local to run local e2e validation")
	}
	requirePBFs(t, localDataDir, localRegions2)
	cleanCacheAndMerged(t, localDataDir)

	start := time.Now()
	if err := convertRegionsIncremental(localRegions2, localDataDir, osmconv.NoopConvertProgress{}, os.Stderr); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("2-REGION COLD-CACHE TIME: %s", elapsed)
	writeTimingRecord(t, localDataDir, "2_region_cold_cache", elapsed, 2, false)
}

// TestE2E_Local_IncrementalAddition times adding Pennsylvania (2nd region)
// with the Delaware cache warm.  Must run after TestE2E_Local_1RegionColdCache.
func TestE2E_Local_IncrementalAddition(t *testing.T) {
	if !e2eLocalEnabled {
		t.Skip("pass -e2e-local to run local e2e validation")
	}
	requirePBFs(t, localDataDir, localRegions2)

	// Verify Delaware cache is warm.
	cacheDir := filepath.Join(localDataDir, "cache")
	for _, region := range localRegions1 {
		cachePath := filepath.Join(cacheDir, cacheFilename(region))
		metaPath := cachePath + ".meta.json"
		pbfPath := filepath.Join(localDataDir, "pbf", pbfFilename(region))
		if !isCacheValid(pbfPath, cachePath, metaPath) {
			t.Fatalf("cache for %s is not warm; run TestE2E_Local_1RegionColdCache first", region)
		}
	}

	// Delete only merged.osm.bz2 to simulate adding a new region.
	mergedBZ2 := filepath.Join(localDataDir, "merged.osm.bz2")
	if err := os.Remove(mergedBZ2); err != nil && !os.IsNotExist(err) {
		t.Fatalf("removing merged.osm.bz2: %v", err)
	}

	start := time.Now()
	if err := convertRegionsIncremental(localRegions2, localDataDir, osmconv.NoopConvertProgress{}, os.Stderr); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("INCREMENTAL 2ND-REGION (Pennsylvania) TIME: %s", elapsed)
	writeTimingRecord(t, localDataDir, "incremental_2nd_region", elapsed, 2, true)

	// Save incremental merged output for correctness verification.
	incrementalPath := filepath.Join(localDataDir, "merged_incremental.osm.bz2")
	if err := os.Rename(mergedBZ2, incrementalPath); err != nil {
		t.Fatalf("saving incremental output: %v", err)
	}
	t.Logf("Incremental merged output saved to %s", incrementalPath)
}

// TestE2E_Local_CorrectnessVerification compares incremental and full outputs.
// Must run after TestE2E_Local_IncrementalAddition.
func TestE2E_Local_CorrectnessVerification(t *testing.T) {
	if !e2eLocalEnabled {
		t.Skip("pass -e2e-local to run local e2e validation")
	}

	incrementalPath := filepath.Join(localDataDir, "merged_incremental.osm.bz2")
	if _, err := os.Stat(incrementalPath); err != nil {
		t.Fatalf("incremental output not found; run TestE2E_Local_IncrementalAddition first")
	}

	// Do a fresh full 2-region conversion.
	cleanCacheAndMerged(t, localDataDir)
	t.Log("Running fresh full 2-region conversion for correctness comparison...")
	if err := convertRegionsIncremental(localRegions2, localDataDir, osmconv.NoopConvertProgress{}, os.Stderr); err != nil {
		t.Fatalf("convertRegionsIncremental: %v", err)
	}
	fullPath := filepath.Join(localDataDir, "merged.osm.bz2")

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
		t.Errorf("  nodes:     incr=%d full=%d diff=%d", incrCounts.nodes, fullCounts.nodes, incrCounts.nodes-fullCounts.nodes)
		t.Errorf("  ways:      incr=%d full=%d diff=%d", incrCounts.ways, fullCounts.ways, incrCounts.ways-fullCounts.ways)
		t.Errorf("  relations: incr=%d full=%d diff=%d", incrCounts.relations, fullCounts.relations, incrCounts.relations-fullCounts.relations)
	}
	writeCorrectnessRecord(t, localDataDir, incrCounts, fullCounts, match)
}

// TestE2E_Local_SpeedupSummary reads recorded timings and prints the speedup.
func TestE2E_Local_SpeedupSummary(t *testing.T) {
	if !e2eLocalEnabled {
		t.Skip("pass -e2e-local to run local e2e validation")
	}

	full2, err := readTimingRecord(localDataDir, "2_region_cold_cache")
	if err != nil {
		t.Fatalf("reading 2-region timing: %v", err)
	}
	incr, err := readTimingRecord(localDataDir, "incremental_2nd_region")
	if err != nil {
		t.Fatalf("reading incremental timing: %v", err)
	}

	speedup := 1.0 - (float64(incr) / float64(full2))
	t.Logf("2-region cold-cache time:     %s", time.Duration(full2))
	t.Logf("Incremental 2nd-region time:  %s", time.Duration(incr))
	t.Logf("Speedup (1 - incr/full2):     %.1f%%", speedup*100)
	if speedup >= 0.50 {
		t.Logf("TARGET MET: speedup %.1f%% >= 50%%", speedup*100)
	} else {
		t.Logf("TARGET NOT MET: speedup %.1f%% < 50%%", speedup*100)
	}
}

// requirePBFs fails the test if any required PBF file is missing.
func requirePBFs(t *testing.T, dataDir string, regions []string) {
	t.Helper()
	pbfDir := filepath.Join(dataDir, "pbf")
	for _, region := range regions {
		p := filepath.Join(pbfDir, pbfFilename(region))
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("required PBF not found: %s\nDownload it from https://download.geofabrik.de/%s-latest.osm.pbf", p, region)
		}
	}
}


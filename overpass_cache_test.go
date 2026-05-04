package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/osmconv"
)

// ---------------------------------------------------------------------------
// Tests for cacheFilename
// ---------------------------------------------------------------------------

func TestCacheFilename_SimpleRegion(t *testing.T) {
	got := cacheFilename("north-america/us/pennsylvania")
	want := "north-america_us_pennsylvania-latest.osm.gz"
	if got != want {
		t.Errorf("cacheFilename = %q, want %q", got, want)
	}
}

func TestCacheFilename_SingleSegment(t *testing.T) {
	got := cacheFilename("europe")
	want := "europe-latest.osm.gz"
	if got != want {
		t.Errorf("cacheFilename = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Tests for isCacheValid
// ---------------------------------------------------------------------------

func TestIsCacheValid_MissingCacheFile(t *testing.T) {
	dir := t.TempDir()
	pbfPath := filepath.Join(dir, "region.osm.pbf")
	if err := os.WriteFile(pbfPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(dir, "region.osm.gz") // does not exist
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")

	if isCacheValid(pbfPath, cachePath, metaPath) {
		t.Error("expected false when cache file missing")
	}
}

func TestIsCacheValid_MissingMetaFile(t *testing.T) {
	dir := t.TempDir()
	pbfPath := filepath.Join(dir, "region.osm.pbf")
	if err := os.WriteFile(pbfPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(dir, "region.osm.gz")
	if err := os.WriteFile(cachePath, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json") // does not exist

	if isCacheValid(pbfPath, cachePath, metaPath) {
		t.Error("expected false when meta file missing")
	}
}

func TestIsCacheValid_CorruptMetaFile(t *testing.T) {
	dir := t.TempDir()
	pbfPath := filepath.Join(dir, "region.osm.pbf")
	if err := os.WriteFile(pbfPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(dir, "region.osm.gz")
	if err := os.WriteFile(cachePath, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")
	if err := os.WriteFile(metaPath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if isCacheValid(pbfPath, cachePath, metaPath) {
		t.Error("expected false when meta file is not valid JSON")
	}
}

func TestIsCacheValid_SizeMismatch(t *testing.T) {
	dir := t.TempDir()
	pbfData := []byte("pbf content")
	pbfPath := filepath.Join(dir, "region.osm.pbf")
	if err := os.WriteFile(pbfPath, pbfData, 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(pbfPath)

	cachePath := filepath.Join(dir, "region.osm.gz")
	if err := os.WriteFile(cachePath, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := cacheMeta{
		PBFSize:  info.Size() + 1, // wrong size
		PBFMtime: info.ModTime().UTC().Truncate(time.Second),
	}
	metaBytes, _ := json.Marshal(meta)
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")
	if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	if isCacheValid(pbfPath, cachePath, metaPath) {
		t.Error("expected false when PBF size mismatches metadata")
	}
}

func TestIsCacheValid_MtimeMismatch(t *testing.T) {
	dir := t.TempDir()
	pbfData := []byte("pbf content")
	pbfPath := filepath.Join(dir, "region.osm.pbf")
	if err := os.WriteFile(pbfPath, pbfData, 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(pbfPath)

	cachePath := filepath.Join(dir, "region.osm.gz")
	if err := os.WriteFile(cachePath, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := cacheMeta{
		PBFSize:  info.Size(),
		PBFMtime: info.ModTime().UTC().Truncate(time.Second).Add(-1 * time.Hour), // wrong mtime
	}
	metaBytes, _ := json.Marshal(meta)
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")
	if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	if isCacheValid(pbfPath, cachePath, metaPath) {
		t.Error("expected false when PBF mtime mismatches metadata")
	}
}

func TestIsCacheValid_HappyPath(t *testing.T) {
	dir := t.TempDir()
	pbfData := []byte("pbf content")
	pbfPath := filepath.Join(dir, "region.osm.pbf")
	if err := os.WriteFile(pbfPath, pbfData, 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(pbfPath)

	cachePath := filepath.Join(dir, "region.osm.gz")
	if err := os.WriteFile(cachePath, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := cacheMeta{
		PBFSize:  info.Size(),
		PBFMtime: info.ModTime().UTC().Truncate(time.Second),
	}
	metaBytes, _ := json.Marshal(meta)
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")
	if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	if !isCacheValid(pbfPath, cachePath, metaPath) {
		t.Error("expected true for valid cache")
	}
}

// ---------------------------------------------------------------------------
// Tests for convertSingleRegionToCache
// ---------------------------------------------------------------------------

func TestConvertSingleRegionToCache_ProducesReadableGzipXML(t *testing.T) {
	dir := t.TempDir()
	pbfData := osmconv.BuildMinimalPBFBytes()
	pbfPath := filepath.Join(dir, "test.osm.pbf")
	if err := os.WriteFile(pbfPath, pbfData, 0o644); err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(dir, "region.osm.gz")
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")

	if err := convertSingleRegionToCache(pbfPath, cachePath, metaPath, osmconv.NoopConvertProgress{}); err != nil {
		t.Fatalf("convertSingleRegionToCache: %v", err)
	}

	// Verify cache file exists.
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not created: %v", err)
	}

	// Collect all objects from the gzip XML cache.
	cacheScanner, err := osmconv.NewGzipXMLScanner(cachePath)
	if err != nil {
		t.Fatalf("NewGzipXMLScanner: %v", err)
	}
	defer cacheScanner.Close()

	var cacheObjects []osmconv.Object
	for cacheScanner.Next() {
		cacheObjects = append(cacheObjects, cacheScanner.Object())
	}
	if err := cacheScanner.Err(); err != nil {
		t.Fatalf("scanner error reading cache: %v", err)
	}

	// Collect all objects directly from the PBF for comparison.
	pbfScanner := osmconv.NewPBFScanner(bytes.NewReader(pbfData))
	var pbfObjects []osmconv.Object
	for pbfScanner.Next() {
		pbfObjects = append(pbfObjects, pbfScanner.Object())
	}
	if err := pbfScanner.Err(); err != nil {
		t.Fatalf("pbf scanner error: %v", err)
	}

	if len(cacheObjects) != len(pbfObjects) {
		t.Fatalf("cache has %d objects, PBF has %d objects; expected equal", len(cacheObjects), len(pbfObjects))
	}
	for i := range pbfObjects {
		got := cacheObjects[i]
		want := pbfObjects[i]
		if got.Type != want.Type || got.ID != want.ID {
			t.Errorf("object[%d]: got type=%d id=%d, want type=%d id=%d", i, got.Type, got.ID, want.Type, want.ID)
			continue
		}
		switch want.Type {
		case osmconv.NodeType:
			if got.Node == nil || want.Node == nil {
				t.Errorf("object[%d] (node %d): nil Node", i, want.ID)
				continue
			}
			// XMLWriter formats coordinates at %.7f precision, so compare
			// with a tolerance matching that precision (1e-7 degrees).
			const coordTolerance = 1e-7
			if math.Abs(got.Node.Lat-want.Node.Lat) > coordTolerance || math.Abs(got.Node.Lon-want.Node.Lon) > coordTolerance {
				t.Errorf("object[%d] (node %d): got lat=%v lon=%v, want lat=%v lon=%v",
					i, want.ID, got.Node.Lat, got.Node.Lon, want.Node.Lat, want.Node.Lon)
			}
			for k, wv := range want.Node.Tags {
				if gv := got.Node.Tags[k]; gv != wv {
					t.Errorf("object[%d] (node %d): tag %q got %q, want %q", i, want.ID, k, gv, wv)
				}
			}
		case osmconv.WayType:
			if got.Way == nil || want.Way == nil {
				t.Errorf("object[%d] (way %d): nil Way", i, want.ID)
				continue
			}
			if len(got.Way.NodeIDs) != len(want.Way.NodeIDs) {
				t.Errorf("object[%d] (way %d): got %d node refs, want %d", i, want.ID, len(got.Way.NodeIDs), len(want.Way.NodeIDs))
			} else {
				for j, wid := range want.Way.NodeIDs {
					if got.Way.NodeIDs[j] != wid {
						t.Errorf("object[%d] (way %d): node ref[%d] got %d, want %d", i, want.ID, j, got.Way.NodeIDs[j], wid)
					}
				}
			}
		case osmconv.RelationType:
			if got.Rel == nil || want.Rel == nil {
				t.Errorf("object[%d] (relation %d): nil Rel", i, want.ID)
			}
		}
	}
}

func TestConvertSingleRegionToCache_WritesCorrectSidecarMetadata(t *testing.T) {
	dir := t.TempDir()
	pbfData := osmconv.BuildMinimalPBFBytes()
	pbfPath := filepath.Join(dir, "test.osm.pbf")
	if err := os.WriteFile(pbfPath, pbfData, 0o644); err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(dir, "region.osm.gz")
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")

	if err := convertSingleRegionToCache(pbfPath, cachePath, metaPath, nil); err != nil {
		t.Fatalf("convertSingleRegionToCache: %v", err)
	}

	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading meta file: %v", err)
	}
	var meta cacheMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("unmarshaling meta: %v", err)
	}

	pbfInfo, _ := os.Stat(pbfPath)
	if meta.PBFSize != pbfInfo.Size() {
		t.Errorf("meta.PBFSize = %d, want %d", meta.PBFSize, pbfInfo.Size())
	}
	wantMtime := pbfInfo.ModTime().UTC().Truncate(time.Second)
	if !meta.PBFMtime.Truncate(time.Second).Equal(wantMtime) {
		t.Errorf("meta.PBFMtime = %v, want %v", meta.PBFMtime, wantMtime)
	}
}

func TestConvertSingleRegionToCache_MetadataRoundTripsIsCacheValid(t *testing.T) {
	dir := t.TempDir()
	pbfData := osmconv.BuildMinimalPBFBytes()
	pbfPath := filepath.Join(dir, "test.osm.pbf")
	if err := os.WriteFile(pbfPath, pbfData, 0o644); err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(dir, "region.osm.gz")
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")

	if err := convertSingleRegionToCache(pbfPath, cachePath, metaPath, nil); err != nil {
		t.Fatalf("convertSingleRegionToCache: %v", err)
	}

	if !isCacheValid(pbfPath, cachePath, metaPath) {
		t.Error("isCacheValid returned false immediately after convertSingleRegionToCache")
	}
}

func TestConvertSingleRegionToCache_AtomicWrite_NoLeftoverOnError(t *testing.T) {
	dir := t.TempDir()

	// Write a non-PBF file to trigger a conversion error.
	pbfPath := filepath.Join(dir, "bad.osm.pbf")
	if err := os.WriteFile(pbfPath, []byte("this is not a pbf file"), 0o644); err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(dir, "region.osm.gz")
	metaPath := filepath.Join(dir, "region.osm.gz.meta.json")

	err := convertSingleRegionToCache(pbfPath, cachePath, metaPath, nil)
	if err == nil {
		t.Fatal("expected error converting invalid PBF, got nil")
	}

	// The final cache file must not exist.
	if _, statErr := os.Stat(cachePath); statErr == nil {
		t.Error("cache file should not exist after failed conversion")
	}

	// No temp .gz files should linger in the cache dir (the defer cleans them up).
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if len(name) > 3 && name[len(name)-3:] == ".gz" {
			t.Errorf("unexpected .gz file left behind: %s", name)
		}
	}
}

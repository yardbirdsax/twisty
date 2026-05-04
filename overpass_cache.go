package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yardbirdsax/twisty/osmconv"
)

// cacheMeta holds the PBF file metadata stored alongside a cache file.
type cacheMeta struct {
	PBFSize  int64     `json:"pbf_size"`
	PBFMtime time.Time `json:"pbf_mtime"`
}

// cacheFilename derives the cache filename for a Geofabrik region path.
// Same logic as pbfFilename but with .osm.gz extension.
func cacheFilename(region string) string {
	return strings.ReplaceAll(region, "/", "_") + "-latest.osm.gz"
}

// isCacheValid returns true when the gzip XML cache at cachePath is valid
// relative to the PBF at pbfPath.  It checks:
//  1. cachePath exists.
//  2. metaPath exists and is parseable as JSON.
//  3. The PBF's current size and mtime match the stored metadata.
func isCacheValid(pbfPath, cachePath, metaPath string) bool {
	// 1. Cache file must exist.
	if _, err := os.Stat(cachePath); err != nil {
		return false
	}

	// 2. Metadata file must exist and be parseable.
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return false
	}
	var meta cacheMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return false
	}

	// 3. PBF stat must match metadata.
	info, err := os.Stat(pbfPath)
	if err != nil {
		return false
	}
	if info.Size() != meta.PBFSize {
		return false
	}
	// Truncate to second precision to avoid filesystem-dependent sub-second differences.
	if !info.ModTime().Truncate(time.Second).Equal(meta.PBFMtime.Truncate(time.Second)) {
		return false
	}

	return true
}

// convertSingleRegionToCache converts the PBF file at pbfPath to a
// gzip-compressed OSM XML cache at cachePath, writing sidecar metadata to
// metaPath.  The cache file is written atomically via a temp file + rename so
// that a crash mid-conversion never leaves a corrupt file behind.
func convertSingleRegionToCache(pbfPath, cachePath, metaPath string, progress osmconv.ConvertProgress) error {
	// Gather PBF size for progress reporting.
	pbfInfo, err := os.Stat(pbfPath)
	if err != nil {
		return fmt.Errorf("stating PBF %s: %w", pbfPath, err)
	}
	if progress == nil {
		progress = osmconv.NoopConvertProgress{}
	}
	progress.SetFiles([]string{pbfPath}, []int64{pbfInfo.Size()})

	// Open PBF and wrap in a counting reader for progress reporting.
	pbfFile, err := os.Open(pbfPath)
	if err != nil {
		return fmt.Errorf("opening PBF %s: %w", pbfPath, err)
	}
	defer pbfFile.Close()

	cr := osmconv.NewCountingReader(pbfFile, 0, progress)
	scanner := osmconv.NewPBFScanner(cr)

	// Create temp file in the same directory as cachePath for atomic write.
	cacheDir := filepath.Dir(cachePath)
	tmp, err := os.CreateTemp(cacheDir, ".cache-*.osm.gz")
	if err != nil {
		return fmt.Errorf("creating temp cache file: %w", err)
	}
	tmpName := tmp.Name()
	// Clean up the temp file if we don't rename it.
	success := false
	defer func() {
		tmp.Close()
		if !success {
			os.Remove(tmpName)
		}
	}()

	gz := gzip.NewWriter(tmp)
	xmlw := osmconv.NewXMLWriter(gz)

	if err := osmconv.Convert(osmconv.ConvertOptions{
		Scanners: []osmconv.ObjectScanner{scanner},
		Writer:   xmlw,
		Progress: progress,
	}); err != nil {
		gz.Close()
		return fmt.Errorf("converting PBF to gzip XML: %w", err)
	}

	if err := gz.Close(); err != nil {
		return fmt.Errorf("closing gzip writer: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Re-stat PBF to capture the authoritative mtime after conversion.
	pbfInfo, err = os.Stat(pbfPath)
	if err != nil {
		return fmt.Errorf("re-stating PBF %s: %w", pbfPath, err)
	}

	// Atomic rename temp -> final cache path.
	if err := os.Rename(tmpName, cachePath); err != nil {
		return fmt.Errorf("renaming temp cache file: %w", err)
	}
	success = true

	// Write sidecar metadata after the rename so a failed metadata write leaves
	// the cache file present but without a sidecar; isCacheValid will treat the
	// cache as invalid and trigger a clean re-conversion.
	meta := cacheMeta{
		PBFSize:  pbfInfo.Size(),
		PBFMtime: pbfInfo.ModTime().UTC().Truncate(time.Second),
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshaling cache metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
		return fmt.Errorf("writing cache metadata %s: %w", metaPath, err)
	}

	return nil
}

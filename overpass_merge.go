package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yardbirdsax/twisty/osmconv"
)

// convertWorkers is how many stale regions convert at the same time.
var convertWorkers = 3

// convertRegionToCache converts one region; a variable so tests can observe
// how the pool schedules regions.
var convertRegionToCache = convertSingleRegionToCache

// regionConvertProgress is a ConvertProgress that can hand out an independent
// handle per region, so regions converting at once do not clobber each other.
type regionConvertProgress interface {
	osmconv.ConvertProgress
	StartRegion(name string, size int64) osmconv.ConvertProgress
}

// regionProgressFor returns the progress a single region's conversion reports
// to. Plain progress reporters are not safe to share between concurrent
// regions, so they get no per-region reporting.
func regionProgressFor(progress osmconv.ConvertProgress, region, pbfPath string) osmconv.ConvertProgress {
	rp, ok := progress.(regionConvertProgress)
	if !ok {
		return osmconv.NoopConvertProgress{}
	}
	var size int64
	if info, err := os.Stat(pbfPath); err == nil {
		size = info.Size()
	}
	return rp.StartRegion(region, size)
}

// convertRegionsIncremental checks per-region cache validity, converts only
// stale regions, and merges all cached regions through the parallel BZ2 writer
// into the final merged.osm.bz2.
func convertRegionsIncremental(allRegions []string, dataDir, pbfDir string, progress osmconv.ConvertProgress, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	cacheDir := filepath.Join(dataDir, "cache")

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	// Convert stale regions through a bounded pool: each region's PBF decode
	// buffers are large, so the pool size caps memory as well as CPU.
	total := len(allRegions)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		sem      = make(chan struct{}, max(convertWorkers, 1))
	)
	failed := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return firstErr != nil
	}
	for i, region := range allRegions {
		pbfPath := filepath.Join(pbfDir, pbfFilename(region))
		cachePath := filepath.Join(cacheDir, cacheFilename(region))
		metaPath := cachePath + ".meta.json"

		if isCacheValid(pbfPath, cachePath, metaPath) {
			fmt.Fprintf(stderr, "Region %d/%d: %s (cached)\n", i+1, total, region)
			continue
		}
		sem <- struct{}{}
		if failed() {
			<-sem
			break
		}
		fmt.Fprintf(stderr, "Region %d/%d: %s (converting...)\n", i+1, total, region)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rp := regionProgressFor(progress, region, pbfPath)
			// Convert reports Done only on success; a failed region must
			// still leave the display.
			defer rp.Done()
			if err := convertRegionToCache(pbfPath, cachePath, metaPath, rp); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("converting region %s: %w", region, err)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}

	// Clean up orphan cache files.
	if err := cleanOrphanCacheFiles(cacheDir, allRegions, stderr); err != nil {
		return fmt.Errorf("cleaning orphan cache files: %w", err)
	}

	// Build list of cache paths in region order.
	cachePaths := make([]string, len(allRegions))
	for i, region := range allRegions {
		cachePaths[i] = filepath.Join(cacheDir, cacheFilename(region))
	}

	mergedBZ2 := filepath.Join(dataDir, "merged.osm.bz2")
	fmt.Fprintf(stderr, "Merging %d region cache(s) into %s...\n", len(cachePaths), mergedBZ2)
	return mergeRegionCachesToBZ2(cachePaths, mergedBZ2, progress)
}

// cleanOrphanCacheFiles deletes .osm.gz files (and their .meta.json sidecars)
// in cacheDir that are not in the activeRegions set.
func cleanOrphanCacheFiles(cacheDir string, activeRegions []string, stderr io.Writer) error {
	expected := make(map[string]bool, len(activeRegions))
	for _, region := range activeRegions {
		expected[cacheFilename(region)] = true
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading cache dir: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".osm.gz") {
			continue
		}
		if expected[name] {
			continue
		}
		// Orphan: delete the cache file and its sidecar.
		cachePath := filepath.Join(cacheDir, name)
		metaPath := cachePath + ".meta.json"
		if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing orphan cache %s: %w", cachePath, err)
		}
		fmt.Fprintf(stderr, "Removed orphan cache file: %s\n", cachePath)
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing orphan meta %s: %w", metaPath, err)
		}
	}

	return nil
}

// mergeRegionCachesToBZ2 reads each gzip XML cache and merges them into a
// single bzip2-compressed OSM XML file using the parallel BZ2 writer.
func mergeRegionCachesToBZ2(cachePaths []string, bz2Path string, progress osmconv.ConvertProgress) error {
	if progress == nil {
		progress = osmconv.NoopConvertProgress{}
	}

	// Gather names and sizes for progress reporting.
	names := make([]string, len(cachePaths))
	sizes := make([]int64, len(cachePaths))
	for i, p := range cachePaths {
		names[i] = p
		if info, err := os.Stat(p); err == nil {
			sizes[i] = info.Size()
		}
	}
	progress.SetFiles(names, sizes)

	scanners := make([]osmconv.ObjectScanner, len(cachePaths))
	closers := make([]io.Closer, len(cachePaths))
	for i, p := range cachePaths {
		s, err := osmconv.NewGzipXMLScanner(p)
		if err != nil {
			for j := 0; j < i; j++ {
				closers[j].Close()
			}
			return fmt.Errorf("opening cache %s: %w", p, err)
		}
		scanners[i] = s
		closers[i] = s
	}
	defer func() {
		for _, c := range closers {
			if c != nil {
				c.Close()
			}
		}
	}()

	outFile, err := os.Create(bz2Path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", bz2Path, err)
	}
	defer outFile.Close()

	bz2w, err := osmconv.NewParallelBZ2Writer(outFile, 0)
	if err != nil {
		return fmt.Errorf("creating parallel bzip2 writer: %w", err)
	}

	xmlw := osmconv.NewXMLWriter(bz2w)

	if err := osmconv.Convert(osmconv.ConvertOptions{
		Scanners: scanners,
		Writer:   xmlw,
		Progress: progress,
	}); err != nil {
		bz2w.Close()
		return err
	}

	if err := bz2w.Close(); err != nil {
		return fmt.Errorf("closing bzip2 writer: %w", err)
	}
	return outFile.Close()
}

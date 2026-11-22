package main

import (
	"context"
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

// geofabrikPBFURL returns the download URL of a region's latest extract. Geofabrik
// keeps each region in a directory of its own name, with the file named for the
// last path segment (north-america/us/vermont/vermont-latest.osm.pbf). The
// older flat layout (north-america/us/vermont-latest.osm.pbf) now redirects to a
// path that 404s.
func geofabrikPBFURL(baseURL, region string) string {
	last := region
	if i := strings.LastIndex(region, "/"); i >= 0 {
		last = region[i+1:]
	}
	return baseURL + "/" + region + "/" + last + "-latest.osm.pbf"
}

// downloadPBF downloads the file at url to destPath atomically via a temp file,
// reporting progress via the provided FileDownloadProgress, and records the
// upstream stamp beside it.
func downloadPBF(url, destPath string, fp FileDownloadProgress) error {
	h, err := fetchToFile(context.Background(), http.DefaultClient, url, destPath, fp)
	if err != nil {
		return err
	}
	if err := recordUpstreamStampForDownload(destPath, h); err != nil {
		return fmt.Errorf("recording upstream stamp: %w", err)
	}
	fp.FileComplete()
	return nil
}

// fetchToFile GETs url into destPath atomically via a temp file in the same
// directory (nothing is left behind on error) and returns the response headers.
func fetchToFile(ctx context.Context, client *http.Client, url, destPath string, fp FileDownloadProgress) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".download-*.osm.pbf")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
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
				return nil, fmt.Errorf("writing temp file: %w", writeErr)
			}
			fp.BytesDownloaded(int64(n))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading response: %w", readErr)
		}
	}

	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return nil, fmt.Errorf("renaming temp file: %w", err)
	}
	return resp.Header, nil
}

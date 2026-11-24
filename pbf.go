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

// fetchToFile GETs url into destPath and returns the response headers. The
// bytes accumulate in a partial file beside destPath, which only a complete
// download is renamed over, so destPath is never seen half written. A failure
// keeps the partial, and the next call for the same destPath continues from it
// if upstream still has the version it came from (see pbf_resume.go); a server
// that no longer has it, or that ignores range requests, gets a full download.
func fetchToFile(ctx context.Context, client *http.Client, url, destPath string, fp FileDownloadProgress) (http.Header, error) {
	removeOrphanedPartials(filepath.Dir(destPath), destPath)
	offset, held := resumePoint(destPath)
	for {
		resp, err := getFrom(ctx, client, url, offset, held)
		if err != nil {
			return nil, err
		}
		h, retry, err := receive(resp, destPath, offset, held, fp)
		resp.Body.Close()
		if retry {
			// The partial cannot be continued: start again from nothing.
			discardPartial(destPath)
			offset, held = 0, version{}
			continue
		}
		return h, err
	}
}

// getFrom requests url, from byte offset on when a partial of version held is
// being continued.
func getFrom(ctx context.Context, client *http.Client, url string, offset int64, held version) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		req.Header.Set("If-Range", held.ifRange())
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	return resp, nil
}

// receive writes resp's body to destPath's partial and, when it is complete,
// renames it into place. offset is how many bytes of version held were already
// on disk. retry is true when the response shows the partial cannot be
// continued and the download should start over.
func receive(resp *http.Response, destPath string, offset int64, held version, fp FileDownloadProgress) (h http.Header, retry bool, err error) {
	var total int64 // size of the whole file when the server says, else -1
	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
		first, t, ok := contentRange(resp.Header.Get("Content-Range"))
		if !ok || first != offset || !held.matches(resp.Header) {
			// Not the bytes we asked for, or from another version: the server
			// did not honour If-Range, so splicing would corrupt the file.
			log.Printf("resume of %s: server sent a different version than the partial; starting over", filepath.Base(destPath))
			return nil, true, nil
		}
		total = t
		h = resp.Header.Clone()
		if h.Get("Last-Modified") == "" && held.lastModified != "" {
			h.Set("Last-Modified", held.lastModified)
		}
	case resp.StatusCode == http.StatusOK:
		// A whole file: the first attempt, a server that ignores Range, or one
		// that no longer has the version we held. Whatever is on disk is stale.
		if offset > 0 {
			log.Printf("resume of %s: upstream changed or does not support ranges; starting over", filepath.Base(destPath))
		}
		offset, total, h = 0, resp.ContentLength, resp.Header
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0:
		log.Printf("resume of %s: partial does not fit the file upstream; starting over", filepath.Base(destPath))
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("GET %s: unexpected status %s", resp.Request.URL, resp.Status)
	}

	flags := os.O_WRONLY | os.O_CREATE
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(partialPath(destPath), flags, 0o644)
	if err != nil {
		discardPartial(destPath) // never leave a record describing bytes we do not hold
		return nil, false, fmt.Errorf("opening partial file: %w", err)
	}
	if offset == 0 {
		// Only once the partial is empty is it safe to say which version its
		// bytes will be from.
		if err := writeVersion(destPath, versionOf(resp.Header)); err != nil {
			f.Close()
			discardPartial(destPath)
			return nil, false, err
		}
	}
	// Reported only now that the response is known to continue the partial, so
	// bytes of a discarded partial never reach the progress bar.
	if offset > 0 {
		fp.BytesDownloaded(offset)
	}

	size, err := copyCounting(f, resp.Body, offset, fp)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("closing partial file: %w", cerr)
	}
	if err != nil {
		return nil, false, err // the partial stays for the next attempt
	}
	if total >= 0 && size != total {
		if size > total {
			discardPartial(destPath)
		}
		return nil, false, errShortFile(size, total)
	}
	if err := os.Rename(partialPath(destPath), destPath); err != nil {
		return nil, false, fmt.Errorf("renaming partial file: %w", err)
	}
	os.Remove(partialMetaPath(destPath)) //nolint:errcheck // a stray meta is cleaned up later
	return h, false, nil
}

// writeVersion records the upstream version a fresh partial is being fetched
// from, or clears the record when the response names none (such a partial can
// never be continued).
func writeVersion(destPath string, v version) error {
	if v.ifRange() == "" {
		os.Remove(partialMetaPath(destPath)) //nolint:errcheck // absent is fine
		return nil
	}
	if err := os.WriteFile(partialMetaPath(destPath), v.encode(), 0o644); err != nil {
		return fmt.Errorf("recording download version: %w", err)
	}
	return nil
}

// copyCounting copies src to dst, reporting each chunk to fp, and returns the
// file's size: start plus the bytes copied.
func copyCounting(dst io.Writer, src io.Reader, start int64, fp FileDownloadProgress) (int64, error) {
	size := start
	buf := make([]byte, 32*1024)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return size, fmt.Errorf("writing partial file: %w", err)
			}
			size += int64(n)
			fp.BytesDownloaded(int64(n))
		}
		if readErr == io.EOF {
			return size, nil
		}
		if readErr != nil {
			return size, fmt.Errorf("reading response: %w", readErr)
		}
	}
}

package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// rangeServer serves a file like Geofabrik: Last-Modified and ETag headers and
// byte-range requests (with If-Range). Its knobs break that in the ways real
// servers and networks do.
type rangeServer struct {
	mu      sync.Mutex
	body    []byte
	modTime time.Time
	etag    string

	cutAfter      int  // cut the next un-ranged response after this many body bytes
	ignoreRange   bool // always answer 200 with the whole file
	ignoreIfRange bool // honour Range even when If-Range names another version
	requests      []rangeRequest
}

type rangeRequest struct{ rangeHeader, ifRange string }

func newRangeServer(t *testing.T, body []byte) (*rangeServer, *httptest.Server) {
	t.Helper()
	rs := &rangeServer{body: body, modTime: time.Date(2026, 11, 21, 11, 0, 0, 0, time.UTC), etag: `"v1"`}
	srv := httptest.NewServer(rs)
	t.Cleanup(srv.Close)
	return rs, srv
}

// publish replaces the file with a new upstream version.
func (rs *rangeServer) publish(body []byte, etag string, modTime time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.body, rs.etag, rs.modTime = body, etag, modTime
}

func (rs *rangeServer) sawRequests() []rangeRequest {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]rangeRequest(nil), rs.requests...)
}

func (rs *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rs.mu.Lock()
	body, etag, modTime := rs.body, rs.etag, rs.modTime
	cut := rs.cutAfter
	if r.Header.Get("Range") == "" {
		rs.cutAfter = 0
	} else {
		cut = 0
	}
	rs.requests = append(rs.requests, rangeRequest{r.Header.Get("Range"), r.Header.Get("If-Range")})
	ignoreRange, ignoreIfRange := rs.ignoreRange, rs.ignoreIfRange
	rs.mu.Unlock()

	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	switch {
	case cut > 0:
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body[:cut]) //nolint:errcheck
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	case ignoreRange:
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body) //nolint:errcheck
	default:
		if ignoreIfRange {
			r.Header.Del("If-Range")
		}
		http.ServeContent(w, r, "", modTime, bytes.NewReader(body))
	}
}

// byteCounter is a FileDownloadProgress that totals the bytes reported to it.
type byteCounter struct {
	n        int64
	complete int
}

func (c *byteCounter) BytesDownloaded(n int64) { c.n += n }
func (c *byteCounter) FileComplete()           { c.complete++ }

// patternBytes returns n deterministic, non-repeating-looking bytes seeded by s.
func patternBytes(n int, s byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7+i/251) + s
	}
	return b
}

func fetch(t *testing.T, srv *httptest.Server, dest string, fp FileDownloadProgress) error {
	t.Helper()
	_, err := fetchToFile(context.Background(), srv.Client(), srv.URL+"/x.osm.pbf", dest, fp)
	return err
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFetchToFile_ResumesAnInterruptedDownload(t *testing.T) {
	body := patternBytes(100_000, 1)
	rs, srv := newRangeServer(t, body)
	rs.cutAfter = 40_000
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")

	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}
	fp := &byteCounter{}
	if err := fetch(t, srv, dest, fp); err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	if got := readFile(t, dest); !bytes.Equal(got, body) {
		t.Errorf("resumed file differs from the original (%d vs %d bytes)", len(got), len(body))
	}
	reqs := rs.sawRequests()
	if len(reqs) != 2 || !strings.HasPrefix(reqs[1].rangeHeader, "bytes=") || reqs[1].rangeHeader == "bytes=0-" {
		t.Fatalf("second request = %+v, want a ranged request for the remainder", reqs)
	}
	if fp.n != int64(len(body)) {
		t.Errorf("progress counted %d bytes, want %d (resumed bytes counted once, no overshoot)", fp.n, len(body))
	}
}

func TestFetchToFile_InterruptedDownloadNeverShowsAPartialFile(t *testing.T) {
	old := []byte("the previous complete extract")
	rs, srv := newRangeServer(t, patternBytes(50_000, 2))
	rs.cutAfter = 10_000
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	if err := os.WriteFile(dest, old, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}

	if got := readFile(t, dest); !bytes.Equal(got, old) {
		t.Errorf("destination changed by an interrupted download: %d bytes", len(got))
	}
}

func TestFetchToFile_DiscardsAPartialFromAnotherUpstreamVersion(t *testing.T) {
	rs, srv := newRangeServer(t, patternBytes(100_000, 3))
	rs.cutAfter = 40_000
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}

	// Upstream publishes a new extract before the next attempt.
	newBody := patternBytes(120_000, 9)
	rs.publish(newBody, `"v2"`, time.Date(2026, 11, 22, 11, 0, 0, 0, time.UTC))
	fp := &byteCounter{}
	if err := fetch(t, srv, dest, fp); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, dest); !bytes.Equal(got, newBody) {
		t.Error("file is not the new upstream version in full")
	}
	if fp.n != int64(len(newBody)) {
		t.Errorf("progress counted %d bytes, want %d (stale partial bytes must not count)", fp.n, len(newBody))
	}
}

func TestFetchToFile_DiscardsAStalePartialEvenIfServerIgnoresIfRange(t *testing.T) {
	rs, srv := newRangeServer(t, patternBytes(100_000, 3))
	rs.cutAfter = 40_000
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}

	newBody := patternBytes(100_000, 9)
	rs.publish(newBody, `"v2"`, time.Date(2026, 11, 22, 11, 0, 0, 0, time.UTC))
	rs.ignoreIfRange = true // answers 206 with the new version's tail
	if err := fetch(t, srv, dest, &byteCounter{}); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, dest); !bytes.Equal(got, newBody) {
		t.Error("new version's tail was spliced onto the old version's head")
	}
}

func TestFetchToFile_ServerWithoutRangeSupportGetsAFullDownload(t *testing.T) {
	body := patternBytes(100_000, 4)
	rs, srv := newRangeServer(t, body)
	rs.cutAfter = 40_000
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}

	rs.ignoreRange = true
	fp := &byteCounter{}
	if err := fetch(t, srv, dest, fp); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, dest); !bytes.Equal(got, body) {
		t.Errorf("file corrupted by a server that ignores Range (%d vs %d bytes)", len(got), len(body))
	}
	if fp.n != int64(len(body)) {
		t.Errorf("progress counted %d bytes, want %d", fp.n, len(body))
	}
}

func TestFetchToFile_PartialAlreadyCompleteStillYieldsTheFile(t *testing.T) {
	body := patternBytes(30_000, 5)
	rs, srv := newRangeServer(t, body)
	rs.cutAfter = len(body) - 1
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}
	// The process died after the last byte but before the rename: the partial
	// now holds the whole file, so a ranged request starts past the end.
	if err := os.WriteFile(partialPath(dest), body, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fetch(t, srv, dest, &byteCounter{}); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, dest); !bytes.Equal(got, body) {
		t.Error("file differs from the original")
	}
}

func TestFetchToFile_PartialOfUnknownVersionIsNotContinued(t *testing.T) {
	body := patternBytes(20_000, 6)
	rs, srv := newRangeServer(t, body)
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	// A partial with no record of the version it came from (for example one
	// left by something else) cannot be trusted.
	if err := os.WriteFile(partialPath(dest), []byte("garbage of unknown origin"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fetch(t, srv, dest, &byteCounter{}); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, dest); !bytes.Equal(got, body) {
		t.Error("unverifiable partial leaked into the file")
	}
	if reqs := rs.sawRequests(); len(reqs) != 1 || reqs[0].rangeHeader != "" {
		t.Errorf("requests = %+v, want one un-ranged request", reqs)
	}
}

func TestFetchToFile_RefusesAShortRangeResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", "100")
			w.Write(make([]byte, 10)) //nolint:errcheck
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		// Claims the file is 100 bytes but ends the range at byte 19.
		w.Header().Set("Content-Range", "bytes 10-19/100")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, 10)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}

	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Error("a file shorter than the server says was accepted")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("an incomplete file was renamed into place")
	}
}

func TestFetchToFile_CleansUpOrphanedPartials(t *testing.T) {
	_, srv := newRangeServer(t, patternBytes(10_000, 7))
	dir := t.TempDir()
	dest := filepath.Join(dir, "v.osm.pbf")
	old := time.Now().Add(-30 * 24 * time.Hour)
	write := func(name string, mtime time.Time) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	legacy := write(".download-123456.osm.pbf", time.Now()) // random name of the old scheme
	abandoned := write("dropped.osm.pbf.partial", old)      // a region no longer loaded
	abandonedMeta := write("dropped.osm.pbf.partial.meta", old)
	inFlight := write("other.osm.pbf.partial", time.Now()) // another download under way
	unrelated := write("keep.osm.pbf", old)

	if err := fetch(t, srv, dest, &byteCounter{}); err != nil {
		t.Fatal(err)
	}

	for _, gone := range []string{legacy, abandoned, abandonedMeta} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("orphan %s was not removed", filepath.Base(gone))
		}
	}
	for _, kept := range []string{inFlight, unrelated} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s must not be removed: %v", filepath.Base(kept), err)
		}
	}
	// A finished download leaves no partial or version record of its own.
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), "v.osm.pbf.partial") {
			t.Errorf("leftover %s after a completed download", en.Name())
		}
	}
}

func TestFetchToFile_FailureBeforeAnyBytesLeavesNothing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	dir := t.TempDir()

	if err := fetch(t, srv, filepath.Join(dir, "v.osm.pbf"), &byteCounter{}); err == nil {
		t.Fatal("expected an error for a 404")
	}

	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		names := make([]string, len(entries))
		for i, en := range entries {
			names[i] = en.Name()
		}
		t.Errorf("leftover files: %s", fmt.Sprint(names))
	}
}

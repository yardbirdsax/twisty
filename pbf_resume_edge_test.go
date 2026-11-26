package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fresh response must never leave a version record that describes bytes the
// partial does not hold: if the partial cannot be rewritten, both go.
func TestFetchToFile_UnwritablePartialLeavesNoVersionRecordBehind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	rs, srv := newRangeServer(t, patternBytes(60_000, 1))
	rs.cutAfter = 20_000
	dir := t.TempDir()
	dest := filepath.Join(dir, "v.osm.pbf")
	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}
	// Upstream moves on, and the old partial can no longer be written.
	rs.publish(patternBytes(60_000, 2), `"v2"`, time.Date(2026, 11, 22, 11, 0, 0, 0, time.UTC))
	if err := os.Chmod(partialPath(dest), 0o444); err != nil {
		t.Fatal(err)
	}

	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Log("fetch succeeded despite the read-only partial (it was replaced)")
		return
	}

	meta, metaErr := os.ReadFile(partialMetaPath(dest))
	_, partialErr := os.Stat(partialPath(dest))
	if metaErr == nil && strings.Contains(string(meta), `"v2"`) && partialErr == nil {
		t.Error("meta names the new version while the partial still holds the old version's bytes")
	}
}

// An ETag-only upstream: the returned headers must not carry an empty
// Last-Modified, which would later fail to parse and mark the region changed.
func TestFetchToFile_ResumeWithoutLastModifiedReturnsNoEmptyHeader(t *testing.T) {
	body := patternBytes(50_000, 3)
	cut := 20_000
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"only-etag"`)
		if r.Header.Get("Range") == "" && cut > 0 {
			w.Header().Set("Content-Length", "50000")
			w.Write(body[:cut]) //nolint:errcheck
			w.(http.Flusher).Flush()
			cut = 0
			panic(http.ErrAbortHandler)
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}

	h, err := fetchToFile(t.Context(), srv.Client(), srv.URL+"/x.osm.pbf", dest, &byteCounter{})
	if err != nil {
		t.Fatal(err)
	}

	if vals, ok := h["Last-Modified"]; ok {
		t.Errorf("Last-Modified = %q, want the header absent", vals)
	}
}

// The age of a partial is judged by the partial, not by its version record,
// which is written once at the start and so looks old on a slow download.
func TestFetchToFile_KeepsTheVersionRecordOfALiveDownload(t *testing.T) {
	_, srv := newRangeServer(t, patternBytes(10_000, 4))
	dir := t.TempDir()
	partial := filepath.Join(dir, "slow.osm.pbf.partial")
	meta := partial + ".meta"
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, p := range []string{partial, meta} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(meta, old, old); err != nil { // the partial itself is fresh
		t.Fatal(err)
	}

	if err := fetch(t, srv, filepath.Join(dir, "v.osm.pbf"), &byteCounter{}); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{partial, meta} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed while its download is live: %v", filepath.Base(p), err)
		}
	}
}

// Throwing away a partial is a decision the user should be able to see.
func TestFetchToFile_SaysWhenItStartsOverInsteadOfResuming(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	rs, srv := newRangeServer(t, patternBytes(100_000, 5))
	rs.cutAfter = 40_000
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	if err := fetch(t, srv, dest, &byteCounter{}); err == nil {
		t.Fatal("expected the cut download to fail")
	}
	rs.publish(patternBytes(100_000, 6), `"v2"`, time.Date(2026, 11, 22, 11, 0, 0, 0, time.UTC))

	if err := fetch(t, srv, dest, &byteCounter{}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(logged.String(), "starting over") {
		t.Errorf("no notice that the partial was discarded; log = %q", logged.String())
	}
}

func TestFetchToFile_ResumingQuietlyWhenItWorks(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	rs, srv := newRangeServer(t, patternBytes(100_000, 7))
	rs.cutAfter = 40_000
	dest := filepath.Join(t.TempDir(), "v.osm.pbf")
	_ = fetch(t, srv, dest, &byteCounter{})

	if err := fetch(t, srv, dest, &byteCounter{}); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(logged.String(), "starting over") {
		t.Errorf("unexpected notice on a clean resume: %q", logged.String())
	}
}

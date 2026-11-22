package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDownloadPBF_RecordsUpstreamStamp(t *testing.T) {
	lm := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", lm.Format(http.TimeFormat))
		w.Write([]byte("pbf"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	region := "north-america/us/ohio"
	dest := filepath.Join(dir, pbfFilename(region))
	if err := downloadPBF(srv.URL+"/x.osm.pbf", dest, NoopFileDownloadProgress{}); err != nil {
		t.Fatal(err)
	}
	got, ok := readUpstreamStamp(dir, region)
	if !ok || !got.Equal(lm) {
		t.Fatalf("stamp = %v, %v; want %v", got, ok, lm)
	}
}

func TestDownloadPBF_NoLastModified_WritesNoStamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pbf"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	region := "europe/malta"
	dest := filepath.Join(dir, pbfFilename(region))
	if err := downloadPBF(srv.URL+"/x.osm.pbf", dest, NoopFileDownloadProgress{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := readUpstreamStamp(dir, region); ok {
		t.Fatal("expected no stamp when upstream sent no Last-Modified")
	}
}

func TestDownloadPBF_FailedDownload_LeavesOldStamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	region := "europe/malta"
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeUpstreamStamp(dir, region, old)
	if err := downloadPBF(srv.URL+"/x", filepath.Join(dir, pbfFilename(region)), NoopFileDownloadProgress{}); err == nil {
		t.Fatal("expected error")
	}
	if got, ok := readUpstreamStamp(dir, region); !ok || !got.Equal(old) {
		t.Fatalf("stamp changed: %v %v", got, ok)
	}
}

func writeLoadedRegions(t *testing.T, dir string, regions string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".regions"), []byte(regions), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillUpstreamStamps_SeedsFromMtime_NoOverwrite(t *testing.T) {
	root := DataRoot{
		OverpassDir: filepath.Join(t.TempDir(), "op"),
		ValhallaDir: filepath.Join(t.TempDir(), "va"),
		PBFDir:      t.TempDir(),
	}
	writeLoadedRegions(t, root.OverpassDir, "europe/malta,europe/monaco")
	writeLoadedRegions(t, root.ValhallaDir, "europe/monaco,europe/andorra")

	mtime := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	for _, r := range []string{"europe/malta", "europe/monaco"} {
		p := filepath.Join(root.PBFDir, pbfFilename(r))
		os.WriteFile(p, []byte("x"), 0o644)
		os.Chtimes(p, mtime, mtime)
	}
	// andorra is loaded but its PBF is missing: skipped.
	existing := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	writeUpstreamStamp(root.PBFDir, "europe/monaco", existing)

	seeded, err := BackfillUpstreamStamps(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeded) != 1 || seeded[0] != "europe/malta" {
		t.Fatalf("seeded = %v, want [europe/malta]", seeded)
	}
	if got, ok := readUpstreamStamp(root.PBFDir, "europe/malta"); !ok || !got.Equal(mtime) {
		t.Errorf("malta stamp = %v %v, want %v", got, ok, mtime)
	}
	if got, _ := readUpstreamStamp(root.PBFDir, "europe/monaco"); !got.Equal(existing) {
		t.Errorf("monaco stamp overwritten: %v", got)
	}
	if _, ok := readUpstreamStamp(root.PBFDir, "europe/andorra"); ok {
		t.Error("stamp invented for missing PBF")
	}
}

type fixedUpstream map[string]time.Time

func (f fixedUpstream) Download(context.Context, string, string) (UpstreamInfo, error) {
	return UpstreamInfo{}, errors.New("fixedUpstream: no downloads")
}

func (f fixedUpstream) Head(_ context.Context, region string) (UpstreamInfo, error) {
	return UpstreamInfo{LastModified: f[region]}, nil
}

func TestUpdateAfterBackfill_UnchangedOnlyIfNotRepublishedSinceMtime(t *testing.T) {
	root := DataRoot{
		OverpassDir: filepath.Join(t.TempDir(), "op"),
		ValhallaDir: filepath.Join(t.TempDir(), "va"),
		PBFDir:      t.TempDir(),
	}
	writeLoadedRegions(t, root.OverpassDir, "europe/malta,europe/monaco")
	mtime := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	for _, r := range []string{"europe/malta", "europe/monaco"} {
		p := filepath.Join(root.PBFDir, pbfFilename(r))
		os.WriteFile(p, []byte("x"), 0o644)
		os.Chtimes(p, mtime, mtime)
	}
	if _, err := BackfillUpstreamStamps(root); err != nil {
		t.Fatal(err)
	}
	deps := UpdateDeps{
		Upstream: fixedUpstream{
			"europe/malta":  mtime.Add(-time.Hour), // published before download finished
			"europe/monaco": mtime.Add(time.Hour),  // republished since
		},
		Runtime:          newFakeRuntime(),
		Root:             root,
		Clock:            func() time.Time { return mtime },
		PBFDataTimestamp: func(string) (time.Time, bool, error) { return time.Time{}, false, nil },
	}
	rep, err := Update(context.Background(), deps, UpdateOptions{Only: "overpass", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	regs := rep.Services[0].Regions
	if regs[0].Changed {
		t.Errorf("malta should be unchanged: %+v", regs[0])
	}
	if !regs[1].Changed {
		t.Errorf("monaco should be changed: %+v", regs[1])
	}
}

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// An update interrupted after some regions were fetched leaves those PBFs on
// disk at the current upstream version. The next run must not fetch them
// again, but must still rebuild every service from them.

func pbfPathFor(e *updaterEnv, region string) string {
	return filepath.Join(e.root.PBFDir, pbfFilename(region))
}

func TestFetched_ValhallaOnlyResumesWithoutRedownloadingFetchedRegions(t *testing.T) {
	e := newValhallaEnv(t, vermont, maine)
	e.makeChanged(vermont)
	e.makeChanged(maine)
	e.upstream.downloadErr[maine] = os.ErrDeadlineExceeded // interrupted after vermont

	rep := e.update(t, UpdateOptions{})
	if findService(t, rep, ServiceValhalla).Outcome != OutcomeFailed {
		t.Fatalf("first run should fail, got %+v", rep.Services)
	}

	e.upstream.downloads = nil
	delete(e.upstream.downloadErr, maine)
	rep = e.update(t, UpdateOptions{})

	if !reflect.DeepEqual(e.upstream.downloads, []string{maine}) {
		t.Errorf("downloads = %v, want only maine", e.upstream.downloads)
	}
	v := findService(t, rep, ServiceValhalla)
	if v.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s), want updated", v.Outcome, v.Detail)
	}
	if !findRegion(t, v, vermont).Changed {
		t.Error("vermont must still count as changed so the service is rebuilt")
	}
	if got := e.liveTile(t); got != "new" {
		t.Errorf("live tiles = %q, want rebuilt", got)
	}
	if got := stampOf(t, e.updaterEnv, ServiceValhalla, vermont); got != "2026-11-15" {
		t.Errorf("vermont stamp = %s, want new upstream version", got)
	}
}

func TestFetched_OverpassOnlyResumesWithoutRedownloadingFetchedRegions(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont, maine)
	e.makeChanged(vermont)
	e.makeChanged(maine)
	e.upstream.downloadErr[maine] = os.ErrDeadlineExceeded

	rep := e.update(t, UpdateOptions{})
	if findService(t, rep, ServiceOverpass).Outcome != OutcomeFailed {
		t.Fatalf("first run should fail, got %+v", rep.Services)
	}

	e.upstream.downloads = nil
	delete(e.upstream.downloadErr, maine)
	rep = e.update(t, UpdateOptions{})

	if !reflect.DeepEqual(e.upstream.downloads, []string{maine}) {
		t.Errorf("downloads = %v, want only maine", e.upstream.downloads)
	}
	o := findService(t, rep, ServiceOverpass)
	if o.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s), want updated", o.Outcome, o.Detail)
	}
	if !findRegion(t, o, vermont).Changed {
		t.Error("vermont must still count as changed so the service is rebuilt")
	}
	if got := e.liveData(t); got != "new" {
		t.Errorf("live data = %q, want rebuilt", got)
	}
}

func TestFetched_BothServicesResumeAndRebuildBoth(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont, maine)
	e.load("valhalla", vermont, t0, t1)
	e.load("valhalla", maine, t0, t1)
	e.makeChanged(vermont)
	e.makeChanged(maine)
	e.liveTilesFor(t)
	e.upstream.downloadErr[maine] = os.ErrDeadlineExceeded

	rep := e.update(t, UpdateOptions{})
	if !rep.Failed() {
		t.Fatalf("first run should fail, got %+v", rep.Services)
	}

	e.upstream.downloads = nil
	delete(e.upstream.downloadErr, maine)
	rep = e.update(t, UpdateOptions{})

	if !reflect.DeepEqual(e.upstream.downloads, []string{maine}) {
		t.Errorf("downloads = %v, want only maine", e.upstream.downloads)
	}
	for _, svc := range allServices {
		s := findService(t, rep, svc)
		if s.Outcome != OutcomeUpdated {
			t.Errorf("%s outcome = %v (%s), want updated", svc, s.Outcome, s.Detail)
		}
		if !findRegion(t, s, vermont).Changed {
			t.Errorf("%s: vermont must still count as changed", svc)
		}
	}
}

func TestFetched_UpstreamMovedOnRedownloads(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	// A complete file fetched at t1, but upstream has since moved on.
	if err := os.WriteFile(pbfPathFor(e.updaterEnv, vermont), []byte("pbf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFetchedMarker(e.root.PBFDir, vermont, t1, 3); err != nil {
		t.Fatal(err)
	}
	e.upstream.info[vermont] = UpstreamInfo{LastModified: t1.Add(24 * time.Hour), ContentLength: 100}

	e.update(t, UpdateOptions{})

	if !reflect.DeepEqual(e.upstream.downloads, []string{vermont}) {
		t.Errorf("downloads = %v, want vermont fetched again", e.upstream.downloads)
	}
}

func TestFetched_TruncatedOrMismatchedFileIsNotTrusted(t *testing.T) {
	cases := map[string]func(t *testing.T, e *valhallaEnv){
		"truncated": func(t *testing.T, e *valhallaEnv) {
			if err := os.WriteFile(pbfPathFor(e.updaterEnv, vermont), []byte("ab"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"no marker": func(t *testing.T, e *valhallaEnv) {
			os.Remove(fetchedMarkerPath(e.root.PBFDir, vermont))
		},
		"older version recorded": func(t *testing.T, e *valhallaEnv) {
			if err := writeFetchedMarker(e.root.PBFDir, vermont, t0, 3); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			e := newValhallaEnv(t, vermont)
			e.makeChanged(vermont)
			// A complete 3-byte file, as fetched at t1.
			if err := os.WriteFile(pbfPathFor(e.updaterEnv, vermont), []byte("pbf"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := writeFetchedMarker(e.root.PBFDir, vermont, t1, 3); err != nil {
				t.Fatal(err)
			}
			corrupt(t, e)

			e.update(t, UpdateOptions{})

			if !reflect.DeepEqual(e.upstream.downloads, []string{vermont}) {
				t.Errorf("downloads = %v, want vermont fetched again", e.upstream.downloads)
			}
		})
	}
}

func TestFetched_ValidFileIsSkipped(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	if err := os.WriteFile(pbfPathFor(e.updaterEnv, vermont), []byte("pbf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFetchedMarker(e.root.PBFDir, vermont, t1, 3); err != nil {
		t.Fatal(err)
	}

	e.update(t, UpdateOptions{})

	if len(e.upstream.downloads) != 0 {
		t.Errorf("downloads = %v, want none", e.upstream.downloads)
	}
}

func TestFetched_ForcedUpdateStillDownloads(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	if err := os.WriteFile(pbfPathFor(e.updaterEnv, vermont), []byte("pbf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFetchedMarker(e.root.PBFDir, vermont, t1, 3); err != nil {
		t.Fatal(err)
	}

	e.update(t, UpdateOptions{Force: true})

	if !reflect.DeepEqual(e.upstream.downloads, []string{vermont}) {
		t.Errorf("downloads = %v, want vermont fetched again under --force", e.upstream.downloads)
	}
}

// The fetched marker must never change what a service reads as the version it
// holds, including the fallback to the download stamp.
func TestFetched_DoesNotChangeAnyServiceStamp(t *testing.T) {
	e := newUpdaterEnv(t)
	writeDownloadStamp(t, e.root.PBFDir, vermont, t0)
	if err := writeFetchedMarker(e.root.PBFDir, vermont, t1, 3); err != nil {
		t.Fatal(err)
	}

	for _, svc := range allServices {
		got, ok := readServiceStamp(e.root.PBFDir, svc, vermont)
		if !ok || !got.Equal(t0) {
			t.Errorf("%s reads stamp %v (ok=%v), want download stamp %v", svc, got, ok, t0)
		}
	}
}

func TestFetched_DryRunIsUnaffected(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	if err := os.WriteFile(pbfPathFor(e.updaterEnv, vermont), []byte("pbf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFetchedMarker(e.root.PBFDir, vermont, t1, 3); err != nil {
		t.Fatal(err)
	}

	rep := e.update(t, UpdateOptions{DryRun: true})

	v := findService(t, rep, ServiceValhalla)
	if v.Outcome != OutcomeChanged || !findRegion(t, v, vermont).Changed {
		t.Errorf("dry run should still report vermont changed, got %+v", v)
	}
	if len(e.upstream.downloads) != 0 {
		t.Errorf("dry run downloaded %v", e.upstream.downloads)
	}
}

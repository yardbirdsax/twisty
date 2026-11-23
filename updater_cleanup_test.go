package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cleanup after a successful swap (deleting the old data, clearing caches) is
// best effort: the new data is already live, so a failure there must not mark
// the service failed or withhold its stamp.

// makeUndeletable makes dir's contents impossible to remove for the rest of
// the test (the directory is restored on cleanup so the temp dir can go).
func makeUndeletable(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

func assertUpdatedWithWarning(t *testing.T, s ServiceReport) {
	t.Helper()
	if s.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s), want updated: the new data is live", s.Outcome, s.Detail)
	}
	if !strings.Contains(s.Detail, "warning") {
		t.Errorf("detail = %q, want a warning about the failed cleanup", s.Detail)
	}
}

func TestValhallaUpdate_OldTilesCleanupFailureIsAWarningAndStillStamps(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	oldTiles := filepath.Join(e.root.ValhallaDir, "tiles.old")
	e.runtime.OnWaitReady = func(string) error {
		makeUndeletable(t, oldTiles)
		return nil
	}

	rep := e.update(t, UpdateOptions{})

	assertUpdatedWithWarning(t, findService(t, rep, "valhalla"))
	if e.liveTile(t) != "new" {
		t.Errorf("live tiles = %q, want new", e.liveTile(t))
	}
	if got := stampOf(t, e.updaterEnv, ServiceValhalla, vermont); got != "2026-11-15" {
		t.Errorf("stamp = %s, want 2026-11-15", got)
	}
}

func TestOverpassUpdate_OldDatabaseCleanupFailureIsAWarningAndStillStamps(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	oldDB := filepath.Join(e.root.OverpassDir, "db.old")
	e.runtime.OnWaitReady = func(url string) error {
		if url == overpassReadyURL(defaultOverpassPort) {
			makeUndeletable(t, oldDB)
		}
		return nil
	}

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	assertUpdatedWithWarning(t, findService(t, rep, "overpass"))
	if e.liveData(t) != "new" {
		t.Errorf("live db = %q, want new", e.liveData(t))
	}
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, vermont); got != "2026-11-15" {
		t.Errorf("stamp = %s, want 2026-11-15", got)
	}
}

func TestOverpassUpdate_CacheClearFailureIsAWarningAndStillStamps(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	makeUndeletable(t, e.root.TileCacheDir)

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	assertUpdatedWithWarning(t, findService(t, rep, "overpass"))
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, vermont); got != "2026-11-15" {
		t.Errorf("stamp = %s, want 2026-11-15", got)
	}
}

func TestOverpassInPlace_CacheClearFailureIsAWarningAndStillStamps(t *testing.T) {
	e := newOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	base := filepath.Dir(e.root.OverpassDir)
	e.root.TileCacheDir = filepath.Join(base, "cache", "overpass")
	if err := os.MkdirAll(e.root.TileCacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root.TileCacheDir, "entry.json"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeUndeletable(t, e.root.TileCacheDir)

	rep := e.update(t, UpdateOptions{InPlace: true})

	assertUpdatedWithWarning(t, findService(t, rep, "overpass"))
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, vermont); got != "2026-11-15" {
		t.Errorf("stamp = %s, want 2026-11-15", got)
	}
}

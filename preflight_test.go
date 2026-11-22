package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSized creates a file of exactly n bytes (creating parent dirs).
func (e *updaterEnv) writeSized(path string, n int) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *updaterEnv) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// preflightEnv lays out an Overpass install: 1000-byte db, 200-byte merged
// archive, 300 bytes of tile cache, 50 of score cache, one region (PBF 3 bytes,
// upstream Content-Length 100) that has changed upstream.
type preflightEnv struct {
	*updaterEnv
	db, merged, tileCache, scoreCache, convCache, pbf string
}

func newPreflightEnv(t *testing.T) *preflightEnv {
	e := newUpdaterEnv(t)
	base := filepath.Dir(e.root.OverpassDir)
	e.root.TileCacheDir = filepath.Join(base, "cache", "overpass")
	e.root.ScoreCacheDir = filepath.Join(base, "cache", "scores")
	p := &preflightEnv{updaterEnv: e}
	p.db = filepath.Join(e.root.OverpassDir, "db", "nodes.dat")
	p.merged = filepath.Join(e.root.OverpassDir, "merged.osm.bz2")
	p.tileCache = filepath.Join(e.root.TileCacheDir, "tile1.json")
	p.scoreCache = filepath.Join(e.root.ScoreCacheDir, "s1.json")
	e.load("overpass", "north-america/us/vermont", t0, t1)
	p.pbf = filepath.Join(e.root.PBFDir, pbfFilename("north-america/us/vermont"))
	p.convCache = filepath.Join(e.root.OverpassDir, "cache", cacheFilename("north-america/us/vermont"))
	e.writeSized(p.db, 1000)
	e.writeSized(p.merged, 200)
	e.writeSized(p.tileCache, 300)
	e.writeSized(p.scoreCache, 50)
	e.writeSized(p.convCache, 70)
	return p
}

// diskOf makes the fake free-space function model a disk of the given capacity
// shared by everything under the env's base dir, so deleting files frees space.
func (e *updaterEnv) diskOf(capacity int64) func(string) (uint64, error) {
	base := filepath.Dir(e.root.OverpassDir)
	return func(string) (uint64, error) {
		used := dirSize(base)
		if used >= capacity {
			return 0, nil
		}
		return uint64(capacity - used), nil
	}
}

// Needed = 1000*1.15 (db) + 100 (changed download) + 200 (merged archive) = 1450.
const preflightNeeded = 1450

func TestPreflight_FitsProceedsWithoutReclaiming(t *testing.T) {
	p := newPreflightEnv(t)
	deps := p.deps()
	deps.FreeSpace = func(string) (uint64, error) { return 5000, nil }

	rep, err := Update(context.Background(), deps, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if rep.Preflight == nil {
		t.Fatal("expected a preflight report")
	}
	if rep.Preflight.Needed != preflightNeeded {
		t.Errorf("needed = %d, want %d", rep.Preflight.Needed, preflightNeeded)
	}
	if rep.Preflight.Free != 5000 || !rep.Preflight.Fits {
		t.Errorf("free=%d fits=%v, want 5000/true", rep.Preflight.Free, rep.Preflight.Fits)
	}
	if len(rep.Preflight.Reclaimed) != 0 {
		t.Errorf("reclaimed %v, want none", rep.Preflight.Reclaimed)
	}
	for _, f := range []string{p.tileCache, p.scoreCache, p.merged, p.convCache} {
		if !p.exists(f) {
			t.Errorf("%s should not have been deleted", f)
		}
	}
	if rep.Failed() {
		t.Error("update should not fail when it fits")
	}
}

func TestPreflight_ReclaimsDerivedDataOnlyUntilItFits(t *testing.T) {
	p := newPreflightEnv(t)
	// Used = 1000+200+300+50+70+3 (pbf) + stamp/regions bytes; pick capacity so
	// free is short of 1450 but deleting the 350 bytes of tile+score cache fixes it.
	used := dirSize(filepath.Dir(p.root.OverpassDir))
	deps := p.deps()
	deps.FreeSpace = p.diskOf(used + preflightNeeded - 340)

	rep, err := Update(context.Background(), deps, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !rep.Preflight.Fits || rep.Failed() {
		t.Fatalf("expected fit after reclaim, got %+v", rep.Preflight)
	}
	if p.exists(p.tileCache) || p.exists(p.scoreCache) {
		t.Error("tile and score cache should have been reclaimed")
	}
	// Reclaiming stops as soon as it fits: later items stay.
	if !p.exists(p.merged) || !p.exists(p.convCache) {
		t.Error("merged archive and conversion cache should be kept once it fits")
	}
	// Never the live database or region PBFs.
	if !p.exists(p.db) || !p.exists(p.pbf) {
		t.Error("live database and region PBF must never be reclaimed")
	}
	if len(rep.Preflight.Reclaimed) != 2 {
		t.Errorf("reclaimed %v, want tile cache and score cache", rep.Preflight.Reclaimed)
	}
}

func TestPreflight_ReclaimOrderReachesMergedArchiveThenConversionCache(t *testing.T) {
	p := newPreflightEnv(t)
	used := dirSize(filepath.Dir(p.root.OverpassDir))
	// Short by 500: caches (350) are not enough; merged archive (200) is.
	deps := p.deps()
	deps.FreeSpace = p.diskOf(used + preflightNeeded - 500)

	rep, err := Update(context.Background(), deps, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Preflight.Fits {
		t.Fatalf("expected fit, got %+v", rep.Preflight)
	}
	if p.exists(p.merged) {
		t.Error("merged archive should have been reclaimed")
	}
	if !p.exists(p.convCache) {
		t.Error("conversion cache should be kept once it fits")
	}
}

func TestPreflight_AbortsWithExactShortfallAndInPlaceHint(t *testing.T) {
	p := newPreflightEnv(t)
	deps := p.deps()
	// Free 100; reclaimable is 350+200+70=620, so max 720 < 1450. Shortfall is
	// measured against free space after everything reclaimable: 1450-720 = 730.
	deps.FreeSpace = func(path string) (uint64, error) {
		return 100 + uint64(0), nil
	}

	rep, err := Update(context.Background(), deps, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !rep.Failed() {
		t.Fatal("expected failure")
	}
	if rep.Preflight.Fits {
		t.Error("preflight should not fit")
	}
	if rep.Preflight.Shortfall != 730 {
		t.Errorf("shortfall = %d, want 730", rep.Preflight.Shortfall)
	}
	op := findService(t, rep, "overpass")
	if op.Outcome != OutcomeFailed {
		t.Errorf("overpass outcome = %v, want failed", op.Outcome)
	}
	if !strings.Contains(op.Detail, "730") || !strings.Contains(op.Detail, "--in-place") {
		t.Errorf("detail %q should name the shortfall and suggest --in-place", op.Detail)
	}
	// Reclaiming cannot help, so nothing is deleted.
	for _, f := range []string{p.tileCache, p.scoreCache, p.merged, p.convCache, p.db, p.pbf} {
		if !p.exists(f) {
			t.Errorf("%s should not have been deleted when update cannot fit anyway", f)
		}
	}
}

func TestPreflight_DryRunReportsPlanAndDeletesNothing(t *testing.T) {
	p := newPreflightEnv(t)
	used := dirSize(filepath.Dir(p.root.OverpassDir))
	deps := p.deps()
	deps.FreeSpace = p.diskOf(used + preflightNeeded - 340)

	rep, err := Update(context.Background(), deps, UpdateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	if rep.Preflight.Needed != preflightNeeded || !rep.Preflight.Fits {
		t.Errorf("preflight = %+v, want needed %d and fits", rep.Preflight, preflightNeeded)
	}
	if len(rep.Preflight.Planned) != 2 {
		t.Errorf("planned = %v, want tile cache and score cache", rep.Preflight.Planned)
	}
	if len(rep.Preflight.Reclaimed) != 0 {
		t.Errorf("dry run reclaimed %v", rep.Preflight.Reclaimed)
	}
	for _, f := range []string{p.tileCache, p.scoreCache, p.merged, p.convCache, p.db} {
		if !p.exists(f) {
			t.Errorf("dry run deleted %s", f)
		}
	}
}

func TestPreflight_SkippedWhenNothingChanged(t *testing.T) {
	p := newPreflightEnv(t)
	p.upstream.info["north-america/us/vermont"] = UpstreamInfo{LastModified: t0, ContentLength: 100}

	rep, err := Update(context.Background(), p.deps(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Preflight != nil {
		t.Errorf("preflight = %+v, want none when nothing changed", rep.Preflight)
	}
}

func TestPreflight_ValhallaOnlyCountsValhallaIntermediatesAndNeverTouchesLiveTiles(t *testing.T) {
	e := newUpdaterEnv(t)
	e.load("valhalla", "north-america/us/vermont", t0, t1)
	tiles := filepath.Join(e.root.ValhallaDir, "tiles", "0", "000.gph")
	mergedPBF := filepath.Join(e.root.ValhallaDir, "merged.osm.pbf")
	e.writeSized(tiles, 400)
	e.writeSized(mergedPBF, 150)
	deps := e.deps()
	deps.FreeSpace = func(string) (uint64, error) { return 600, nil }

	rep, err := Update(context.Background(), deps, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// 100 download + 150 merged PBF + 400 staging tiles; no database.
	if rep.Preflight.Needed != 650 {
		t.Errorf("needed = %d, want 650", rep.Preflight.Needed)
	}
	if !e.exists(tiles) {
		t.Error("live tiles must never be reclaimed")
	}
	if e.exists(mergedPBF) {
		t.Error("old merged PBF is reclaimable derived data")
	}
}

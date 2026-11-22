package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// stagedOverpassEnv is an updaterEnv with a live Overpass install: an old database on
// disk and the old container running. The fake converter writes the merged
// archive; the fake staging container "imports" by writing "new" into the
// database directory it mounts.
type stagedOverpassEnv struct {
	*updaterEnv
	liveDB    string
	merged    string
	tileCache string
	scoreDB   string
	converted [][]string // region lists the converter was asked for
	convErr   error
}

const stagingName = overpassContainerName + "-staging"

func newStagedOverpassEnv(t *testing.T, regions ...string) *stagedOverpassEnv {
	t.Helper()
	e := &stagedOverpassEnv{updaterEnv: newUpdaterEnv(t)}
	e.liveDB = filepath.Join(e.root.OverpassDir, "db")
	e.merged = filepath.Join(e.root.OverpassDir, "merged.osm.bz2")
	base := filepath.Dir(e.root.OverpassDir)
	e.root.TileCacheDir = filepath.Join(base, "cache", "overpass")
	e.root.ScoreCacheDir = filepath.Join(base, "cache", "scores")
	for _, d := range []string{e.root.TileCacheDir, e.root.ScoreCacheDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "entry.json"), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range regions {
		e.load("overpass", r, t0, t0)
	}
	e.writeDB(t, e.liveDB, "old")
	if err := os.WriteFile(e.merged, []byte("old merged"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.runtime.seedRunning(overpassContainerSpec(overpassContainerName, defaultOverpassPort, e.liveDB, e.merged))
	e.runtime.OnRun = func(spec ContainerSpec) error {
		if spec.Name == stagingName {
			dir, _ := mountHostDir(spec, "/db")
			return os.WriteFile(filepath.Join(dir, "data"), []byte("new"), 0o644)
		}
		return nil
	}
	return e
}

func (e *stagedOverpassEnv) deps() UpdateDeps {
	d := e.updaterEnv.deps()
	d.Root = e.root
	d.ConvertOverpass = func(_ context.Context, all []string) error {
		e.converted = append(e.converted, all)
		if e.convErr != nil {
			return e.convErr
		}
		return os.WriteFile(e.merged, []byte("new merged"), 0o644)
	}
	return d
}

func (e *stagedOverpassEnv) writeDB(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *stagedOverpassEnv) liveData(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.liveDB, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e *stagedOverpassEnv) makeChanged(region string) {
	e.upstream.info[region] = UpstreamInfo{LastModified: t1, ContentLength: 100}
}

func (e *stagedOverpassEnv) update(t *testing.T, opts UpdateOptions) UpdateReport {
	t.Helper()
	rep, err := Update(context.Background(), e.deps(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func (e *stagedOverpassEnv) assertNoLeftovers(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(e.root.OverpassDir)
	for _, en := range entries {
		if strings.Contains(en.Name(), "staging") || strings.Contains(en.Name(), ".old") {
			t.Errorf("leftover %s in overpass dir", en.Name())
		}
	}
}

func stampOf(t *testing.T, e *updaterEnv, svc Service, region string) string {
	t.Helper()
	ts, ok := readServiceStamp(e.root.PBFDir, svc, region)
	if !ok {
		return "none"
	}
	return ts.Format("2006-01-02")
}

func TestOverpassUpdate_StagedSwapKeepsOldServingUntilProbePasses(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)

	var oldServingDuringImport bool
	var oldDBDuringImport string
	var oldKeptDuringProbe bool
	baseRun := e.runtime.OnRun
	e.runtime.OnRun = func(spec ContainerSpec) error {
		if spec.Name == stagingName {
			oldServingDuringImport = e.runtime.IsRunning(overpassContainerName)
			oldDBDuringImport = e.liveData(t)
		}
		return baseRun(spec)
	}
	liveURL := overpassReadyURL(defaultOverpassPort)
	e.runtime.OnWaitReady = func(url string) error {
		if url == liveURL {
			_, err := os.Stat(filepath.Join(e.root.OverpassDir, "db.old"))
			oldKeptDuringProbe = err == nil
		}
		return nil
	}

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s), want updated", o.Outcome, o.Detail)
	}
	if !oldServingDuringImport || oldDBDuringImport != "old" {
		t.Errorf("old container must keep serving the old db during the staged import (running=%v db=%q)",
			oldServingDuringImport, oldDBDuringImport)
	}
	if !oldKeptDuringProbe {
		t.Error("old database must still exist while the new container is probed")
	}
	want := []string{
		"run " + stagingName,
		"ready " + overpassReadyURL(defaultOverpassStagingPort),
		"stop " + stagingName,
		"rm " + stagingName,
		"stop " + overpassContainerName,
		"rm " + overpassContainerName,
		"run " + overpassContainerName,
		"ready " + liveURL,
	}
	// A defensive removal of any stale staging container may precede the run.
	got := e.runtime.calls
	if len(got) > 0 && got[0] == "rm "+stagingName {
		got = got[1:]
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v\nwant    %v", got, want)
	}
	if d := e.liveData(t); d != "new" {
		t.Errorf("live db = %q, want new", d)
	}
	if b, _ := os.ReadFile(e.merged); string(b) != "new merged" {
		t.Errorf("merged archive = %q, want the rebuilt one", b)
	}
	spec, _ := e.runtime.spec(overpassContainerName)
	if host, _ := mountHostDir(spec, "/db"); host != e.liveDB {
		t.Errorf("new container mounts %q, want live db path %q", host, e.liveDB)
	}
	e.assertNoLeftovers(t)
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, vermont); got != "2026-11-15" {
		t.Errorf("stamp = %s, want 2026-11-15", got)
	}
}

func TestOverpassUpdate_ClearsDerivedCachesAfterSwapAndReportsThem(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	for _, d := range []string{e.root.TileCacheDir, e.root.ScoreCacheDir} {
		if entries, _ := os.ReadDir(d); len(entries) != 0 {
			t.Errorf("%s still holds %d entries", d, len(entries))
		}
	}
	o := findService(t, rep, "overpass")
	if !strings.Contains(o.Detail, e.root.TileCacheDir) || !strings.Contains(o.Detail, e.root.ScoreCacheDir) {
		t.Errorf("detail should name the removed caches, got %q", o.Detail)
	}
}

func TestOverpassUpdate_OnlyChangedRegionsDownloadedAllConverted(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont, maine)
	e.makeChanged(vermont)

	e.update(t, UpdateOptions{Only: "overpass"})

	if !reflect.DeepEqual(e.upstream.downloads, []string{vermont}) {
		t.Errorf("downloads = %v, want only vermont", e.upstream.downloads)
	}
	if len(e.converted) != 1 || !reflect.DeepEqual(e.converted[0], []string{vermont, maine}) {
		t.Errorf("converted = %v, want one conversion over both regions", e.converted)
	}
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, maine); got != "2026-11-01" {
		t.Errorf("unchanged region's stamp moved to %s", got)
	}
}

func TestOverpassUpdate_FailedImportDiscardsStagingAndLeavesOldServing(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	e.runtime.OnWaitReady = func(url string) error {
		if url == overpassReadyURL(defaultOverpassStagingPort) {
			return errors.New("import blew up")
		}
		return nil
	}
	e.writeDB(t, e.liveDB, "old")

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	o := findService(t, rep, "overpass")
	if o.Outcome != OutcomeFailed || !strings.Contains(o.Detail, "import") || !strings.Contains(o.Detail, "import blew up") {
		t.Errorf("outcome = %v detail = %q, want failed naming the import stage", o.Outcome, o.Detail)
	}
	if !e.runtime.IsRunning(overpassContainerName) {
		t.Error("old container must still be running")
	}
	for _, c := range e.runtime.calls {
		if c == "stop "+overpassContainerName {
			t.Error("live container must not be stopped when the import fails")
		}
	}
	if _, ok := e.runtime.spec(stagingName); ok {
		t.Error("staging container must be removed")
	}
	if d := e.liveData(t); d != "old" {
		t.Errorf("live db = %q, want old", d)
	}
	e.assertNoLeftovers(t)
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, vermont); got != "2026-11-01" {
		t.Errorf("stamp moved to %s after a failed update", got)
	}
	if entries, _ := os.ReadDir(e.root.TileCacheDir); len(entries) == 0 {
		t.Error("caches must survive a failed update")
	}
}

func TestOverpassUpdate_ProbeFailureRestoresOldDatabaseAndContainer(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	liveURL := overpassReadyURL(defaultOverpassPort)
	probes := 0
	e.runtime.OnWaitReady = func(url string) error {
		if url == liveURL {
			probes++
			if probes == 1 {
				return errors.New("never became ready")
			}
		}
		return nil
	}

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	o := findService(t, rep, "overpass")
	if o.Outcome != OutcomeFailed || !strings.Contains(o.Detail, "probe") || !strings.Contains(o.Detail, "rolled back") {
		t.Errorf("outcome = %v detail = %q, want failed at probe, rolled back", o.Outcome, o.Detail)
	}
	if d := e.liveData(t); d != "old" {
		t.Errorf("live db = %q, want the old one restored", d)
	}
	if !e.runtime.IsRunning(overpassContainerName) {
		t.Error("old container must be running again")
	}
	if probes != 2 {
		t.Errorf("live probes = %d, want 2 (new, then restarted old)", probes)
	}
	e.assertNoLeftovers(t)
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, vermont); got != "2026-11-01" {
		t.Errorf("stamp moved to %s after a failed update", got)
	}
	if entries, _ := os.ReadDir(e.root.TileCacheDir); len(entries) == 0 {
		t.Error("caches must survive a failed update")
	}
}

func TestOverpassUpdate_ConvertFailureTouchesNothingLive(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	e.convErr = errors.New("bad pbf")

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	o := findService(t, rep, "overpass")
	if o.Outcome != OutcomeFailed || !strings.Contains(o.Detail, "convert") {
		t.Errorf("outcome = %v detail = %q, want failed at convert", o.Outcome, o.Detail)
	}
	if len(e.runtime.calls) != 0 {
		t.Errorf("runtime touched: %v", e.runtime.calls)
	}
	if d := e.liveData(t); d != "old" {
		t.Errorf("live db = %q", d)
	}
}

func TestOverpassUpdate_StaleStagingAndInterruptedSwapAreRecovered(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	// A crash left stale staging data, and died between the two renames.
	e.writeDB(t, filepath.Join(e.root.OverpassDir, "db.staging"), "junk")
	if err := os.Rename(e.liveDB, filepath.Join(e.root.OverpassDir, "db.old")); err != nil {
		t.Fatal(err)
	}

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s)", o.Outcome, o.Detail)
	}
	if d := e.liveData(t); d != "new" {
		t.Errorf("live db = %q, want new", d)
	}
	e.assertNoLeftovers(t)
}

func TestOverpassUpdate_DryRunDoesNothing(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)

	rep := e.update(t, UpdateOptions{Only: "overpass", DryRun: true})

	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeChanged {
		t.Errorf("outcome = %v, want changed", o.Outcome)
	}
	if len(e.runtime.calls) != 0 || len(e.upstream.downloads) != 0 || len(e.converted) != 0 {
		t.Errorf("dry run did work: calls=%v downloads=%v", e.runtime.calls, e.upstream.downloads)
	}
}

// ─── shared upstream stamp ───────────────────────────────────────────────────

// Both services load vermont. A stamp is per region, so it may only move once
// every service loading the region has been refreshed.
func newBothServicesEnv(t *testing.T) *stagedOverpassEnv {
	t.Helper()
	e := newStagedOverpassEnv(t, vermont)
	e.load("valhalla", vermont, t0, t1) // same upstream info; valhalla side of the region
	e.makeChanged(vermont)
	e.liveTilesFor(t)
	return e
}

func (e *stagedOverpassEnv) liveTilesFor(t *testing.T) {
	t.Helper()
	live := filepath.Join(e.root.ValhallaDir, "tiles")
	if err := writeFakeValhallaTiles(live, "old"); err != nil {
		t.Fatal(err)
	}
	e.runtime.seedRunning(valhallaContainerSpec(defaultValhallaPort, live))
	baseOneShot := e.runtime.OnRunToCompletion
	e.runtime.OnRunToCompletion = func(spec ContainerSpec) error {
		if baseOneShot != nil {
			if err := baseOneShot(spec); err != nil {
				return err
			}
		}
		if dir, ok := mountHostDir(spec, "/custom_files"); ok {
			return writeFakeValhallaTiles(dir, "new")
		}
		if dir, ok := mountHostDir(spec, "/output"); ok {
			return os.WriteFile(filepath.Join(dir, "merged.osm.pbf"), []byte("merged"), 0o644)
		}
		return nil
	}
}

func TestStamp_WrittenOnlyOnceEveryServiceLoadingTheRegionIsUpdated(t *testing.T) {
	e := newBothServicesEnv(t)

	rep := e.update(t, UpdateOptions{})

	for _, s := range rep.Services {
		if s.Outcome != OutcomeUpdated {
			t.Fatalf("%s = %v (%s), want updated", s.Service, s.Outcome, s.Detail)
		}
	}
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, vermont); got != "2026-11-15" {
		t.Errorf("stamp = %s, want 2026-11-15 after both services updated", got)
	}
}

func TestStamp_OverpassFailureAfterValhallaSuccessLeavesRegionStale(t *testing.T) {
	e := newBothServicesEnv(t)
	e.convErr = errors.New("conversion failed")

	rep := e.update(t, UpdateOptions{})

	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeUpdated {
		t.Fatalf("valhalla = %v (%s)", v.Outcome, v.Detail)
	}
	if got := stampOf(t, e.updaterEnv, ServiceOverpass, vermont); got != "2026-11-01" {
		t.Errorf("stamp = %s: overpass never got the new data, the region must still read as changed", got)
	}
	// And the next update, with overpass healthy, picks the region up again.
	e.convErr = nil
	rep = e.update(t, UpdateOptions{Only: "overpass"})
	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUpdated {
		t.Errorf("retry overpass = %v (%s), want updated", o.Outcome, o.Detail)
	}
}

func TestStamp_OnlyValhallaStampsValhallaWithoutHidingStalenessFromOverpass(t *testing.T) {
	e := newBothServicesEnv(t)

	rep := e.update(t, UpdateOptions{Only: "valhalla"})

	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeUpdated {
		t.Fatalf("valhalla = %v (%s)", v.Outcome, v.Detail)
	}
	// Valhalla holds the new version now: re-running it must not rebuild.
	rep = e.update(t, UpdateOptions{Only: "valhalla", DryRun: true})
	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeUnchanged {
		t.Errorf("valhalla after its own refresh = %v, want unchanged", v.Outcome)
	}
	// Overpass never got it.
	rep = e.update(t, UpdateOptions{Only: "overpass", DryRun: true})
	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeChanged {
		t.Errorf("overpass = %v, want changed", o.Outcome)
	}
}

func TestStamp_OnlyOverpassStampsOverpassWithoutHidingStalenessFromValhalla(t *testing.T) {
	e := newBothServicesEnv(t)

	rep := e.update(t, UpdateOptions{Only: "overpass"})

	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUpdated {
		t.Fatalf("overpass = %v (%s)", o.Outcome, o.Detail)
	}
	rep = e.update(t, UpdateOptions{Only: "overpass", DryRun: true})
	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUnchanged {
		t.Errorf("overpass after its own refresh = %v, want unchanged (hours-long rebuild must not repeat)", o.Outcome)
	}
	rep = e.update(t, UpdateOptions{Only: "valhalla", DryRun: true})
	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeChanged {
		t.Errorf("valhalla = %v, want changed", v.Outcome)
	}
}

func TestStamp_FailedServiceDoesNotKeepSuccessfulServiceStale(t *testing.T) {
	e := newBothServicesEnv(t)
	e.convErr = errors.New("conversion failed")

	e.update(t, UpdateOptions{})

	rep := e.update(t, UpdateOptions{DryRun: true})
	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeUnchanged {
		t.Errorf("valhalla = %v, want unchanged: it was refreshed", v.Outcome)
	}
	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeChanged {
		t.Errorf("overpass = %v, want changed: it failed", o.Outcome)
	}
}

func TestStamp_ValhallaOnlyRegionStampsAfterValhallaAlone(t *testing.T) {
	e := newBothServicesEnv(t)
	e.load("valhalla", maine, t0, t1) // maine is loaded by valhalla only
	e.makeChanged(maine)

	e.update(t, UpdateOptions{Only: "valhalla"})

	if got := stampOf(t, e.updaterEnv, ServiceValhalla, maine); got != "2026-11-15" {
		t.Errorf("maine stamp = %s, want 2026-11-15", got)
	}
}

// ─── start after the merged archive is gone ──────────────────────────────────

func TestOverpassStart_SkipsConversionWhenImportedAndArchiveRemoved(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "db")
	if err := os.MkdirAll(db, 0o755); err != nil {
		t.Fatal(err)
	}
	merged := filepath.Join(dir, "merged.osm.bz2")

	if !overpassNeedsConversion(dir) {
		t.Error("no archive and no completed import: conversion is needed")
	}
	if err := markOverpassImported(db); err != nil {
		t.Fatal(err)
	}
	if overpassNeedsConversion(dir) {
		t.Error("import completed: a removed archive must not trigger re-conversion")
	}
	// Growing the region set wipes db/ (taking the marker with it), so the
	// archive is rebuilt.
	if err := os.RemoveAll(db); err != nil {
		t.Fatal(err)
	}
	if !overpassNeedsConversion(dir) {
		t.Error("db wiped: conversion needed again")
	}
	if err := os.WriteFile(merged, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if overpassNeedsConversion(dir) {
		t.Error("archive present: no conversion")
	}
}

func TestOverpassContainerSpec_WithoutArchiveOmitsPlanetMount(t *testing.T) {
	spec := overpassContainerSpec(overpassContainerName, 8080, "/d/db", "")
	for _, m := range spec.Mounts {
		if strings.HasPrefix(m.Container, "/data/planet.osm.bz2") {
			t.Error("no archive: nothing to mount at the planet path (docker would create a directory)")
		}
	}
	if hostDB, _ := mountHostDir(spec, "/db"); hostDB != "/d/db" {
		t.Errorf("db mount = %q", hostDB)
	}
}

func TestValhallaOnlyUpdate_LeavesDerivedCachesUntouched(t *testing.T) {
	e := newBothServicesEnv(t)

	rep := e.update(t, UpdateOptions{Only: "valhalla"})

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeUpdated {
		t.Fatalf("valhalla = %v (%s)", v.Outcome, v.Detail)
	}
	for _, d := range []string{e.root.TileCacheDir, e.root.ScoreCacheDir} {
		if entries, _ := os.ReadDir(d); len(entries) != 1 {
			t.Errorf("%s has %d entries, want the 1 original", d, len(entries))
		}
	}
	if strings.Contains(v.Detail, "cache") {
		t.Errorf("valhalla detail mentions caches: %q", v.Detail)
	}
}

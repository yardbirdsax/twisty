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

const (
	vermont = "north-america/us/vermont"
	maine   = "north-america/us/maine"
)

// valhallaEnv is an updaterEnv with a live Valhalla install: old tiles on disk
// and the old container running.
type valhallaEnv struct {
	*updaterEnv
	liveTiles string
}

func newValhallaEnv(t *testing.T, regions ...string) *valhallaEnv {
	t.Helper()
	e := &valhallaEnv{updaterEnv: newUpdaterEnv(t)}
	e.liveTiles = filepath.Join(e.root.ValhallaDir, "tiles")
	for _, r := range regions {
		e.load("valhalla", r, t0, t0) // up to date until a test says otherwise
	}
	if err := writeFakeValhallaTiles(e.liveTiles, "old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root.ValhallaDir, builtMarkerName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.runtime.seedRunning(valhallaContainerSpec(defaultValhallaPort, e.liveTiles))
	// The fake build container writes "new" tiles into whatever it mounts, and
	// the fake osmium container writes its merged output.
	e.runtime.OnRunToCompletion = func(spec ContainerSpec) error {
		if dir, ok := mountHostDir(spec, "/custom_files"); ok {
			return writeFakeValhallaTiles(dir, "new")
		}
		if dir, ok := mountHostDir(spec, "/output"); ok {
			return os.WriteFile(filepath.Join(dir, "merged.osm.pbf"), []byte("merged"), 0o644)
		}
		return nil
	}
	return e
}

// makeChanged marks region as having a newer upstream version.
func (e *valhallaEnv) makeChanged(region string) {
	e.upstream.info[region] = UpstreamInfo{LastModified: t1, ContentLength: 100}
}

func (e *valhallaEnv) liveTile(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.liveTiles, "tiles", "0", "000", "000.gph"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e *valhallaEnv) assertNoLeftovers(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(e.root.ValhallaDir)
	for _, en := range entries {
		if strings.Contains(en.Name(), "staging") || strings.Contains(en.Name(), ".old") {
			t.Errorf("leftover %s in valhalla dir", en.Name())
		}
	}
}

func (e *valhallaEnv) update(t *testing.T, opts UpdateOptions) UpdateReport {
	t.Helper()
	opts.Only = "valhalla"
	rep, err := Update(context.Background(), e.deps(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestValhallaUpdate_SwapsToNewTilesAndRecordsStamp(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)

	// Observations taken from inside the fake containers: what the real world
	// would look like at that moment.
	var oldServingDuringBuild bool
	var oldTilesIntactDuringBuild string
	var oldTilesKeptDuringProbe bool
	buildHook := e.runtime.OnRunToCompletion
	e.runtime.OnRunToCompletion = func(spec ContainerSpec) error {
		oldServingDuringBuild = e.runtime.IsRunning(valhallaContainerName)
		oldTilesIntactDuringBuild = e.liveTile(t)
		return buildHook(spec)
	}
	e.runtime.OnWaitReady = func(string) error {
		_, err := os.Stat(filepath.Join(e.root.ValhallaDir, "tiles.old"))
		oldTilesKeptDuringProbe = err == nil
		return nil
	}

	rep := e.update(t, UpdateOptions{})

	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s), want updated", v.Outcome, v.Detail)
	}
	if !oldServingDuringBuild || oldTilesIntactDuringBuild != "old" {
		t.Errorf("old container must keep serving old tiles during the build (running=%v tiles=%q)",
			oldServingDuringBuild, oldTilesIntactDuringBuild)
	}
	if !oldTilesKeptDuringProbe {
		t.Error("old tiles must still exist while the new container is probed")
	}
	wantCalls := []string{
		"oneshot " + valhallaImage,
		"stop " + valhallaContainerName,
		"rm " + valhallaContainerName,
		"run " + valhallaContainerName,
		"ready http://localhost:8002/status",
	}
	if !reflect.DeepEqual(e.runtime.calls, wantCalls) {
		t.Errorf("calls = %v\nwant    %v", e.runtime.calls, wantCalls)
	}
	if got := e.liveTile(t); got != "new" {
		t.Errorf("live tiles = %q, want new", got)
	}
	spec, _ := e.runtime.spec(valhallaContainerName)
	if host, _ := mountHostDir(spec, "/custom_files"); host != e.liveTiles {
		t.Errorf("new container mounts %q, want the live tiles path %q", host, e.liveTiles)
	}
	if !e.runtime.IsRunning(valhallaContainerName) {
		t.Error("new container should be running")
	}
	e.assertNoLeftovers(t)

	if !reflect.DeepEqual(e.upstream.downloads, []string{vermont}) {
		t.Errorf("downloads = %v", e.upstream.downloads)
	}
	if b, _ := os.ReadFile(filepath.Join(e.root.PBFDir, pbfFilename(vermont))); !strings.Contains(string(b), "2026-11-15") {
		t.Errorf("PBF not refreshed: %q", b)
	}
	if got, ok := readServiceStamp(e.root.PBFDir, ServiceValhalla, vermont); !ok || !got.Equal(t1) {
		t.Errorf("stamp = %v (%v), want %v", got, ok, t1)
	}
}

func TestValhallaUpdate_OnlyChangedRegionsAreDownloadedAllAreMerged(t *testing.T) {
	e := newValhallaEnv(t, vermont, maine)
	e.makeChanged(vermont)

	rep := e.update(t, UpdateOptions{})

	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s)", v.Outcome, v.Detail)
	}
	if !reflect.DeepEqual(e.upstream.downloads, []string{vermont}) {
		t.Errorf("downloads = %v, want only vermont", e.upstream.downloads)
	}
	// Valhalla corrupts tiles from several extracts, so all loaded regions are
	// merged first (osmium) and then built.
	wantCalls := []string{"oneshot " + osmiumImage, "oneshot " + valhallaImage}
	if !reflect.DeepEqual(e.runtime.calls[:2], wantCalls) {
		t.Errorf("first calls = %v, want %v", e.runtime.calls[:2], wantCalls)
	}
	if got, _ := readServiceStamp(e.root.PBFDir, ServiceValhalla, maine); !got.Equal(t0) {
		t.Errorf("unchanged region's stamp moved to %v", got)
	}
}

func TestValhallaUpdate_BuildFailureLeavesOldServiceUntouched(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	e.runtime.OnRunToCompletion = func(spec ContainerSpec) error {
		if dir, ok := mountHostDir(spec, "/custom_files"); ok {
			_ = writeFakeValhallaTiles(dir, "half-built")
		}
		return errors.New("exit status 137")
	}

	rep := e.update(t, UpdateOptions{})

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeFailed || !rep.Failed() {
		t.Fatalf("outcome = %v, want failed", v.Outcome)
	}
	if !strings.Contains(v.Detail, "build") || !strings.Contains(v.Detail, "exit status 137") {
		t.Errorf("detail %q should name the build stage and the cause", v.Detail)
	}
	for _, c := range e.runtime.calls {
		if strings.HasPrefix(c, "stop") || strings.HasPrefix(c, "rm") || strings.HasPrefix(c, "run") {
			t.Errorf("old container was disturbed: %v", e.runtime.calls)
		}
	}
	if !e.runtime.IsRunning(valhallaContainerName) || e.liveTile(t) != "old" {
		t.Error("old container and tiles must be intact")
	}
	e.assertNoLeftovers(t)
	if got, _ := readServiceStamp(e.root.PBFDir, ServiceValhalla, vermont); !got.Equal(t0) {
		t.Errorf("stamp moved to %v after a failed update", got)
	}
}

func TestValhallaUpdate_IncompleteBuildOutputCountsAsBuildFailure(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	e.runtime.OnRunToCompletion = func(ContainerSpec) error { return nil } // exits 0, writes nothing

	v := findService(t, e.update(t, UpdateOptions{}), "valhalla")

	if v.Outcome != OutcomeFailed || !strings.Contains(v.Detail, "build") {
		t.Errorf("= %v %q, want failed at build", v.Outcome, v.Detail)
	}
	if e.liveTile(t) != "old" {
		t.Error("live tiles must be untouched")
	}
}

func TestValhallaUpdate_ProbeFailureRollsBack(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	probes := 0
	e.runtime.OnWaitReady = func(string) error {
		probes++
		if probes == 1 {
			return errors.New("timed out")
		}
		return nil
	}

	rep := e.update(t, UpdateOptions{})

	v := findService(t, rep, "valhalla")
	if v.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %v, want failed", v.Outcome)
	}
	if !strings.Contains(v.Detail, "probe") || !strings.Contains(v.Detail, "timed out") {
		t.Errorf("detail %q should name the probe stage and cause", v.Detail)
	}
	wantCalls := []string{
		"oneshot " + valhallaImage,
		"stop " + valhallaContainerName, "rm " + valhallaContainerName, "run " + valhallaContainerName,
		"ready http://localhost:8002/status",
		// rollback: remove the failed new container, restart the old one on old tiles
		"stop " + valhallaContainerName, "rm " + valhallaContainerName, "run " + valhallaContainerName,
	}
	if !reflect.DeepEqual(e.runtime.calls[:len(wantCalls)], wantCalls) {
		t.Errorf("calls = %v\nwant prefix %v", e.runtime.calls, wantCalls)
	}
	if !e.runtime.IsRunning(valhallaContainerName) {
		t.Error("old container should be running again")
	}
	if e.liveTile(t) != "old" {
		t.Errorf("live tiles = %q, want the old tiles restored", e.liveTile(t))
	}
	e.assertNoLeftovers(t)
	if got, _ := readServiceStamp(e.root.PBFDir, ServiceValhalla, vermont); !got.Equal(t0) {
		t.Errorf("stamp moved to %v after a failed update", got)
	}
}

func TestValhallaUpdate_NewContainerFailingToStartRollsBack(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	runs := 0
	e.runtime.OnRun = func(ContainerSpec) error {
		runs++
		if runs == 1 {
			return errors.New("port is already allocated")
		}
		return nil
	}

	v := findService(t, e.update(t, UpdateOptions{}), "valhalla")

	if v.Outcome != OutcomeFailed || !strings.Contains(v.Detail, "swap") {
		t.Errorf("= %v %q, want failed at swap", v.Outcome, v.Detail)
	}
	if !e.runtime.IsRunning(valhallaContainerName) || e.liveTile(t) != "old" {
		t.Error("old container and tiles should be restored")
	}
	e.assertNoLeftovers(t)
}

func TestValhallaUpdate_DoesNotRestartOldContainerThatWasNotRunning(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	e.runtime.running[valhallaContainerName] = false
	e.runtime.OnWaitReady = func(string) error { return errors.New("never ready") }

	findService(t, e.update(t, UpdateOptions{}), "valhalla")

	if e.runtime.IsRunning(valhallaContainerName) {
		t.Error("rollback must not start a container that was not running before the update")
	}
	if e.liveTile(t) != "old" {
		t.Error("old tiles should be restored")
	}
}

func TestValhallaUpdate_DownloadFailureFailsAtDownloadStage(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	e.upstream.downloadErr[vermont] = errors.New("connection reset")

	v := findService(t, e.update(t, UpdateOptions{}), "valhalla")

	if v.Outcome != OutcomeFailed || !strings.Contains(v.Detail, "download") || !strings.Contains(v.Detail, "connection reset") {
		t.Errorf("= %v %q", v.Outcome, v.Detail)
	}
	if len(e.runtime.calls) != 0 {
		t.Errorf("runtime touched: %v", e.runtime.calls)
	}
}

func TestValhallaUpdate_DryRunDownloadsAndBuildsNothing(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)

	rep := e.update(t, UpdateOptions{DryRun: true})

	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeChanged {
		t.Errorf("outcome = %v, want changed (reported, not applied)", v.Outcome)
	}
	if len(e.upstream.downloads) != 0 || len(e.runtime.calls) != 0 {
		t.Errorf("dry run acted: downloads=%v calls=%v", e.upstream.downloads, e.runtime.calls)
	}
}

func TestValhallaUpdate_ForceRebuildsUpToDateRegions(t *testing.T) {
	e := newValhallaEnv(t, vermont)

	rep := e.update(t, UpdateOptions{Force: true})

	if v := findService(t, rep, "valhalla"); v.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s)", v.Outcome, v.Detail)
	}
	if e.liveTile(t) != "new" || len(e.upstream.downloads) != 1 {
		t.Error("--force should re-download and rebuild")
	}
}

func TestValhallaUpdate_StaleStagingFromACrashedRunIsDiscarded(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	stale := filepath.Join(e.root.ValhallaDir, "tiles.staging")
	if err := writeFakeValhallaTiles(stale, "crashed"); err != nil {
		t.Fatal(err)
	}
	// A crash mid-swap would also leave tiles.old; it must not shadow live data.
	e.runtime.OnRunToCompletion = func(spec ContainerSpec) error {
		dir, _ := mountHostDir(spec, "/custom_files")
		if b, _ := os.ReadFile(filepath.Join(dir, "tiles", "0", "000", "000.gph")); string(b) == "crashed" {
			t.Error("build started on top of stale staging data")
		}
		return writeFakeValhallaTiles(dir, "new")
	}

	if v := findService(t, e.update(t, UpdateOptions{}), "valhalla"); v.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s)", v.Outcome, v.Detail)
	}
}

func TestValhallaUpdate_FailureDoesNotStopOverpassBeingConsidered(t *testing.T) {
	e := newValhallaEnv(t, vermont)
	e.makeChanged(vermont)
	e.load("overpass", maine, t0, t0)
	e.upstream.downloadErr[vermont] = errors.New("boom")

	rep, err := Update(context.Background(), e.deps(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if findService(t, rep, "valhalla").Outcome != OutcomeFailed || findService(t, rep, "overpass").Outcome != OutcomeUnchanged {
		t.Errorf("services = %+v", rep.Services)
	}
}

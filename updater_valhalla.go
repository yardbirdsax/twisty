package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const defaultReadyTimeout = 5 * time.Minute

// updateRun carries state shared by the per-service updates of one Update call.
type updateRun struct {
	deps UpdateDeps
	// fetched records regions already downloaded this run, so a region loaded
	// by both services is downloaded once. Maps region to the upstream version
	// fetched.
	fetched map[string]UpstreamInfo
	// held records, per service, the upstream version of each region on disk
	// when that service built from it. A service that then succeeds is stamped
	// with exactly these versions (see stampRefreshed).
	held map[Service]map[string]UpstreamInfo
}

func newUpdateRun(deps UpdateDeps) *updateRun {
	return &updateRun{
		deps:    deps,
		fetched: map[string]UpstreamInfo{},
		held:    map[Service]map[string]UpstreamInfo{},
	}
}

// fetchRegions downloads each region's current extract into the shared PBF
// directory, skipping regions already fetched this run, and notes which fetched
// versions svc will build from. Stamps are NOT written here: they are written
// only after svc's refresh succeeded, so a failed refresh is retried next time.
func (u *updateRun) fetchRegions(ctx context.Context, svc Service, regions []string) error {
	if err := os.MkdirAll(u.deps.Root.PBFDir, 0o755); err != nil {
		return err
	}
	for _, region := range regions {
		if _, done := u.fetched[region]; done {
			continue
		}
		dest := filepath.Join(u.deps.Root.PBFDir, pbfFilename(region))
		info, err := u.deps.Upstream.Download(ctx, region, dest)
		if err != nil {
			return fmt.Errorf("%s: %w", region, err)
		}
		u.fetched[region] = info
	}
	// svc builds from every PBF on disk it loads, including ones an earlier
	// service fetched this run.
	held := map[string]UpstreamInfo{}
	for _, region := range loadedRegions(u.deps.Root, svc) {
		if info, ok := u.fetched[region]; ok {
			held[region] = info
		}
	}
	u.held[svc] = held
	return nil
}

// stampRefreshed records, for each service that was refreshed this run, the
// upstream version of every region it built from. Stamps are per service, so a
// service that succeeded is never rebuilt because another failed, and a service
// that was not refreshed keeps its old stamp and still reads as stale. A region
// whose download carried no Last-Modified is recorded as unknown. A stamping
// error fails that service.
func (u *updateRun) stampRefreshed(services []ServiceReport) {
	for i := range services {
		sr := &services[i]
		if sr.Outcome != OutcomeUpdated {
			continue
		}
		if err := u.stampService(sr.Service); err != nil {
			sr.Outcome = OutcomeFailed
			sr.Detail = atStage(stageStamp, err).Error()
		}
	}
}

func (u *updateRun) stampService(svc Service) error {
	for region, info := range u.held[svc] {
		if err := writeServiceStamp(u.deps.Root.PBFDir, svc, region, info.LastModified); err != nil {
			return fmt.Errorf("writing stamp for %s: %w", region, err)
		}
	}
	return nil
}

// Stages of a service update, as named in failure details.
const (
	stageDownload = "download"
	stageConvert  = "convert"
	stageBuild    = "build"
	stageImport   = "import"
	stageSwap     = "swap"
	stageProbe    = "probe"
	stageCleanup  = "cleanup"
	stageCache    = "cache"
	stageStamp    = "stamp"
)

// stageError names the stage of a service update that failed.
type stageError struct {
	stage string
	err   error
}

func (e *stageError) Error() string { return "stage " + e.stage + ": " + e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

func atStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &stageError{stage: stage, err: err}
}

// refreshValhalla refreshes the Valhalla tiles for the changed regions in sr.
// The old container keeps serving while new tiles build into a staging
// directory beside the live ones. The swap is stop, remove, move, run; the old
// tiles are deleted only once the new container answers /status. On any
// failure the staging data is discarded and the old tiles and container are
// restored, and the error names the stage that broke. The returned detail
// carries warnings about cleanup that failed after the new data went live.
func (u *updateRun) refreshValhalla(ctx context.Context, sr *ServiceReport) (string, error) {
	deps := u.deps
	dir := deps.Root.ValhallaDir

	if err := u.fetchRegions(ctx, ServiceValhalla, sr.changedRegions()); err != nil {
		return "", atStage(stageDownload, err)
	}

	// Build into staging while the old container keeps serving.
	live := filepath.Join(dir, "tiles")
	absLive, err := filepath.Abs(live)
	if err != nil {
		return "", atStage(stageSwap, err)
	}
	port := deps.valhallaPort()
	d := dirSwap{
		container: valhallaContainerName,
		live:      live,
		staging:   filepath.Join(dir, "tiles.staging"),
		old:       filepath.Join(dir, "tiles.old"),
		noun:      "tiles",
		spec:      valhallaContainerSpec(port, absLive),
		ready: func(ctx context.Context) error {
			return u.waitReady(ctx, valhallaStatusURL(port), defaultReadyTimeout)
		},
	}
	if err := u.buildValhallaTiles(ctx, loadedRegions(deps.Root, ServiceValhalla), d.staging); err != nil {
		os.RemoveAll(d.staging)
		return "", atStage(stageBuild, err)
	}
	if err := restoreInterruptedSwap(d); err != nil {
		os.RemoveAll(d.staging)
		return "", atStage(stageSwap, err)
	}

	if err := u.swap(ctx, d); err != nil {
		return "", err
	}

	// The new container passed its probe: only now is the old data expendable.
	// The new data is live, so failures from here on are warnings.
	var warnings []string
	warnings = append(warnings, discardOld(d.old, d.noun))
	if err := os.WriteFile(filepath.Join(dir, builtMarkerName), nil, 0o644); err != nil {
		warnings = append(warnings, warning("could not record the tiles as built: %v", err))
	}
	return joinDetail(warnings...), nil
}

// buildValhallaTiles builds tiles for all regions into staging (merging their
// PBFs first when there are several: Valhalla corrupts tiles built from
// multiple extracts directly). staging is created fresh.
func (u *updateRun) buildValhallaTiles(ctx context.Context, regions []string, staging string) error {
	rt := u.deps.Runtime
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	absStaging, err := filepath.Abs(staging)
	if err != nil {
		return err
	}
	var paths, names []string
	for _, region := range regions {
		abs, err := filepath.Abs(filepath.Join(u.deps.Root.PBFDir, pbfFilename(region)))
		if err != nil {
			return err
		}
		paths = append(paths, abs)
		names = append(names, pbfFilename(region))
	}
	if len(paths) > 1 {
		merged, err := filepath.Abs(filepath.Join(u.deps.Root.ValhallaDir, "merged.osm.pbf"))
		if err != nil {
			return err
		}
		if err := rt.RunToCompletion(ctx, osmiumMergeSpec(merged, paths)); err != nil {
			return fmt.Errorf("merging PBFs: %w", err)
		}
		if fi, err := os.Stat(merged); err != nil || fi.Size() == 0 {
			return fmt.Errorf("merged PBF missing or empty: %s", merged)
		}
		paths, names = []string{merged}, []string{"merged.osm.pbf"}
	}
	if err := rt.RunToCompletion(ctx, valhallaBuildSpec(absStaging, paths, names)); err != nil {
		return fmt.Errorf("building tiles: %w", err)
	}
	if ok, problem := tilesComplete(staging); !ok {
		return fmt.Errorf("build produced incomplete tiles (%s)", problem)
	}
	return nil
}

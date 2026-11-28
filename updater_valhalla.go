package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
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
	// plan lists the distinct regions this run will fetch, in fetch order, with
	// the upstream size of each. A region changed for both services appears
	// once. It fixes each download's position and the total for progress.
	plan      []plannedFetch
	downloads bool // some download has started, so progress needs closing
}

// plannedFetch is one region this run expects to download.
type plannedFetch struct {
	region string
	bytes  int64 // upstream size, -1 when unknown
}

func newUpdateRun(deps UpdateDeps, services []ServiceReport) *updateRun {
	return &updateRun{
		deps:    deps,
		fetched: map[string]UpstreamInfo{},
		held:    map[Service]map[string]UpstreamInfo{},
		plan:    planFetches(services),
	}
}

// planFetches lists the regions the services marked changed will download, each
// once, in the order the services are refreshed.
func planFetches(services []ServiceReport) []plannedFetch {
	var plan []plannedFetch
	seen := map[string]bool{}
	for _, sr := range services {
		if sr.Outcome != OutcomeChanged {
			continue
		}
		for _, r := range sr.Regions {
			if !r.Changed || seen[r.Region] {
				continue
			}
			seen[r.Region] = true
			size := r.ContentLength
			if size <= 0 {
				size = -1
			}
			plan = append(plan, plannedFetch{region: r.Region, bytes: size})
		}
	}
	return plan
}

// startDownload opens progress for region's download and returns its handle.
func (u *updateRun) startDownload(region, dest string) FileDownloadProgress {
	if u.deps.Progress == nil {
		return NoopFileDownloadProgress{}
	}
	u.downloads = true
	for i, p := range u.plan {
		if p.region == region {
			return u.deps.Progress.StartFile(dest, i, len(u.plan), p.bytes)
		}
	}
	// Not planned: it takes the next position, growing the total, so positions
	// stay distinct and never exceed it.
	u.plan = append(u.plan, plannedFetch{region: region, bytes: -1})
	return u.deps.Progress.StartFile(dest, len(u.plan)-1, len(u.plan), -1)
}

// dropFromPlan removes a region that will not be downloaded from the planned
// fetches, so progress counts only real downloads.
func (u *updateRun) dropFromPlan(region string) {
	for i, p := range u.plan {
		if p.region == region {
			u.plan = append(u.plan[:i], u.plan[i+1:]...)
			return
		}
	}
}

// finishDownloads closes download progress once the run has fetched everything
// it will.
func (u *updateRun) finishDownloads() {
	if u.downloads {
		u.deps.Progress.Done()
	}
}

// fetchRegions downloads each region's current extract into the shared PBF
// directory, all at the same time, skipping regions already fetched this run,
// and notes which fetched versions svc will build from. A region whose PBF an
// interrupted earlier run already fetched at the current upstream version is
// not downloaded again, but still counts as fetched so svc is rebuilt from it.
//
// If out is non-nil, each region is sent on it once its PBF is complete (or was
// already), and out is closed when every download has finished. cancel, if
// non-nil, is called with the first failure so the caller's other stages stop
// too. Errors are returned at stageDownload, and the first one cancels the
// remaining downloads.
//
// Stamps are NOT written here: they are written only after svc's refresh
// succeeded, so a failed refresh is retried next time.
func (u *updateRun) fetchRegions(ctx context.Context, svc Service, regions []RegionStatus, out chan<- string, cancel context.CancelCauseFunc) error {
	if out != nil {
		defer close(out)
	}
	// fail tags err as a download failure and stops the caller's other stages.
	fail := func(err error) error {
		err = atStage(stageDownload, err)
		if cancel != nil {
			cancel(err)
		}
		return err
	}
	if err := os.MkdirAll(u.deps.Root.PBFDir, 0o755); err != nil {
		return fail(err)
	}
	pbfDir := u.deps.Root.PBFDir

	// Decide what to download before starting anything: skips and progress
	// registration touch run state, which the downloads then leave alone.
	type fetch struct {
		region, dest string
		fp           FileDownloadProgress
	}
	var (
		todo  []fetch
		ready []string // already on disk at the current version
	)
	for _, rs := range regions {
		region := rs.Region
		if _, done := u.fetched[region]; done {
			ready = append(ready, region)
			continue
		}
		// A forced refresh downloads regardless.
		if rs.Reason != "forced" && pbfAlreadyFetched(pbfDir, region, rs.Upstream) {
			u.deps.logf("%s/%s: already fetched at current upstream version, skipping download", svc, region)
			u.dropFromPlan(region)
			u.fetched[region] = UpstreamInfo{LastModified: rs.Upstream}
			ready = append(ready, region)
			continue
		}
		dest := filepath.Join(pbfDir, pbfFilename(region))
		// The file is about to be replaced; its old marker no longer describes it.
		removeFetchedMarker(pbfDir, region)
		todo = append(todo, fetch{region: region, dest: dest, fp: u.startDownload(region, dest)})
	}

	dl, ctx := errgroup.WithContext(ctx)
	// send hands a finished region to the next stage, giving up if the
	// pipeline stopped, so a failure elsewhere cannot strand a sender.
	send := func(region string) error {
		if out == nil {
			return nil
		}
		select {
		case out <- region:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var mu sync.Mutex // guards u.fetched
	for _, region := range ready {
		dl.Go(func() error { return send(region) })
	}
	for _, f := range todo {
		dl.Go(func() error {
			if err := u.downloadRegion(ctx, f.region, f.dest, f.fp, &mu); err != nil {
				return fail(err)
			}
			return send(f.region)
		})
	}
	if err := dl.Wait(); err != nil {
		return err
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

// downloadRegion fetches one region to dest and records its fetched version.
// mu guards u.fetched, which other downloads write at the same time.
func (u *updateRun) downloadRegion(ctx context.Context, region, dest string, fp FileDownloadProgress, mu *sync.Mutex) error {
	info, err := u.deps.Upstream.Download(ctx, region, dest, fp)
	if err != nil {
		return fmt.Errorf("%s: %w", region, err)
	}
	fp.FileComplete()
	pbfDir := u.deps.Root.PBFDir
	if fi, err := os.Stat(dest); err == nil && !info.LastModified.IsZero() {
		if err := writeFetchedMarker(pbfDir, region, info.LastModified, fi.Size()); err != nil {
			return fmt.Errorf("%s: recording fetched version: %w", region, err)
		}
	}
	mu.Lock()
	u.fetched[region] = info
	mu.Unlock()
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

// stageName is a stage of a service update, as named in failure details.
type stageName string

const (
	stageDownload stageName = "download"
	stageConvert  stageName = "convert"
	stageBuild    stageName = "build"
	stageImport   stageName = "import"
	stageSwap     stageName = "swap"
	stageProbe    stageName = "probe"
	stageCleanup  stageName = "cleanup"
	stageCache    stageName = "cache"
	stageStamp    stageName = "stamp"
)

// stageError names the stage of a service update that failed.
type stageError struct {
	stage stageName
	err   error
}

func (e *stageError) Error() string { return "stage " + string(e.stage) + ": " + e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

func atStage(stage stageName, err error) error {
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

	if err := u.fetchRegions(ctx, ServiceValhalla, sr.changedStatuses(), nil, nil); err != nil {
		return "", err
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

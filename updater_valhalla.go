package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
}

func newUpdateRun(deps UpdateDeps) *updateRun {
	return &updateRun{deps: deps, fetched: map[string]UpstreamInfo{}}
}

// fetchRegions downloads each region's current extract into the shared PBF
// directory, skipping regions already fetched this run. Stamps are NOT written
// here: callers stamp only after their refresh succeeded, so a failed refresh
// is retried next time.
func (u *updateRun) fetchRegions(ctx context.Context, regions []string) error {
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
	return nil
}

// stampRefreshed records the upstream version of each region fetched this run,
// but only for regions every service that loads them was refreshed with. The
// stamp is per region and shared by both services, so stamping after one
// service alone would make the other think the region is current and never
// refresh it. A region left unstamped reads as changed next run (the safe
// direction). A region whose download carried no Last-Modified loses its
// stamp. A stamping error fails the services that load the region.
func (u *updateRun) stampRefreshed(services []ServiceReport) {
	updated := map[string]bool{}
	for _, s := range services {
		if s.Outcome == OutcomeUpdated {
			updated[s.Service] = true
		}
	}
	for region, info := range u.fetched {
		ready := true
		var loaders []string
		for _, svc := range []string{"overpass", "valhalla"} {
			for _, r := range loadedRegions(u.deps.Root, svc) {
				if r == region {
					loaders = append(loaders, svc)
					ready = ready && updated[svc]
				}
			}
		}
		if !ready || len(loaders) == 0 {
			continue
		}
		var err error
		if info.LastModified.IsZero() {
			if rmErr := os.Remove(upstreamStampPath(u.deps.Root.PBFDir, region)); rmErr != nil && !os.IsNotExist(rmErr) {
				err = rmErr
			}
		} else {
			err = writeUpstreamStamp(u.deps.Root.PBFDir, region, info.LastModified)
		}
		if err == nil {
			continue
		}
		for i := range services {
			for _, svc := range loaders {
				if services[i].Service == svc && services[i].Outcome == OutcomeUpdated {
					services[i].Outcome = OutcomeFailed
					services[i].Detail = fmt.Sprintf("stage stamp: writing stamp for %s: %v", region, err)
				}
			}
		}
	}
}

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

// updateValhalla refreshes the Valhalla tiles for the changed regions in sr.
// The old container keeps serving while new tiles build into a staging
// directory beside the live ones. The swap is stop, remove, move, run; the old
// tiles are deleted only once the new container answers /status. On any
// failure the staging data is discarded and the old tiles and container are
// restored, and sr is marked failed with the stage that broke.
func (u *updateRun) updateValhalla(ctx context.Context, sr *ServiceReport) {
	if err := u.runValhallaUpdate(ctx, sr); err != nil {
		sr.Outcome = OutcomeFailed
		sr.Detail = err.Error()
		return
	}
	sr.Outcome = OutcomeUpdated
}

func (u *updateRun) runValhallaUpdate(ctx context.Context, sr *ServiceReport) error {
	deps := u.deps
	rt := deps.Runtime
	dir := deps.Root.ValhallaDir
	all := loadedRegions(deps.Root, "valhalla")
	var changed []string
	for _, r := range sr.Regions {
		if r.Changed {
			changed = append(changed, r.Region)
		}
	}

	// Stage: download.
	if err := u.fetchRegions(ctx, changed); err != nil {
		return atStage("download", err)
	}

	// Stage: build into staging while the old container keeps serving.
	staging := filepath.Join(dir, "tiles.staging")
	live := filepath.Join(dir, "tiles")
	old := filepath.Join(dir, "tiles.old")
	if err := u.buildValhallaTiles(ctx, all, staging); err != nil {
		os.RemoveAll(staging)
		return atStage("build", err)
	}

	// Recover from a crash that died between the two renames of a swap.
	if _, err := os.Stat(live); os.IsNotExist(err) {
		if _, oldErr := os.Stat(old); oldErr == nil {
			if err := os.Rename(old, live); err != nil {
				os.RemoveAll(staging)
				return atStage("swap", fmt.Errorf("restoring tiles left by an interrupted swap: %w", err))
			}
		}
	}
	os.RemoveAll(old)

	absLive, err := filepath.Abs(live)
	if err != nil {
		os.RemoveAll(staging)
		return atStage("swap", err)
	}
	port := deps.ValhallaPort
	if port == 0 {
		port = defaultValhallaPort
	}
	spec := valhallaContainerSpec(port, absLive)
	wasRunning := rt.IsRunning(valhallaContainerName)

	// Stage: swap, then probe the new container.
	movedOld, movedNew := false, false
	swapErr := func() error {
		if err := rt.Stop(ctx, valhallaContainerName); err != nil {
			return atStage("swap", err)
		}
		if err := rt.Remove(ctx, valhallaContainerName); err != nil {
			return atStage("swap", err)
		}
		if err := os.Rename(live, old); err != nil {
			return atStage("swap", err)
		}
		movedOld = true
		if err := os.Rename(staging, live); err != nil {
			return atStage("swap", err)
		}
		movedNew = true
		if err := rt.Run(ctx, spec); err != nil {
			return atStage("swap", err)
		}
		timeout := deps.ReadyTimeout
		if timeout == 0 {
			timeout = defaultReadyTimeout
		}
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if err := rt.WaitReady(probeCtx, valhallaStatusURL(port)); err != nil {
			return atStage("probe", err)
		}
		return nil
	}()

	if swapErr != nil {
		var notes []string
		// Rollback uses a context that survives cancellation of ctx itself.
		rctx := context.WithoutCancel(ctx)
		rt.Stop(rctx, valhallaContainerName)   //nolint:errcheck
		rt.Remove(rctx, valhallaContainerName) //nolint:errcheck
		if movedNew {
			if err := os.RemoveAll(live); err != nil {
				notes = append(notes, fmt.Sprintf("removing new tiles: %v", err))
			}
		}
		if movedOld {
			if err := os.Rename(old, live); err != nil {
				notes = append(notes, fmt.Sprintf("restoring old tiles from %s: %v", old, err))
			}
		}
		os.RemoveAll(staging)
		if wasRunning {
			if err := rt.Run(rctx, spec); err != nil {
				notes = append(notes, fmt.Sprintf("restarting old container: %v", err))
			} else if err := rt.WaitReady(rctx, valhallaStatusURL(port)); err != nil {
				notes = append(notes, fmt.Sprintf("old container did not become ready: %v", err))
			}
		}
		if len(notes) > 0 {
			return fmt.Errorf("%w; rollback problems: %s", swapErr, strings.Join(notes, "; "))
		}
		return fmt.Errorf("%w (rolled back; old tiles and container restored)", swapErr)
	}

	// The new container passed its probe: only now is the old data expendable.
	if err := os.RemoveAll(old); err != nil {
		return atStage("cleanup", err)
	}
	if err := os.WriteFile(filepath.Join(dir, builtMarkerName), nil, 0o644); err != nil {
		return atStage("cleanup", err)
	}
	return nil
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

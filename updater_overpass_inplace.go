package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const defaultOverpassReadyTimeout = 30 * time.Minute

// updateOverpassInPlace refreshes Overpass by stopping it, wiping the database
// and re-importing from the refreshed data: downtime for the whole import, but
// no second copy of the database on disk. It runs only when the user asked for
// it with --in-place.
func (u *updateRun) updateOverpassInPlace(ctx context.Context, sr *ServiceReport) {
	if err := u.runOverpassInPlace(ctx, sr); err != nil {
		sr.Outcome = OutcomeFailed
		sr.Detail = err.Error()
		return
	}
	// The re-import changed the data, so derived caches are stale.
	detail, err := clearDerivedCaches(u.deps.Root)
	if err != nil {
		sr.Outcome = OutcomeFailed
		sr.Detail = err.Error()
		return
	}
	sr.Outcome = OutcomeUpdated
	sr.Detail = detail
}

func (u *updateRun) runOverpassInPlace(ctx context.Context, sr *ServiceReport) error {
	deps := u.deps
	rt := deps.Runtime
	liveName, _ := deps.overpassNames()
	dir := deps.Root.OverpassDir
	all := loadedRegions(deps.Root, "overpass")
	var changed []string
	for _, r := range sr.Regions {
		if r.Changed {
			changed = append(changed, r.Region)
		}
	}

	// Stage: download, then convert, both while the old database still serves,
	// so a failure here costs no downtime and loses nothing.
	if err := u.fetchRegions(ctx, changed); err != nil {
		return atStage("download", err)
	}
	if err := u.convertOverpass(ctx, all); err != nil {
		return atStage("convert", err)
	}

	db, err := filepath.Abs(filepath.Join(dir, "db"))
	if err != nil {
		return atStage("import", err)
	}
	merged, err := filepath.Abs(filepath.Join(dir, "merged.osm.bz2"))
	if err != nil {
		return atStage("import", err)
	}
	port := deps.OverpassPort
	if port == 0 {
		port = defaultOverpassPort
	}

	// Stage: point of no return. Stop, wipe, and let the fresh container import.
	if err := rt.Stop(ctx, liveName); err != nil {
		return atStage("import", err)
	}
	if err := rt.Remove(ctx, liveName); err != nil {
		return atStage("import", err)
	}
	if err := os.RemoveAll(db); err != nil {
		return atStage("import", fmt.Errorf("wiping database: %w", err))
	}
	if err := os.MkdirAll(db, 0o755); err != nil {
		return atStage("import", err)
	}
	if err := rt.Run(ctx, overpassContainerSpec(liveName, port, db, merged)); err != nil {
		return atStage("import", err)
	}
	timeout := deps.ReadyTimeout
	if timeout == 0 {
		timeout = defaultOverpassReadyTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := rt.WaitReady(probeCtx, overpassReadyURL(port)); err != nil {
		return atStage("import", fmt.Errorf("%w (the database was wiped; re-run `twisty update --in-place` or `twisty overpass start`)", err))
	}

	// The shared per-region stamp is written by Update once every service
	// loading the region has been refreshed (see stampRefreshed).
	return nil
}

// convertOverpass rebuilds merged.osm.bz2 from the region PBFs.
func (u *updateRun) convertOverpass(ctx context.Context, regions []string) error {
	if u.deps.ConvertOverpass != nil {
		return u.deps.ConvertOverpass(ctx, regions)
	}
	root := u.deps.Root
	return convertRegionsIncremental(regions, root.OverpassDir, root.PBFDir, newConvertProgress(os.Stderr), os.Stderr)
}

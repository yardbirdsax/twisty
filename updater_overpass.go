package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	overpassStagingContainerName = overpassContainerName + "-staging"
	// defaultOverpassStagingPort is the spare host port the temporary staging
	// container listens on while the live container keeps serving.
	defaultOverpassStagingPort = 8081
	// overpassImportedMarker lives in the database directory and records that
	// an import into it completed and the container served from it. It lets
	// `start` know the merged archive is no longer needed.
	// defaultOverpassImportTimeout bounds the staged import (hours in practice).
	defaultOverpassImportTimeout = 6 * time.Hour
	overpassImportedMarker       =".twisty-imported"
)

// overpassContainerSpec describes an Overpass container serving (or importing
// into) absDBDir. absMergedBZ2 is the archive to import from; when empty the
// database is already imported and no archive is mounted.
func overpassContainerSpec(name string, port int, absDBDir, absMergedBZ2 string) ContainerSpec {
	spec := ContainerSpec{
		Name:    name,
		Image:   overpassImage,
		Ports:   []PortMapping{{Host: port, Container: 80}},
		Mounts:  []Mount{{Host: absDBDir, Container: "/db"}},
		Restart: "unless-stopped",
		Env:     []string{"OVERPASS_MODE=init"},
	}
	if absMergedBZ2 != "" {
		spec.Mounts = append(spec.Mounts, Mount{Host: absMergedBZ2, Container: "/data/planet.osm.bz2:ro"})
		spec.Env = append(spec.Env, "OVERPASS_PLANET_URL=file:///data/planet.osm.bz2")
	}
	spec.Env = append(spec.Env,
		"OVERPASS_USE_AREAS=false",
		"OVERPASS_META=no",
		"OVERPASS_COMPRESSION=no",
	)
	return spec
}

// overpassReadyURL is a lightweight GET query that answers 200 once the
// Overpass API serves.
func overpassReadyURL(port int) string {
	return "http://localhost:" + strconv.Itoa(port) + "/api/interpreter?data=" +
		url.QueryEscape("[out:json][timeout:1];out;")
}

// overpassNeedsConversion reports whether `start` must (re)build the merged
// archive: it is missing and no completed import is recorded. Once an import
// completed, a removed archive is not a reason to convert again.
func overpassNeedsConversion(dataDir string) bool {
	if _, err := os.Stat(filepath.Join(dataDir, "merged.osm.bz2")); err == nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dataDir, "db", overpassImportedMarker)); err == nil {
		return false
	}
	return true
}

func markOverpassImported(dbDir string) error {
	return os.WriteFile(filepath.Join(dbDir, overpassImportedMarker), nil, 0o644)
}

// updateOverpass refreshes the Overpass database for the changed regions in
// sr. The new database is imported by a temporary staging container on a spare
// port while the live container keeps serving; the swap is stop, move, start.
// The old database is deleted only once the new container answers its probe,
// and on any failure it is moved back and the old container restarted.
func (u *updateRun) updateOverpass(ctx context.Context, sr *ServiceReport) {
	detail, err := u.runOverpassUpdate(ctx, sr)
	if err != nil {
		sr.Outcome = OutcomeFailed
		sr.Detail = err.Error()
		return
	}
	sr.Outcome = OutcomeUpdated
	sr.Detail = detail
}

func (u *updateRun) runOverpassUpdate(ctx context.Context, sr *ServiceReport) (string, error) {
	deps := u.deps
	rt := deps.Runtime
	liveName, stagingName := deps.overpassNames()
	dir := deps.Root.OverpassDir
	all := loadedRegions(deps.Root, "overpass")
	var changed []string
	for _, r := range sr.Regions {
		if r.Changed {
			changed = append(changed, r.Region)
		}
	}

	// Stage: download, then convert changed regions and rebuild the archive.
	if err := u.fetchRegions(ctx, changed); err != nil {
		return "", atStage("download", err)
	}
	if err := u.convertOverpass(ctx, all); err != nil {
		return "", atStage("convert", err)
	}

	live := filepath.Join(dir, "db")
	staging := filepath.Join(dir, "db.staging")
	old := filepath.Join(dir, "db.old")
	merged := filepath.Join(dir, "merged.osm.bz2")
	absLive, err := filepath.Abs(live)
	if err != nil {
		return "", atStage("import", err)
	}
	absStaging, err := filepath.Abs(staging)
	if err != nil {
		return "", atStage("import", err)
	}
	absMerged, err := filepath.Abs(merged)
	if err != nil {
		return "", atStage("import", err)
	}
	port := deps.OverpassPort
	if port == 0 {
		port = defaultOverpassPort
	}
	stagePort := deps.OverpassStagingPort
	if stagePort == 0 {
		stagePort = defaultOverpassStagingPort
	}
	probe := func(ctx context.Context, port int, dflt time.Duration) error {
		timeout := deps.ReadyTimeout
		if timeout == 0 {
			timeout = dflt
		}
		pctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return rt.WaitReady(pctx, overpassReadyURL(port))
	}

	// Recover from a crash that died between the two renames of a swap.
	if _, err := os.Stat(live); os.IsNotExist(err) {
		if _, oldErr := os.Stat(old); oldErr == nil {
			if err := os.Rename(old, live); err != nil {
				return "", atStage("swap", fmt.Errorf("restoring database left by an interrupted swap: %w", err))
			}
		}
	}
	os.RemoveAll(old)

	// Stage: import into a fresh staging database while the old container serves.
	discardStaging := func() {
		rctx := context.WithoutCancel(ctx)
		rt.Stop(rctx, stagingName)   //nolint:errcheck
		rt.Remove(rctx, stagingName) //nolint:errcheck
		os.RemoveAll(staging)
	}
	if err := os.RemoveAll(staging); err != nil {
		return "", atStage("import", err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", atStage("import", err)
	}
	rt.Remove(ctx, stagingName) //nolint:errcheck // stale one from a crashed run
	stageSpec := overpassContainerSpec(stagingName, stagePort, absStaging, absMerged)
	if err := rt.Run(ctx, stageSpec); err != nil {
		discardStaging()
		return "", atStage("import", err)
	}
	if err := probe(ctx, stagePort, defaultOverpassImportTimeout); err != nil {
		discardStaging()
		return "", atStage("import", err)
	}
	if err := markOverpassImported(staging); err != nil {
		discardStaging()
		return "", atStage("import", err)
	}
	if err := rt.Stop(ctx, stagingName); err != nil {
		discardStaging()
		return "", atStage("import", err)
	}
	if err := rt.Remove(ctx, stagingName); err != nil {
		discardStaging()
		return "", atStage("import", err)
	}

	// Stage: swap, then probe the new container.
	liveSpec := overpassContainerSpec(liveName, port, absLive, absMerged)
	wasRunning := rt.IsRunning(liveName)
	movedOld, movedNew := false, false
	swapErr := func() error {
		if err := rt.Stop(ctx, liveName); err != nil {
			return atStage("swap", err)
		}
		if err := rt.Remove(ctx, liveName); err != nil {
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
		if err := rt.Run(ctx, liveSpec); err != nil {
			return atStage("swap", err)
		}
		if err := probe(ctx, port, defaultReadyTimeout); err != nil {
			return atStage("probe", err)
		}
		return nil
	}()

	if swapErr != nil {
		var notes []string
		rctx := context.WithoutCancel(ctx)
		rt.Stop(rctx, liveName)   //nolint:errcheck
		rt.Remove(rctx, liveName) //nolint:errcheck
		if movedNew {
			if err := os.RemoveAll(live); err != nil {
				notes = append(notes, fmt.Sprintf("removing new database: %v", err))
			}
		}
		if movedOld {
			if err := os.Rename(old, live); err != nil {
				notes = append(notes, fmt.Sprintf("restoring old database from %s: %v", old, err))
			}
		}
		os.RemoveAll(staging)
		if wasRunning {
			if err := rt.Run(rctx, liveSpec); err != nil {
				notes = append(notes, fmt.Sprintf("restarting old container: %v", err))
			} else if err := probe(rctx, port, defaultReadyTimeout); err != nil {
				notes = append(notes, fmt.Sprintf("old container did not become ready: %v", err))
			}
		}
		if len(notes) > 0 {
			return "", fmt.Errorf("%w; rollback problems: %s", swapErr, strings.Join(notes, "; "))
		}
		return "", fmt.Errorf("%w (rolled back; old database and container restored)", swapErr)
	}

	// The new container passed its probe: only now is the old data expendable.
	if err := os.RemoveAll(old); err != nil {
		return "", atStage("cleanup", err)
	}

	return clearDerivedCaches(deps.Root)
}

// clearDerivedCaches runs after Overpass data changed: every derived result is
// stale, and tiles are not tracked by region, so both caches are cleared
// entirely. It returns a report line naming the caches that held entries.
func clearDerivedCaches(root DataRoot) (string, error) {
	var removed []string
	for _, cache := range []string{root.TileCacheDir, root.ScoreCacheDir} {
		if cache == "" {
			continue
		}
		entries, _ := os.ReadDir(cache)
		if len(entries) == 0 {
			continue
		}
		for _, en := range entries {
			if err := os.RemoveAll(filepath.Join(cache, en.Name())); err != nil {
				return "", atStage("cache", err)
			}
		}
		removed = append(removed, cache)
	}
	if len(removed) > 0 {
		return "cleared caches: " + strings.Join(removed, ", "), nil
	}
	return "", nil
}

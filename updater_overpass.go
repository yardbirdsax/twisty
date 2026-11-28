package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	overpassStagingContainerName = overpassContainerName + "-staging"
	// defaultOverpassStagingPort is the spare host port the temporary staging
	// container listens on while the live container keeps serving.
	defaultOverpassStagingPort = 8081
	// defaultOverpassImportTimeout bounds the staged import (hours in practice).
	defaultOverpassImportTimeout = 6 * time.Hour
	// overpassImportedMarker lives in the database directory and records that
	// an import into it completed and the container served from it. It lets
	// `start` know the merged archive is no longer needed.
	overpassImportedMarker = ".twisty-imported"
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

// overpassPaths are the on-disk locations of an Overpass refresh: the live and
// staging databases, where the replaced one waits, and the merged archive.
type overpassPaths struct {
	live, staging, old  string
	absLive, absStaging string
	absMerged           string
}

func newOverpassPaths(dir string) (overpassPaths, error) {
	p := overpassPaths{
		live:    filepath.Join(dir, "db"),
		staging: filepath.Join(dir, "db.staging"),
		old:     filepath.Join(dir, "db.old"),
	}
	var err error
	if p.absLive, err = filepath.Abs(p.live); err != nil {
		return p, err
	}
	if p.absStaging, err = filepath.Abs(p.staging); err != nil {
		return p, err
	}
	p.absMerged, err = filepath.Abs(filepath.Join(dir, "merged.osm.bz2"))
	return p, err
}

// prepareOverpassData downloads the changed regions and re-converts them into
// the merged archive, both while the old database still serves, so a failure
// costs no downtime and loses nothing. Shared by the staged and in-place
// refreshes.
//
// Each region converts as soon as its own download finishes, so network and CPU
// work overlap; the merge starts only once every region is converted. The first
// failure of either stage stops the pipeline and is returned with its stage and
// region.
func (u *updateRun) prepareOverpassData(ctx context.Context, sr *ServiceReport) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	fetched := make(chan string)
	var fetching sync.WaitGroup
	var downloadTook time.Duration // written by the fetching goroutine, read after fetching.Wait
	fetching.Go(func() {
		start := time.Now()
		err := u.fetchRegions(ctx, ServiceOverpass, sr.changedStatuses(), fetched, cancel)
		downloadTook = time.Since(start)
		if err != nil {
			cancel(err) // a no-op when fetchRegions already cancelled with it
		}
	})

	// Converting must keep draining fetched until it closes, so no download is
	// left blocked on a send; once the pipeline has stopped it does no work.
	var converting errgroup.Group
	converting.SetLimit(max(convertWorkers, 1))
	// The convert stage is the span from the first region starting to convert
	// to the last finishing; it overlaps the download stage.
	var (
		spanMu                 sync.Mutex
		convertFrom, convertTo time.Time
	)
	for region := range fetched {
		converting.Go(func() error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			began := time.Now()
			err := u.convertRegion(ctx, region)
			ended := time.Now()
			spanMu.Lock()
			if convertFrom.IsZero() || began.Before(convertFrom) {
				convertFrom = began
			}
			if ended.After(convertTo) {
				convertTo = ended
			}
			spanMu.Unlock()
			if err != nil {
				err = atStage(stageConvert, fmt.Errorf("converting region %s: %w", region, err))
				cancel(err)
			}
			return err
		})
	}
	_ = converting.Wait()
	fetching.Wait()
	sr.Timings = append(sr.Timings, StageTiming{stageDownload, downloadTook})
	if !convertFrom.IsZero() {
		sr.Timings = append(sr.Timings, StageTiming{stageConvert, convertTo.Sub(convertFrom)})
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}

	err := sr.timed(stageMerge, func() error {
		return u.convertOverpass(ctx, loadedRegions(u.deps.Root, ServiceOverpass))
	})
	return atStage(stageConvert, err)
}

// refreshOverpassStaged refreshes the Overpass database for the changed
// regions in sr. The new database is imported by a temporary staging container
// on a spare port while the live container keeps serving; the swap is stop,
// move, start. The old database is deleted only once the new container answers
// its probe, and on any failure it is moved back and the old container
// restarted. The returned detail says what was cleared and carries warnings
// about cleanup that failed after the new data went live.
func (u *updateRun) refreshOverpassStaged(ctx context.Context, sr *ServiceReport) (string, error) {
	deps := u.deps
	liveName, _ := deps.overpassNames()
	if err := u.prepareOverpassData(ctx, sr); err != nil {
		return "", err
	}
	p, err := newOverpassPaths(deps.Root.OverpassDir)
	if err != nil {
		return "", atStage(stageImport, err)
	}
	port := deps.overpassPort()
	d := dirSwap{
		container: liveName,
		live:      p.live,
		staging:   p.staging,
		old:       p.old,
		noun:      "database",
		spec:      overpassContainerSpec(liveName, port, p.absLive, p.absMerged),
		ready: func(ctx context.Context) error {
			return u.waitReady(ctx, overpassReadyURL(port), defaultReadyTimeout)
		},
	}
	if err := restoreInterruptedSwap(d); err != nil {
		return "", atStage(stageSwap, err)
	}
	if err := sr.timed(stageImport, func() error { return u.importIntoStaging(ctx, p) }); err != nil {
		return "", err
	}
	if err := u.swap(ctx, d); err != nil {
		return "", err
	}

	// The new container passed its probe: only now is the old data expendable.
	// The new data is live, so failures from here on are warnings.
	cleared, cacheWarnings := clearDerivedCaches(deps.Root)
	return joinDetail(discardOld(d.old, d.noun), cleared, cacheWarnings), nil
}

// importIntoStaging imports the merged archive into a fresh staging database
// with a temporary staging container, while the live container keeps serving.
// On failure the staging container and database are discarded.
func (u *updateRun) importIntoStaging(ctx context.Context, p overpassPaths) error {
	deps := u.deps
	rt := deps.Runtime
	_, stagingName := deps.overpassNames()
	stagePort := deps.overpassStagingPort()

	fail := func(err error) error {
		rctx := context.WithoutCancel(ctx)
		rt.Stop(rctx, stagingName)   //nolint:errcheck
		rt.Remove(rctx, stagingName) //nolint:errcheck
		os.RemoveAll(p.staging)
		return atStage(stageImport, err)
	}
	if err := os.RemoveAll(p.staging); err != nil {
		return atStage(stageImport, err)
	}
	if err := os.MkdirAll(p.staging, 0o755); err != nil {
		return atStage(stageImport, err)
	}
	rt.Remove(ctx, stagingName) //nolint:errcheck // stale one from a crashed run
	if err := rt.Run(ctx, overpassContainerSpec(stagingName, stagePort, p.absStaging, p.absMerged)); err != nil {
		return fail(err)
	}
	if err := u.waitReady(ctx, overpassReadyURL(stagePort), defaultOverpassImportTimeout); err != nil {
		return fail(err)
	}
	if err := markOverpassImported(p.staging); err != nil {
		return fail(err)
	}
	if err := rt.Stop(ctx, stagingName); err != nil {
		return fail(err)
	}
	if err := rt.Remove(ctx, stagingName); err != nil {
		return fail(err)
	}
	return nil
}

// clearDerivedCaches runs after Overpass data changed: every derived result is
// stale, and tiles are not tracked by region, so both caches are cleared
// entirely. It returns a report line saying what was removed from which cache,
// and a warning line for anything that could not be removed (the data is
// already live, so that is not a failure).
func clearDerivedCaches(root DataRoot) (cleared, warnings string) {
	var removed, problems []string
	for _, c := range root.derivedCaches() {
		entries, _ := os.ReadDir(c.dir)
		if len(entries) == 0 {
			continue
		}
		n := 0
		for _, en := range entries {
			if err := os.RemoveAll(filepath.Join(c.dir, en.Name())); err != nil {
				problems = append(problems, fmt.Sprintf("%s %s: %v", c.label, c.dir, err))
				continue
			}
			n++
		}
		if n > 0 {
			removed = append(removed, fmt.Sprintf("%s %s (%d %s)", c.label, c.dir, n, plural(n, "entry", "entries")))
		}
	}
	if len(removed) > 0 {
		cleared = "cleared " + strings.Join(removed, ", ")
	}
	if len(problems) > 0 {
		warnings = warning("the update succeeded but could not fully clear: %s", strings.Join(problems, "; "))
	}
	return cleared, warnings
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

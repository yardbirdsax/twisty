package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ─── injected dependencies ───────────────────────────────────────────────────

// UpstreamInfo is what a HEAD request to Geofabrik tells us about a region's
// current extract.
type UpstreamInfo struct {
	LastModified  time.Time
	ContentLength int64
}

// UpstreamClient talks to the upstream extract provider (Geofabrik).
type UpstreamClient interface {
	// Head returns the Last-Modified and Content-Length of a region's extract.
	Head(ctx context.Context, region string) (UpstreamInfo, error)
	// Download fetches a region's current extract to destPath, atomically
	// (nothing is left at destPath or beside it on error), and returns the
	// upstream Last-Modified of the version it fetched. It does not write the
	// upstream stamp; the updater does that once the refresh has succeeded.
	Download(ctx context.Context, region, destPath string) (UpstreamInfo, error)
}

// DataRoot locates twisty's on-disk state. Tests point it at a temp directory.
type DataRoot struct {
	OverpassDir string // holds .regions, db/, merged.osm.bz2, cache/
	ValhallaDir string // holds .regions, tiles/
	PBFDir      string // shared region PBFs and their upstream stamps
	// Derived Overpass caches (empty means none); reclaimable by the preflight.
	TileCacheDir  string
	ScoreCacheDir string
}

// UpdateDeps bundles everything the updater touches outside itself.
type UpdateDeps struct {
	Upstream  UpstreamClient
	Runtime   ContainerRuntime
	Root      DataRoot
	Clock     func() time.Time
	FreeSpace func(path string) (uint64, error)
	// PBFDataTimestamp returns the osmosis_replication_timestamp from a PBF
	// header (the true age of the data it holds). ok is false if absent.
	PBFDataTimestamp func(pbfPath string) (ts time.Time, ok bool, err error)
	// ValhallaPort is the host port of the Valhalla container (0 = default).
	ValhallaPort int
	// ReadyTimeout bounds each readiness probe (0 = 5 minutes).
	ReadyTimeout time.Duration
}

// ─── options and results ─────────────────────────────────────────────────────

// UpdateOptions are the user-facing flags of `twisty update`.
type UpdateOptions struct {
	Only    string // "", "overpass" or "valhalla"
	Force   bool
	DryRun  bool
	InPlace bool
}

type Outcome string

const (
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeChanged means upstream has newer data for at least one region and
	// the service was not refreshed (a dry run, or refresh not implemented yet).
	OutcomeChanged Outcome = "changed"
	// OutcomeUpdated means the service was refreshed and is serving new data.
	OutcomeUpdated Outcome = "updated"
	OutcomeFailed  Outcome = "failed"
)

// RegionStatus is the staleness verdict for one region of one service.
type RegionStatus struct {
	Region  string
	Changed bool
	// Reason explains the verdict: "upstream newer", "no stamp", "forced",
	// "up to date".
	Reason string
	// HeldStamp is the upstream Last-Modified of the version we hold (zero if
	// none); Upstream is the current upstream Last-Modified.
	HeldStamp time.Time
	Upstream  time.Time
	// DataAge is the age of the data we hold, from the PBF header timestamp.
	DataAge    time.Duration
	HasDataAge bool
	// ContentLength is the upstream download size of the current extract.
	ContentLength int64
	Err           error
}

type ServiceReport struct {
	Service string // "overpass" or "valhalla"
	Outcome Outcome
	Detail  string
	Regions []RegionStatus
}

type UpdateReport struct {
	Services []ServiceReport
	// Preflight is the disk-space check; nil when nothing changed.
	Preflight *PreflightReport
}

// Failed reports whether any service failed.
func (r UpdateReport) Failed() bool {
	for _, s := range r.Services {
		if s.Outcome == OutcomeFailed {
			return true
		}
	}
	return false
}

// ─── entry point ─────────────────────────────────────────────────────────────

// Update checks each loaded region against upstream and reports, per service,
// what changed. Valhalla is considered before Overpass.
func Update(ctx context.Context, deps UpdateDeps, opts UpdateOptions) (UpdateReport, error) {
	services := []string{"valhalla", "overpass"} // quick service first
	switch opts.Only {
	case "":
	case "valhalla", "overpass":
		services = []string{opts.Only}
	default:
		return UpdateReport{}, fmt.Errorf("invalid --only value %q: want overpass or valhalla", opts.Only)
	}

	// A dry run modifies nothing, so it neither needs nor takes the lock.
	if !opts.DryRun {
		release, err := acquireUpdateLock(deps.Root.PBFDir)
		if err != nil {
			return UpdateReport{}, err
		}
		defer release()
	}

	var rep UpdateReport
	for _, svc := range services {
		rep.Services = append(rep.Services, checkService(ctx, deps, opts, svc))
	}
	runPreflight(deps, opts, &rep)

	if !opts.DryRun {
		run := newUpdateRun(deps)
		for i := range rep.Services {
			sr := &rep.Services[i]
			// Overpass refresh arrives with its own ticket; it stays "changed".
			if sr.Service == "valhalla" && sr.Outcome == OutcomeChanged {
				run.updateValhalla(ctx, sr)
			}
		}
	}
	return rep, nil
}

func checkService(ctx context.Context, deps UpdateDeps, opts UpdateOptions, svc string) ServiceReport {
	sr := ServiceReport{Service: svc, Outcome: OutcomeUnchanged}
	regions := loadedRegions(deps.Root, svc)
	if len(regions) == 0 {
		sr.Detail = "no regions loaded"
		return sr
	}
	var failures []string
	for _, region := range regions {
		rs := RegionStatus{Region: region}
		info, err := deps.Upstream.Head(ctx, region)
		if err != nil {
			rs.Err = err
			failures = append(failures, fmt.Sprintf("%s: %v", region, err))
			sr.Regions = append(sr.Regions, rs)
			continue
		}
		rs.Upstream = info.LastModified
		rs.ContentLength = info.ContentLength
		held, hasStamp := readUpstreamStamp(deps.Root.PBFDir, region)
		rs.HeldStamp = held
		switch {
		case opts.Force:
			rs.Changed, rs.Reason = true, "forced"
		case !hasStamp:
			rs.Changed, rs.Reason = true, "no stamp"
		case info.LastModified.After(held):
			rs.Changed, rs.Reason = true, "upstream newer"
		default:
			rs.Reason = "up to date"
		}
		if rs.Changed {
			sr.Outcome = OutcomeChanged
		}
		if ts, ok, err := deps.PBFDataTimestamp(filepath.Join(deps.Root.PBFDir, pbfFilename(region))); err == nil && ok {
			rs.DataAge, rs.HasDataAge = deps.Clock().Sub(ts), true
		}
		sr.Regions = append(sr.Regions, rs)
	}
	if len(failures) > 0 {
		sr.Outcome = OutcomeFailed
		sr.Detail = "checking upstream: " + strings.Join(failures, "; ")
	}
	return sr
}

// ─── state on disk ───────────────────────────────────────────────────────────

func serviceDir(root DataRoot, svc string) string {
	if svc == "valhalla" {
		return root.ValhallaDir
	}
	return root.OverpassDir
}

// loadedRegions reads the region list from a service's .regions stamp file.
func loadedRegions(root DataRoot, svc string) []string {
	b, err := os.ReadFile(filepath.Join(serviceDir(root, svc), ".regions"))
	if err != nil {
		return nil
	}
	var out []string
	for _, r := range strings.Split(strings.TrimSpace(string(b)), ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

func upstreamStampPath(pbfDir, region string) string {
	return filepath.Join(pbfDir, pbfFilename(region)+".upstream")
}

// writeUpstreamStamp records the upstream Last-Modified of the PBF held for region.
func writeUpstreamStamp(pbfDir, region string, lastModified time.Time) error {
	return os.WriteFile(upstreamStampPath(pbfDir, region),
		[]byte(lastModified.UTC().Format(time.RFC3339)), 0o644)
}

// readUpstreamStamp returns the stamp for region; ok is false if missing or unreadable.
func readUpstreamStamp(pbfDir, region string) (time.Time, bool) {
	b, err := os.ReadFile(upstreamStampPath(pbfDir, region))
	if err != nil {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

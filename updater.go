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
	// Download fetches a region's current extract to destPath, reporting bytes
	// to fp, and returns the upstream Last-Modified of the version it fetched.
	// destPath is only ever replaced by a complete file. On error it keeps any
	// partial download beside destPath so the next attempt can resume it. It
	// does not write the upstream stamp; the updater does that once the
	// refresh has succeeded.
	Download(ctx context.Context, region, destPath string, fp FileDownloadProgress) (UpstreamInfo, error)
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

type namedDir struct{ label, dir string }

// derivedCaches lists the configured derived Overpass caches, labelled for
// messages.
func (r DataRoot) derivedCaches() []namedDir {
	var out []namedDir
	if r.TileCacheDir != "" {
		out = append(out, namedDir{"tile cache", r.TileCacheDir})
	}
	if r.ScoreCacheDir != "" {
		out = append(out, namedDir{"score cache", r.ScoreCacheDir})
	}
	return out
}

// UpdateDeps bundles everything the updater touches outside itself.
type UpdateDeps struct {
	Upstream UpstreamClient
	// Progress reports region downloads (nil = silent).
	Progress  DownloadProgress
	Runtime   ContainerRuntime
	Root      DataRoot
	Clock     func() time.Time
	FreeSpace func(path string) (uint64, error)
	// PBFDataTimestamp returns the osmosis_replication_timestamp from a PBF
	// header (the true age of the data it holds). ok is false if absent.
	PBFDataTimestamp func(pbfPath string) (ts time.Time, ok bool, err error)
	// ValhallaPort is the host port of the Valhalla container (0 = default).
	ValhallaPort int
	// OverpassPort is the host port of the live Overpass container (0 = default).
	OverpassPort int
	// ReadyTimeout bounds each readiness probe (0 = the per-probe default).
	ReadyTimeout time.Duration

	// Test seams: production leaves these zero. They let tests (including the
	// Docker end-to-end test) run against throwaway containers, ports and a
	// stubbed converter.

	// OverpassStagingPort is the spare host port of the temporary staging
	// container used by the staged import (0 = default).
	OverpassStagingPort int
	// OverpassContainerName names the live Overpass container; the staging
	// container is that name plus "-staging" (empty = default).
	OverpassContainerName string
	// ConvertOverpass rebuilds merged.osm.bz2 from the region PBFs for the given
	// regions (nil = the real incremental conversion).
	ConvertOverpass func(ctx context.Context, regions []string) error
	// ConvertOverpassRegion converts one region's PBF into its cache as soon as
	// that region has downloaded, ahead of the merge (nil = the real
	// conversion).
	ConvertOverpassRegion func(ctx context.Context, region string) error

	// Logf receives verbose progress lines (nil = silent).
	Logf func(format string, args ...any)
}

// logf writes a verbose progress line if logging is enabled.
func (d UpdateDeps) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
	}
}

// overpassNames returns the live and staging Overpass container names.
func (d UpdateDeps) overpassNames() (live, staging string) {
	live = d.OverpassContainerName
	if live == "" {
		live = overpassContainerName
	}
	return live, live + "-staging"
}

// Service names one of the two local data services twisty keeps fresh.
type Service string

const (
	ServiceValhalla Service = "valhalla"
	ServiceOverpass Service = "overpass"
)

// allServices is every service, in the order `update` refreshes them: the
// quick one (Valhalla) first.
var allServices = []Service{ServiceValhalla, ServiceOverpass}

// ─── options and results ─────────────────────────────────────────────────────

// UpdateOptions are the user-facing flags of `twisty update`.
type UpdateOptions struct {
	Only    string // "", "overpass" or "valhalla" (the raw flag value)
	Force   bool
	DryRun  bool
	InPlace bool
	Verbose bool // log each step with timings to stderr
}

type Outcome string

const (
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeChanged means upstream has newer data for at least one region and
	// the service was not refreshed (a dry run).
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
	Service Service
	Outcome Outcome
	Detail  string
	Regions []RegionStatus
}

// changedRegions lists the regions of this service that need refreshing.
func (sr ServiceReport) changedRegions() []string {
	var out []string
	for _, r := range sr.Regions {
		if r.Changed {
			out = append(out, r.Region)
		}
	}
	return out
}

// changedStatuses lists the statuses of the regions that need refreshing.
func (sr ServiceReport) changedStatuses() []RegionStatus {
	var out []RegionStatus
	for _, r := range sr.Regions {
		if r.Changed {
			out = append(out, r)
		}
	}
	return out
}

// finish records the result of a refresh: failed with the error, or updated
// with detail (what was cleared, plus any warnings).
func (sr *ServiceReport) finish(detail string, err error) {
	if err != nil {
		sr.Outcome = OutcomeFailed
		sr.Detail = err.Error()
		return
	}
	sr.Outcome = OutcomeUpdated
	sr.Detail = detail
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
	services := allServices
	switch Service(opts.Only) {
	case "":
	case ServiceValhalla, ServiceOverpass:
		services = []Service{Service(opts.Only)}
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
		deps.logf("checking service %s", svc)
		rep.Services = append(rep.Services, checkService(ctx, deps, opts, svc))
	}
	deps.logf("running disk preflight")
	runPreflight(deps, opts, &rep)
	deps.logf("preflight done")

	if !opts.DryRun {
		run := newUpdateRun(deps, rep.Services)
		for i := range rep.Services {
			sr := &rep.Services[i]
			if sr.Outcome != OutcomeChanged {
				continue
			}
			switch {
			case sr.Service == ServiceValhalla:
				sr.finish(run.refreshValhalla(ctx, sr))
			case sr.Service == ServiceOverpass && opts.InPlace:
				sr.finish(run.refreshOverpassInPlace(ctx, sr))
			case sr.Service == ServiceOverpass:
				sr.finish(run.refreshOverpassStaged(ctx, sr))
			}
		}
		run.finishDownloads()
		run.stampRefreshed(rep.Services)
	}
	return rep, nil
}

func checkService(ctx context.Context, deps UpdateDeps, opts UpdateOptions, svc Service) ServiceReport {
	sr := ServiceReport{Service: svc, Outcome: OutcomeUnchanged}
	regions := loadedRegions(deps.Root, svc)
	deps.logf("%s: loaded regions %v (from %s)", svc, regions, filepath.Join(serviceDir(deps.Root, svc), ".regions"))
	if len(regions) == 0 {
		sr.Detail = "no regions loaded"
		return sr
	}
	var failures []string
	for _, region := range regions {
		rs := RegionStatus{Region: region}
		deps.logf("%s/%s: HEAD upstream", svc, region)
		info, err := deps.Upstream.Head(ctx, region)
		deps.logf("%s/%s: HEAD returned (err=%v)", svc, region, err)
		if err != nil {
			rs.Err = err
			failures = append(failures, fmt.Sprintf("%s: %v", region, err))
			sr.Regions = append(sr.Regions, rs)
			continue
		}
		rs.Upstream = info.LastModified
		rs.ContentLength = info.ContentLength
		held, hasStamp := readServiceStamp(deps.Root.PBFDir, svc, region)
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
		pbfPath := filepath.Join(deps.Root.PBFDir, pbfFilename(region))
		deps.logf("%s/%s: reading PBF header %s", svc, region, pbfPath)
		ts, ok, err := deps.PBFDataTimestamp(pbfPath)
		deps.logf("%s/%s: PBF header read (ok=%v err=%v)", svc, region, ok, err)
		if err == nil && ok {
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

func serviceDir(root DataRoot, svc Service) string {
	if svc == ServiceValhalla {
		return root.ValhallaDir
	}
	return root.OverpassDir
}

// loadedRegions reads the region list from a service's .regions stamp file.
func loadedRegions(root DataRoot, svc Service) []string {
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

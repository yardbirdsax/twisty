package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/yardbirdsax/twisty/osmconv"
)

// UpdateLocations are the user-chosen directories of `twisty update`; empty
// means the default under ~/.twisty.
type UpdateLocations struct {
	// DataDir is the base directory holding the services' data: Overpass under
	// <DataDir>/overpass and Valhalla under <DataDir>/valhalla, i.e. what the
	// `start` commands' --data-dir would be for each service. The shared PBF
	// directory (and so the update lock) is not moved by it, as for `start`.
	DataDir string
	// CacheDir is the Overpass tile cache directory (as for `score --cache-dir`);
	// the score cache lives beside it in a sibling "scores" directory.
	CacheDir string
}

// updateRunner executes an update with the given options and locations, writing
// user-facing output to out. The production runner wires real dependencies;
// tests inject a capturing runner to check flag mapping.
type updateRunner func(ctx context.Context, opts UpdateOptions, loc UpdateLocations, out io.Writer) error

func newUpdateCmd() *cobra.Command {
	return newUpdateCmdWith(runUpdate)
}

func newUpdateCmdWith(run updateRunner) *cobra.Command {
	var opts UpdateOptions
	var loc UpdateLocations
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Refresh the local Overpass and Valhalla data for the regions already loaded",
		Long: "Checks Geofabrik for each region already loaded by `overpass start` / " +
			"`valhalla start` and reports which changed. Never adds regions; use " +
			"`start --regions` for that.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), opts, loc, cmd.ErrOrStderr())
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Only, "only", "", "Limit to one service: overpass or valhalla")
	f.BoolVar(&opts.Force, "force", false, "Treat every region as changed, skipping the upstream comparison")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Report what would happen without modifying anything")
	f.BoolVar(&opts.InPlace, "in-place", false, "Re-import Overpass in place (full downtime) instead of staging")
	f.StringVar(&loc.CacheDir, "cache-dir", "", "Overpass tile cache directory to clear after an Overpass refresh (default: ~/.twisty/cache/overpass/)")
	cmd.PersistentFlags().StringVar(&loc.DataDir, "data-dir", "", "Base directory holding the overpass/ and valhalla/ data directories (default: ~/.twisty)")
	cmd.AddCommand(newUpdateBackfillCmd(&loc))
	return cmd
}

// runUpdate is the production updateRunner.
func runUpdate(ctx context.Context, opts UpdateOptions, loc UpdateLocations, out io.Writer) error {
	return runUpdateWith(ctx, productionUpdateDepsAt(loc), opts, out)
}

// runUpdateWith runs the update with injected dependencies, writes the report
// and a closing per-service summary to out, and returns a non-nil error if any
// service failed (so the process exits non-zero).
func runUpdateWith(ctx context.Context, deps UpdateDeps, opts UpdateOptions, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rep, err := Update(ctx, deps, opts)
	if err != nil {
		return err
	}
	renderUpdateReport(out, rep, opts)
	renderUpdateSummary(out, rep)
	if rep.Failed() {
		return fmt.Errorf("update failed")
	}
	return nil
}

// renderUpdateSummary lists each service's final outcome, one per line.
func renderUpdateSummary(w io.Writer, rep UpdateReport) {
	fmt.Fprintln(w, "Summary:")
	for _, s := range rep.Services {
		fmt.Fprintf(w, "  %s: %s\n", s.Service, s.Outcome)
	}
}

// dataRootFor resolves where twisty's state lives for the given locations.
func dataRootFor(loc UpdateLocations) DataRoot {
	root := DataRoot{
		OverpassDir: resolveOverpassDataDir(""),
		ValhallaDir: resolveValhallaDataDir(""),
		PBFDir:      resolvePBFDir(""),

		TileCacheDir:  defaultTileCacheDir(),
		ScoreCacheDir: defaultScoreCacheDir(),
	}
	if loc.DataDir != "" {
		root.OverpassDir = filepath.Join(loc.DataDir, "overpass")
		root.ValhallaDir = filepath.Join(loc.DataDir, "valhalla")
	}
	if loc.CacheDir != "" {
		root.TileCacheDir = loc.CacheDir
		root.ScoreCacheDir = filepath.Join(filepath.Dir(loc.CacheDir), "scores")
	}
	return root
}

func productionUpdateDepsAt(loc UpdateLocations) UpdateDeps {
	return UpdateDeps{
		Upstream:         geofabrikUpstream{client: http.DefaultClient, progress: newDownloadProgress(os.Stderr)},
		Runtime:          dockerRuntime{},
		Root:             dataRootFor(loc),
		Clock:            time.Now,
		FreeSpace:        diskFreeBytes,
		PBFDataTimestamp: readPBFDataTimestamp,
	}
}

// geofabrikUpstream is the production UpstreamClient. baseURL defaults to
// Geofabrik; progress (optional) reports download progress.
type geofabrikUpstream struct {
	client   *http.Client
	baseURL  string
	progress DownloadProgress
}

func (g geofabrikUpstream) url(region string) string {
	base := g.baseURL
	if base == "" {
		base = geofabrikBaseURL
	}
	return geofabrikPBFURL(base, region)
}

func (g geofabrikUpstream) Download(ctx context.Context, region, destPath string) (UpstreamInfo, error) {
	var fp FileDownloadProgress = NoopFileDownloadProgress{}
	if g.progress != nil {
		fp = g.progress.StartFile(destPath, 0, 1, -1)
	}
	h, err := fetchToFile(ctx, g.client, g.url(region), destPath, fp)
	if err != nil {
		return UpstreamInfo{}, err
	}
	fp.FileComplete()
	lm, err := http.ParseTime(h.Get("Last-Modified"))
	if err != nil {
		// No usable header: the zero time makes the region count as changed
		// again on the next run, the safe direction.
		return UpstreamInfo{}, nil
	}
	return UpstreamInfo{LastModified: lm.UTC()}, nil
}

func (g geofabrikUpstream) Head(ctx context.Context, region string) (UpstreamInfo, error) {
	url := g.url(region)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return UpstreamInfo{}, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return UpstreamInfo{}, fmt.Errorf("HEAD %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpstreamInfo{}, fmt.Errorf("HEAD %s: unexpected status %s", url, resp.Status)
	}
	lm, err := http.ParseTime(resp.Header.Get("Last-Modified"))
	if err != nil {
		return UpstreamInfo{}, fmt.Errorf("HEAD %s: bad Last-Modified %q: %w", url, resp.Header.Get("Last-Modified"), err)
	}
	return UpstreamInfo{LastModified: lm.UTC(), ContentLength: resp.ContentLength}, nil
}

func diskFreeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

func readPBFDataTimestamp(path string) (time.Time, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false, err
	}
	defer f.Close()
	return osmconv.ReadPBFHeaderTimestamp(f)
}

// ─── output ──────────────────────────────────────────────────────────────────

func renderUpdateReport(w io.Writer, rep UpdateReport, opts UpdateOptions) {
	anyChanged, anyUpdated := false, false
	for _, s := range rep.Services {
		fmt.Fprintf(w, "%s: %s", s.Service, s.Outcome)
		if s.Detail != "" {
			fmt.Fprintf(w, " (%s)", s.Detail)
		}
		fmt.Fprintln(w)
		for _, r := range s.Regions {
			verdict := "unchanged"
			if r.Changed {
				verdict = "changed (" + r.Reason + ")"
			}
			if r.Err != nil {
				verdict = "error: " + r.Err.Error()
			}
			fmt.Fprintf(w, "  %-40s %s", r.Region, verdict)
			if r.HasDataAge {
				fmt.Fprintf(w, ", data age %s", formatAge(r.DataAge))
			}
			fmt.Fprintln(w)
		}
		if s.Outcome == OutcomeChanged {
			anyChanged = true
		}
		if s.Outcome == OutcomeUpdated {
			anyUpdated = true
		}
	}
	renderPreflight(w, rep.Preflight)
	switch {
	case opts.DryRun:
		fmt.Fprintln(w, "dry run: nothing was modified")
	case !anyChanged && !anyUpdated && !rep.Failed():
		fmt.Fprintln(w, "Nothing to update.")
	}
}

// formatAge renders a duration as e.g. "1d12h" or "5h".
func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	hours := int(d.Hours())
	if hours >= 24 {
		return strconv.Itoa(hours/24) + "d" + strconv.Itoa(hours%24) + "h"
	}
	return strconv.Itoa(hours) + "h"
}

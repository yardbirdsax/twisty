package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// dbSafetyMarginPercent is the headroom applied to the current Overpass
// database size when estimating the size of its replacement.
const dbSafetyMarginPercent = 115

// ReclaimItem is a derived-data path the preflight may delete to make room.
type ReclaimItem struct {
	Path string
	// What says what the path holds, for reports: "Overpass tile cache".
	What string
	Size int64
}

// PreflightReport is the outcome of the disk-space check.
type PreflightReport struct {
	Needed uint64 // estimated bytes the refresh requires
	Free   uint64 // free bytes before any reclaiming
	// Fits is true if Needed fits in Free, after reclaiming if necessary.
	Fits bool
	// Shortfall is how many bytes are still missing after reclaiming everything
	// reclaimable (zero when Fits).
	Shortfall uint64
	// Warn is set when a shortfall is only reported, not enforced (--in-place).
	Warn bool
	// Planned lists what would be (or was) reclaimed to make the estimate fit.
	Planned []ReclaimItem
	// Reclaimed lists what was actually deleted (empty on --dry-run).
	Reclaimed []ReclaimItem
	// SuggestInPlace is set when --in-place would help: Overpass is being
	// refreshed in staged mode, so a second copy of its database is part of
	// the estimate. It is not true for a Valhalla-only shortfall.
	SuggestInPlace bool
}

func defaultTileCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".twisty", "cache", "overpass")
}

func defaultScoreCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".twisty", "cache", "scores")
}

// dirSize returns the total size of regular files under path (or the size of
// path itself if it is a file). Missing paths count as zero.
func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// runPreflight estimates the disk space the refresh needs, reclaims derived
// data if (and only if) that makes it fit, and records the result on rep. If it
// cannot fit even after reclaiming, the changed services are marked failed with
// the exact shortfall. Nothing is deleted when opts.DryRun is set, or when
// reclaiming could not make the update fit anyway. Live databases, live tiles
// and region PBFs are never candidates.
func runPreflight(deps UpdateDeps, opts UpdateOptions, rep *UpdateReport) {
	changed := map[Service]bool{}
	downloads := map[string]int64{} // region -> bytes, deduped across services
	changedOverpassRegions := []string{}
	for _, s := range rep.Services {
		if s.Outcome != OutcomeChanged {
			continue
		}
		changed[s.Service] = true
		for _, r := range s.Regions {
			if r.Changed {
				downloads[r.Region] = r.ContentLength
				if s.Service == ServiceOverpass {
					changedOverpassRegions = append(changedOverpassRegions, r.Region)
				}
			}
		}
	}
	if len(changed) == 0 || deps.FreeSpace == nil {
		return
	}
	root := deps.Root

	var need int64
	for _, n := range downloads {
		need += n
	}
	var reclaimable []ReclaimItem // in reclaim order
	addReclaim := func(path, what string) {
		if path == "" {
			return
		}
		deps.logf("preflight: sizing %s", path)
		size := dirSize(path)
		deps.logf("preflight: %s = %d bytes", path, size)
		if size > 0 {
			reclaimable = append(reclaimable, ReclaimItem{Path: path, What: what, Size: size})
		}
	}

	if changed[ServiceOverpass] {
		if !opts.InPlace {
			need += dirSize(filepath.Join(root.OverpassDir, "db")) * dbSafetyMarginPercent / 100
		}
		merged := filepath.Join(root.OverpassDir, "merged.osm.bz2")
		need += dirSize(merged)
		// Order: tile+score caches, old merged archive, conversion caches of
		// changed regions (Valhalla's merged PBF slots in before them below).
		addReclaim(root.TileCacheDir, "Overpass tile cache")
		addReclaim(root.ScoreCacheDir, "Overpass score cache")
		addReclaim(merged, "old Overpass merged archive")
	}
	if changed[ServiceValhalla] {
		mergedPBF := filepath.Join(root.ValhallaDir, "merged.osm.pbf")
		need += dirSize(mergedPBF) + dirSize(filepath.Join(root.ValhallaDir, "tiles"))
		addReclaim(mergedPBF, "old Valhalla merged PBF")
	}
	for _, region := range changedOverpassRegions {
		addReclaim(filepath.Join(root.OverpassDir, "cache", cacheFilename(region)), "Overpass conversion cache for "+region)
		addReclaim(filepath.Join(root.OverpassDir, "cache", cacheFilename(region)+".meta.json"), "Overpass conversion cache metadata for "+region)
	}

	freeDir := root.OverpassDir
	if !changed[ServiceOverpass] {
		freeDir = root.ValhallaDir
	}
	deps.logf("preflight: statfs %s", freeDir)
	free, err := deps.FreeSpace(freeDir)
	if err != nil {
		failChanged(rep, fmt.Sprintf("disk preflight: checking free space: %v", err))
		return
	}

	pf := &PreflightReport{Needed: uint64(need), Free: free,
		SuggestInPlace: changed[ServiceOverpass] && !opts.InPlace}
	rep.Preflight = pf

	avail := free
	for _, item := range reclaimable {
		if avail >= pf.Needed {
			break
		}
		pf.Planned = append(pf.Planned, item)
		avail += uint64(item.Size)
	}
	if avail < pf.Needed {
		pf.Shortfall = pf.Needed - avail
		pf.Planned = nil // reclaiming cannot make it fit, so reclaim nothing
		if opts.InPlace {
			// The user chose the low-disk path: report the shortfall, don't abort.
			pf.Warn = true
			return
		}
		if opts.DryRun {
			return
		}
		hint := ""
		if pf.SuggestInPlace {
			hint = " or re-run with --in-place"
		}
		failChanged(rep, fmt.Sprintf(
			"disk preflight: need %d bytes, have %d free; short by %d bytes even after reclaiming derived data; "+
				"free up space%s", pf.Needed, free, pf.Shortfall, hint))
		return
	}
	pf.Fits = true
	if opts.DryRun {
		return
	}
	for _, item := range pf.Planned {
		if err := os.RemoveAll(item.Path); err != nil {
			pf.Fits = false
			failChanged(rep, fmt.Sprintf("disk preflight: reclaiming %s: %v", item.Path, err))
			return
		}
		pf.Reclaimed = append(pf.Reclaimed, item)
	}
}

func failChanged(rep *UpdateReport, detail string) {
	for i := range rep.Services {
		if rep.Services[i].Outcome == OutcomeChanged {
			rep.Services[i].Outcome = OutcomeFailed
			rep.Services[i].Detail = detail
		}
	}
}

func renderPreflight(w io.Writer, pf *PreflightReport) {
	if pf == nil {
		return
	}
	fmt.Fprintf(w, "disk: need %d bytes, free %d bytes\n", pf.Needed, pf.Free)
	for _, item := range pf.Planned {
		verb := "would reclaim"
		for _, r := range pf.Reclaimed {
			if r.Path == item.Path {
				verb = "reclaimed"
			}
		}
		fmt.Fprintf(w, "  %s %s %s (%d bytes)\n", verb, item.What, item.Path, item.Size)
	}
	if !pf.Fits && pf.Warn {
		fmt.Fprintf(w, "  warning: short by %d bytes even after reclaiming; continuing because --in-place was given\n", pf.Shortfall)
	} else if !pf.Fits {
		hint := ""
		if pf.SuggestInPlace {
			hint = " or use --in-place"
		}
		fmt.Fprintf(w, "  short by %d bytes even after reclaiming; free up space%s\n", pf.Shortfall, hint)
	}
}

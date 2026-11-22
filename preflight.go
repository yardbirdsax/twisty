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
	// Planned lists what would be (or was) reclaimed to make the estimate fit.
	Planned []ReclaimItem
	// Reclaimed lists what was actually deleted (empty on --dry-run).
	Reclaimed []ReclaimItem
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
	changed := map[string]bool{}
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
				if s.Service == "overpass" {
					changedOverpassRegions = append(changedOverpassRegions, r.Region)
				}
			}
		}
	}
	if len(changed) == 0 {
		return
	}
	root := deps.Root

	var need int64
	for _, n := range downloads {
		need += n
	}
	var reclaimable []ReclaimItem // in reclaim order
	addReclaim := func(path string) {
		if path == "" {
			return
		}
		if size := dirSize(path); size > 0 {
			reclaimable = append(reclaimable, ReclaimItem{Path: path, Size: size})
		}
	}

	if changed["overpass"] {
		if !opts.InPlace {
			need += dirSize(filepath.Join(root.OverpassDir, "db")) * dbSafetyMarginPercent / 100
		}
		merged := filepath.Join(root.OverpassDir, "merged.osm.bz2")
		need += dirSize(merged)
		// Order: tile+score caches, old merged archive, conversion caches of
		// changed regions (Valhalla's merged PBF slots in before them below).
		addReclaim(root.TileCacheDir)
		addReclaim(root.ScoreCacheDir)
		addReclaim(merged)
	}
	if changed["valhalla"] {
		mergedPBF := filepath.Join(root.ValhallaDir, "merged.osm.pbf")
		need += dirSize(mergedPBF) + dirSize(filepath.Join(root.ValhallaDir, "tiles"))
		addReclaim(mergedPBF)
	}
	for _, region := range changedOverpassRegions {
		addReclaim(filepath.Join(root.OverpassDir, "cache", cacheFilename(region)))
		addReclaim(filepath.Join(root.OverpassDir, "cache", cacheFilename(region)+".meta.json"))
	}

	freeDir := root.OverpassDir
	if !changed["overpass"] {
		freeDir = root.ValhallaDir
	}
	free, err := deps.FreeSpace(freeDir)
	if err != nil {
		failChanged(rep, fmt.Sprintf("disk preflight: checking free space: %v", err))
		return
	}

	pf := &PreflightReport{Needed: uint64(need), Free: free}
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
		if opts.DryRun {
			return
		}
		failChanged(rep, fmt.Sprintf(
			"disk preflight: need %d bytes, have %d free; short by %d bytes even after reclaiming derived data; "+
				"free up space or re-run with --in-place", pf.Needed, free, pf.Shortfall))
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
		fmt.Fprintf(w, "  %s %s (%d bytes)\n", verb, item.Path, item.Size)
	}
	if !pf.Fits {
		fmt.Fprintf(w, "  short by %d bytes even after reclaiming; free up space or use --in-place\n", pf.Shortfall)
	}
}

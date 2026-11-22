package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// recordUpstreamStampForDownload writes the upstream Last-Modified of a freshly
// downloaded PBF to the stamp file beside it (the same file readUpstreamStamp
// reads for the region). A response without a parsable Last-Modified writes
// nothing, so the region counts as changed on the next update.
func recordUpstreamStampForDownload(destPath string, h http.Header) error {
	lm, err := http.ParseTime(h.Get("Last-Modified"))
	if err != nil {
		// No usable header: drop any stale stamp for the replaced file.
		if rmErr := os.Remove(destPath + ".upstream"); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
		return nil
	}
	return os.WriteFile(destPath+".upstream", []byte(lm.UTC().Format(time.RFC3339)), 0o644)
}

// BackfillUpstreamStamps is the one-off migration for installs that predate
// stamps: for every loaded region whose PBF exists but has no stamp, it seeds
// the stamp from the PBF's modification time. Safe because mtime is when the
// download finished, which is later than the upstream Last-Modified of that
// version. Existing stamps are never overwritten. Returns the regions seeded.
func BackfillUpstreamStamps(root DataRoot) ([]string, error) {
	set := map[string]struct{}{}
	for _, svc := range []string{"overpass", "valhalla"} {
		for _, r := range loadedRegions(root, svc) {
			set[r] = struct{}{}
		}
	}
	regions := make([]string, 0, len(set))
	for r := range set {
		regions = append(regions, r)
	}
	sort.Strings(regions)

	var seeded []string
	for _, region := range regions {
		if _, ok := readUpstreamStamp(root.PBFDir, region); ok {
			continue
		}
		fi, err := os.Stat(filepath.Join(root.PBFDir, pbfFilename(region)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return seeded, fmt.Errorf("stat PBF for %s: %w", region, err)
		}
		if err := writeUpstreamStamp(root.PBFDir, region, fi.ModTime()); err != nil {
			return seeded, fmt.Errorf("writing stamp for %s: %w", region, err)
		}
		seeded = append(seeded, region)
	}
	return seeded, nil
}

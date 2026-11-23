package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Upstream stamps record which upstream (Geofabrik) version of a region's
// extract is held. There are two kinds, both files beside the region PBF:
//
//   - the download stamp, "<pbf>.upstream", written when a PBF is downloaded by
//     `start` (the version of the file on disk);
//   - one service stamp per service, "<pbf>.upstream.<service>", written by
//     `update` once that service was rebuilt from the version (the version the
//     service actually serves). Each service tracks its own, so refreshing one
//     never hides staleness from the other, and a service that succeeded is not
//     rebuilt just because the other failed.
//
// A service with no stamp file of its own falls back to the download stamp,
// which is how installs that predate per-service stamps keep working.

// unknownStamp is the content of a service stamp for a download that carried
// no usable Last-Modified: it reads as "no stamp" and blocks the fallback to
// the download stamp, so the region counts as changed on the next update.
const unknownStamp = "unknown"

func upstreamStampPath(pbfDir, region string) string {
	return filepath.Join(pbfDir, pbfFilename(region)+".upstream")
}

func serviceStampPath(pbfDir string, svc Service, region string) string {
	return upstreamStampPath(pbfDir, region) + "." + string(svc)
}

func parseStamp(b []byte) (time.Time, bool) {
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// readUpstreamStamp returns the download stamp for region; ok is false if
// missing or unreadable.
func readUpstreamStamp(pbfDir, region string) (time.Time, bool) {
	b, err := os.ReadFile(upstreamStampPath(pbfDir, region))
	if err != nil {
		return time.Time{}, false
	}
	return parseStamp(b)
}

// hasServiceStamp reports whether svc has a stamp file of its own for region
// (even an "unknown" one).
func hasServiceStamp(pbfDir string, svc Service, region string) bool {
	_, err := os.Stat(serviceStampPath(pbfDir, svc, region))
	return err == nil
}

// readServiceStamp returns the upstream version svc holds for region: its own
// stamp, or the download stamp when it has none. ok is false if neither is
// usable.
func readServiceStamp(pbfDir string, svc Service, region string) (time.Time, bool) {
	b, err := os.ReadFile(serviceStampPath(pbfDir, svc, region))
	if err != nil {
		if os.IsNotExist(err) {
			return readUpstreamStamp(pbfDir, region)
		}
		return time.Time{}, false
	}
	return parseStamp(b)
}

// writeServiceStamp records the upstream Last-Modified of the version svc now
// holds for region. A zero lastModified records "unknown".
func writeServiceStamp(pbfDir string, svc Service, region string, lastModified time.Time) error {
	content := unknownStamp
	if !lastModified.IsZero() {
		content = lastModified.UTC().Format(time.RFC3339)
	}
	return os.WriteFile(serviceStampPath(pbfDir, svc, region), []byte(content), 0o644)
}

// recordUpstreamStampForDownload writes the upstream Last-Modified of a freshly
// downloaded PBF to the download stamp beside it. A response without a
// parsable Last-Modified writes nothing, so the region counts as changed on the
// next update.
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
// stamps: for every loaded region whose PBF exists and for which a service has
// no stamp at all (neither its own nor a download stamp), it seeds that
// service's stamp from the PBF's modification time. Safe because mtime is when
// the download finished, which is later than the upstream Last-Modified of
// that version. Existing stamps are never overwritten. Returns the regions
// seeded for at least one service.
//
// Caveat: a stamp is a claim about the data a service serves, and backfill
// cannot verify it. After an interrupted update, new PBFs exist beside old
// stamps; backfill leaves those stamps alone, which is the safe direction.
func BackfillUpstreamStamps(root DataRoot) ([]string, error) {
	loaders := map[string][]Service{}
	for _, svc := range allServices {
		for _, r := range loadedRegions(root, svc) {
			loaders[r] = append(loaders[r], svc)
		}
	}
	regions := make([]string, 0, len(loaders))
	for r := range loaders {
		regions = append(regions, r)
	}
	sort.Strings(regions)

	var seeded []string
	for _, region := range regions {
		var fi os.FileInfo
		didSeed := false
		for _, svc := range loaders[region] {
			if hasServiceStamp(root.PBFDir, svc, region) {
				continue
			}
			if _, ok := readUpstreamStamp(root.PBFDir, region); ok {
				continue
			}
			if fi == nil {
				var err error
				fi, err = os.Stat(filepath.Join(root.PBFDir, pbfFilename(region)))
				if err != nil {
					if os.IsNotExist(err) {
						break
					}
					return seeded, fmt.Errorf("stat PBF for %s: %w", region, err)
				}
			}
			if err := writeServiceStamp(root.PBFDir, svc, region, fi.ModTime()); err != nil {
				return seeded, fmt.Errorf("writing stamp for %s: %w", region, err)
			}
			didSeed = true
		}
		if didSeed {
			seeded = append(seeded, region)
		}
	}
	return seeded, nil
}

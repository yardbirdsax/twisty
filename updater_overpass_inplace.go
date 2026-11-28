package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yardbirdsax/twisty/osmconv"
)

const defaultOverpassReadyTimeout = 30 * time.Minute

// refreshOverpassInPlace refreshes Overpass by stopping it, wiping the
// database and re-importing from the refreshed data: downtime for the whole
// import, but no second copy of the database on disk. It runs only when the
// user asked for it with --in-place. The re-import changed the data, so derived
// caches are cleared afterwards; the returned detail says what was cleared and
// carries warnings about cleanup that failed after the new data went live.
func (u *updateRun) refreshOverpassInPlace(ctx context.Context, sr *ServiceReport) (string, error) {
	deps := u.deps
	rt := deps.Runtime
	liveName, _ := deps.overpassNames()

	// Download and convert first, while the old database still serves, so a
	// failure here costs no downtime and loses nothing.
	if err := u.prepareOverpassData(ctx, sr); err != nil {
		return "", err
	}
	p, err := newOverpassPaths(deps.Root.OverpassDir)
	if err != nil {
		return "", atStage(stageImport, err)
	}
	port := deps.overpassPort()

	// Point of no return. Stop, wipe, and let the fresh container import.
	if err := rt.Stop(ctx, liveName); err != nil {
		return "", atStage(stageImport, err)
	}
	if err := rt.Remove(ctx, liveName); err != nil {
		return "", atStage(stageImport, err)
	}
	if err := os.RemoveAll(p.absLive); err != nil {
		return "", atStage(stageImport, fmt.Errorf("wiping database: %w", err))
	}
	if err := os.MkdirAll(p.absLive, 0o755); err != nil {
		return "", atStage(stageImport, err)
	}
	if err := rt.Run(ctx, overpassContainerSpec(liveName, port, p.absLive, p.absMerged)); err != nil {
		return "", atStage(stageImport, err)
	}
	if err := u.waitReady(ctx, overpassReadyURL(port), defaultOverpassReadyTimeout); err != nil {
		return "", atStage(stageImport, fmt.Errorf("%w (the database was wiped; re-run `twisty update --in-place` or `twisty overpass start`)", err))
	}

	// The per-service stamp is written by Update (see stampRefreshed).
	cleared, warnings := clearDerivedCaches(deps.Root)
	return joinDetail(cleared, warnings), nil
}

// convertOverpass rebuilds merged.osm.bz2 from the region PBFs.
func (u *updateRun) convertOverpass(ctx context.Context, regions []string) error {
	if u.deps.ConvertOverpass != nil {
		return u.deps.ConvertOverpass(ctx, regions)
	}
	root := u.deps.Root
	return convertRegionsIncremental(regions, root.OverpassDir, root.PBFDir, newConvertProgress(os.Stderr), os.Stderr)
}

// convertRegion converts one downloaded region into its cache ahead of the
// merge, unless the cache is already valid. When the whole conversion is
// stubbed, that stub owns conversion and nothing happens here.
func (u *updateRun) convertRegion(ctx context.Context, region string) error {
	if f := u.deps.ConvertOverpassRegion; f != nil {
		return f(ctx, region)
	}
	if u.deps.ConvertOverpass != nil {
		return nil
	}
	root := u.deps.Root
	pbfPath, cachePath, metaPath := regionCachePaths(root.OverpassDir, root.PBFDir, region)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	if isCacheValid(pbfPath, cachePath, metaPath) {
		return nil
	}
	// No per-region bar: it would overwrite the download bar still on screen.
	return convertRegionToCache(pbfPath, cachePath, metaPath, osmconv.NoopConvertProgress{})
}

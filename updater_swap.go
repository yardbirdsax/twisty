package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// ─── defaults for injected settings ──────────────────────────────────────────

func (d UpdateDeps) valhallaPort() int {
	if d.ValhallaPort == 0 {
		return defaultValhallaPort
	}
	return d.ValhallaPort
}

func (d UpdateDeps) overpassPort() int {
	if d.OverpassPort == 0 {
		return defaultOverpassPort
	}
	return d.OverpassPort
}

func (d UpdateDeps) overpassStagingPort() int {
	if d.OverpassStagingPort == 0 {
		return defaultOverpassStagingPort
	}
	return d.OverpassStagingPort
}

// readyTimeout is the bound for one readiness probe: the injected override, or
// fallback when none was given.
func (d UpdateDeps) readyTimeout(fallback time.Duration) time.Duration {
	if d.ReadyTimeout == 0 {
		return fallback
	}
	return d.ReadyTimeout
}

// waitReady polls url until it answers, bounded by the configured timeout (or
// fallback).
func (u *updateRun) waitReady(ctx context.Context, url string, fallback time.Duration) error {
	pctx, cancel := context.WithTimeout(ctx, u.deps.readyTimeout(fallback))
	defer cancel()
	return u.deps.Runtime.WaitReady(pctx, url)
}

// ─── directory swap ──────────────────────────────────────────────────────────

// dirSwap replaces a service's live data directory with a freshly built
// staging directory under a container that serves from it. It is the part of
// every Valhalla and staged Overpass refresh that must not lose data.
type dirSwap struct {
	container string // the live container's name
	live      string // the directory the container serves from
	staging   string // the fresh data, built beside live
	old       string // where the replaced data waits until the new data is proven
	noun      string // what the directory holds, for messages: "tiles", "database"
	spec      ContainerSpec
	// ready probes the (new, then if rolled back the restored) container.
	ready func(ctx context.Context) error
}

// restoreInterruptedSwap recovers from a crash that died between the two
// renames of a swap (live gone, old still there) by putting old back, then
// discards any leftover old directory.
func restoreInterruptedSwap(d dirSwap) error {
	if _, err := os.Stat(d.live); os.IsNotExist(err) {
		if _, oldErr := os.Stat(d.old); oldErr == nil {
			if err := os.Rename(d.old, d.live); err != nil {
				return fmt.Errorf("restoring %s left by an interrupted swap: %w", d.noun, err)
			}
		}
	}
	os.RemoveAll(d.old)
	return nil
}

// swap stops and removes the live container, moves live aside, moves staging
// into place, starts the new container and probes it. The replaced data is left
// at d.old for the caller to delete once it is satisfied. On any failure it
// restores the old data and (if the old container was running when the swap
// began) restarts it, discards staging, and returns an error saying whether the
// rollback was clean.
func (u *updateRun) swap(ctx context.Context, d dirSwap) error {
	rt := u.deps.Runtime
	// Sampled here, right before the container is stopped, not earlier: a
	// container that died during the (possibly hours-long) preceding stages is
	// not resurrected by a rollback.
	wasRunning := rt.IsRunning(d.container)
	movedOld, movedNew := false, false

	swapErr := func() error {
		if err := rt.Stop(ctx, d.container); err != nil {
			return atStage(stageSwap, err)
		}
		if err := rt.Remove(ctx, d.container); err != nil {
			return atStage(stageSwap, err)
		}
		if err := os.Rename(d.live, d.old); err != nil {
			return atStage(stageSwap, err)
		}
		movedOld = true
		if err := os.Rename(d.staging, d.live); err != nil {
			return atStage(stageSwap, err)
		}
		movedNew = true
		if err := rt.Run(ctx, d.spec); err != nil {
			return atStage(stageSwap, err)
		}
		return atStage(stageProbe, d.ready(ctx))
	}()
	if swapErr == nil {
		return nil
	}

	// Rollback uses a context that survives cancellation of ctx itself.
	rctx := context.WithoutCancel(ctx)
	var notes []string
	rt.Stop(rctx, d.container)   //nolint:errcheck
	rt.Remove(rctx, d.container) //nolint:errcheck
	if movedNew {
		if err := os.RemoveAll(d.live); err != nil {
			notes = append(notes, fmt.Sprintf("removing new %s: %v", d.noun, err))
		}
	}
	if movedOld {
		if err := os.Rename(d.old, d.live); err != nil {
			notes = append(notes, fmt.Sprintf("restoring old %s from %s: %v", d.noun, d.old, err))
		}
	}
	os.RemoveAll(d.staging)
	if wasRunning {
		if err := rt.Run(rctx, d.spec); err != nil {
			notes = append(notes, fmt.Sprintf("restarting old container: %v", err))
		} else if err := d.ready(rctx); err != nil {
			notes = append(notes, fmt.Sprintf("old container did not become ready: %v", err))
		}
	}
	if len(notes) > 0 {
		return fmt.Errorf("%w; rollback problems: %s", swapErr, strings.Join(notes, "; "))
	}
	return fmt.Errorf("%w (rolled back; old %s and container restored)", swapErr, d.noun)
}

// ─── post-success cleanup ────────────────────────────────────────────────────

// warning formats a non-fatal problem for a service's report detail.
func warning(format string, args ...any) string {
	return "warning: " + fmt.Sprintf(format, args...)
}

// joinDetail joins the non-empty parts of a report detail.
func joinDetail(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "; ")
}

// discardOld deletes the replaced data once the new data is live and proven.
// A failure cannot undo the update, so it is returned as a warning (empty on
// success), not an error.
func discardOld(old, noun string) string {
	if err := os.RemoveAll(old); err != nil {
		return warning("the update succeeded but the old %s could not be removed from %s: %v", noun, old, err)
	}
	return ""
}

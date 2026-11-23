package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A caller (the real-Docker e2e test) can rename the Overpass containers so the
// update never touches the default twisty-overpass containers.
func TestOverpassUpdate_CustomContainerNameIsUsedForLiveAndStaging(t *testing.T) {
	e := newStagedOverpassEnv(t, vermont)
	e.makeChanged(vermont)
	const custom = "twisty-e2e-custom"
	e.runtime.seedRunning(overpassContainerSpec(custom, defaultOverpassPort, e.liveDB, e.merged))
	e.runtime.OnRun = func(spec ContainerSpec) error {
		if spec.Name == custom+"-staging" {
			dir, _ := mountHostDir(spec, "/db")
			return os.WriteFile(filepath.Join(dir, "data"), []byte("new"), 0o644)
		}
		return nil
	}
	d := e.deps()
	d.OverpassContainerName = custom

	rep, err := Update(t.Context(), d, UpdateOptions{Only: "overpass"})
	if err != nil {
		t.Fatal(err)
	}
	if o := findService(t, rep, "overpass"); o.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %v (%s), want updated", o.Outcome, o.Detail)
	}
	for _, c := range e.runtime.calls {
		if c == "run "+overpassContainerName || c == "stop "+overpassContainerName || c == "rm "+overpassContainerName ||
			c == "run "+stagingName || c == "stop "+stagingName || c == "rm "+stagingName {
			t.Errorf("default container touched: %q (calls: %v)", c, e.runtime.calls)
		}
	}
	if !e.runtime.IsRunning(custom) {
		t.Errorf("custom live container not running; calls: %v", e.runtime.calls)
	}
}

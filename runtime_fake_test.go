package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// fakeRuntime is an in-memory ContainerRuntime for tests of any service's
// update (Valhalla now; the Overpass swap and rollback tickets reuse it).
//
// State: it tracks named containers like Docker does. Run fails if the name is
// already taken (so a missing Remove before Run shows up as an error), and
// Stop/Remove are idempotent, matching the ContainerRuntime contract.
//
// Observation: calls records every mutating call in order, as strings:
//
//	"run NAME", "stop NAME", "rm NAME", "oneshot IMAGE", "ready URL"
//
// IsRunning is a pure query and is not recorded.
//
// Fault and behavior injection: set the On* hooks. A hook runs before the
// fake's own bookkeeping; a non-nil error from it makes the call fail without
// changing state. Hooks may also act as the "container's work", for example
// writing files into a mounted host directory to simulate an import or build,
// or inspecting the filesystem to assert what the real container would see at
// that moment.
type fakeRuntime struct {
	calls []string

	containers map[string]ContainerSpec // existing containers (running or not)
	running    map[string]bool

	OnRun             func(spec ContainerSpec) error
	OnRunToCompletion func(spec ContainerSpec) error
	OnStop            func(name string) error
	OnRemove          func(name string) error
	OnWaitReady       func(url string) error
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{
		containers: map[string]ContainerSpec{},
		running:    map[string]bool{},
	}
}

// seedRunning registers a container that is already running before the update
// starts.
func (f *fakeRuntime) seedRunning(spec ContainerSpec) {
	f.containers[spec.Name] = spec
	f.running[spec.Name] = true
}

// spec returns the spec the named container was last started with.
func (f *fakeRuntime) spec(name string) (ContainerSpec, bool) {
	s, ok := f.containers[name]
	return s, ok
}

func (f *fakeRuntime) IsRunning(name string) bool { return f.running[name] }

func (f *fakeRuntime) Run(_ context.Context, spec ContainerSpec) error {
	f.calls = append(f.calls, "run "+spec.Name)
	if f.OnRun != nil {
		if err := f.OnRun(spec); err != nil {
			return err
		}
	}
	if _, taken := f.containers[spec.Name]; taken {
		return fmt.Errorf("fake runtime: container name %q already in use", spec.Name)
	}
	f.containers[spec.Name] = spec
	f.running[spec.Name] = true
	return nil
}

func (f *fakeRuntime) RunToCompletion(_ context.Context, spec ContainerSpec) error {
	f.calls = append(f.calls, "oneshot "+spec.Image)
	if f.OnRunToCompletion != nil {
		return f.OnRunToCompletion(spec)
	}
	return nil
}

func (f *fakeRuntime) Stop(_ context.Context, name string) error {
	f.calls = append(f.calls, "stop "+name)
	if f.OnStop != nil {
		if err := f.OnStop(name); err != nil {
			return err
		}
	}
	f.running[name] = false
	return nil
}

func (f *fakeRuntime) Remove(_ context.Context, name string) error {
	f.calls = append(f.calls, "rm "+name)
	if f.OnRemove != nil {
		if err := f.OnRemove(name); err != nil {
			return err
		}
	}
	delete(f.containers, name)
	delete(f.running, name)
	return nil
}

func (f *fakeRuntime) WaitReady(_ context.Context, url string) error {
	f.calls = append(f.calls, "ready "+url)
	if f.OnWaitReady != nil {
		return f.OnWaitReady(url)
	}
	return nil
}

// mountHostDir returns the host directory a spec mounts at containerPath.
func mountHostDir(spec ContainerSpec, containerPath string) (string, bool) {
	for _, m := range spec.Mounts {
		if m.Container == containerPath {
			return m.Host, true
		}
	}
	return "", false
}

// writeFakeValhallaTiles populates host dir with the minimal file set that
// tilesComplete accepts, tagging each tile with content so tests can tell
// which build produced them.
func writeFakeValhallaTiles(dir, content string) error {
	for _, level := range []string{"0", "1", "2"} {
		p := filepath.Join(dir, "tiles", level, "000", "000.gph")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "valhalla.json"), []byte("{}"), 0o644)
}

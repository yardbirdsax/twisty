package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// PortMapping publishes a container port on the host.
type PortMapping struct{ Host, Container int }

// Mount is a host directory or file bind-mounted into a container.
type Mount struct{ Host, Container string }

// ContainerSpec describes a container to run, independent of how it is run. It
// is the single description both the real Docker runtime and test fakes
// consume, and what a service's `start` and `update` paths share.
type ContainerSpec struct {
	Name    string // empty for one-shot containers
	Image   string
	Ports   []PortMapping
	Mounts  []Mount
	Restart string // docker restart policy, e.g. "unless-stopped"; empty for none
	Command []string
}

// dockerRunArgs renders the spec as `docker run` arguments: a detached named
// container, or (detached false) a one-shot that is removed when it exits.
func (s ContainerSpec) dockerRunArgs(detached bool) []string {
	args := []string{"run"}
	if detached {
		args = append(args, "-d")
	} else {
		args = append(args, "--rm")
	}
	if s.Name != "" {
		args = append(args, "--name", s.Name)
	}
	if s.Restart != "" {
		args = append(args, "--restart", s.Restart)
	}
	for _, p := range s.Ports {
		args = append(args, "-p", fmt.Sprintf("%d:%d", p.Host, p.Container))
	}
	for _, m := range s.Mounts {
		args = append(args, "-v", m.Host+":"+m.Container)
	}
	args = append(args, s.Image)
	return append(args, s.Command...)
}

// ContainerRuntime drives the Docker containers behind the services. It is
// service-agnostic: the Valhalla update uses it now, and the Overpass swap and
// rollback reuse it unchanged.
//
// Contract: Stop and Remove are idempotent (a container that does not exist is
// not an error). Run fails if the name is already in use, so callers must
// Remove a replaced container first.
type ContainerRuntime interface {
	// IsRunning reports whether the named container exists and is running.
	IsRunning(name string) bool
	// Run starts a detached, named container.
	Run(ctx context.Context, spec ContainerSpec) error
	// RunToCompletion runs a one-shot container (an import, build or merge)
	// and returns when it exits; a non-zero exit is an error.
	RunToCompletion(ctx context.Context, spec ContainerSpec) error
	Stop(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	// WaitReady polls url until it answers HTTP 200 or ctx ends.
	WaitReady(ctx context.Context, url string) error
}

// dockerRuntime is the production ContainerRuntime, over the docker CLI.
type dockerRuntime struct{}

func (dockerRuntime) IsRunning(name string) bool { return isContainerRunning(name) }

func (dockerRuntime) Run(ctx context.Context, spec ContainerSpec) error {
	out, err := exec.CommandContext(ctx, "docker", spec.dockerRunArgs(true)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %s: %v: %s", spec.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (dockerRuntime) RunToCompletion(ctx context.Context, spec ContainerSpec) error {
	cmd := exec.CommandContext(ctx, "docker", spec.dockerRunArgs(false)...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run %s: %w", spec.Image, err)
	}
	return nil
}

func (dockerRuntime) Stop(ctx context.Context, name string) error {
	return dockerIgnoreMissing(ctx, "stop", name)
}

func (dockerRuntime) Remove(ctx context.Context, name string) error {
	return dockerIgnoreMissing(ctx, "rm", name)
}

func dockerIgnoreMissing(ctx context.Context, verb, name string) error {
	out, err := exec.CommandContext(ctx, "docker", verb, name).CombinedOutput()
	if err != nil && !isNoSuchContainer(string(out)) {
		return fmt.Errorf("docker %s %s: %v: %s", verb, name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// isNoSuchContainer recognises docker's "no such container" error text.
func isNoSuchContainer(output string) bool {
	return strings.Contains(strings.ToLower(output), "no such container")
}

func (dockerRuntime) WaitReady(ctx context.Context, url string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s to answer 200", url)
		case <-ticker.C:
			if probeURL(url) {
				return nil
			}
		}
	}
}

// probeURL returns true if a GET of url answers HTTP 200.
func probeURL(url string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// valhallaContainerSpec is the long-running Valhalla routing container serving
// tiles from absTilesDir (an absolute path).
func valhallaContainerSpec(port int, absTilesDir string) ContainerSpec {
	return ContainerSpec{
		Name:    valhallaContainerName,
		Image:   valhallaImage,
		Ports:   []PortMapping{{Host: port, Container: 8002}},
		Mounts:  []Mount{{Host: absTilesDir, Container: "/custom_files"}},
		Restart: "unless-stopped",
		Command: []string{"valhalla_service", "/custom_files/valhalla.json", "1"},
	}
}

func valhallaStatusURL(port int) string {
	return "http://localhost:" + strconv.Itoa(port) + "/status"
}

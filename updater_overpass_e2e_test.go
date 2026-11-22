//go:build e2e_docker

package main

// updater_overpass_e2e_test.go: a real staged Overpass update through Update
// with the real dockerRuntime, against a tiny PBF.
//
// Not part of the default suite. Run with:
//
//	go test -tags e2e_docker -run TestE2E_OverpassStagedUpdate -v -timeout 20m .
//
// Its main purpose is to verify the spec's key assumption: that restarting the
// Overpass container against an already-imported database does NOT re-import,
// so the swap gap is seconds, not the length of an import. The test asserts it
// from the new live container's own logs (the image prints "No database
// directory. Initializing" and "Database created" only when it imports).
//
// Isolation (this must never touch the user's real containers or ~/.twisty):
//   - every data directory lives under one temp dir;
//   - containers are named twisty-e2e-<pid>-<n> (live) and <that>-staging via
//     UpdateDeps.OverpassContainerName, never twisty-overpass;
//   - host ports are freshly allocated, never 8080/8081;
//   - cleanup runs `docker rm -f` on exactly those two names;
//   - the test refuses to run if a name could collide with the defaults.
//
// It is skipped (not failed) when the Docker daemon or the Overpass image is
// not available locally; the image is never pulled.

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yardbirdsax/twisty/osmconv"
)

// tinyPBFUpstream is an UpstreamClient whose single region serves the minimal
// two-node PBF and always reports a newer Last-Modified than any stamp.
type tinyPBFUpstream struct{ lastModified time.Time }

func (u tinyPBFUpstream) Head(context.Context, string) (UpstreamInfo, error) {
	return UpstreamInfo{LastModified: u.lastModified}, nil
}

func (u tinyPBFUpstream) Download(_ context.Context, _, destPath string) (UpstreamInfo, error) {
	if err := os.WriteFile(destPath, osmconv.BuildMinimalPBFBytes(), 0o644); err != nil {
		return UpstreamInfo{}, err
	}
	return UpstreamInfo{LastModified: u.lastModified}, nil
}

func e2eDocker(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func freeHostPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestE2E_OverpassStagedUpdate_NoReimportAfterSwap(t *testing.T) {
	if _, err := e2eDocker(t, "info", "--format", "{{.ServerVersion}}"); err != nil {
		t.Skipf("Docker daemon not available: %v", err)
	}
	if _, err := e2eDocker(t, "image", "inspect", overpassImage); err != nil {
		t.Skipf("image %s is not present locally (this test never pulls it)", overpassImage)
	}

	// ── isolation ──
	liveName := fmt.Sprintf("twisty-e2e-%d-%d", os.Getpid(), time.Now().UnixNano()%1_000_000)
	stagingName := liveName + "-staging"
	if liveName == overpassContainerName || stagingName == overpassStagingContainerName ||
		!strings.HasPrefix(liveName, "twisty-e2e-") {
		t.Fatalf("refusing to run: container names %q/%q could collide with the real ones", liveName, stagingName)
	}
	t.Cleanup(func() {
		e2eDocker(t, "rm", "-f", liveName)    //nolint:errcheck
		e2eDocker(t, "rm", "-f", stagingName) //nolint:errcheck
	})
	livePort, stagePort := freeHostPort(t), freeHostPort(t)
	for stagePort == livePort {
		stagePort = freeHostPort(t)
	}
	if livePort == defaultOverpassPort || stagePort == defaultOverpassStagingPort {
		t.Skip("allocated a default port; rerun")
	}

	base, err := os.MkdirTemp("", "twisty-e2e-update-")
	if err != nil {
		t.Fatal(err)
	}
	// Root-owned container files may resist removal; the dir is under the OS
	// temp area either way.
	t.Cleanup(func() { os.RemoveAll(base) })
	root := DataRoot{
		OverpassDir: filepath.Join(base, "overpass"),
		ValhallaDir: filepath.Join(base, "valhalla"),
		PBFDir:      filepath.Join(base, "pbf"),
	}
	for _, d := range []string{root.OverpassDir, root.ValhallaDir, root.PBFDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const region = "test/tiny"
	stale := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.WriteFile(filepath.Join(root.OverpassDir, ".regions"), []byte(region), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeServiceStamp(root.PBFDir, ServiceOverpass, region, stale); err != nil {
		t.Fatal(err)
	}

	// ── seed: the "currently installed" Overpass, as `overpass start` leaves it ──
	pbf := filepath.Join(root.PBFDir, pbfFilename(region))
	if err := os.WriteFile(pbf, osmconv.BuildMinimalPBFBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := convertRegionsIncremental([]string{region}, root.OverpassDir, root.PBFDir, osmconv.NoopConvertProgress{}, io.Discard); err != nil {
		t.Fatalf("seed conversion: %v", err)
	}
	liveDB := filepath.Join(root.OverpassDir, "db")
	if err := os.MkdirAll(liveDB, 0o755); err != nil {
		t.Fatal(err)
	}
	absDB, _ := filepath.Abs(liveDB)
	absMerged, _ := filepath.Abs(filepath.Join(root.OverpassDir, "merged.osm.bz2"))
	rt := dockerRuntime{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := rt.Run(ctx, overpassContainerSpec(liveName, livePort, absDB, absMerged)); err != nil {
		t.Fatalf("starting seed container: %v", err)
	}
	seedStart := time.Now()
	if err := rt.WaitReady(ctx, overpassReadyURL(livePort)); err != nil {
		logs, _ := e2eDocker(t, "logs", liveName)
		t.Fatalf("seed container never became ready: %v\n%s", err, logs)
	}
	t.Logf("seed import + ready took %s", time.Since(seedStart).Round(time.Second))
	// Positive control: an importing container does print the import markers,
	// so their absence after the swap is meaningful rather than a wording change.
	seedLogs, _ := e2eDocker(t, "logs", liveName)
	if !strings.Contains(seedLogs, "No database directory. Initializing") {
		t.Fatalf("control failed: a fresh import did not log the expected marker; update the markers below.\n%s", seedLogs)
	}
	if err := markOverpassImported(liveDB); err != nil {
		t.Fatal(err)
	}
	// Tag the old generation so we can tell the database was replaced.
	if err := os.WriteFile(filepath.Join(liveDB, "generation"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldID, err := e2eDocker(t, "inspect", "-f", "{{.Id}}", liveName)
	if err != nil {
		t.Fatalf("inspect seed container: %v: %s", err, oldID)
	}

	// ── the real staged update ──
	deps := UpdateDeps{
		Upstream:              tinyPBFUpstream{lastModified: time.Now().UTC().Truncate(time.Second)},
		Runtime:               rt,
		Root:                  root,
		Clock:                 time.Now,
		FreeSpace:             diskFreeBytes,
		PBFDataTimestamp:      readPBFDataTimestamp,
		OverpassPort:          livePort,
		OverpassStagingPort:   stagePort,
		OverpassContainerName: liveName,
		ReadyTimeout:          10 * time.Minute,
	}
	start := time.Now()
	rep, err := Update(ctx, deps, UpdateOptions{Only: "overpass"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	t.Logf("Update (download, convert, staged import, swap, probe) took %s", time.Since(start).Round(time.Second))
	var svc *ServiceReport
	for i := range rep.Services {
		if rep.Services[i].Service == "overpass" {
			svc = &rep.Services[i]
		}
	}
	if svc == nil || svc.Outcome != OutcomeUpdated {
		t.Fatalf("overpass outcome = %+v, want updated", svc)
	}

	// ── post-swap state ──
	newID, err := e2eDocker(t, "inspect", "-f", "{{.Id}}", liveName)
	if err != nil {
		t.Fatalf("live container missing after update: %v: %s", err, newID)
	}
	if newID == oldID {
		t.Error("live container was not replaced")
	}
	if !rt.IsRunning(liveName) {
		t.Error("live container is not running after the swap")
	}
	if out, err := e2eDocker(t, "inspect", "-f", "{{.Id}}", stagingName); err == nil {
		t.Errorf("staging container %s still exists: %s", stagingName, out)
	}
	if _, err := os.Stat(filepath.Join(liveDB, "generation")); !os.IsNotExist(err) {
		t.Errorf("live db still carries the old generation tag (err=%v): swap did not replace the database", err)
	}
	for _, f := range []string{"init_done", overpassImportedMarker} {
		if _, err := os.Stat(filepath.Join(liveDB, f)); err != nil {
			t.Errorf("new live db is missing %s: %v", f, err)
		}
	}
	for _, leftover := range []string{"db.staging", "db.old"} {
		if _, err := os.Stat(filepath.Join(root.OverpassDir, leftover)); !os.IsNotExist(err) {
			t.Errorf("leftover %s (err=%v)", leftover, err)
		}
	}

	// ── the key assumption: the post-swap container serves without re-importing ──
	logs, err := e2eDocker(t, "logs", liveName)
	if err != nil {
		t.Fatalf("docker logs: %v", err)
	}
	t.Logf("post-swap container logs:\n%s", logs)
	for _, importMarker := range []string{"No database directory. Initializing", "Database created"} {
		if strings.Contains(logs, importMarker) {
			t.Errorf("ASSUMPTION FALSE: post-swap container re-imported (its logs contain %q). "+
				"The swap gap is then a full import, not seconds; revisit the swap design.", importMarker)
		}
	}

	// ...and it actually answers a real query with the imported data.
	q := "http://localhost:" + fmt.Sprint(livePort) + "/api/interpreter?data=" + "%5Bout%3Ajson%5D%3Bnode(1)%3Bout%3B"
	resp, err := http.Get(q)
	if err != nil {
		t.Fatalf("query post-swap container: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !regexp.MustCompile(`"id":\s*1\b`).Match(body) {
		t.Errorf("node(1) query: status %d, body %s", resp.StatusCode, body)
	}
}

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	overpassContainerName = "twisty-overpass"
	overpassImage         = "wiktorn/overpass-api"
	overpassRepoURL       = "https://github.com/wiktorn/Overpass-API.git"
	overpassVersion       = "0.7.62.4"
	geofabrikBaseURL      = "https://download.geofabrik.de"
	defaultOverpassPort   = 8080
)

func newOverpassCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "overpass",
		Short:         "Manage a local Overpass API instance via Docker",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		newOverpassStartCmd(),
		newOverpassStopCmd(),
		newOverpassStatusCmd(),
		newOverpassCleanCmd(),
		newOverpassLogsCmd(),
		newOverpassBuildCmd(),
	)
	return cmd
}

func newOverpassStartCmd() *cobra.Command {
	var (
		regions    string
		port       int
		dataDir    string
		cpuprofile string
	)
	cmd := &cobra.Command{
		Use:           "start",
		Short:         "Start the local Overpass API container",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverpassStart(regions, port, dataDir, cpuprofile, cmd.ErrOrStderr())
		},
	}
	f := cmd.Flags()
	f.StringVar(&regions, "regions", "", "Comma-separated Geofabrik region paths (e.g. north-america/us/new-york)")
	f.IntVar(&port, "port", defaultOverpassPort, "Port to expose the Overpass API on")
	f.StringVar(&dataDir, "data-dir", "", "Directory for Overpass data files")
	f.StringVar(&cpuprofile, "cpuprofile", "", "Write CPU profile to this path (scoped to PBF conversion)")
	return cmd
}

func newOverpassStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "stop",
		Short:         "Stop the local Overpass API container",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverpassStop(cmd.ErrOrStderr())
		},
	}
}

func newOverpassStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "status",
		Short:         "Show status of the local Overpass API container",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverpassStatus(cmd.ErrOrStderr())
		},
	}
}

func newOverpassCleanCmd() *cobra.Command {
	var dataDir string
	cmd := &cobra.Command{
		Use:           "clean",
		Short:         "Stop the container and remove all Overpass data",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverpassClean(dataDir, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Directory for Overpass data files")
	return cmd
}

func newOverpassLogsCmd() *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:           "logs",
		Short:         "Stream logs from the Overpass API container",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverpassLogs(lines)
		},
	}
	cmd.Flags().IntVar(&lines, "lines", 0, "Number of log lines to show (0 = follow)")
	return cmd
}

func newOverpassBuildCmd() *cobra.Command {
	var srcDir string
	cmd := &cobra.Command{
		Use:           "build",
		Short:         "Build the Overpass API Docker image from source",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverpassBuild(srcDir, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&srcDir, "src-dir", "", "Directory for Overpass source files (default: data-dir/overpass-src)")
	return cmd
}

func runOverpassStart(regions string, port int, dataDir string, cpuprofile string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dataDir = resolveOverpassDataDir(dataDir)

	// Parse new regions from the flag.
	var newRegions []string
	if regions != "" {
		parts := strings.Split(regions, ",")
		for _, r := range parts {
			r = strings.TrimSpace(r)
			if r != "" {
				newRegions = append(newRegions, r)
			}
		}
	}

	pbfDir := filepath.Join(dataDir, "pbf")
	dbDir := filepath.Join(dataDir, "db")
	mergedBZ2 := filepath.Join(dataDir, "merged.osm.bz2")
	stampFile := filepath.Join(dataDir, ".regions")

	// Create data directories.
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		return fmt.Errorf("creating pbf dir: %w", err)
	}
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return fmt.Errorf("creating db dir: %w", err)
	}

	// Load previously loaded regions from stamp file and merge with new ones.
	var existingRegions []string
	if stamp, err := os.ReadFile(stampFile); err == nil {
		for _, r := range strings.Split(strings.TrimSpace(string(stamp)), ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				existingRegions = append(existingRegions, r)
			}
		}
	}

	allRegions := mergeRegions(existingRegions, newRegions)
	if len(allRegions) == 0 {
		return fmt.Errorf("no regions specified and no cached regions found; use --regions to specify regions")
	}
	allRegionsKey := strings.Join(allRegions, ",")
	existingKey := strings.Join(existingRegions, ",")

	// If regions grew, wipe db and merged PBF to force re-import with full set.
	if allRegionsKey != existingKey {
		if len(existingRegions) > 0 {
			fmt.Fprintf(stderr, "Adding new regions; re-importing with full set.\n")
		}
		if err := os.RemoveAll(dbDir); err != nil {
			return fmt.Errorf("removing db dir: %w", err)
		}
		if err := os.MkdirAll(dbDir, 0o755); err != nil {
			return fmt.Errorf("recreating db dir: %w", err)
		}
		if err := os.Remove(mergedBZ2); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing merged BZ2: %w", err)
		}
	}

	// Collect URLs and dest paths for missing PBF files, then download in parallel.
	downloadProgress := newDownloadProgress(os.Stderr)
	var missingURLs, missingPaths []string
	for _, region := range allRegions {
		filename := pbfFilename(region)
		destPath := filepath.Join(pbfDir, filename)
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			url := geofabrikBaseURL + "/" + region + "-latest.osm.pbf"
			fmt.Fprintf(stderr, "Downloading %s -> %s\n", url, destPath)
			missingURLs = append(missingURLs, url)
			missingPaths = append(missingPaths, destPath)
		} else {
			fmt.Fprintf(stderr, "PBF already exists: %s\n", destPath)
		}
	}
	if len(missingURLs) > 0 {
		if err := downloadPBFsParallel(missingURLs, missingPaths, downloadProgress); err != nil {
			return fmt.Errorf("downloading PBF: %w", err)
		}
		downloadProgress.Done()
	}

	// Convert PBF files to per-region caches and merge into BZ2.
	if _, err := os.Stat(mergedBZ2); os.IsNotExist(err) {
		// Start CPU profiling if requested.
		if cpuprofile != "" {
			profFile, err := os.Create(cpuprofile)
			if err != nil {
				return fmt.Errorf("creating CPU profile file: %w", err)
			}
			if err := pprof.StartCPUProfile(profFile); err != nil {
				profFile.Close()
				return fmt.Errorf("starting CPU profile: %w", err)
			}
			defer func() {
				pprof.StopCPUProfile()
				profFile.Close()
				fmt.Fprintf(stderr, "CPU profile written to %s\n", cpuprofile)
			}()
		}

		if err := convertRegionsIncremental(allRegions, dataDir, newConvertProgress(os.Stderr), stderr); err != nil {
			return fmt.Errorf("converting regions: %w", err)
		}
	} else {
		fmt.Fprintf(stderr, "BZ2 already exists: %s\n", mergedBZ2)
	}

	// Write stamp file.
	if err := os.WriteFile(stampFile, []byte(allRegionsKey), 0o644); err != nil {
		return fmt.Errorf("writing regions stamp: %w", err)
	}

	// If container is already running, just report the URL.
	if isContainerRunning(overpassContainerName) {
		fmt.Fprintf(stderr, "Overpass API already running: http://localhost:%d/api/interpreter\n", port)
		return nil
	}

	// Ensure the Overpass image is built for the current architecture.
	if err := ensureOverpassImage(dataDir); err != nil {
		return fmt.Errorf("ensuring overpass image: %w", err)
	}

	// Remove any pre-existing stopped container.
	dockerCmd("rm", overpassContainerName) //nolint:errcheck — ignore errors if not present

	// Resolve absolute paths for volume mounts.
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("resolving data dir: %w", err)
	}
	absMergedBZ2, err := filepath.Abs(mergedBZ2)
	if err != nil {
		return fmt.Errorf("resolving merged BZ2 path: %w", err)
	}

	runArgs := overpassDockerRunArgs(port, absDataDir+"/db", absMergedBZ2)
	out, err := dockerCmd(runArgs...)
	if err != nil {
		return fmt.Errorf("starting overpass container: %v\n%s", err, out)
	}

	endpoint := fmt.Sprintf("http://localhost:%d/api/interpreter", port)
	fmt.Fprintf(stderr, "Overpass API starting. Streaming logs until ready...\n")
	fmt.Fprintf(stderr, "Endpoint: %s\n\n", endpoint)

	// Stream container logs in the background.
	logCmd := exec.Command("docker", "logs", "-f", overpassContainerName)
	logCmd.Stdout = stderr
	logCmd.Stderr = stderr
	if err := logCmd.Start(); err != nil {
		fmt.Fprintf(stderr, "warning: could not stream container logs: %v\n", err)
	}
	defer func() {
		if logCmd.Process != nil {
			logCmd.Process.Kill()
			logCmd.Wait()
		}
	}()

	// Poll for readiness with a 30 minute timeout (imports can be slow).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := waitForOverpass(ctx, endpoint); err != nil {
		fmt.Fprintf(stderr, "\nOverpass API failed to become ready: %v\n", err)
		fmt.Fprintf(stderr, "Container is still running. Use 'twisty overpass logs' to inspect.\n")
		return fmt.Errorf("overpass API failed to become ready: %w", err)
	}

	fmt.Fprintf(stderr, "\nOverpass API is ready: %s\n", endpoint)
	return nil
}

// pbfFilename returns the local PBF filename for a Geofabrik region path.
// Slashes in the region path are replaced with underscores so the name is
// safe to use as a flat filename.
func pbfFilename(region string) string {
	return strings.ReplaceAll(region, "/", "_") + "-latest.osm.pbf"
}

// mergeRegions returns the sorted union of existing and new region lists.
func mergeRegions(existing, add []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(add))
	for _, r := range existing {
		seen[r] = struct{}{}
	}
	for _, r := range add {
		seen[r] = struct{}{}
	}
	merged := make([]string, 0, len(seen))
	for r := range seen {
		merged = append(merged, r)
	}
	sort.Strings(merged)
	return merged
}

// overpassDockerRunArgs builds the arguments for `docker run` to start the
// Overpass API container. Extracted for testability.
func overpassDockerRunArgs(port int, absDBDir, absMergedBZ2 string) []string {
	return []string{
		"run", "-d",
		"--name", overpassContainerName,
		"--restart", "unless-stopped",
		"-p", fmt.Sprintf("%d:80", port),
		"-v", absDBDir + ":/db",
		"-v", absMergedBZ2 + ":/data/planet.osm.bz2:ro",
		"-e", "OVERPASS_MODE=init",
		"-e", "OVERPASS_PLANET_URL=file:///data/planet.osm.bz2",
		"-e", "OVERPASS_USE_AREAS=false",
		"-e", "OVERPASS_META=no",
		"-e", "OVERPASS_COMPRESSION=no",
		overpassImage,
	}
}

// probeOverpass sends a lightweight query to the Overpass API endpoint and
// returns true if it gets an HTTP 200 response.
func probeOverpass(endpoint string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(endpoint, "application/x-www-form-urlencoded",
		strings.NewReader("data=[out:json][timeout:1];out;"))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitForOverpass streams Docker container logs to logWriter while polling the
// API endpoint. It returns nil when the API is ready, or an error if the
// context is canceled (e.g. timeout).
func waitForOverpass(ctx context.Context, endpoint string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for Overpass API to become ready")
		case <-ticker.C:
			if probeOverpass(endpoint) {
				return nil
			}
		}
	}
}

func runOverpassStop(stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dockerCmd("stop", overpassContainerName) //nolint:errcheck — ignore errors
	fmt.Fprintln(stderr, "Overpass API stopped.")
	return nil
}

func runOverpassStatus(stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	if isContainerRunning(overpassContainerName) {
		fmt.Fprintf(stderr, "Overpass API is running: http://localhost:%d/api/interpreter\n", defaultOverpassPort)
	} else {
		fmt.Fprintln(stderr, "Overpass API is not running.")
	}
	return nil
}

func runOverpassClean(dataDir string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dataDir = resolveOverpassDataDir(dataDir)

	dockerCmd("stop", overpassContainerName) //nolint:errcheck
	if err := os.RemoveAll(dataDir); err != nil {
		return fmt.Errorf("removing data dir %s: %w", dataDir, err)
	}
	fmt.Fprintf(stderr, "Overpass container stopped and data directory %q removed.\n", dataDir)
	return nil
}

func runOverpassLogs(_ int) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found in PATH: %w", err)
	}
	argv := []string{"docker", "logs", "-f", overpassContainerName}
	if err := syscall.Exec(dockerPath, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec docker logs: %w", err)
	}
	return nil
}

func runOverpassBuild(srcDir string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	// srcDir flag is not used as the data-dir source; use resolveOverpassDataDir for data location.
	// If srcDir is empty, resolve via default data dir.
	dataDir := resolveOverpassDataDir("")
	if srcDir != "" {
		dataDir = srcDir
	}

	if err := buildOverpassImage(dataDir); err != nil {
		return fmt.Errorf("building overpass image: %w", err)
	}
	fmt.Fprintln(stderr, "Overpass image built successfully.")
	return nil
}

// ensureOverpassImage checks if the Overpass image exists for the current
// architecture and builds it if not.
func ensureOverpassImage(dataDir string) error {
	arch, err := imageArch(overpassImage)
	if err == nil && arch == runtime.GOARCH {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Overpass image not found for %s; building from source...\n", runtime.GOARCH)
	return buildOverpassImage(dataDir)
}

// imageArch returns the architecture of a local Docker image.
func imageArch(image string) (string, error) {
	out, err := dockerCmd("image", "inspect", image, "--format", "{{.Architecture}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// buildOverpassImage clones the Overpass-API repo, generates a Dockerfile from
// the template with the pinned version, and builds the Docker image.
func buildOverpassImage(dataDir string) error {
	srcDir := filepath.Join(dataDir, "overpass-src")

	// Clone or update the repo.
	if _, err := os.Stat(filepath.Join(srcDir, ".git")); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "Cloning Overpass-API repository...")
		if err := os.MkdirAll(filepath.Dir(srcDir), 0o755); err != nil {
			return fmt.Errorf("creating source dir: %w", err)
		}
		if err := dockerCmdStreaming("git", "clone", "--depth=1", overpassRepoURL, srcDir); err != nil {
			return fmt.Errorf("cloning repo: %w", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Updating Overpass-API repository...")
		if err := dockerCmdStreaming("git", "-C", srcDir, "pull", "--ff-only"); err != nil {
			return fmt.Errorf("updating repo: %w", err)
		}
	}

	// Generate Dockerfile from template by substituting the version.
	templatePath := filepath.Join(srcDir, "Dockerfile.template")
	tmpl, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("reading Dockerfile template: %w", err)
	}
	dockerfile := strings.NewReplacer(
		"{version}", overpassVersion,
		"{{", "{",
		"}}", "}",
	).Replace(string(tmpl))
	dockerfilePath := filepath.Join(srcDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("writing Dockerfile: %w", err)
	}

	// Patch requirements.txt: upstream pins osmium~=3.7.0 which has no ARM64 wheel.
	reqPath := filepath.Join(srcDir, "requirements.txt")
	reqData, err := os.ReadFile(reqPath)
	if err != nil {
		return fmt.Errorf("reading requirements.txt: %w", err)
	}
	patched := strings.ReplaceAll(string(reqData), "osmium~=3.7.0", "osmium>=3.7.0")
	if err := os.WriteFile(reqPath, []byte(patched), 0o644); err != nil {
		return fmt.Errorf("writing requirements.txt: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Building Overpass image (v%s)...\n", overpassVersion)
	buildArgs := overpassImageBuildArgs(srcDir)
	if err := dockerCmdStreaming("docker", buildArgs...); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// overpassImageBuildArgs returns the docker build arguments for building the
// Overpass image. Extracted for testability.
func overpassImageBuildArgs(buildContext string) []string {
	args := []string{"build"}
	if runtime.GOARCH == "arm64" {
		args = append(args, "--platform", "linux/arm64")
	}
	args = append(args, "-t", overpassImage, buildContext)
	return args
}

// downloadPBFsParallel downloads all urls[i] to destPaths[i] concurrently.
// All downloads start simultaneously; the first error is returned after all
// goroutines finish.
func downloadPBFsParallel(urls, destPaths []string, progress DownloadProgress) error {
	errs := make([]error, len(urls))
	var wg sync.WaitGroup
	for i := range urls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fp := progress.StartFile(destPaths[idx], idx, len(urls), -1)
			errs[idx] = downloadPBF(urls[idx], destPaths[idx], fp)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// downloadPBF downloads the file at url to destPath atomically via a temp file,
// reporting progress via the provided FileDownloadProgress.
func downloadPBF(url, destPath string, fp FileDownloadProgress) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".download-*.osm.pbf")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := tmp.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("writing temp file: %w", writeErr)
			}
			fp.BytesDownloaded(int64(n))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("reading response: %w", readErr)
		}
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}
	fp.FileComplete()
	return nil
}

// dockerCmd runs a docker subcommand, captures combined output, and returns it.
func dockerCmd(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// dockerCmdStreaming runs a command with stdout and stderr wired to os.Stderr
// so the user can see real-time progress output.
func dockerCmdStreaming(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// isContainerRunning returns true when the named Docker container is running.
func isContainerRunning(name string) bool {
	out, err := dockerCmd("inspect", "-f", "{{.State.Running}}", name)
	if err != nil {
		return false
	}
	return strings.Contains(out, "true")
}

// resolveOverpassDataDir returns dir if non-empty, otherwise defaults to
// ~/.twisty/overpass (matching the home-directory cache pattern used by
// other twisty commands).
func resolveOverpassDataDir(dir string) string {
	if dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	return filepath.Join(home, ".twisty", "overpass")
}

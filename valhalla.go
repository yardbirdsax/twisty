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
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/spf13/cobra"
)

const (
	valhallaContainerName = "twisty-valhalla"
	valhallaImage         = "ghcr.io/valhalla/valhalla:latest"
	osmiumImage           = "iboates/osmium"
	defaultValhallaPort   = 8002
)

func newValhallaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "valhalla",
		Short:         "Manage a local Valhalla routing server via Docker",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		newValhallaStartCmd(),
		newValhallaStopCmd(),
		newValhallaStatusCmd(),
		newValhallaCleanCmd(),
		newValhallaLogsCmd(),
	)
	return cmd
}

func newValhallaStartCmd() *cobra.Command {
	var (
		regions string
		port    int
		dataDir string
	)
	cmd := &cobra.Command{
		Use:           "start",
		Short:         "Start the local Valhalla routing server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaStart(regions, port, dataDir, cmd.ErrOrStderr())
		},
	}
	f := cmd.Flags()
	f.StringVar(&regions, "regions", "", "Comma-separated Geofabrik region paths (e.g. north-america/us/tennessee)")
	f.IntVar(&port, "port", defaultValhallaPort, "Port to expose the Valhalla API on")
	f.StringVar(&dataDir, "data-dir", "", "Directory for Valhalla data files")
	return cmd
}

func newValhallaStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "stop",
		Short:         "Stop the local Valhalla routing server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaStop(cmd.ErrOrStderr())
		},
	}
}

func newValhallaStatusCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:           "status",
		Short:         "Show status of the local Valhalla routing server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaStatus(port, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().IntVar(&port, "port", defaultValhallaPort, "Port the Valhalla server is running on")
	return cmd
}

func newValhallaCleanCmd() *cobra.Command {
	var dataDir string
	cmd := &cobra.Command{
		Use:           "clean",
		Short:         "Stop the container and remove all Valhalla data",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaClean(dataDir, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Directory for Valhalla data files")
	return cmd
}

func newValhallaLogsCmd() *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:           "logs",
		Short:         "Stream logs from the Valhalla routing server container",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValhallaLogs(lines)
		},
	}
	cmd.Flags().IntVar(&lines, "lines", 0, "Number of log lines to show (0 = follow)")
	return cmd
}

func runValhallaStart(regions string, port int, dataDir string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dataDir = resolveValhallaDataDir(dataDir)

	var newRegions []string
	if regions != "" {
		for _, r := range strings.Split(regions, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				newRegions = append(newRegions, r)
			}
		}
	}

	// Validate region names to prevent shell injection.
	for _, r := range newRegions {
		if err := validateRegion(r); err != nil {
			return err
		}
	}

	tilesDir := filepath.Join(dataDir, "tiles")
	stampFile := filepath.Join(dataDir, ".regions")
	pbfDir := resolvePBFDir("")

	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		return fmt.Errorf("creating pbf dir: %w", err)
	}
	if err := os.MkdirAll(tilesDir, 0o755); err != nil {
		return fmt.Errorf("creating tiles dir: %w", err)
	}

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

	// If regions grew, wipe tiles and merged PBF to force a full rebuild.
	if allRegionsKey != existingKey {
		if len(existingRegions) > 0 {
			fmt.Fprintf(stderr, "Adding new regions; rebuilding tiles with full set.\n")
		}
		if err := os.RemoveAll(tilesDir); err != nil {
			return fmt.Errorf("removing tiles dir: %w", err)
		}
		if err := os.MkdirAll(tilesDir, 0o755); err != nil {
			return fmt.Errorf("recreating tiles dir: %w", err)
		}
		mergedPBF := filepath.Join(dataDir, "merged.osm.pbf")
		if err := os.Remove(mergedPBF); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing merged PBF: %w", err)
		}
	}

	// Download missing PBFs.
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

	// Build tiles if not already built.
	entries, _ := os.ReadDir(tilesDir)
	if len(entries) == 0 {
		fmt.Fprintf(stderr, "Building Valhalla tiles...\n")

		// Stamp the intended regions before launching the build so that a
		// crash or OOM kill does not cause the next invocation to wipe and
		// restart from scratch when tiles are already partially built.
		if err := stampRegions(stampFile, allRegions); err != nil {
			return err
		}

		// Collect absolute PBF paths for all regions.
		var absPBFPaths []string
		for _, region := range allRegions {
			abs, err := filepath.Abs(filepath.Join(pbfDir, pbfFilename(region)))
			if err != nil {
				return fmt.Errorf("resolving pbf path: %w", err)
			}
			absPBFPaths = append(absPBFPaths, abs)
		}

		// When multiple regions are requested, merge them into a single PBF
		// first. Valhalla produces corrupted tiles when given multiple extracts
		// directly (https://github.com/valhalla/valhalla/issues/3925).
		var buildPBFPaths []string
		var buildPBFNames []string
		if len(allRegions) > 1 {
			mergedPBF, err := filepath.Abs(filepath.Join(dataDir, "merged.osm.pbf"))
			if err != nil {
				return fmt.Errorf("resolving merged PBF path: %w", err)
			}
			fmt.Fprintf(stderr, "Merging %d PBF files...\n", len(absPBFPaths))
			if err := mergePBFsWithOsmium(mergedPBF, absPBFPaths, stderr); err != nil {
				return fmt.Errorf("merging PBF files: %w", err)
			}
			buildPBFPaths = []string{mergedPBF}
			buildPBFNames = []string{"merged.osm.pbf"}
		} else {
			buildPBFPaths = absPBFPaths
			buildPBFNames = []string{pbfFilename(allRegions[0])}
		}

		absTilesDir, err := filepath.Abs(tilesDir)
		if err != nil {
			return fmt.Errorf("resolving tiles dir: %w", err)
		}
		buildArgs := valhallaDockerBuildArgs(absTilesDir, buildPBFPaths, buildPBFNames)
		if err := dockerCmdStreaming("docker", buildArgs...); err != nil {
			return fmt.Errorf("building Valhalla tiles: %w", err)
		}
	} else {
		fmt.Fprintf(stderr, "Tiles already exist: %s\n", tilesDir)
		if err := stampRegions(stampFile, allRegions); err != nil {
			return err
		}
	}

	// If container is already running, just report the URL.
	if isContainerRunning(valhallaContainerName) {
		fmt.Fprintf(stderr, "Valhalla routing server already running: http://localhost:%d\n", port)
		return nil
	}

	// Remove any pre-existing stopped container.
	dockerCmd("rm", valhallaContainerName) //nolint:errcheck

	absTilesDir, err := filepath.Abs(tilesDir)
	if err != nil {
		return fmt.Errorf("resolving tiles dir: %w", err)
	}

	runArgs := valhallaDockerRunArgs(port, absTilesDir)
	out, err := dockerCmd(runArgs...)
	if err != nil {
		return fmt.Errorf("starting valhalla container: %v\n%s", err, out)
	}

	endpoint := fmt.Sprintf("http://localhost:%d", port)
	fmt.Fprintf(stderr, "Valhalla routing server starting. Streaming logs until ready...\n")
	fmt.Fprintf(stderr, "Endpoint: %s\n\n", endpoint)

	logCmd := exec.Command("docker", "logs", "-f", valhallaContainerName)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := waitForValhalla(ctx, endpoint); err != nil {
		fmt.Fprintf(stderr, "\nValhalla failed to become ready: %v\n", err)
		fmt.Fprintf(stderr, "Container is still running. Use 'twisty valhalla logs' to inspect.\n")
		return fmt.Errorf("valhalla failed to become ready: %w", err)
	}

	fmt.Fprintf(stderr, "\nValhalla routing server is ready: %s\n", endpoint)
	return nil
}

func runValhallaStop(stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dockerCmd("stop", valhallaContainerName) //nolint:errcheck
	fmt.Fprintln(stderr, "Valhalla routing server stopped.")
	return nil
}

func runValhallaStatus(port int, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	if isContainerRunning(valhallaContainerName) {
		fmt.Fprintf(stderr, "Valhalla routing server is running: http://localhost:%d\n", port)
	} else {
		fmt.Fprintln(stderr, "Valhalla routing server is not running.")
	}
	return nil
}

func runValhallaClean(dataDir string, stderr io.Writer) error {
	if stderr == nil {
		stderr = os.Stderr
	}
	dataDir = resolveValhallaDataDir(dataDir)
	dockerCmd("stop", valhallaContainerName) //nolint:errcheck
	dockerCmd("rm", valhallaContainerName)   //nolint:errcheck
	if err := os.RemoveAll(dataDir); err != nil {
		return fmt.Errorf("removing data dir %s: %w", dataDir, err)
	}
	fmt.Fprintf(stderr, "Valhalla container stopped and data directory %q removed.\n", dataDir)
	return nil
}

func runValhallaLogs(lines int) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found in PATH: %w", err)
	}
	var argv []string
	if lines > 0 {
		argv = []string{"docker", "logs", "--tail", strconv.Itoa(lines), valhallaContainerName}
	} else {
		argv = []string{"docker", "logs", "-f", valhallaContainerName}
	}
	if err := syscall.Exec(dockerPath, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec docker logs: %w", err)
	}
	return nil
}

// valhallaDockerBuildArgs returns the `docker run --rm` arguments to build
// Valhalla tiles from one or more PBF files. absTilesDir is the absolute path
// to the tiles directory, absPBFPaths and pbfFilenames are parallel slices of
// absolute host paths and container-side filenames for each PBF.
func valhallaDockerBuildArgs(absTilesDir string, absPBFPaths, pbfFilenames []string) []string {
	args := []string{"run", "--rm", "-v", absTilesDir + ":/custom_files"}
	for i, path := range absPBFPaths {
		args = append(args, "-v", path+":/pbf/"+pbfFilenames[i])
	}
	args = append(args, valhallaImage, "bash", "-c")

	// Build the bash command that runs all four valhalla build steps.
	// PBFs are mounted under /pbf/ to avoid conflicting with the /custom_files directory mount.
	pbfContainerPaths := make([]string, len(pbfFilenames))
	for i, f := range pbfFilenames {
		pbfContainerPaths[i] = "/pbf/" + f
	}
	pbfArgs := strings.Join(pbfContainerPaths, " ")
	bashCmd := fmt.Sprintf(
		"valhalla_build_config --mjolnir-tile-dir /custom_files/tiles --mjolnir-timezone /usr/share/zoneinfo/UTC --mjolnir-admin /custom_files/admins.sqlite > /custom_files/valhalla.json && "+
			"valhalla_build_timezones /custom_files/valhalla.json && "+
			"valhalla_build_admins --config /custom_files/valhalla.json %s && "+
			"valhalla_build_tiles -c /custom_files/valhalla.json %s",
		pbfArgs, pbfArgs,
	)
	args = append(args, bashCmd)
	return args
}

// valhallaDockerRunArgs returns the `docker run -d` arguments to start the
// Valhalla routing server. absTilesDir must be an absolute path.
func valhallaDockerRunArgs(port int, absTilesDir string) []string {
	return []string{
		"run", "-d",
		"--name", valhallaContainerName,
		"--restart", "unless-stopped",
		"-p", fmt.Sprintf("%d:8002", port),
		"-v", absTilesDir + ":/custom_files",
		valhallaImage,
		"valhalla_service", "/custom_files/valhalla.json", "1",
	}
}

// probeValhalla sends a GET /status request to the Valhalla endpoint and
// returns true if it gets an HTTP 200 response.
func probeValhalla(endpoint string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(endpoint + "/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitForValhalla polls the Valhalla /status endpoint until it returns HTTP 200
// or the context is cancelled.
func waitForValhalla(ctx context.Context, endpoint string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for Valhalla to become ready")
		case <-ticker.C:
			if probeValhalla(endpoint) {
				return nil
			}
		}
	}
}

// resolveValhallaDataDir returns dir if non-empty, otherwise defaults to
// ~/.twisty/valhalla.
func resolveValhallaDataDir(dir string) string {
	if dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	return filepath.Join(home, ".twisty", "valhalla")
}

// stampRegions writes the sorted region list to stampFile so subsequent
// invocations can detect which regions are already prepared.
func stampRegions(stampFile string, regions []string) error {
	sorted := make([]string, len(regions))
	copy(sorted, regions)
	sort.Strings(sorted)
	if err := os.WriteFile(stampFile, []byte(strings.Join(sorted, ",")), 0o644); err != nil {
		return fmt.Errorf("writing regions stamp: %w", err)
	}
	return nil
}

// validateRegion checks that a region name contains only safe characters:
// letters, digits, '/', '-', and '_'. This prevents shell injection when
// region names are interpolated into shell commands.
func validateRegion(r string) error {
	for _, c := range r {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '/' && c != '-' && c != '_' {
			return fmt.Errorf("region %q contains invalid character %q", r, c)
		}
	}
	return nil
}

// osmiumMergeArgs returns the `docker run --rm` arguments to merge multiple
// PBF files into one using the osmium container. absMergedPBF is the absolute
// host path for the output file; absPBFPaths are the absolute host paths of
// the input PBF files.
func osmiumMergeArgs(absMergedPBF string, absPBFPaths []string) []string {
	outputDir := filepath.Dir(absMergedPBF)
	outputFilename := filepath.Base(absMergedPBF)

	args := []string{"run", "--rm", "-v", outputDir + ":/output"}
	for _, p := range absPBFPaths {
		filename := filepath.Base(p)
		args = append(args, "-v", p+":/pbf/"+filename)
	}
	args = append(args, osmiumImage, "merge", "-O", "-o", "/output/"+outputFilename)
	for _, p := range absPBFPaths {
		args = append(args, "/pbf/"+filepath.Base(p))
	}
	return args
}

// mergePBFsWithOsmium runs the osmium container to merge absPBFPaths into a
// single PBF at absMergedPBF, streaming output to stderr.
func mergePBFsWithOsmium(absMergedPBF string, absPBFPaths []string, stderr io.Writer) error {
	args := osmiumMergeArgs(absMergedPBF, absPBFPaths)
	cmd := exec.Command("docker", args...)
	// Route both stdout and stderr to the caller's stderr writer so that
	// osmium progress output is visible to the user rather than swallowed.
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	info, err := os.Stat(absMergedPBF)
	if err != nil {
		return fmt.Errorf("merged PBF not created: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("merged PBF is empty: %s", absMergedPBF)
	}
	return nil
}

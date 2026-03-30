package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dsnet/compress/bzip2"
	"github.com/yardbirdsax/twisty/osmconv"
)

const (
	overpassContainerName  = "twisty-overpass"
	overpassImage          = "wiktorn/overpass-api"
	overpassRepoURL        = "https://github.com/wiktorn/Overpass-API.git"
	overpassVersion        = "0.7.62.4"
	geofabrikBaseURL       = "https://download.geofabrik.de"
	defaultOverpassPort = 8080
)

func runOverpass(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: twisty overpass <start|stop|status|clean|logs>")
		os.Exit(1)
	}
	switch args[0] {
	case "start":
		runOverpassStart(args[1:])
	case "stop":
		runOverpassStop(args[1:])
	case "status":
		runOverpassStatus(args[1:])
	case "clean":
		runOverpassClean(args[1:])
	case "logs":
		runOverpassLogs(args[1:])
	case "build":
		runOverpassBuild(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown overpass subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func runOverpassStart(args []string) {
	fs := flag.NewFlagSet("overpass start", flag.ExitOnError)
	regions := fs.String("regions", "", "Comma-separated Geofabrik region paths (e.g. north-america/us/new-york)")
	port := fs.Int("port", defaultOverpassPort, "Port to expose the Overpass API on")
	dataDir := fs.String("data-dir", "", "Directory for Overpass data files")
	fs.Parse(args)
	*dataDir = resolveOverpassDataDir(*dataDir)

	if *regions == "" {
		fmt.Fprintln(os.Stderr, "Error: -regions is required")
		fs.Usage()
		os.Exit(1)
	}

	// Parse new regions from the flag.
	parts := strings.Split(*regions, ",")
	var newRegions []string
	for _, r := range parts {
		r = strings.TrimSpace(r)
		if r != "" {
			newRegions = append(newRegions, r)
		}
	}

	pbfDir := filepath.Join(*dataDir, "pbf")
	dbDir := filepath.Join(*dataDir, "db")
	mergedBZ2 := filepath.Join(*dataDir, "merged.osm.bz2")
	stampFile := filepath.Join(*dataDir, ".regions")

	// Create data directories.
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		log.Fatalf("creating pbf dir: %v", err)
	}
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		log.Fatalf("creating db dir: %v", err)
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
	allRegionsKey := strings.Join(allRegions, ",")
	existingKey := strings.Join(existingRegions, ",")

	// If regions grew, wipe db and merged PBF to force re-import with full set.
	if allRegionsKey != existingKey {
		if len(existingRegions) > 0 {
			fmt.Fprintf(os.Stderr, "Adding new regions; re-importing with full set.\n")
		}
		if err := os.RemoveAll(dbDir); err != nil {
			log.Fatalf("removing db dir: %v", err)
		}
		if err := os.MkdirAll(dbDir, 0o755); err != nil {
			log.Fatalf("recreating db dir: %v", err)
		}
		if err := os.Remove(mergedBZ2); err != nil && !os.IsNotExist(err) {
			log.Fatalf("removing merged BZ2: %v", err)
		}
	}

	// Download any missing PBF files.
	for _, region := range allRegions {
		filename := pbfFilename(region)
		destPath := filepath.Join(pbfDir, filename)
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			url := geofabrikBaseURL + "/" + region + "-latest.osm.pbf"
			fmt.Fprintf(os.Stderr, "Downloading %s -> %s\n", url, destPath)
			if err := downloadPBF(url, destPath); err != nil {
				log.Fatalf("downloading %s: %v", url, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "PBF already exists: %s\n", destPath)
		}
	}

	// Convert PBF files directly to BZ2 (merge + convert in one pass).
	if _, err := os.Stat(mergedBZ2); os.IsNotExist(err) {
		var pbfPaths []string
		for _, region := range allRegions {
			filename := pbfFilename(region)
			pbfPaths = append(pbfPaths, filepath.Join(pbfDir, filename))
		}
		fmt.Fprintf(os.Stderr, "Converting %d PBF file(s) to BZ2...\n", len(pbfPaths))
		if err := convertPBFsToBZ2(pbfPaths, mergedBZ2, os.Stderr); err != nil {
			log.Fatalf("converting PBF to BZ2: %v", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "BZ2 already exists: %s\n", mergedBZ2)
	}

	// Write stamp file.
	if err := os.WriteFile(stampFile, []byte(allRegionsKey), 0o644); err != nil {
		log.Fatalf("writing regions stamp: %v", err)
	}

	// If container is already running, just report the URL.
	if isContainerRunning(overpassContainerName) {
		fmt.Fprintf(os.Stderr, "Overpass API already running: http://localhost:%d/api/interpreter\n", *port)
		return
	}

	// Ensure the Overpass image is built for the current architecture.
	if err := ensureOverpassImage(*dataDir); err != nil {
		log.Fatalf("ensuring overpass image: %v", err)
	}

	// Remove any pre-existing stopped container.
	dockerCmd("rm", overpassContainerName) //nolint:errcheck — ignore errors if not present

	// Resolve absolute paths for volume mounts.
	absDataDir, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("resolving data dir: %v", err)
	}
	absMergedBZ2, err := filepath.Abs(mergedBZ2)
	if err != nil {
		log.Fatalf("resolving merged BZ2 path: %v", err)
	}

	runArgs := overpassDockerRunArgs(*port, absDataDir+"/db", absMergedBZ2)
	out, err := dockerCmd(runArgs...)
	if err != nil {
		log.Fatalf("starting overpass container: %v\n%s", err, out)
	}

	endpoint := fmt.Sprintf("http://localhost:%d/api/interpreter", *port)
	fmt.Fprintf(os.Stderr, "Overpass API starting. Streaming logs until ready...\n")
	fmt.Fprintf(os.Stderr, "Endpoint: %s\n\n", endpoint)

	// Stream container logs in the background.
	logCmd := exec.Command("docker", "logs", "-f", overpassContainerName)
	logCmd.Stdout = os.Stderr
	logCmd.Stderr = os.Stderr
	if err := logCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not stream container logs: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "\nOverpass API failed to become ready: %v\n", err)
		fmt.Fprintf(os.Stderr, "Container is still running. Use 'twisty overpass logs' to inspect.\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\nOverpass API is ready: %s\n", endpoint)
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

func runOverpassStop(_ []string) {
	dockerCmd("stop", overpassContainerName) //nolint:errcheck — ignore errors
	fmt.Fprintln(os.Stderr, "Overpass API stopped.")
}

func runOverpassStatus(_ []string) {
	if isContainerRunning(overpassContainerName) {
		fmt.Fprintf(os.Stderr, "Overpass API is running: http://localhost:%d/api/interpreter\n", defaultOverpassPort)
	} else {
		fmt.Fprintln(os.Stderr, "Overpass API is not running.")
	}
}

func runOverpassClean(args []string) {
	fs := flag.NewFlagSet("overpass clean", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "Directory for Overpass data files")
	fs.Parse(args)
	*dataDir = resolveOverpassDataDir(*dataDir)

	dockerCmd("stop", overpassContainerName) //nolint:errcheck
	if err := os.RemoveAll(*dataDir); err != nil {
		log.Fatalf("removing data dir %s: %v", *dataDir, err)
	}
	fmt.Fprintf(os.Stderr, "Overpass container stopped and data directory %q removed.\n", *dataDir)
}

func runOverpassLogs(_ []string) {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		log.Fatalf("docker not found in PATH: %v", err)
	}
	argv := []string{"docker", "logs", "-f", overpassContainerName}
	if err := syscall.Exec(dockerPath, argv, os.Environ()); err != nil {
		log.Fatalf("exec docker logs: %v", err)
	}
}

func runOverpassBuild(args []string) {
	fs := flag.NewFlagSet("overpass build", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "Directory for Overpass data files")
	fs.Parse(args)
	*dataDir = resolveOverpassDataDir(*dataDir)

	if err := buildOverpassImage(*dataDir); err != nil {
		log.Fatalf("building overpass image: %v", err)
	}
	fmt.Fprintln(os.Stderr, "Overpass image built successfully.")
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

// downloadPBF downloads the file at url to destPath atomically via a temp file,
// printing progress to stderr.
func downloadPBF(url, destPath string) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	total := resp.ContentLength // -1 if unknown

	// Write to a temp file in the same directory then rename for atomicity.
	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".download-*.osm.pbf")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op if rename succeeded
	}()

	const progressInterval = 10 * 1024 * 1024 // 10 MB
	var downloaded int64
	var nextThreshold int64 = progressInterval

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := tmp.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("writing temp file: %w", writeErr)
			}
			downloaded += int64(n)
			if downloaded >= nextThreshold {
				if total > 0 {
					pct := float64(downloaded) / float64(total) * 100
					fmt.Fprintf(os.Stderr, "  downloaded %.1f MB / %.1f MB (%.0f%%)\n",
						float64(downloaded)/1024/1024, float64(total)/1024/1024, pct)
					// Advance threshold by ~5% or 10 MB, whichever is larger.
					step := max(int64(float64(total)*0.05), progressInterval)
					nextThreshold = downloaded + step
				} else {
					fmt.Fprintf(os.Stderr, "  downloaded %.1f MB\n", float64(downloaded)/1024/1024)
					nextThreshold = downloaded + progressInterval
				}
			}
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
	fmt.Fprintf(os.Stderr, "  done (%.1f MB)\n", float64(downloaded)/1024/1024)
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

// convertPBFsToBZ2 reads one or more PBF files, merges them in sorted order,
// and writes bzip2-compressed OSM XML.
func convertPBFsToBZ2(pbfPaths []string, bz2Path string, progress io.Writer) error {
	scanners := make([]osmconv.ObjectScanner, len(pbfPaths))
	closers := make([]io.Closer, len(pbfPaths))
	for i, p := range pbfPaths {
		f, err := os.Open(p)
		if err != nil {
			// Close any already-opened files
			for j := 0; j < i; j++ {
				closers[j].Close()
			}
			return fmt.Errorf("opening %s: %w", p, err)
		}
		closers[i] = f
		scanners[i] = osmconv.NewPBFScanner(f)
	}
	defer func() {
		for _, c := range closers {
			if c != nil {
				c.Close()
			}
		}
	}()

	outFile, err := os.Create(bz2Path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", bz2Path, err)
	}
	defer outFile.Close()

	bz2w, err := bzip2.NewWriter(outFile, nil)
	if err != nil {
		return fmt.Errorf("creating bzip2 writer: %w", err)
	}

	xmlw := osmconv.NewXMLWriter(bz2w)

	if err := osmconv.Convert(osmconv.ConvertOptions{
		Scanners: scanners,
		Writer:   xmlw,
		Progress: progress,
	}); err != nil {
		bz2w.Close()
		return err
	}

	if err := bz2w.Close(); err != nil {
		return fmt.Errorf("closing bzip2 writer: %w", err)
	}
	return outFile.Close()
}


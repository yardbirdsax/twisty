# Task 001: Set Up Project Structure and Cobra Command Skeleton

## Summary

Establish the foundational project structure for the GPX converter feature. Create the `auth/` package for credential storage interfaces, extend the existing `gpx/` package with route data types, and wire up a new `newGpxCmd()` function directly in `main.go`. This task provides the scaffolding on which all subsequent tasks depend.

## Dependencies

None - this is the foundational task.

## Context: Existing Project Structure

**IMPORTANT:** This project does NOT use a `cmd/` subdirectory or `internal/` packages. All commands are defined as functions in `main.go` and registered in `newRootCmd()`. All packages live at the project root (e.g., `gpx/`, `quality/`, `route/`).

There is already an existing `gpx/` package at `gpx/gpx.go` containing:
- `Waypoint` struct (GPX waypoint XML element)
- `GPX`, `Track`, `TrackSeg`, `TrackPoint` structs (GPX document structure)
- `WriteGPX` and `WriteGPXWithWaypoints` functions

New code for this feature must extend the existing `gpx/` package, **not** create a new one.

## Detailed Directions

### 1. Create Directory Structure

Create only the following new directory:

```
auth/
  - (package placeholder for authentication tasks)
```

The `gpx/` directory already exists and will be extended in later tasks.

### 2. Define Core Auth Interface

Create `auth/store.go` with the `CredentialStore` interface:

```go
package auth

import "errors"

var ErrNotFound = errors.New("credential not found")

// CredentialStore defines the interface for secure credential persistence.
type CredentialStore interface {
	// Get retrieves a credential value by key.
	// Returns ErrNotFound if the key does not exist.
	Get(key string) (string, error)

	// Set stores a credential value under the given key.
	Set(key, value string) error

	// Delete removes a credential by key.
	// Does not error if the key does not exist.
	Delete(key string) error
}
```

### 3. Define Route Data Types in Existing GPX Package

Add `gpx/route.go` with route data types used during conversion. These are distinct from the existing GPX XML output types in `gpx/gpx.go`:

```go
package gpx

// RouteData represents the extracted route from Google Maps.
type RouteData struct {
	StartName       string
	DestinationName string
	RouteWaypoints  []RouteWaypoint
	TrackPoints     []TrackCoord
}

// RouteWaypoint represents a significant point on the route (start, stop, destination).
// Distinct from the existing Waypoint type which is the GPX XML element.
type RouteWaypoint struct {
	Name      string
	Latitude  float64
	Longitude float64
}

// TrackCoord represents a geographic coordinate along the route track.
// Distinct from the existing TrackPoint type which is the GPX XML element.
type TrackCoord struct {
	Latitude  float64
	Longitude float64
}
```

### 4. Add the GPX Command to main.go

Add a `newGpxCmd()` function to `main.go`, following the same pattern as the existing commands (`newRouteCmd`, `newScoreCmd`, etc.):

```go
func newGpxCmd() *cobra.Command {
	var mapsURL string
	var outPath string

	cmd := &cobra.Command{
		Use:   "gpx",
		Short: "Convert a Google Maps shared link to a GPX file",
		Long: `Export a driving route from a Google Maps shared link as a GPX file
for use in offline navigation applications like OSMAnd.

Example:
  twisty gpx --maps-url https://maps.app.goo.gl/... --out route.gpx`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement in Task 006
			return fmt.Errorf("not implemented")
		},
	}

	cmd.Flags().StringVar(&mapsURL, "maps-url", "", "Google Maps shared link URL (required)")
	cmd.Flags().StringVar(&outPath, "out", "", "Output GPX file path (required)")
	cmd.MarkFlagRequired("maps-url") //nolint:errcheck
	cmd.MarkFlagRequired("out")      //nolint:errcheck

	return cmd
}
```

### 5. Register the Command in newRootCmd()

Update `newRootCmd()` in `main.go` to include `newGpxCmd()`:

```go
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "twisty",
		Short:         "Twisty — motorcycle route finder",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		newRouteCmd(),
		newFetchCmd(),
		newScoreCmd(),
		newRandomCmd(),
		newOverpassCmd(),
		newGpxCmd(), // Add this line
	)
	return cmd
}
```

### 6. Verify Build

Ensure the code compiles without errors:

```bash
go build ./...
```

Verify the command is recognized:

```bash
go run . gpx --help
```

Expected output should show the `gpx` command with `--maps-url` and `--out` flags.

## Acceptance Criteria

- [ ] `auth/store.go` defines `CredentialStore` interface with `Get`, `Set`, `Delete` methods and `ErrNotFound` error
- [ ] `gpx/route.go` defines `RouteData`, `RouteWaypoint`, and `TrackCoord` types
- [ ] `newGpxCmd()` function added to `main.go` with `--maps-url` and `--out` flags (both required)
- [ ] `newGpxCmd()` registered in `newRootCmd()` in `main.go`
- [ ] `go build ./...` compiles without errors
- [ ] `go run . gpx --help` displays help text with `--maps-url` and `--out` flags

## Notes

- All new packages live at the project root, not under `internal/` or `cmd/`.
- The `gpx/` package already exists — new types added in this task must not conflict with existing types (`Waypoint`, `Track`, `TrackSeg`, `TrackPoint`, `GPX`).
- The command is `twisty gpx --maps-url <url> --out <file>` — there is NO `convert` subcommand.
- The `auth/` package is a new package; subsequent tasks will add implementations to it.

---

# Task 001 Review: Set Up Project Structure and Cobra Command Skeleton

**Reviewer:** Principal Engineer
**Date:** 2026-04-11
**Verdict:** APPROVED

---

## Summary

This task implements the foundational scaffolding for the GPX converter feature: the `auth/` package with a `CredentialStore` interface, route data types in `gpx/route.go`, and a `gpx` Cobra command skeleton in `main.go`. All three files match the spec exactly.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/auth/store.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/gpx/route.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` (newGpxCmd, newRootCmd) | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `auth/store.go` defines `CredentialStore` interface with `Get`, `Set`, `Delete` methods and `ErrNotFound` error | PASS |
| `gpx/route.go` defines `RouteData`, `RouteWaypoint`, and `TrackCoord` types | PASS |
| `newGpxCmd()` function added to `main.go` with `--maps-url` and `--out` flags (both required) | PASS |
| `newGpxCmd()` registered in `newRootCmd()` in `main.go` | PASS |
| `go build ./...` compiles without errors | PASS |
| `go run . gpx --help` displays help text with `--maps-url` and `--out` flags | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go build ./...           # Clean build, no errors
go run . gpx --help      # Displays correct help with --maps-url and --out flags
make test                # All tests pass (auth has no test files, as expected for an interface-only package)
make lint                # Blocked by sandbox filesystem permissions (go build cache), not a code issue
```

---

## Final Verdict

**APPROVED**

The implementation matches the task specification exactly. All three new files (`auth/store.go`, `gpx/route.go`, and the `newGpxCmd()` addition to `main.go`) are correct, the project builds cleanly, existing tests continue to pass, and the `gpx --help` output shows the expected flags.

# Task 006: Integrate Components and Implement Command Handler

## Summary

Wire together all the components (authentication, API client, GPX generation) into a cohesive command handler. Implement the `newGpxCmd()` function fully in `main.go`, including credential management, error handling with user-facing messages, and file output.

## Dependencies

Task 001 through Task 005 - all component implementations must be complete.

## Context: Project Structure

**IMPORTANT:** This project puts all commands in `main.go`. There is NO `cmd/gpx/` package, NO `cmd/root.go`. The `newGpxCmd()` skeleton was added in Task 001. This task replaces its `// TODO` with real implementation.

All imports use the project's root-level packages:
- `github.com/yardbirdsax/twisty/auth` (NOT `internal/auth`)
- `github.com/yardbirdsax/twisty/gpx` (NOT `internal/gpx`)

## Detailed Directions

### 1. Create a Service Layer in the GPX Package

Create `gpx/service.go` to orchestrate the conversion pipeline:

```go
package gpx

import (
	"context"
	"fmt"

	"github.com/yardbirdsax/twisty/auth"
)

// Service orchestrates the GPX conversion pipeline.
type Service struct {
	authenticator *auth.Authenticator
}

// NewService creates a new GPX conversion service.
func NewService(authenticator *auth.Authenticator) *Service {
	return &Service{authenticator: authenticator}
}

// ConvertToFile converts a Google Maps URL to a GPX file at outPath.
func (s *Service) ConvertToFile(ctx context.Context, mapsURL, outPath string) error {
	// Get or refresh authentication token
	token, err := s.authenticator.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Extract route from Google Maps
	client := NewMapsClient(token.AccessToken)
	routeData, err := client.GetRoute(ctx, mapsURL)
	if err != nil {
		return err // Already has a user-friendly message from GetRoute
	}

	// Generate GPX file
	if err := ConvertRouteToGPX(routeData, outPath); err != nil {
		return fmt.Errorf("failed to generate GPX file: %w", err)
	}

	return nil
}
```

### 2. Implement newGpxCmd() in main.go

Replace the `// TODO: Implement in Task 006` stub in `main.go` with a full implementation. The `newGpxCmd()` function signature does not change (it returns `*cobra.Command` and uses `--maps-url` and `--out` flags). Only the `RunE` body changes:

```go
func newGpxCmd() *cobra.Command {
	var mapsURL string
	var outPath string

	cmd := &cobra.Command{
		Use:   "gpx",
		Short: "Convert a Google Maps shared link to a GPX file",
		Long: `Export a driving route from a Google Maps shared link as a GPX file
for use in offline navigation applications like OSMAnd.

The first time you run this command, you will be prompted to authenticate
with Google. Your credentials are stored securely and reused automatically.

Example:
  twisty gpx --maps-url https://maps.app.goo.gl/... --out route.gpx`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGpx(cmd.Context(), mapsURL, outPath)
		},
	}

	cmd.Flags().StringVar(&mapsURL, "maps-url", "", "Google Maps shared link URL (required)")
	cmd.Flags().StringVar(&outPath, "out", "", "Output GPX file path (required)")
	cmd.MarkFlagRequired("maps-url") //nolint:errcheck
	cmd.MarkFlagRequired("out")      //nolint:errcheck

	return cmd
}

func runGpx(ctx context.Context, mapsURL, outPath string) error {
	store, err := auth.NewCredentialStore("twisty-maps")
	if err != nil {
		return fmt.Errorf("failed to initialize credential store: %w", err)
	}

	// Built-in OAuth credentials — set at compile time via ldflags or constants.
	// Replace with actual client ID/secret before release.
	oauthConfig := auth.DefaultGoogleMapsOAuthConfig()
	oauthConfig.ClientID = builtInGoogleClientID
	oauthConfig.ClientSecret = builtInGoogleClientSecret

	authenticator := auth.NewAuthenticator(oauthConfig, store)
	service := gpx.NewService(authenticator)

	fmt.Fprintln(os.Stderr, "Converting Google Maps route to GPX...")

	if err := service.ConvertToFile(ctx, mapsURL, outPath); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Route exported: %s\n", outPath)
	return nil
}
```

Add the built-in credential constants near the top of `main.go` (or in a separate `credentials.go` file in the main package if preferred):

```go
// builtInGoogleClientID and builtInGoogleClientSecret are the OAuth credentials
// for the twisty Google Cloud project. These can be overridden at build time via
// -ldflags "-X main.builtInGoogleClientID=<id> -X main.builtInGoogleClientSecret=<secret>"
var (
	builtInGoogleClientID     = ""
	builtInGoogleClientSecret = ""
)
```

### 3. Add Required Imports to main.go

Ensure `main.go` imports:

```go
import (
    // ... existing imports ...
    "github.com/yardbirdsax/twisty/auth"
    "github.com/yardbirdsax/twisty/gpx"
)
```

### 4. Write Tests

Create `gpx/service_test.go`:

```go
package gpx

import (
	"testing"
)

// MockCredentialStore implements auth.CredentialStore for testing.
type MockCredentialStore struct {
	data map[string]string
}

func NewMockCredentialStore() *MockCredentialStore {
	return &MockCredentialStore{data: make(map[string]string)}
}

func (m *MockCredentialStore) Get(key string) (string, error) {
	if val, ok := m.data[key]; ok {
		return val, nil
	}
	return "", errNotFound
}

func (m *MockCredentialStore) Set(key, value string) error {
	m.data[key] = value
	return nil
}

func (m *MockCredentialStore) Delete(key string) error {
	delete(m.data, key)
	return nil
}

// errNotFound is a local sentinel for testing — mirrors auth.ErrNotFound.
var errNotFound = errors.New("credential not found")

func TestNewService(t *testing.T) {
	// Service creation is validated via build; only smoke test here
	// since the Authenticator requires live OAuth for full test
	if NewService == nil {
		t.Error("NewService should be defined")
	}
}
```

### 5. Verify Build and Test

```bash
go build ./...
go test ./... -v
```

Run a smoke test to verify command is available:

```bash
go run . gpx --help
```

Expected output shows `--maps-url` and `--out` flags.

## Acceptance Criteria

- [ ] `gpx/service.go` implements `Service` with `ConvertToFile(ctx, mapsURL, outPath)` method
- [ ] `newGpxCmd()` in `main.go` calls `runGpx()` via `RunE`
- [ ] `runGpx()` initializes credential store, authenticator, and service in sequence
- [ ] Built-in OAuth client ID/secret are set via package-level vars (overridable via ldflags)
- [ ] Errors are returned (not printed directly in `runGpx`) so Cobra handles stderr output
- [ ] Success message printed to stderr after successful file write
- [ ] `go build ./...` compiles without errors
- [ ] `go run . gpx --help` shows correct help with `--maps-url` and `--out` flags

## Notes

- There is NO `convert` subcommand. The command is `twisty gpx --maps-url <url> --out <file>`.
- Import paths are `github.com/yardbirdsax/twisty/auth` and `github.com/yardbirdsax/twisty/gpx` — no `internal/` prefix.
- Per project convention (see existing commands), errors are printed to stderr before the process exits. Cobra handles this when `RunE` returns an error, so `runGpx` should return errors rather than printing them directly.
- Built-in credentials can be left empty for development and set via ldflags in CI/release builds.

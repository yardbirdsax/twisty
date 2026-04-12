# Task 007: Add Comprehensive Tests and Quality Assurance

## Summary

Implement comprehensive unit and integration tests covering all components. Ensure error paths are tested, edge cases are handled, and the feature is robust. This task validates that all components work correctly in isolation and together.

## Dependencies

Task 001 through Task 006 - all components must be implemented.

## Context: Project Structure

All packages are at the project root. Test files live alongside their package files:
- `auth/` — auth package tests (store, token, PKCE, OAuth)
- `gpx/` — gpx package tests (URL validation, polyline decoding, converter, service)

Test commands:
```bash
go test ./...           # all packages
go test ./auth/... -v  # auth package only
go test ./gpx/... -v   # gpx package only
```

## Detailed Directions

### 1. Add Token and Auth Error Path Tests

Create `auth/auth_test.go`:

```go
package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTokenRefreshBuffer(t *testing.T) {
	// Token expiring within 1 minute should be treated as expired
	token := &Token{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(30 * time.Second),
	}
	if !token.IsExpired() {
		t.Error("token expiring within 1-minute buffer should be treated as expired")
	}
}

func TestTokenAlreadyExpired(t *testing.T) {
	token := &Token{
		AccessToken: "old-token",
		ExpiresAt:   time.Now().Add(-time.Hour),
	}
	if !token.IsExpired() {
		t.Error("token expired 1 hour ago should be detected as expired")
	}
}

func TestTokenNotExpired(t *testing.T) {
	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	if token.IsExpired() {
		t.Error("token valid for 24 hours should not be detected as expired")
	}
}

func TestStateUniqueness(t *testing.T) {
	state1, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	state2, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	if state1 == state2 {
		t.Error("successive states should be unique")
	}
}

func TestFileStoreDirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "dir", "creds.json")

	store, err := NewFileStore(nestedPath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if store == nil {
		t.Error("store should not be nil")
	}
}

func TestFileStoreMultipleKeys(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(filepath.Join(tmpDir, "creds.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	keys := map[string]string{"token1": "value1", "token2": "value2", "token3": "value3"}
	for k, v := range keys {
		if err := store.Set(k, v); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}

	for k, want := range keys {
		got, err := store.Get(k)
		if err != nil {
			t.Fatalf("Get(%q): %v", k, err)
		}
		if got != want {
			t.Errorf("Get(%q) = %q, want %q", k, got, want)
		}
	}
}
```

### 2. Add GPX Coordinate Boundary Tests

Create `gpx/validation_test.go`:

```go
package gpx

import (
	"path/filepath"
	"testing"
)

func TestGPXCoordinateBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		lat     float64
		lon     float64
		wantErr bool
	}{
		{"valid center", 0, 0, false},
		{"valid north pole", 90, 0, false},
		{"valid south pole", -90, 0, false},
		{"valid antimeridian east", 0, 180, false},
		{"valid antimeridian west", 0, -180, false},
		{"invalid north", 90.1, 0, true},
		{"invalid south", -90.1, 0, true},
		{"invalid east", 0, 180.1, true},
		{"invalid west", 0, -180.1, true},
	}

	tmpDir := t.TempDir()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &RouteData{
				StartName:       "A",
				DestinationName: "B",
				RouteWaypoints: []RouteWaypoint{
					{Name: "A", Latitude: tt.lat, Longitude: tt.lon},
					{Name: "B", Latitude: 0, Longitude: 0},
				},
				TrackPoints: []TrackCoord{
					{Latitude: tt.lat, Longitude: tt.lon},
					{Latitude: 0, Longitude: 0},
				},
			}
			outPath := filepath.Join(tmpDir, tt.name+".gpx")
			err := ConvertRouteToGPX(data, outPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRouteToGPX: expected error=%v, got %v", tt.wantErr, err)
			}
		})
	}
}
```

### 3. Add URL Parsing Tests

Create `gpx/url_parsing_test.go`:

```go
package gpx

import (
	"testing"
)

func TestParseSharedLinkVariations(t *testing.T) {
	client := NewMapsClient("test-token")

	tests := []struct {
		name      string
		url       string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "two-stop path URL",
			url:       "https://maps.google.com/maps/dir/New%20York/Boston",
			wantCount: 2,
		},
		{
			name:      "three-stop path URL",
			url:       "https://maps.google.com/maps/dir/Home/Office/Gym",
			wantCount: 3,
		},
		{
			name:    "single location (no destination)",
			url:     "https://maps.google.com/maps/dir/OnlyOneStop",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			waypoints, err := client.parseSharedLink(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSharedLink: expected error=%v, got %v", tt.wantErr, err)
			}
			if !tt.wantErr && len(waypoints) != tt.wantCount {
				t.Errorf("parseSharedLink: expected %d waypoints, got %d", tt.wantCount, len(waypoints))
			}
		})
	}
}

func TestValidateGoogleMapsURLVariations(t *testing.T) {
	tests := []struct {
		url       string
		wantErr   bool
	}{
		{"https://maps.google.com/maps/dir/Home/Work", false},
		{"https://maps.app.goo.gl/abc123", false},
		{"https://google.com/maps/dir/A/B", false},
		{"https://example.com/maps/dir/A/B", true},
		{"https://notmaps.google.com/something", true},
		{"not a url at all", true},
		{"", true},
	}

	for _, tt := range tests {
		err := ValidateGoogleMapsURL(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateGoogleMapsURL(%q): expected error=%v, got %v", tt.url, tt.wantErr, err)
		}
	}
}
```

### 4. Add Shared Test Data Helpers

Create `gpx/testhelpers_test.go`:

```go
package gpx

// testRouteData returns sample route data for use in tests.
func testRouteData() *RouteData {
	return &RouteData{
		StartName:       "Home",
		DestinationName: "Work",
		RouteWaypoints: []RouteWaypoint{
			{Name: "Home", Latitude: 40.7128, Longitude: -74.0060},
			{Name: "Gas Station", Latitude: 40.7300, Longitude: -74.0000},
			{Name: "Work", Latitude: 40.7580, Longitude: -73.9855},
		},
		TrackPoints: []TrackCoord{
			{Latitude: 40.7128, Longitude: -74.0060},
			{Latitude: 40.7135, Longitude: -74.0059},
			{Latitude: 40.7300, Longitude: -74.0000},
			{Latitude: 40.7500, Longitude: -73.9900},
			{Latitude: 40.7580, Longitude: -73.9855},
		},
	}
}
```

### 5. Run Full Test Suite

```bash
go test ./... -v -race -count=1
```

Address any failures or race conditions before marking complete.

Check test coverage:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

Aim for >80% coverage in `auth/` and `gpx/` packages.

## Acceptance Criteria

- [ ] Token expiry tests cover: already expired, expiring within buffer, valid
- [ ] `FileStore` tests cover: Set/Get/Delete, ErrNotFound, persistence across instances, nested directory creation
- [ ] PKCE tests cover: uniqueness of verifiers and states
- [ ] GPX coordinate boundary tests cover all four extremes and invalid values
- [ ] URL parsing tests cover path-based waypoints, multiple stops, and invalid URLs
- [ ] All tests pass: `go test ./... -v`
- [ ] No race conditions: `go test ./... -race`
- [ ] Test coverage >80% for `auth/` and `gpx/` packages

## Notes

- Tests use table-driven patterns for variations.
- No external dependencies in tests (no live API calls, no keychain access).
- The `MockCredentialStore` from Task 006 can be reused here if needed.
- Coverage gaps in the OAuth interactive flow are acceptable since it requires a live browser — focus coverage on testable units (token logic, PKCE, file store, polyline decoding, GPX generation).

---

# Task 007 Review: Add Comprehensive Tests and Quality Assurance

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-04-11
**Verdict:** APPROVED

---

## Summary

Implements comprehensive unit tests for auth token logic, FileStore, PKCE/state generation, OAuth flow, GPX coordinate boundaries, URL validation/parsing, Maps client, and service layer. All acceptance criteria are met.

### Files Reviewed

| File | Status |
|------|--------|
| `auth/auth_test.go` | Reviewed |
| `auth/oauth.go` | Reviewed |
| `gpx/validation_test.go` | Reviewed |
| `gpx/url_parsing_test.go` | Reviewed |
| `gpx/testhelpers_test.go` | Reviewed |
| `gpx/maps_client_test.go` | Reviewed |
| `gpx/service_test.go` | Reviewed |
| `gpx/service.go` | Reviewed |
| `gpx/url_validator.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| Token expiry tests cover: already expired, expiring within buffer, valid | PASS |
| `FileStore` tests cover: Set/Get/Delete, ErrNotFound, persistence across instances, nested directory creation | PASS |
| PKCE tests cover: uniqueness of verifiers and states | PASS |
| GPX coordinate boundary tests cover all four extremes and invalid values | PASS |
| URL parsing tests cover path-based waypoints, multiple stops, and invalid URLs | PASS |
| All tests pass: `go test ./... -v` | PASS |
| No race conditions: `go test ./... -race` | PASS |
| Test coverage >80% for `auth/` and `gpx/` packages | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test ./auth/... ./gpx/... -v -race -count=1   # all tests pass, no races
go test ./auth/... ./gpx/... -coverprofile=/tmp/coverage.out && go tool cover -func=/tmp/coverage.out
# auth/: 83.3%, gpx/: 80.8%
go vet ./auth/... ./gpx/...                       # clean
```

---

## Final Verdict

**APPROVED**

All acceptance criteria met. Coverage is 83.3% (`auth/`) and 80.8% (`gpx/`), both above the 80% threshold. All tests pass with no race conditions. The URL validator path-matching bug from the prior review has been fixed (`strings.HasPrefix` instead of `strings.Contains`).

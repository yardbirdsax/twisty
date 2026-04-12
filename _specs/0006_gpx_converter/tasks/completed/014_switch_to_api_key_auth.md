# Task 014: Switch GPX Command from OAuth to API Key Authentication

## Summary

Replace the OAuth-based authentication path in the GPX command with a simpler API key. The Routes API does not support OAuth user authentication; API keys are the correct mechanism. The `auth/` package and its OAuth machinery are left fully intact and untouched — they are simply no longer wired into the GPX command, preserving the option to revisit OAuth in the future.

## Dependencies

Task 013 (Routes API client) should be complete first, as this task changes how the client is authenticated, not how it makes requests.

## Context: What Changes and What Stays

**Stays completely unchanged:**
- Everything in `auth/` — `oauth.go`, `token.go`, `store.go`, `keychain.go`, `filestore.go`, `pkce.go`, and all tests
- `gpx/maps_client.go` request/response structure and Routes API logic
- `gpx/service.go`'s `convertWithClient` method and `RouteGetter` interface

**Changes:**
- `gpx/maps_client.go`: rename `accessToken` field to `apiKey`; pass it as a `?key=` query parameter instead of an `Authorization` header
- `gpx/service.go`: `NewService` accepts `apiKey string` instead of `*auth.Authenticator`; `ConvertToFile` no longer calls `GetToken`
- `gpx/service_test.go`: remove `MockCredentialStore`, `auth` import, and authenticator setup from `TestNewService`
- `main.go`: add `builtInGoogleAPIKey`; update `runGpx` to use it directly; keep `builtInGoogleClientID` and `builtInGoogleClientSecret` as reserved/unused vars

## Detailed Directions

### 1. Update `gpx/maps_client.go`

Rename the `accessToken` field to `apiKey` throughout, and update `callRoutesAPI` to pass it as a query parameter instead of an Authorization header.

In the struct and constructor:

```go
type MapsClient struct {
    apiKey     string
    httpClient *http.Client
}

func NewMapsClient(apiKey string) *MapsClient {
    return &MapsClient{
        apiKey: apiKey,
        httpClient: &http.Client{
            Timeout: 30 * time.Second,
        },
    }
}
```

In `callRoutesAPI`, replace the Authorization header with a `key` query parameter:

```go
reqURL, err := url.Parse(routesAPIURL)
if err != nil {
    return nil, fmt.Errorf("parse routes API URL: %w", err)
}
q := reqURL.Query()
q.Set("key", c.apiKey)
reqURL.RawQuery = q.Encode()

req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), strings.NewReader(string(body)))
if err != nil {
    return nil, fmt.Errorf("create request: %w", err)
}
req.Header.Set("Content-Type", "application/json")
req.Header.Set("X-Goog-FieldMask", "routes.polyline.encodedPolyline,routes.legs.startLocation,routes.legs.endLocation")
// No Authorization header — API key is in the query string
```

### 2. Update `gpx/service.go`

Remove the `auth` import and authenticator field. Accept an API key string directly:

```go
package gpx

import (
    "context"
    "fmt"
)

// Service orchestrates the GPX conversion pipeline.
type Service struct {
    apiKey string
}

// NewService creates a new GPX conversion service.
func NewService(apiKey string) *Service {
    return &Service{apiKey: apiKey}
}

// ConvertToFile is the end-to-end pipeline: it fetches the route from the
// Routes API using the provided API key and writes a GPX 1.1 file to outPath.
func (s *Service) ConvertToFile(ctx context.Context, mapsURL, outPath string) error {
    client := NewMapsClient(s.apiKey)
    return s.convertWithClient(ctx, client, mapsURL, outPath)
}
```

### 3. Update `gpx/service_test.go`

Remove the `MockCredentialStore`, `auth` import, and authenticator from `TestNewService`. The test just verifies `NewService` returns non-nil:

```go
func TestNewService(t *testing.T) {
    svc := NewService("test-api-key")
    if svc == nil {
        t.Error("NewService should return a non-nil Service")
    }
}
```

Delete the `MockCredentialStore` type, `NewMockCredentialStore`, `errNotFound`, and all associated methods — they are no longer needed now that `Service` has no auth dependency.

### 4. Update `main.go`

Add `builtInGoogleAPIKey` alongside the existing OAuth vars (which are kept as reserved):

```go
// builtInGoogleClientID and builtInGoogleClientSecret are reserved for a future
// OAuth flow. They are currently unused. Set at build time via -ldflags if needed.
var (
    builtInGoogleClientID     = ""
    builtInGoogleClientSecret = ""
)

// builtInGoogleAPIKey is the Routes API key injected at build time via:
// -ldflags "-X main.builtInGoogleAPIKey=<key>"
var builtInGoogleAPIKey = ""
```

Replace the body of `runGpx`:

```go
func runGpx(ctx context.Context, mapsURL, outPath string) error {
    if builtInGoogleAPIKey == "" {
        return fmt.Errorf("no Google API key configured; rebuild with make build")
    }

    service := gpx.NewService(builtInGoogleAPIKey)

    fmt.Fprintln(os.Stderr, "Converting Google Maps route to GPX...")

    if err := service.ConvertToFile(ctx, mapsURL, outPath); err != nil {
        return err
    }

    fmt.Fprintf(os.Stderr, "Route exported: %s\n", outPath)
    return nil
}
```

Remove the `auth` import from `main.go` if it is no longer referenced elsewhere.

### 5. Update Tests in `gpx/maps_client_test.go`

`TestCallRoutesAPI_OK` currently asserts `Authorization: Bearer test-token`. Update it to verify the API key appears in the request URL instead:

```go
// Verify API key is in query string, not Authorization header
if key := capturedReq.URL.Query().Get("key"); key != "test-token" {
    t.Errorf("key query param = %q, want %q", key, "test-token")
}
if auth := capturedReq.Header.Get("Authorization"); auth != "" {
    t.Errorf("Authorization header should not be set, got %q", auth)
}
```

### 6. Verify

```bash
go build ./...
go test ./gpx/... -v
go test ./... -short
```

Then smoke test:

```bash
make build
./bin/twisty gpx \
  --maps-url "https://www.google.com/maps/dir/40.238122,-75.526346/40.3625144,-75.6776312/40.445452,-75.802636/Speedway,+14233+Kutztown+Rd,+Fleetwood,+PA+19522/@40.2381255,-75.5323253,668m/data=!3m1!1e3!4m11!4m10!1m0!1m0!1m5!1m1!1s0x89c5d6cd37212bc1:0xd23839ad4c4b15f0!2m2!1d-75.8399983!2d40.4856114!3e0!5m1!1e4?entry=ttu&g_ep=EgoyMDI2MDQwOC4wIKXMDSoASAFQ" \
  --out /tmp/test.gpx
```

## Acceptance Criteria

- [ ] `MapsClient.apiKey` replaces `accessToken`; the Routes API request uses `?key=<apiKey>` with no `Authorization` header
- [ ] `NewService(apiKey string)` compiles and no longer imports `auth`
- [ ] `MockCredentialStore` and related auth test scaffolding removed from `gpx/service_test.go`
- [ ] `main.go` has `builtInGoogleAPIKey` var; `builtInGoogleClientID` and `builtInGoogleClientSecret` are retained but marked as reserved in a comment
- [ ] `runGpx` returns a clear error if `builtInGoogleAPIKey` is empty
- [ ] `auth` import removed from `main.go` if it is otherwise unused
- [ ] `TestCallRoutesAPI_OK` asserts API key in query string, not Authorization header
- [ ] `go test ./... -short` passes with no failures
- [ ] `go build ./...` compiles without errors
- [ ] Nothing in `auth/` is modified

## Notes

- The `auth/` package import in `gpx/service_test.go` must be removed since `MockCredentialStore` is deleted. Confirm no other types from `auth` are referenced in that file.
- The API key is passed in the query string rather than a header because that is what the Routes API expects for key-based auth. Do not use `Authorization: Bearer <key>` — that is the OAuth pattern and will not work.
- `builtInGoogleClientID` and `builtInGoogleClientSecret` are intentionally kept unused in `main.go`. Do not delete them or add `//nolint` pragmas — they are reserved for future use.

---

# Task 014 Review: Switch GPX Command from OAuth to API Key Authentication

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-04-12
**Verdict:** APPROVED

---

## Summary

Replaced OAuth-based authentication in the GPX command with API key authentication. `MapsClient` now passes the key as a `?key=` query parameter, `Service` accepts a plain string instead of an authenticator, and `main.go` introduces `builtInGoogleAPIKey` with an empty-check guard in `runGpx`.

### Files Reviewed

| File | Status |
|------|--------|
| `gpx/maps_client.go` | Reviewed |
| `gpx/maps_client_test.go` | Reviewed |
| `gpx/service.go` | Reviewed |
| `gpx/service_test.go` | Reviewed |
| `main.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `MapsClient.apiKey` replaces `accessToken`; request uses `?key=` with no `Authorization` header | PASS |
| `NewService(apiKey string)` compiles and no longer imports `auth` | PASS |
| `MockCredentialStore` and related auth test scaffolding removed from `gpx/service_test.go` | PASS |
| `main.go` has `builtInGoogleAPIKey`; OAuth vars retained with reserved comment | PASS |
| `runGpx` returns clear error if `builtInGoogleAPIKey` is empty | PASS |
| `auth` import removed from `main.go` | PASS |
| `TestCallRoutesAPI_OK` asserts API key in query string, not Authorization header | PASS |
| `go test ./... -short` passes | PASS |
| `go build ./...` compiles without errors | PASS |
| Nothing in `auth/` modified | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go build ./...         # clean, no errors
go test -short ./...   # all packages pass
make lint              # go vet clean, no issues
git diff --name-only HEAD auth/  # no auth/ files changed
```

---

## Final Verdict

**APPROVED**

All acceptance criteria met. Implementation matches the specification exactly; no issues found.

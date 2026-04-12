# Task 012: Fix OAuth Scope for Routes API

## Summary

The current OAuth scope in `DefaultGoogleMapsOAuthConfig()` is `https://www.googleapis.com/auth/maps-platform.routesPreferredApi`, which Google rejects with "Some requested scopes were invalid." Replace it with the correct scope for the Google Routes API.

## Dependencies

None — this is a one-line change in `auth/oauth.go`.

## Context

The scope is set in `auth/oauth.go:33`:

```go
Scope: "https://www.googleapis.com/auth/maps-platform.routesPreferredApi",
```

The Routes API (`routes.googleapis.com`) is the target API (Task 013 will switch the Maps client to it). The correct OAuth 2.0 scope for the Routes API must be confirmed during implementation from the [Routes API authorization documentation](https://developers.google.com/maps/documentation/routes/authorizing). The expected value is one of:

- `https://www.googleapis.com/auth/maps-platform.routespreferred` (Routes API-specific scope)
- `https://www.googleapis.com/auth/cloud-platform` (broad Google Cloud scope, use only if the API-specific scope does not exist)

Prefer the narrowest scope that works.

## Detailed Directions

### 1. Verify the Correct Scope

Before writing code, check the official Routes API authorization documentation to confirm the exact scope string. Do not guess — use whatever the documentation specifies.

### 2. Update `DefaultGoogleMapsOAuthConfig` in `auth/oauth.go`

Replace the `Scope` value:

```go
func DefaultGoogleMapsOAuthConfig() *OAuthConfig {
    return &OAuthConfig{
        AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
        TokenURL: "https://oauth2.googleapis.com/token",
        Scope:    "<verified-scope-from-docs>",
    }
}
```

Also update the comment on the function to reference the Routes API rather than the generic "Maps API":

```go
// DefaultGoogleMapsOAuthConfig returns the default Google OAuth configuration for the Routes API.
```

### 3. Verify

Build and run the OAuth flow end-to-end:

```bash
make build
./bin/twisty gpx --maps-url "https://maps.google.com/maps/dir/Home/Work" --out /tmp/test.gpx
```

The browser should open, you should be able to sign in, and the OAuth flow should complete without a scope error. The subsequent API call will fail (that's fixed in Task 013) — what matters here is that the OAuth consent screen accepts the scope.

## Acceptance Criteria

- [ ] `DefaultGoogleMapsOAuthConfig()` uses a scope verified from the Routes API documentation
- [ ] The scope is the narrowest one that grants access to the Routes API
- [ ] The function comment references the Routes API
- [ ] `go build ./...` compiles without errors
- [ ] The OAuth browser flow completes without a "requested scopes were invalid" error

## Notes

- Only `auth/oauth.go` should be modified.
- Do not change `AuthURL` or `TokenURL` — those are correct for Google OAuth 2.0.
- The existing token stored in the keychain (from any previous login attempt with the bad scope) should be deleted before testing so a fresh OAuth flow is triggered. The token is stored under the `oauth_token` key in the `twisty-maps` keychain service.

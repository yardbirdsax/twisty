# Task 003: Implement OAuth 2.0 PKCE Authentication Flow

## Summary

Implement a complete OAuth 2.0 PKCE flow for Google Maps API authentication. This includes starting a loopback listener, generating PKCE values, handling the OAuth redirect, exchanging authorization codes for tokens, and managing token refresh. Users should experience frictionless browser-based login with automatic token management.

## Dependencies

Task 001, Task 002 - the `CredentialStore` interface and implementations must be in place.

## Context: Project Structure

All new files go in the `auth/` package at the project root (e.g., `auth/token.go`, `auth/pkce.go`, `auth/oauth.go`). There is no `internal/` directory in this project.

## Detailed Directions

### 1. Define OAuth Token Structure

Create `auth/token.go`:

```go
package auth

import (
	"time"
)

// Token represents an OAuth 2.0 token pair with metadata.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// IsExpired returns true if the token is expired or expiring within 1 minute.
func (t *Token) IsExpired() bool {
	return time.Now().Add(time.Minute).After(t.ExpiresAt)
}
```

### 2. Implement PKCE Utilities

Create `auth/pkce.go`:

```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCEPair holds the code verifier and challenge.
type PKCEPair struct {
	Verifier  string
	Challenge string
}

// GeneratePKCE creates a PKCE code verifier and challenge.
func GeneratePKCE() (*PKCEPair, error) {
	// Generate 32 random bytes for the verifier
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}

	// Base64-URL encode the verifier (without padding)
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	// Create challenge: base64-URL(sha256(verifier))
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return &PKCEPair{
		Verifier:  verifier,
		Challenge: challenge,
	}, nil
}

// GenerateState creates a random state parameter for CSRF protection.
func GenerateState() (string, error) {
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(stateBytes), nil
}
```

### 3. Implement OAuth Authenticator

Create `auth/oauth.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const consentFlowTimeout = 5 * time.Minute

// OAuthConfig holds OAuth client credentials and endpoints.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	Scope        string
}

// DefaultGoogleMapsOAuthConfig returns the default Google OAuth configuration for Maps API.
func DefaultGoogleMapsOAuthConfig() *OAuthConfig {
	return &OAuthConfig{
		AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",
		Scope:    "https://www.googleapis.com/auth/maps-platform.routesPreferredApi",
	}
}

// Authenticator handles OAuth 2.0 PKCE flow.
type Authenticator struct {
	config  *OAuthConfig
	store   CredentialStore
	timeout time.Duration
}

// NewAuthenticator creates a new OAuth authenticator.
func NewAuthenticator(config *OAuthConfig, store CredentialStore) *Authenticator {
	return &Authenticator{
		config:  config,
		store:   store,
		timeout: consentFlowTimeout,
	}
}

// GetToken retrieves a valid access token, refreshing if necessary.
// If a cached token exists and is still valid, it returns immediately.
// If the token is expired but a refresh token exists, it transparently
// refreshes the token without user interaction.
// If no valid token exists, it initiates the interactive OAuth PKCE flow,
// opening a browser for user authentication.
func (a *Authenticator) GetToken(ctx context.Context) (*Token, error) {
	// Try to load existing token
	token, err := a.loadToken()
	if err == nil && !token.IsExpired() {
		return token, nil
	}

	// Token missing or expired; try refresh if we have a refresh token
	if err == nil && token.RefreshToken != "" {
		newToken, err := a.refreshToken(ctx, token)
		if err == nil {
			return newToken, nil
		}
		// If refresh fails, fall through to interactive login
	}

	// No valid token; initiate interactive OAuth flow
	return a.interactiveLogin(ctx)
}

// loadToken retrieves the token from credential store.
func (a *Authenticator) loadToken() (*Token, error) {
	tokenJSON, err := a.store.Get("oauth_token")
	if err != nil {
		return nil, err
	}

	var token Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	return &token, nil
}

// saveToken stores the token in credential store.
func (a *Authenticator) saveToken(token *Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return a.store.Set("oauth_token", string(data))
}

// interactiveLogin initiates the PKCE OAuth flow with browser redirect.
func (a *Authenticator) interactiveLogin(ctx context.Context) (*Token, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("generate PKCE: %w", err)
	}

	state, err := GenerateState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	// Start loopback listener on a random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start loopback listener: %w", err)
	}
	defer ln.Close()

	redirectURI := fmt.Sprintf("http://%s/callback", ln.Addr().String())
	authURL := a.buildAuthURL(redirectURI, state, pkce.Challenge)

	fmt.Printf("Open the following URL in your browser to authorize twisty:\n\n%s\n\nWaiting for authorization (timeout: %s)...\n", authURL, a.timeout)
	openBrowser(authURL)

	code, returnedState, err := a.waitForCallback(ctx, ln)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if returnedState != state {
		return nil, fmt.Errorf("state parameter mismatch (CSRF validation failed)")
	}

	token, err := a.exchangeCode(ctx, redirectURI, code, pkce.Verifier)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	if err := a.saveToken(token); err != nil {
		return nil, fmt.Errorf("store token: %w", err)
	}

	return token, nil
}

// buildAuthURL constructs the Google authorization URL.
func (a *Authenticator) buildAuthURL(redirectURI, state, challenge string) string {
	params := url.Values{}
	params.Set("client_id", a.config.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", a.config.Scope)
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")

	return a.config.AuthURL + "?" + params.Encode()
}

// waitForCallback listens for the OAuth callback with a timeout.
func (a *Authenticator) waitForCallback(ctx context.Context, ln net.Listener) (string, string, error) {
	codeCh := make(chan string, 1)
	stateCh := make(chan string, 1)
	errCh := make(chan error, 1)

	srv := &http.Server{}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		returnedState := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")

		if code == "" {
			select {
			case errCh <- fmt.Errorf("callback received no authorization code: %s", r.URL.Query().Get("error")):
			default:
			}
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}

		fmt.Fprint(w, "Authorization successful! You may close this tab.")
		select {
		case codeCh <- code:
		default:
		}
		select {
		case stateCh <- returnedState:
		default:
		}
	})
	go srv.Serve(ln) //nolint:errcheck

	flowCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	select {
	case code := <-codeCh:
		returnedState := <-stateCh
		srv.Close()
		return code, returnedState, nil
	case err := <-errCh:
		return "", "", fmt.Errorf("consent flow callback error: %w", err)
	case <-flowCtx.Done():
		return "", "", fmt.Errorf("consent flow timed out after %s", a.timeout)
	}
}

// exchangeCode exchanges the authorization code for a token.
func (a *Authenticator) exchangeCode(ctx context.Context, redirectURI, code, verifier string) (*Token, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("client_id", a.config.ClientID)
	data.Set("client_secret", a.config.ClientSecret)
	data.Set("code_verifier", verifier)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	return &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

// refreshToken uses the refresh token to obtain a new access token.
func (a *Authenticator) refreshToken(ctx context.Context, oldToken *Token) (*Token, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", oldToken.RefreshToken)
	data.Set("client_id", a.config.ClientID)
	data.Set("client_secret", a.config.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// If refresh fails, delete the token and force re-authentication
		_ = a.store.Delete("oauth_token")
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	token := &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: oldToken.RefreshToken, // Keep original refresh token if not updated
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if tokenResp.RefreshToken != "" {
		token.RefreshToken = tokenResp.RefreshToken
	}

	if err := a.saveToken(token); err != nil {
		return nil, fmt.Errorf("store refreshed token: %w", err)
	}

	return token, nil
}

// openBrowser opens the given URL in the default browser.
func openBrowser(urlStr string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", urlStr)
	case "linux":
		cmd = exec.Command("xdg-open", urlStr)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", urlStr)
	default:
		return
	}
	_ = cmd.Run()
}
```

### 4. Write Unit Tests

Create `auth/oauth_test.go`:

```go
package auth

import (
	"testing"
	"time"
)

func TestGeneratePKCE(t *testing.T) {
	pkce1, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}

	pkce2, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}

	if pkce1.Verifier == pkce2.Verifier {
		t.Error("PKCE verifiers should be unique")
	}

	if pkce1.Challenge == "" || pkce2.Challenge == "" {
		t.Error("PKCE challenge should not be empty")
	}
}

func TestGenerateState(t *testing.T) {
	state1, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState failed: %v", err)
	}

	state2, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState failed: %v", err)
	}

	if state1 == state2 {
		t.Error("states should be unique")
	}

	if state1 == "" || state2 == "" {
		t.Error("state should not be empty")
	}
}

func TestTokenIsExpired(t *testing.T) {
	expired := &Token{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(-time.Minute),
	}
	if !expired.IsExpired() {
		t.Error("expired token should return true for IsExpired()")
	}

	expiringSoon := &Token{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(30 * time.Second), // Within 1-minute buffer
	}
	if !expiringSoon.IsExpired() {
		t.Error("token expiring within 1 minute should return true for IsExpired()")
	}

	valid := &Token{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	if valid.IsExpired() {
		t.Error("valid token should return false for IsExpired()")
	}
}
```

### 5. Verify Build

```bash
go build ./...
go test ./auth/... -v
```

All tests should pass.

## Acceptance Criteria

- [ ] `auth/token.go` defines `Token` struct with `IsExpired()` method (1-minute buffer)
- [ ] `auth/pkce.go` implements `GeneratePKCE` (verifier and S256 challenge) and `GenerateState`
- [ ] `auth/oauth.go` implements `Authenticator` with PKCE consent flow
- [ ] `GetToken` retrieves cached token or initiates refresh/interactive login as needed
- [ ] `interactiveLogin` opens browser and starts loopback listener on random port
- [ ] Loopback listener captures authorization code and state via HTTP handler
- [ ] `exchangeCode` exchanges code for token via Google token endpoint
- [ ] `refreshToken` uses refresh token to obtain new access token; deletes stale token on failure
- [ ] Token is persisted to credential store after acquisition
- [ ] 5-minute timeout enforced on authentication flow
- [ ] State parameter validated for CSRF protection
- [ ] Unit tests pass for PKCE generation, state generation, and token expiry
- [ ] `go build ./...` compiles without errors

## Notes

- Built-in credentials (client ID/secret) will be set on `OAuthConfig` before creating `Authenticator`. How to supply these (compile-time constants, config file, etc.) is an implementation decision for Task 006.
- The loopback listener approach avoids the need for a persistent web server and stays local-only.
- Token refresh happens transparently; users should not be prompted within a 24-hour period under normal usage.
- The 5-minute timeout prevents users from being stuck waiting indefinitely.

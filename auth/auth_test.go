package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func TestFileStoreErrNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(filepath.Join(tmpDir, "creds.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	_, err = store.Get("missing-key")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFileStoreDeleteAndPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "creds.json")

	store1, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	if err := store1.Set("key", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store1.Delete("key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Reload from disk and confirm key is absent.
	store2, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("NewFileStore (second): %v", err)
	}
	_, err = store2.Get("key")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete+reload, got %v", err)
	}
}

func TestWaitForCallback_Success(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	cfg := &OAuthConfig{ClientID: "c", AuthURL: "http://a", TokenURL: "http://t"}
	a := NewAuthenticator(cfg, &noopStore{})
	a.timeout = 5 * time.Second

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		code, _, err := a.waitForCallback(context.Background(), ln)
		if err != nil {
			errCh <- err
			return
		}
		codeCh <- code
	}()

	// Give the server a moment to start.
	time.Sleep(10 * time.Millisecond)

	callbackURL := fmt.Sprintf("http://%s/callback?code=mycode&state=mystate", ln.Addr().String())
	resp, err := http.Get(callbackURL) //nolint:noctx
	if err != nil {
		t.Fatalf("http.Get callback: %v", err)
	}
	resp.Body.Close()

	select {
	case code := <-codeCh:
		if code != "mycode" {
			t.Errorf("code = %q, want %q", code, "mycode")
		}
	case err := <-errCh:
		t.Fatalf("waitForCallback returned error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback")
	}
}

func TestWaitForCallback_NoCode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	cfg := &OAuthConfig{ClientID: "c", AuthURL: "http://a", TokenURL: "http://t"}
	a := NewAuthenticator(cfg, &noopStore{})
	a.timeout = 5 * time.Second

	errCh := make(chan error, 1)
	go func() {
		_, _, err := a.waitForCallback(context.Background(), ln)
		errCh <- err
	}()

	time.Sleep(10 * time.Millisecond)

	callbackURL := fmt.Sprintf("http://%s/callback?error=access_denied", ln.Addr().String())
	resp, err := http.Get(callbackURL) //nolint:noctx
	if err != nil {
		t.Fatalf("http.Get callback: %v", err)
	}
	resp.Body.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error when no code is returned")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback error")
	}
}

// noopStore is a minimal CredentialStore for tests that don't need persistence.
type noopStore struct{}

func (n *noopStore) Get(_ string) (string, error)    { return "", ErrNotFound }
func (n *noopStore) Set(_, _ string) error           { return nil }
func (n *noopStore) Delete(_ string) error           { return nil }

func TestWaitForCallback_Timeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	cfg := &OAuthConfig{ClientID: "c", AuthURL: "http://a", TokenURL: "http://t"}
	a := NewAuthenticator(cfg, &noopStore{})
	a.timeout = 50 * time.Millisecond // very short timeout

	_, _, err = a.waitForCallback(context.Background(), ln)
	if err == nil {
		t.Error("expected timeout error from waitForCallback")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDefaultGoogleMapsOAuthConfig(t *testing.T) {
	cfg := DefaultGoogleMapsOAuthConfig()
	if cfg == nil {
		t.Fatal("DefaultGoogleMapsOAuthConfig returned nil")
	}
	if cfg.AuthURL == "" {
		t.Error("AuthURL should not be empty")
	}
	if cfg.TokenURL == "" {
		t.Error("TokenURL should not be empty")
	}
	if cfg.Scope == "" {
		t.Error("Scope should not be empty")
	}
}

func TestNewCredentialStore(t *testing.T) {
	store, err := NewCredentialStore("twisty-test")
	if err != nil {
		t.Fatalf("NewCredentialStore: %v", err)
	}
	if store == nil {
		t.Error("NewCredentialStore should return a non-nil store")
	}
}

func TestKeychainStore_SetGetDelete(t *testing.T) {
	// Use a unique service name to avoid collisions with real credentials.
	store := NewKeychainStore("twisty-test-" + t.Name())

	const key = "test-key"
	const value = "test-value-42"

	// Ensure clean state.
	_ = store.Delete(key)

	// Get on missing key should return ErrNotFound.
	_, err := store.Get(key)
	if err != ErrNotFound {
		t.Logf("Get on missing key returned %v (expected ErrNotFound; keychain may differ)", err)
	}

	if err := store.Set(key, value); err != nil {
		t.Skipf("KeychainStore.Set failed (keychain may be unavailable in this environment): %v", err)
	}
	defer store.Delete(key) //nolint:errcheck

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("KeychainStore.Get: %v", err)
	}
	if got != value {
		t.Errorf("Get = %q, want %q", got, value)
	}

	if err := store.Delete(key); err != nil {
		t.Fatalf("KeychainStore.Delete: %v", err)
	}
	_, err = store.Get(key)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestGetToken_ValidCached(t *testing.T) {
	// When a valid non-expired token is stored, GetToken should return it without
	// attempting a refresh or interactive login.
	tmpDir := t.TempDir()
	store, err := NewFileStore(filepath.Join(tmpDir, "creds.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	cfg := &OAuthConfig{
		ClientID: "test-client",
		AuthURL:  "https://accounts.example.com/auth",
		TokenURL: "https://accounts.example.com/token",
	}
	a := NewAuthenticator(cfg, store)

	want := &Token{
		AccessToken:  "cached-access",
		RefreshToken: "cached-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	if err := a.saveToken(want); err != nil {
		t.Fatalf("saveToken: %v", err)
	}

	got, err := a.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got.AccessToken != want.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, want.AccessToken)
	}
}

func TestBuildAuthURL(t *testing.T) {
	cfg := &OAuthConfig{
		ClientID: "my-client-id",
		AuthURL:  "https://accounts.example.com/auth",
		Scope:    "openid",
	}
	store, err := NewFileStore(filepath.Join(t.TempDir(), "creds.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	a := NewAuthenticator(cfg, store)

	redirectURI := "http://localhost:12345/callback"
	state := "test-state-value"
	challenge := "test-challenge-value"

	rawURL := a.buildAuthURL(redirectURI, state, challenge)
	if !strings.HasPrefix(rawURL, cfg.AuthURL) {
		t.Errorf("URL should start with AuthURL, got %q", rawURL)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	q := parsed.Query()

	checks := map[string]string{
		"response_type":         "code",
		"client_id":             "my-client-id",
		"redirect_uri":          redirectURI,
		"state":                 state,
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
	}
	for param, want := range checks {
		if got := q.Get(param); got != want {
			t.Errorf("param %q = %q, want %q", param, got, want)
		}
	}
}

func TestLoadAndSaveToken(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(filepath.Join(tmpDir, "creds.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	cfg := &OAuthConfig{
		ClientID: "test-client",
		AuthURL:  "https://accounts.example.com/auth",
		TokenURL: "https://accounts.example.com/token",
	}
	a := NewAuthenticator(cfg, store)

	// loadToken on empty store should return an error.
	if _, err := a.loadToken(); err == nil {
		t.Error("loadToken on empty store should return an error")
	}

	want := &Token{
		AccessToken:  "acc-123",
		RefreshToken: "ref-456",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}
	if err := a.saveToken(want); err != nil {
		t.Fatalf("saveToken: %v", err)
	}

	got, err := a.loadToken()
	if err != nil {
		t.Fatalf("loadToken: %v", err)
	}
	if got.AccessToken != want.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, want.AccessToken)
	}
	if got.RefreshToken != want.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, want.RefreshToken)
	}
	if got.TokenType != want.TokenType {
		t.Errorf("TokenType = %q, want %q", got.TokenType, want.TokenType)
	}
}

// tokenServerResponse is the JSON shape returned by the token endpoint.
type tokenServerResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func TestExchangeCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))

		if vals.Get("grant_type") != "authorization_code" {
			http.Error(w, "bad grant_type", http.StatusBadRequest)
			return
		}

		resp := tokenServerResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &OAuthConfig{
		ClientID:     "cid",
		ClientSecret: "csecret",
		TokenURL:     srv.URL,
	}
	a := NewAuthenticator(cfg, &noopStore{})

	token, err := a.exchangeCode(context.Background(), "http://localhost/cb", "auth-code", "verifier")
	if err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}
	if token.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "new-access")
	}
}

func TestExchangeCode_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &OAuthConfig{ClientID: "cid", ClientSecret: "csecret", TokenURL: srv.URL}
	a := NewAuthenticator(cfg, &noopStore{})

	_, err := a.exchangeCode(context.Background(), "http://localhost/cb", "auth-code", "verifier")
	if err == nil {
		t.Error("exchangeCode should return error on HTTP 401")
	}
}

func TestRefreshToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))

		if vals.Get("grant_type") != "refresh_token" {
			http.Error(w, "bad grant_type", http.StatusBadRequest)
			return
		}

		resp := tokenServerResponse{
			AccessToken: "refreshed-access",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &OAuthConfig{ClientID: "cid", ClientSecret: "csecret", TokenURL: srv.URL}
	store := &noopStore{}
	a := NewAuthenticator(cfg, store)

	old := &Token{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	token, err := a.refreshToken(context.Background(), old)
	if err != nil {
		t.Fatalf("refreshToken: %v", err)
	}
	if token.AccessToken != "refreshed-access" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "refreshed-access")
	}
	// When no new refresh token returned, old one should be preserved.
	if token.RefreshToken != "old-refresh" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "old-refresh")
	}
}

func TestRefreshToken_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &OAuthConfig{ClientID: "cid", ClientSecret: "csecret", TokenURL: srv.URL}
	a := NewAuthenticator(cfg, &noopStore{})

	old := &Token{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	_, err := a.refreshToken(context.Background(), old)
	if err == nil {
		t.Error("refreshToken should return error on HTTP 401")
	}
}

func TestLoadToken_CorruptJSON(t *testing.T) {
	cfg := &OAuthConfig{ClientID: "c", AuthURL: "http://a", TokenURL: "http://t"}
	a := NewAuthenticator(cfg, &corruptJSONStore{})
	_, err := a.loadToken()
	if err == nil {
		t.Error("loadToken with corrupt JSON should return error")
	}
}

// corruptJSONStore always returns invalid JSON for Get.
type corruptJSONStore struct{ noopStore }

func (c *corruptJSONStore) Get(_ string) (string, error) { return "not-valid-json!!!", nil }

func TestExchangeCode_BadJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "not-json")
	}))
	defer srv.Close()

	cfg := &OAuthConfig{ClientID: "cid", ClientSecret: "csecret", TokenURL: srv.URL}
	a := NewAuthenticator(cfg, &noopStore{})
	_, err := a.exchangeCode(context.Background(), "http://localhost/cb", "code", "verifier")
	if err == nil {
		t.Error("exchangeCode should return error for invalid JSON response")
	}
}

func TestRefreshToken_BadJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "not-json")
	}))
	defer srv.Close()

	cfg := &OAuthConfig{ClientID: "cid", ClientSecret: "csecret", TokenURL: srv.URL}
	a := NewAuthenticator(cfg, &noopStore{})
	old := &Token{AccessToken: "old", RefreshToken: "ref", ExpiresAt: time.Now().Add(-time.Hour)}
	_, err := a.refreshToken(context.Background(), old)
	if err == nil {
		t.Error("refreshToken should return error for invalid JSON response")
	}
}

func TestRefreshToken_SaveFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tokenServerResponse{AccessToken: "new", TokenType: "Bearer", ExpiresIn: 3600}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &OAuthConfig{ClientID: "cid", ClientSecret: "csecret", TokenURL: srv.URL}
	a := NewAuthenticator(cfg, &failSetStore{})
	old := &Token{AccessToken: "old", RefreshToken: "ref", ExpiresAt: time.Now().Add(-time.Hour)}
	_, err := a.refreshToken(context.Background(), old)
	if err == nil {
		t.Error("refreshToken should return error when saveToken fails")
	}
}

// failSetStore returns an error on Set.
type failSetStore struct{ noopStore }

func (f *failSetStore) Set(_, _ string) error { return fmt.Errorf("write failed") }

func TestFileStore_LoadCorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "creds.json")
	if err := os.WriteFile(path, []byte("not-json!!!"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := NewFileStore(path)
	if err == nil {
		t.Error("NewFileStore should fail when existing credentials file contains invalid JSON")
	}
}

func TestExchangeCode_InvalidTokenURL(t *testing.T) {
	cfg := &OAuthConfig{ClientID: "cid", TokenURL: "://invalid-url"}
	a := NewAuthenticator(cfg, &noopStore{})
	_, err := a.exchangeCode(context.Background(), "http://localhost/cb", "code", "verifier")
	if err == nil {
		t.Error("exchangeCode should fail with invalid token URL")
	}
}

func TestRefreshToken_InvalidTokenURL(t *testing.T) {
	cfg := &OAuthConfig{ClientID: "cid", TokenURL: "://invalid-url"}
	a := NewAuthenticator(cfg, &noopStore{})
	old := &Token{AccessToken: "old", RefreshToken: "ref", ExpiresAt: time.Now().Add(-time.Hour)}
	_, err := a.refreshToken(context.Background(), old)
	if err == nil {
		t.Error("refreshToken should fail with invalid token URL")
	}
}

func TestGetToken_InteractiveFlowTimeout(t *testing.T) {
	// With no stored token and no refresh token, GetToken falls through to interactiveLogin.
	// A very short timeout makes waitForCallback return immediately, covering interactiveLogin.
	cfg := &OAuthConfig{ClientID: "test-client", AuthURL: "http://127.0.0.1:1/auth", TokenURL: "http://t"}
	a := NewAuthenticator(cfg, &noopStore{})
	a.timeout = 1 * time.Millisecond
	a.openBrowserFn = func(string) {} // suppress actual browser open

	_, err := a.GetToken(context.Background())
	if err == nil {
		t.Error("GetToken should return error when interactive flow times out")
	}
}

func TestGetToken_RefreshExpired(t *testing.T) {
	// When the stored token is expired and has a refresh token, GetToken should
	// attempt to refresh using the token endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tokenServerResponse{
			AccessToken: "refreshed-access",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	store, err := NewFileStore(filepath.Join(tmpDir, "creds.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	cfg := &OAuthConfig{
		ClientID:     "cid",
		ClientSecret: "csecret",
		TokenURL:     srv.URL,
	}
	a := NewAuthenticator(cfg, store)

	expired := &Token{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	if err := a.saveToken(expired); err != nil {
		t.Fatalf("saveToken: %v", err)
	}

	got, err := a.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got.AccessToken != "refreshed-access" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "refreshed-access")
	}
}

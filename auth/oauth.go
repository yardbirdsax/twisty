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

// DefaultGoogleMapsOAuthConfig returns the default Google OAuth configuration for the Routes API.
func DefaultGoogleMapsOAuthConfig() *OAuthConfig {
	return &OAuthConfig{
		AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",
		Scope:    "https://www.googleapis.com/auth/maps-platform.routespreferred",
	}
}

// Authenticator handles OAuth 2.0 PKCE flow.
type Authenticator struct {
	config        *OAuthConfig
	store         CredentialStore
	timeout       time.Duration
	openBrowserFn func(string)
}

// NewAuthenticator creates a new OAuth authenticator.
func NewAuthenticator(config *OAuthConfig, store CredentialStore) *Authenticator {
	return &Authenticator{
		config:        config,
		store:         store,
		timeout:       consentFlowTimeout,
		openBrowserFn: openBrowser,
	}
}

// GetToken retrieves a valid access token using the following strategy:
// 1. Load any stored token; if it is still valid, return it immediately.
// 2. If the stored token is expired but has a refresh token, attempt a silent refresh.
// 3. If no token exists or the refresh fails, fall back to an interactive browser-based OAuth 2.0 PKCE flow.
func (a *Authenticator) GetToken(ctx context.Context) (*Token, error) {
	token, err := a.loadToken()
	if err == nil && !token.IsExpired() {
		return token, nil
	}

	if err == nil && token.RefreshToken != "" {
		newToken, err := a.refreshToken(ctx, token)
		if err == nil {
			return newToken, nil
		}
	}

	return a.interactiveLogin(ctx)
}

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

func (a *Authenticator) saveToken(token *Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return a.store.Set("oauth_token", string(data))
}

func (a *Authenticator) interactiveLogin(ctx context.Context) (*Token, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("generate PKCE: %w", err)
	}

	state, err := GenerateState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start loopback listener: %w", err)
	}
	defer ln.Close()

	redirectURI := fmt.Sprintf("http://%s/callback", ln.Addr().String())
	authURL := a.buildAuthURL(redirectURI, state, pkce.Challenge)

	fmt.Printf("Open the following URL in your browser to authorize twisty:\n\n%s\n\nWaiting for authorization (timeout: %s)...\n", authURL, a.timeout)
	a.openBrowserFn(authURL)

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
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx) //nolint:errcheck
		return code, returnedState, nil
	case err := <-errCh:
		return "", "", fmt.Errorf("consent flow callback error: %w", err)
	case <-flowCtx.Done():
		return "", "", fmt.Errorf("consent flow timed out after %s", a.timeout)
	}
}

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
		RefreshToken: oldToken.RefreshToken,
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

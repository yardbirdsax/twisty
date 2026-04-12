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
		ExpiresAt:   time.Now().Add(30 * time.Second),
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

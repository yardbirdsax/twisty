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
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}

	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

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

package auth

import (
	"errors"
	"fmt"
	"os"
	"runtime"
)

var ErrNotFound = errors.New("credential not found")

// CredentialStore is a secure key-value store for sensitive credentials.
// Implementations must never log the stored values.
type CredentialStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// NewCredentialStore creates a platform-appropriate credential store.
// On macOS, returns a Keychain-based store.
// On other platforms, returns a file-based store.
func NewCredentialStore(service string) (CredentialStore, error) {
	if runtime.GOOS == "darwin" {
		return NewKeychainStore(service), nil
	}

	// Fallback: use file-based store
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not determine home directory: %w", err)
	}
	credPath := fmt.Sprintf("%s/.twisty/credentials/%s.json", home, service)
	return NewFileStore(credPath)
}

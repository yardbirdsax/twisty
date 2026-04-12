package auth

import "errors"

var ErrNotFound = errors.New("credential not found")

// CredentialStore defines the interface for secure credential persistence.
type CredentialStore interface {
	// Get retrieves a credential value by key.
	// Returns ErrNotFound if the key does not exist.
	Get(key string) (string, error)

	// Set stores a credential value under the given key.
	Set(key, value string) error

	// Delete removes a credential by key.
	// Does not error if the key does not exist.
	Delete(key string) error
}

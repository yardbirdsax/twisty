# Task 002: Implement Credential Store Abstraction and Platform-Specific Storage

## Summary

Implement the credential store abstraction with platform-specific implementations for secure token storage. On macOS, use the system keychain; on other platforms, provide a secure file-based fallback. This enables transparent credential persistence across OAuth sessions without exposing storage details to callers.

## Dependencies

Task 001 - the `CredentialStore` interface must be defined first.

## Context: Project Structure

All new files go in the `auth/` package at the project root (e.g., `auth/keychain.go`, `auth/filestore.go`). There is no `internal/` directory in this project.

## Detailed Directions

### 1. Implement Keychain-Based Credential Store (macOS)

Create `auth/keychain.go`:

```go
package auth

import (
	"fmt"

	"github.com/keybase/go-keychain"
)

// KeychainStore implements CredentialStore using macOS Keychain.
type KeychainStore struct {
	service string
}

// NewKeychainStore creates a new Keychain-based credential store.
func NewKeychainStore(service string) *KeychainStore {
	return &KeychainStore{service: service}
}

// Get retrieves a credential from the Keychain.
func (ks *KeychainStore) Get(key string) (string, error) {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ks.service)
	item.SetAccount(key)
	item.SetReturnData(true)

	results, err := keychain.QueryItem(item)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", ErrNotFound
	}

	return string(results[0].Data), nil
}

// Set stores a credential in the Keychain.
func (ks *KeychainStore) Set(key, value string) error {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ks.service)
	item.SetAccount(key)
	item.SetData([]byte(value))
	item.SetAccessible(keychain.AccessibleAfterFirstUnlock)

	// Try to delete existing item first
	_ = keychain.DeleteItem(item)

	// Now add the new item
	return keychain.AddItem(item)
}

// Delete removes a credential from the Keychain.
func (ks *KeychainStore) Delete(key string) error {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ks.service)
	item.SetAccount(key)

	return keychain.DeleteItem(item)
}
```

### 2. Implement Fallback File-Based Credential Store

Create `auth/filestore.go` for non-macOS systems:

```go
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileStore implements CredentialStore using file-based storage.
// Used as a fallback on systems where keychain is not available.
type FileStore struct {
	path string
	data map[string]string
}

// NewFileStore creates a new file-based credential store.
// Path should be in a secure, user-specific directory (e.g., ~/.twisty/credentials).
func NewFileStore(path string) (*FileStore, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create credentials directory: %w", err)
	}

	fs := &FileStore{
		path: path,
		data: make(map[string]string),
	}

	// Load existing data if file exists
	if _, err := os.Stat(path); err == nil {
		if err := fs.load(); err != nil {
			return nil, fmt.Errorf("failed to load credentials: %w", err)
		}
	}

	return fs, nil
}

// Get retrieves a credential from the file store.
func (fs *FileStore) Get(key string) (string, error) {
	if value, ok := fs.data[key]; ok {
		return value, nil
	}
	return "", ErrNotFound
}

// Set stores a credential in the file store.
func (fs *FileStore) Set(key, value string) error {
	fs.data[key] = value
	return fs.save()
}

// Delete removes a credential from the file store.
func (fs *FileStore) Delete(key string) error {
	delete(fs.data, key)
	return fs.save()
}

func (fs *FileStore) load() error {
	data, err := os.ReadFile(fs.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &fs.data)
}

func (fs *FileStore) save() error {
	data, err := json.Marshal(fs.data)
	if err != nil {
		return err
	}
	return os.WriteFile(fs.path, data, 0600)
}
```

### 3. Create Store Factory Function

Update `auth/store.go` with a factory function:

```go
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
```

### 4. Add Dependency Management

Update `go.mod` to include the keychain library (for macOS):

```bash
go get github.com/keybase/go-keychain
```

Note: The file-based store uses only standard library, so no additional dependencies are needed for the fallback.

### 5. Write Unit Tests

Create `auth/store_test.go`:

```go
package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStore(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "creds.json")

	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Test Set and Get
	if err := store.Set("token", "test-value"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	value, err := store.Get("token")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if value != "test-value" {
		t.Errorf("expected 'test-value', got '%s'", value)
	}

	// Test ErrNotFound
	_, err = store.Get("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Test Delete
	if err := store.Delete("token"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = store.Get("token")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestFileStorePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "creds.json")

	// Create store and set value
	store1, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := store1.Set("token", "persisted-value"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Create new store instance from same path
	store2, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Verify persisted value is loaded
	value, err := store2.Get("token")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if value != "persisted-value" {
		t.Errorf("expected 'persisted-value', got '%s'", value)
	}
}
```

### 6. Verify Tests Pass

Run the test suite:

```bash
go test ./auth/... -v
```

All tests should pass.

## Acceptance Criteria

- [ ] `auth/keychain.go` implements `CredentialStore` using macOS Keychain
- [ ] `auth/filestore.go` implements `CredentialStore` using file-based storage
- [ ] `NewCredentialStore` factory function routes to appropriate implementation based on `runtime.GOOS`
- [ ] File store creates directory with 0700 permissions if not present
- [ ] File store persists data to JSON file with 0600 permissions
- [ ] `Get` returns `ErrNotFound` for non-existent keys
- [ ] `Set` stores and retrieves values correctly
- [ ] `Delete` removes values and persists deletion
- [ ] Unit tests pass for `FileStore` (Set, Get, Delete, ErrNotFound, persistence)
- [ ] `go build ./...` compiles without errors

## Notes

- The Keychain implementation uses `github.com/keybase/go-keychain`, a battle-tested library for macOS Keychain access.
- File store is human-readable JSON for debugging but with restrictive file permissions (0600).
- The factory function ensures callers never need to know which implementation is in use.
- For future platforms (Linux, Windows), additional implementations can be added to the factory.

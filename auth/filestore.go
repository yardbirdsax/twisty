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
func NewFileStore(path string) (*FileStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create credentials directory: %w", err)
	}

	fs := &FileStore{
		path: path,
		data: make(map[string]string),
	}

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

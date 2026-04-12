package auth

import (
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

	_, err = store.Get("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

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

	store1, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := store1.Set("token", "persisted-value"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	store2, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	value, err := store2.Get("token")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if value != "persisted-value" {
		t.Errorf("expected 'persisted-value', got '%s'", value)
	}
}

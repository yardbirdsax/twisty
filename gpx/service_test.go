package gpx

import (
	"errors"
	"testing"

	"github.com/yardbirdsax/twisty/auth"
)

// MockCredentialStore implements auth.CredentialStore for testing.
type MockCredentialStore struct {
	data map[string]string
}

func NewMockCredentialStore() *MockCredentialStore {
	return &MockCredentialStore{data: make(map[string]string)}
}

func (m *MockCredentialStore) Get(key string) (string, error) {
	if val, ok := m.data[key]; ok {
		return val, nil
	}
	return "", errNotFound
}

func (m *MockCredentialStore) Set(key, value string) error {
	m.data[key] = value
	return nil
}

func (m *MockCredentialStore) Delete(key string) error {
	delete(m.data, key)
	return nil
}

// errNotFound is a local sentinel for testing — mirrors auth.ErrNotFound.
var errNotFound = errors.New("credential not found")

func TestNewService(t *testing.T) {
	store := NewMockCredentialStore()
	oauthConfig := auth.DefaultGoogleMapsOAuthConfig()
	authenticator := auth.NewAuthenticator(oauthConfig, store)
	svc := NewService(authenticator)
	if svc == nil {
		t.Error("NewService should return a non-nil Service")
	}
}

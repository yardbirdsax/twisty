package gpx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// stubRouteGetter is a RouteGetter that returns fixed data or a fixed error.
type stubRouteGetter struct {
	data *RouteData
	err  error
}

func (s *stubRouteGetter) GetRoute(_ context.Context, _ string) (*RouteData, error) {
	return s.data, s.err
}

func TestConvertWithClient_Success(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	svc := &Service{}
	stub := &stubRouteGetter{data: testRouteData()}

	if err := svc.convertWithClient(context.Background(), stub, "https://maps.google.com/maps/dir/Home/Work", outPath); err != nil {
		t.Fatalf("convertWithClient: %v", err)
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("expected output file to exist: %v", err)
	}
}

func TestConvertWithClient_RouteGetterError(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "route.gpx")

	svc := &Service{}
	stub := &stubRouteGetter{err: errors.New("API failure")}

	if err := svc.convertWithClient(context.Background(), stub, "https://maps.google.com/maps/dir/Home/Work", outPath); err == nil {
		t.Error("expected error when RouteGetter returns an error")
	}
}

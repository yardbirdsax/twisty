package gpx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewService(t *testing.T) {
	svc := NewService("test-api-key")
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

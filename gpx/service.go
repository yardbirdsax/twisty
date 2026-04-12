package gpx

import (
	"context"
	"fmt"
)

// RouteGetter retrieves route data from a Google Maps URL.
type RouteGetter interface {
	GetRoute(ctx context.Context, mapsURL string) (*RouteData, error)
}

// Service orchestrates the GPX conversion pipeline.
type Service struct {
	apiKey string
}

// NewService creates a new GPX conversion service.
func NewService(apiKey string) *Service {
	return &Service{apiKey: apiKey}
}

// ConvertToFile is the end-to-end pipeline: it fetches the route from the
// Routes API using the provided API key and writes a GPX 1.1 file to outPath.
func (s *Service) ConvertToFile(ctx context.Context, mapsURL, outPath string) error {
	client := NewMapsClient(s.apiKey)
	return s.convertWithClient(ctx, client, mapsURL, outPath)
}

// convertWithClient converts a Google Maps URL to a GPX file using the provided RouteGetter.
// Separated from ConvertToFile to allow unit testing with a mock client.
func (s *Service) convertWithClient(ctx context.Context, client RouteGetter, mapsURL, outPath string) error {
	routeData, err := client.GetRoute(ctx, mapsURL)
	if err != nil {
		return err // Already has a user-friendly message from GetRoute
	}

	// Generate GPX file
	if err := ConvertRouteToGPX(routeData, outPath); err != nil {
		return fmt.Errorf("failed to generate GPX file: %w", err)
	}

	return nil
}

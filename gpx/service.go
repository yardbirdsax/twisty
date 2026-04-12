package gpx

import (
	"context"
	"fmt"

	"github.com/yardbirdsax/twisty/auth"
)

// Service orchestrates the GPX conversion pipeline.
type Service struct {
	authenticator *auth.Authenticator
}

// NewService creates a new GPX conversion service.
func NewService(authenticator *auth.Authenticator) *Service {
	return &Service{authenticator: authenticator}
}

// ConvertToFile converts a Google Maps URL to a GPX file at outPath.
func (s *Service) ConvertToFile(ctx context.Context, mapsURL, outPath string) error {
	// Get or refresh authentication token
	token, err := s.authenticator.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Extract route from Google Maps
	client := NewMapsClient(token.AccessToken)
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

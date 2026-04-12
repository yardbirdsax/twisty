package gpx

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateGoogleMapsURL checks if the provided URL is a valid Google Maps shared link.
func ValidateGoogleMapsURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	host := parsed.Host
	if !strings.Contains(host, "maps.google.com") &&
		!strings.Contains(host, "maps.app.goo.gl") &&
		!(strings.Contains(host, "google.com") && strings.Contains(parsed.Path, "/maps")) {
		return fmt.Errorf("not a valid Google Maps shared link. Expected format: maps.google.com/maps/dir/... or maps.app.goo.gl/...")
	}

	return nil
}

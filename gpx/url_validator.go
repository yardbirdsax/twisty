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
	if host != "maps.google.com" &&
		host != "maps.app.goo.gl" &&
		!((host == "google.com" || host == "www.google.com") && strings.HasPrefix(parsed.Path, "/maps/")) {
		return fmt.Errorf("not a valid Google Maps shared link. Expected format: maps.google.com/maps/dir/... or maps.app.goo.gl/...")
	}

	return nil
}

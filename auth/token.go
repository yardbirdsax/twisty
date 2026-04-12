package auth

import (
	"time"
)

// Token represents an OAuth 2.0 token pair with metadata.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// IsExpired returns true if the token is expired or expiring within 1 minute.
func (t *Token) IsExpired() bool {
	return time.Now().Add(time.Minute).After(t.ExpiresAt)
}

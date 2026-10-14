package hytaleapi

import (
	"context"
	"net/http"
	"time"
)

// Profile is a game profile owned by the authenticated account.
type Profile struct {
	UUID     string `json:"uuid"`
	Username string `json:"username"`
}

// Profiles lists the game profiles of an account.
type Profiles struct {
	Owner    string    `json:"owner"`
	Profiles []Profile `json:"profiles"`
}

// GameSession holds the tokens a server needs to authenticate.
type GameSession struct {
	SessionToken  string    `json:"sessionToken"`
	IdentityToken string    `json:"identityToken"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// GetProfiles lists the game profiles available to the access token.
func (c *Client) GetProfiles(ctx context.Context, accessToken string) (Profiles, error) {
	var out Profiles
	err := c.sendJSON(ctx, http.MethodGet, c.endpoints.Profiles, accessToken, nil, &out)
	return out, err
}

// NewGameSession creates a game session for the given profile.
func (c *Client) NewGameSession(ctx context.Context, accessToken, profileUUID string) (GameSession, error) {
	var out GameSession
	body := map[string]string{"uuid": profileUUID}
	err := c.sendJSON(ctx, http.MethodPost, c.endpoints.GameSessionNew, accessToken, body, &out)
	return out, err
}

// TerminateGameSession ends the session identified by sessionToken.
func (c *Client) TerminateGameSession(ctx context.Context, sessionToken string) error {
	return c.sendJSON(ctx, http.MethodDelete, c.endpoints.GameSession, sessionToken, nil, nil)
}

package hytaleauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"hytale-server-manager/internal/hytaleapi"
)

// Errors returned by the Service.
var (
	ErrNotAuthenticated = errors.New("no Hytale account is connected")
	ErrNoProfile        = errors.New("no Hytale profile selected")
	ErrUnknownProfile   = errors.New("profil Hytale inconnu")
	ErrReauthRequired   = errors.New("the Hytale session expired, reconnect the account")
)

const (
	accessTokenMargin = 2 * time.Minute
	// Refresh tokens live 30 days and rotate on use: refreshing daily keeps
	// them alive even when the game server is stopped for a long time.
	keepAliveAge   = 24 * time.Hour
	keepAliveCheck = time.Hour
)

// Status summarises the Hytale authentication for the interface.
type Status struct {
	Authenticated bool        `json:"authenticated"`
	ProfileUUID   string      `json:"profileUuid,omitempty"`
	ProfileName   string      `json:"profileName,omitempty"`
	RefreshedAt   time.Time   `json:"refreshedAt,omitzero"`
	Login         LoginStatus `json:"login"`
}

// GameSessionGrant holds what the server process needs to authenticate.
type GameSessionGrant struct {
	SessionToken  string
	IdentityToken string
	OwnerUUID     string
	ExpiresAt     time.Time
}

// Service owns the Hytale credentials of the server operator.
type Service struct {
	api   *hytaleapi.Client
	store *TokenStore
	login *DeviceLogin

	mu       sync.Mutex // serialises token rotation
	creds    Credentials
	hasCreds bool
}

// NewService loads persisted credentials and prepares the login flow.
func NewService(api *hytaleapi.Client, store *TokenStore) (*Service, error) {
	creds, found, err := store.Load()
	if err != nil {
		return nil, err
	}
	s := &Service{api: api, store: store, creds: creds, hasCreds: found}
	s.login = NewDeviceLogin(api, s.completeLogin)
	return s, nil
}

// Status returns the current authentication status.
func (s *Service) Status() Status {
	s.mu.Lock()
	st := Status{
		Authenticated: s.hasCreds,
		ProfileUUID:   s.creds.ProfileUUID,
		ProfileName:   s.creds.ProfileName,
		RefreshedAt:   s.creds.RefreshedAt,
	}
	s.mu.Unlock()
	st.Login = s.login.Status()
	return st
}

// Authenticated reports whether usable credentials are stored.
func (s *Service) Authenticated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasCreds
}

// Ready reports whether game sessions can be created (account + profile).
func (s *Service) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasCreds && s.creds.ProfileUUID != ""
}

// AccessToken returns a valid OAuth access token, refreshing it if needed.
func (s *Service) AccessToken(ctx context.Context) (string, error) {
	return s.accessToken(ctx)
}

// StartLogin starts (or returns) the device code flow.
func (s *Service) StartLogin(ctx context.Context) (LoginStatus, error) {
	return s.login.Start(ctx)
}

// CancelLogin aborts a pending device code flow.
func (s *Service) CancelLogin() { s.login.Cancel() }

// Logout forgets the stored credentials.
func (s *Service) Logout() error {
	s.login.Cancel()
	s.login.Reset()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clearLocked()
}

// Profiles lists the game profiles of the connected account.
func (s *Service) Profiles(ctx context.Context) ([]hytaleapi.Profile, error) {
	token, err := s.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	profiles, err := s.api.GetProfiles(ctx, token)
	if err != nil {
		return nil, err
	}
	if profiles.Profiles == nil {
		return []hytaleapi.Profile{}, nil
	}
	return profiles.Profiles, nil
}

// SelectProfile chooses the profile used to create game sessions.
func (s *Service) SelectProfile(ctx context.Context, uuid string) error {
	profiles, err := s.Profiles(ctx)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if p.UUID == uuid {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.creds.ProfileUUID, s.creds.ProfileName = p.UUID, p.Username
			return s.store.Save(s.creds)
		}
	}
	return ErrUnknownProfile
}

// NewGameSession creates a fresh game session for the selected profile.
func (s *Service) NewGameSession(ctx context.Context) (GameSessionGrant, error) {
	token, err := s.accessToken(ctx)
	if err != nil {
		return GameSessionGrant{}, err
	}
	s.mu.Lock()
	profile := s.creds.ProfileUUID
	s.mu.Unlock()
	if profile == "" {
		return GameSessionGrant{}, ErrNoProfile
	}
	session, err := s.api.NewGameSession(ctx, token, profile)
	if err != nil {
		return GameSessionGrant{}, fmt.Errorf("create game session: %w", err)
	}
	return GameSessionGrant{
		SessionToken:  session.SessionToken,
		IdentityToken: session.IdentityToken,
		OwnerUUID:     profile,
		ExpiresAt:     session.ExpiresAt,
	}, nil
}

// TerminateGameSession ends a game session (best effort on server stop).
func (s *Service) TerminateGameSession(ctx context.Context, sessionToken string) error {
	return s.api.TerminateGameSession(ctx, sessionToken)
}

// KeepAlive periodically rotates the refresh token so it never expires.
func (s *Service) KeepAlive(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(keepAliveCheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := s.refreshIfStale(ctx); err != nil {
			logger.Warn("hytale token keep-alive failed", "error", err)
		}
	}
}

func (s *Service) refreshIfStale(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasCreds || time.Since(s.creds.RefreshedAt) < keepAliveAge {
		return nil
	}
	_, err := s.refreshLocked(ctx)
	return err
}

func (s *Service) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasCreds {
		return "", ErrNotAuthenticated
	}
	if time.Until(s.creds.AccessExpiresAt) > accessTokenMargin {
		return s.creds.AccessToken, nil
	}
	return s.refreshLocked(ctx)
}

func (s *Service) refreshLocked(ctx context.Context) (string, error) {
	tok, err := s.api.RefreshToken(ctx, s.creds.RefreshToken)
	if hytaleapi.IsUnauthorized(err) {
		if clearErr := s.clearLocked(); clearErr != nil {
			return "", clearErr
		}
		return "", ErrReauthRequired
	}
	if err != nil {
		return "", fmt.Errorf("refresh hytale token: %w", err)
	}
	now := time.Now()
	s.creds.AccessToken = tok.AccessToken
	s.creds.AccessExpiresAt = tok.ExpiresAt(now)
	s.creds.RefreshedAt = now
	if tok.RefreshToken != "" {
		s.creds.RefreshToken = tok.RefreshToken
	}
	if err := s.store.Save(s.creds); err != nil {
		return "", fmt.Errorf("persist rotated token: %w", err)
	}
	return s.creds.AccessToken, nil
}

func (s *Service) clearLocked() error {
	s.creds, s.hasCreds = Credentials{}, false
	return s.store.Clear()
}

// completeLogin stores the tokens obtained by the device flow and picks the
// profile automatically when the account owns exactly one.
func (s *Service) completeLogin(ctx context.Context, tok hytaleapi.Token) error {
	now := time.Now()
	creds := Credentials{
		RefreshToken:    tok.RefreshToken,
		AccessToken:     tok.AccessToken,
		AccessExpiresAt: tok.ExpiresAt(now),
		RefreshedAt:     now,
	}
	profiles, err := s.api.GetProfiles(ctx, tok.AccessToken)
	if err != nil {
		return fmt.Errorf("fetch profiles: %w", err)
	}
	if len(profiles.Profiles) == 1 {
		creds.ProfileUUID = profiles.Profiles[0].UUID
		creds.ProfileName = profiles.Profiles[0].Username
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.Save(creds); err != nil {
		return err
	}
	s.creds, s.hasCreds = creds, true
	return nil
}

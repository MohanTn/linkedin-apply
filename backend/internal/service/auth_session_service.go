package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// Sentinel login errors surfaced to the caller/user.
var (
	ErrInvalidCreds = errors.New("invalid_credentials")
	ErrNeeds2FA     = errors.New("needs_2fa")
)

// AuthSessionService owns the logged-in browser session per profile/platform.
type AuthSessionService struct {
	profiles *ProfileService
	sessions SessionStore
	browser  browser.Browser
	ttl      time.Duration
}

func NewAuthSessionService(profiles *ProfileService, sessions SessionStore, b browser.Browser) *AuthSessionService {
	return &AuthSessionService{profiles: profiles, sessions: sessions, browser: b, ttl: 7 * 24 * time.Hour}
}

// EnsureSession returns an authenticated session, reusing a valid persisted one
// or performing a fresh login. Non-active logins persist their status and return
// ErrInvalidCreds / ErrNeeds2FA.
func (s *AuthSessionService) EnsureSession(ctx context.Context, profileID, platform string) (*models.BrowserSession, error) {
	if existing, err := s.sessions.Get(ctx, profileID, platform); err == nil {
		if existing.Status == models.SessionActive && existing.ExpiresAt.After(time.Now()) && len(existing.Cookies) > 0 {
			ok, cerr := s.browser.CheckSession(ctx, platform, existing.Cookies)
			if cerr == nil && ok {
				return existing, nil
			}
		}
	} else if !isNotFound(err) {
		return nil, err
	}
	return s.login(ctx, profileID, platform)
}

// Login forces a fresh login and returns the resulting session (or a sentinel
// error). Used by the validate-credentials handler.
func (s *AuthSessionService) Login(ctx context.Context, profileID, platform string) (*models.BrowserSession, error) {
	return s.login(ctx, profileID, platform)
}

// SessionStatus reports the stored session state WITHOUT performing a login, so
// selecting a profile in the UI never triggers a (doomed, bot-blocked) headless
// login or overwrites a session seeded by the interactive login script.
// Returns "active" only when a non-expired session with cookies exists.
func (s *AuthSessionService) SessionStatus(ctx context.Context, profileID, platform string) string {
	sess, err := s.sessions.Get(ctx, profileID, platform)
	if err != nil {
		return "none"
	}
	if sess.Status == models.SessionActive && sess.ExpiresAt.After(time.Now()) && len(sess.Cookies) > 0 {
		return models.SessionActive
	}
	return sess.Status
}

func (s *AuthSessionService) login(ctx context.Context, profileID, platform string) (*models.BrowserSession, error) {
	email, password, err := s.profiles.GetCredentials(profileID, platform)
	if err != nil {
		return nil, err
	}
	res, err := s.browser.Login(ctx, platform, email, password)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	sess := &models.BrowserSession{
		ID:        uuid.NewString(),
		ProfileID: profileID,
		Platform:  platform,
		Status:    res.Outcome,
		LastLogin: time.Now(),
	}
	switch res.Outcome {
	case browser.OutcomeActive:
		sess.Cookies = res.Cookies
		sess.ExpiresAt = time.Now().Add(s.ttl)
	}
	if err := s.sessions.Upsert(ctx, sess); err != nil {
		return nil, err
	}

	switch res.Outcome {
	case browser.OutcomeActive:
		return sess, nil
	case browser.OutcomeInvalidCreds:
		return sess, ErrInvalidCreds
	case browser.OutcomeNeeds2FA:
		return sess, ErrNeeds2FA
	default:
		return sess, fmt.Errorf("unexpected login outcome %q", res.Outcome)
	}
}

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// SessionRepo persists logged-in browser sessions per profile/platform.
type SessionRepo struct {
	db *sql.DB
}

func NewSessionRepo(db *sql.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

// Get returns the session for a profile/platform, or ErrNotFound.
func (r *SessionRepo) Get(ctx context.Context, profileID, platform string) (*models.BrowserSession, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, profile_id, platform, cookies, status, last_login, expires_at
		 FROM browser_sessions WHERE profile_id = $1 AND platform = $2`, profileID, platform)
	var s models.BrowserSession
	var cookies []byte
	var lastLogin, expiresAt sql.NullTime
	err := row.Scan(&s.ID, &s.ProfileID, &s.Platform, &cookies, &s.Status, &lastLogin, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.Cookies = cookies
	s.LastLogin = lastLogin.Time
	s.ExpiresAt = expiresAt.Time
	return &s, nil
}

// Upsert stores or replaces the session for a profile/platform.
func (r *SessionRepo) Upsert(ctx context.Context, s *models.BrowserSession) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO browser_sessions (id, profile_id, platform, cookies, status, last_login, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (profile_id, platform) DO UPDATE SET
		   cookies = EXCLUDED.cookies,
		   status = EXCLUDED.status,
		   last_login = EXCLUDED.last_login,
		   expires_at = EXCLUDED.expires_at`,
		s.ID, s.ProfileID, s.Platform, nullJSON(s.Cookies), s.Status, nullTime(s.LastLogin), nullTime(s.ExpiresAt))
	return err
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// scaffold:inject

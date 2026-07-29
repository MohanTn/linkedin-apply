// Package service holds the business logic: profile loading, login automation,
// scraping, company verification, and discovery orchestration.
package service

import (
	"context"
	"encoding/json"

	"github.com/mohan/linkedin-apply-backend/internal/models"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
)

// Consumer-defined store interfaces (satisfied by the concrete repos) so
// services can be unit-tested against fakes.

type ProfileStore interface {
	GetByID(ctx context.Context, id string) (*models.Profile, error)
	GetAll(ctx context.Context) ([]models.Profile, error)
	Upsert(ctx context.Context, p *models.Profile) error
	// SeedIfAbsent inserts only when the id is unknown, so env-seeded profiles are
	// created once and never overwrite later edits.
	SeedIfAbsent(ctx context.Context, p *models.Profile) error
	Delete(ctx context.Context, id string) error
	// GetPrefs returns stored search prefs JSON (nil when unset); SetPrefs saves.
	GetPrefs(ctx context.Context, id string) ([]byte, error)
	SetPrefs(ctx context.Context, id string, prefs []byte) error
}

type JobStore interface {
	Upsert(ctx context.Context, j *models.Job) (*models.Job, error)
}

// JobReader fetches a stored job by id (for on-demand ATS matching).
type JobReader interface {
	GetByID(ctx context.Context, id string) (*models.Job, error)
}

// ShortlistEntryStore is the surface the ATS runner needs: read one entry and
// write back its match score.
type ShortlistEntryStore interface {
	Get(ctx context.Context, id string) (*models.ShortlistEntry, error)
	UpdateAts(ctx context.Context, id string, score int, details json.RawMessage) error
}

type VerificationStore interface {
	GetByCompany(ctx context.Context, company string) (*models.CompanyVerification, error)
	Upsert(ctx context.Context, cv *models.CompanyVerification) error
}

type ShortlistStore interface {
	Upsert(ctx context.Context, e *models.ShortlistEntry) (*models.ShortlistEntry, error)
}

type ResumeStore interface {
	Upsert(ctx context.Context, r *models.Resume) error
	GetByProfile(ctx context.Context, profileID string) (*models.Resume, error)
}

type SnapshotStore interface {
	Insert(ctx context.Context, s *models.CompanySnapshot) error
	Recent(ctx context.Context, company string, limit int) ([]models.CompanySnapshot, error)
}

type SessionStore interface {
	Get(ctx context.Context, profileID, platform string) (*models.BrowserSession, error)
	Upsert(ctx context.Context, s *models.BrowserSession) error
}

// isNotFound reports whether err is the repository's not-found sentinel.
func isNotFound(err error) bool {
	return err == repository.ErrNotFound
}

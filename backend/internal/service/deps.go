// Package service holds the business logic: profile loading, login automation,
// scraping, company verification, and discovery orchestration.
package service

import (
	"context"

	"github.com/mohan/linkedin-apply-backend/internal/models"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
)

// Consumer-defined store interfaces (satisfied by the concrete repos) so
// services can be unit-tested against fakes.

type ProfileStore interface {
	GetByID(ctx context.Context, id string) (*models.Profile, error)
	GetAll(ctx context.Context) ([]models.Profile, error)
	Upsert(ctx context.Context, p *models.Profile) error
}

type JobStore interface {
	Upsert(ctx context.Context, j *models.Job) (*models.Job, error)
}

type VerificationStore interface {
	GetByCompany(ctx context.Context, company string) (*models.CompanyVerification, error)
	Upsert(ctx context.Context, cv *models.CompanyVerification) error
}

type ShortlistStore interface {
	Upsert(ctx context.Context, e *models.ShortlistEntry) (*models.ShortlistEntry, error)
}

type SessionStore interface {
	Get(ctx context.Context, profileID, platform string) (*models.BrowserSession, error)
	Upsert(ctx context.Context, s *models.BrowserSession) error
}

// isNotFound reports whether err is the repository's not-found sentinel.
func isNotFound(err error) bool {
	return err == repository.ErrNotFound
}

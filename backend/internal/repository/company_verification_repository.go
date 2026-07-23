package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// CompanyVerificationRepo caches ghost-job checks with a TTL.
type CompanyVerificationRepo struct {
	db *sql.DB
}

func NewCompanyVerificationRepo(db *sql.DB) *CompanyVerificationRepo {
	return &CompanyVerificationRepo{db: db}
}

// GetByCompany returns the cached verification only if it has not expired.
// Returns ErrNotFound when absent or stale.
func (r *CompanyVerificationRepo) GetByCompany(ctx context.Context, company string) (*models.CompanyVerification, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, company, is_ghost_job, score, verification_methods, details, expires_at, created_at
		 FROM company_verifications
		 WHERE company = $1 AND expires_at > CURRENT_TIMESTAMP`, company)
	cv, err := scanVerification(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return cv, err
}

// Upsert stores a verification result, replacing any prior row for the company.
// On a company conflict the existing row's id is preserved; that canonical id is
// written back into cv.ID so callers referencing it (e.g. shortlist.verification_id)
// point at the row that actually exists.
func (r *CompanyVerificationRepo) Upsert(ctx context.Context, cv *models.CompanyVerification) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO company_verifications (id, company, is_ghost_job, score, verification_methods, details, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (company) DO UPDATE SET
		   is_ghost_job = EXCLUDED.is_ghost_job,
		   score = EXCLUDED.score,
		   verification_methods = EXCLUDED.verification_methods,
		   details = EXCLUDED.details,
		   expires_at = EXCLUDED.expires_at,
		   created_at = CURRENT_TIMESTAMP
		 RETURNING id`,
		cv.ID, cv.Company, cv.IsGhostJob, cv.Score, nullJSON(cv.VerificationMethods), nullJSON(cv.Details), cv.ExpiresAt).Scan(&cv.ID)
}

func scanVerification(s scanner) (*models.CompanyVerification, error) {
	var cv models.CompanyVerification
	var methods, details []byte
	if err := s.Scan(&cv.ID, &cv.Company, &cv.IsGhostJob, &cv.Score, &methods, &details, &cv.ExpiresAt, &cv.CreatedAt); err != nil {
		return nil, err
	}
	cv.VerificationMethods = methods
	cv.Details = details
	return &cv, nil
}

// scaffold:inject

package repository

import (
	"context"
	"database/sql"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// JobRepo persists scraped job listings.
type JobRepo struct {
	db *sql.DB
}

func NewJobRepo(db *sql.DB) *JobRepo {
	return &JobRepo{db: db}
}

// Upsert inserts a job or, on external_job_id conflict, refreshes the mutable
// fields while preserving first_seen_at. Returns the stored row.
func (r *JobRepo) Upsert(ctx context.Context, j *models.Job) (*models.Job, error) {
	row := r.db.QueryRowContext(ctx,
		`INSERT INTO jobs (id, external_job_id, title, company, apply_url, platform, posted_at, location, salary, raw_data)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (external_job_id) DO UPDATE SET
		   title = EXCLUDED.title,
		   company = EXCLUDED.company,
		   apply_url = EXCLUDED.apply_url,
		   posted_at = EXCLUDED.posted_at,
		   location = EXCLUDED.location,
		   salary = EXCLUDED.salary,
		   raw_data = EXCLUDED.raw_data
		 RETURNING id, external_job_id, title, company, apply_url, platform, posted_at, first_seen_at, location, salary, created_at`,
		j.ID, j.ExternalJobID, j.Title, j.Company, j.ApplyURL, j.Platform, j.PostedAt,
		nullStr(j.Location), nullStr(j.Salary), nullJSON(j.RawData))
	return scanJob(row)
}

func (r *JobRepo) GetByID(ctx context.Context, id string) (*models.Job, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, external_job_id, title, company, apply_url, platform, posted_at, first_seen_at, location, salary, created_at
		 FROM jobs WHERE id = $1`, id)
	return scanJob(row)
}

func (r *JobRepo) ExistsByExternalID(ctx context.Context, extID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE external_job_id = $1)`, extID).Scan(&exists)
	return exists, err
}

func scanJob(s scanner) (*models.Job, error) {
	var j models.Job
	var extID, location, salary sql.NullString
	if err := s.Scan(&j.ID, &extID, &j.Title, &j.Company, &j.ApplyURL, &j.Platform,
		&j.PostedAt, &j.FirstSeenAt, &location, &salary, &j.CreatedAt); err != nil {
		return nil, err
	}
	j.ExternalJobID = extID.String
	j.Location = location.String
	j.Salary = salary.String
	return &j, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullJSON returns SQL NULL for empty JSON so JSONB columns don't reject an
// empty string as invalid JSON.
func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return []byte(b)
}

// scaffold:inject

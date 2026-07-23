package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// ResumeRepo persists one active resume per profile.
type ResumeRepo struct {
	db *sql.DB
}

func NewResumeRepo(db *sql.DB) *ResumeRepo {
	return &ResumeRepo{db: db}
}

// Upsert stores (or replaces) the profile's resume.
func (r *ResumeRepo) Upsert(ctx context.Context, res *models.Resume) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resumes (id, profile_id, filename, content, text, uploaded_at)
		 VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
		 ON CONFLICT (profile_id) DO UPDATE SET
		   filename = EXCLUDED.filename,
		   content = EXCLUDED.content,
		   text = EXCLUDED.text,
		   uploaded_at = CURRENT_TIMESTAMP`,
		res.ID, res.ProfileID, res.Filename, res.Content, res.Text)
	return err
}

// GetByProfile returns the profile's resume, or ErrNotFound.
func (r *ResumeRepo) GetByProfile(ctx context.Context, profileID string) (*models.Resume, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, profile_id, filename, content, text, uploaded_at
		 FROM resumes WHERE profile_id = $1`, profileID)
	var res models.Resume
	if err := row.Scan(&res.ID, &res.ProfileID, &res.Filename, &res.Content, &res.Text, &res.UploadedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &res, nil
}

// scaffold:inject

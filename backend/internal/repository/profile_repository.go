package repository

import (
	"context"
	"database/sql"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// ProfileRepo persists profiles.
type ProfileRepo struct {
	db *sql.DB
}

func NewProfileRepo(db *sql.DB) *ProfileRepo {
	return &ProfileRepo{db: db}
}

func (r *ProfileRepo) GetByID(ctx context.Context, id string) (*models.Profile, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, linkedin_email, glassdoor_email, profile_data_path, created_at, updated_at
		 FROM profiles WHERE id = $1`, id)
	return scanProfile(row)
}

func (r *ProfileRepo) GetAll(ctx context.Context) ([]models.Profile, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, linkedin_email, glassdoor_email, profile_data_path, created_at, updated_at
		 FROM profiles ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// Upsert inserts or updates a profile by id.
func (r *ProfileRepo) Upsert(ctx context.Context, p *models.Profile) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO profiles (id, name, linkedin_email, glassdoor_email, profile_data_path, updated_at)
		 VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
		 ON CONFLICT (id) DO UPDATE SET
		   name = EXCLUDED.name,
		   linkedin_email = EXCLUDED.linkedin_email,
		   glassdoor_email = EXCLUDED.glassdoor_email,
		   profile_data_path = EXCLUDED.profile_data_path,
		   updated_at = CURRENT_TIMESTAMP`,
		p.ID, p.Name, p.LinkedinEmail, p.GlassdoorEmail, p.ProfileDataPath)
	return err
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanProfile(s scanner) (*models.Profile, error) {
	var p models.Profile
	var linkedin, glassdoor, path sql.NullString
	if err := s.Scan(&p.ID, &p.Name, &linkedin, &glassdoor, &path, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.LinkedinEmail = linkedin.String
	p.GlassdoorEmail = glassdoor.String
	p.ProfileDataPath = path.String
	return &p, nil
}

// scaffold:inject

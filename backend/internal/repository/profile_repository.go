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
		`SELECT id, name, linkedin_email, glassdoor_email, profile_data_path, source, created_at, updated_at
		 FROM profiles WHERE id = $1 AND deleted_at IS NULL`, id)
	p, err := scanProfile(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return p, err
}

func (r *ProfileRepo) GetAll(ctx context.Context) ([]models.Profile, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, linkedin_email, glassdoor_email, profile_data_path, source, created_at, updated_at
		 FROM profiles WHERE deleted_at IS NULL ORDER BY created_at DESC`)
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
	source := p.Source
	if source == "" {
		source = models.ProfileSourceEnv
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO profiles (id, name, linkedin_email, glassdoor_email, profile_data_path, source, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
		 ON CONFLICT (id) DO UPDATE SET
		   name = EXCLUDED.name,
		   linkedin_email = EXCLUDED.linkedin_email,
		   glassdoor_email = EXCLUDED.glassdoor_email,
		   profile_data_path = EXCLUDED.profile_data_path,
		   source = EXCLUDED.source,
		   updated_at = CURRENT_TIMESTAMP`,
		p.ID, p.Name, p.LinkedinEmail, p.GlassdoorEmail, p.ProfileDataPath, source)
	return err
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
// GetPrefs returns the profile's stored search_prefs JSON, or nil when unset.
func (r *ProfileRepo) GetPrefs(ctx context.Context, id string) ([]byte, error) {
	var prefs []byte
	err := r.db.QueryRowContext(ctx, `SELECT search_prefs FROM profiles WHERE id = $1`, id).Scan(&prefs)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return prefs, err
}

// SetPrefs persists the profile's search_prefs JSON.
func (r *ProfileRepo) SetPrefs(ctx context.Context, id string, prefs []byte) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE profiles SET search_prefs = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
		id, prefs)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProfile(s scanner) (*models.Profile, error) {
	var p models.Profile
	var linkedin, glassdoor, path, source sql.NullString
	if err := s.Scan(&p.ID, &p.Name, &linkedin, &glassdoor, &path, &source, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.LinkedinEmail = linkedin.String
	p.GlassdoorEmail = glassdoor.String
	p.ProfileDataPath = path.String
	p.Source = source.String
	return &p, nil
}

// Delete removes everything hanging off a profile and then soft-deletes the
// profile itself. The surviving row is a tombstone: it stops an env-seeded
// profile from being re-created by the next load, and keeps historical foreign
// keys valid. Shortlist rows and discovery runs are cleared explicitly because
// their foreign keys predate the ON DELETE CASCADE used by newer tables.
func (r *ProfileRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, q := range []string{
		`DELETE FROM shortlist WHERE profile_id = $1`,
		`DELETE FROM discovery_runs WHERE profile_id = $1`,
		`DELETE FROM resumes WHERE profile_id = $1`,
		`DELETE FROM browser_sessions WHERE profile_id = $1`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE profiles SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SeedIfAbsent inserts a profile only when no row with that id exists yet. Env
// vars seed a profile once; after that the database is authoritative, so edits
// survive a restart and a deleted profile (whose tombstone row remains) is not
// silently re-created.
func (r *ProfileRepo) SeedIfAbsent(ctx context.Context, p *models.Profile) error {
	source := p.Source
	if source == "" {
		source = models.ProfileSourceEnv
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO profiles (id, name, linkedin_email, glassdoor_email, profile_data_path, source, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
		 ON CONFLICT (id) DO NOTHING`,
		p.ID, p.Name, p.LinkedinEmail, p.GlassdoorEmail, p.ProfileDataPath, source)
	return err
}

// scaffold:inject

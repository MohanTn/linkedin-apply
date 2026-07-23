package repository

import (
	"context"
	"database/sql"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

type DiscoveryRunRepo struct {
	db *sql.DB
}

func NewDiscoveryRunRepo(db *sql.DB) *DiscoveryRunRepo {
	return &DiscoveryRunRepo{db: db}
}

func (r *DiscoveryRunRepo) Create(ctx context.Context, run *models.DiscoveryRun) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO discovery_runs (id, profile_id, started_at) VALUES ($1, $2, $3)`,
		run.ID, run.ProfileID, run.StartedAt).Err()
}

func (r *DiscoveryRunRepo) MarkComplete(ctx context.Context, runID string) error {
	return r.db.QueryRowContext(ctx,
		`UPDATE discovery_runs SET completed_at = CURRENT_TIMESTAMP WHERE id = $1`,
		runID).Err()
}

// scaffold:inject

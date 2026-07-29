package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// ShortlistFilter narrows a cockpit shortlist query.
type ShortlistFilter struct {
	ProfileID    string
	Status       string // "" = any
	IncludeGhost bool
	MinScore     int
	Limit        int
	Offset       int
}

// ShortlistRepo persists shortlist entries.
type ShortlistRepo struct {
	db *sql.DB
}

func NewShortlistRepo(db *sql.DB) *ShortlistRepo {
	return &ShortlistRepo{db: db}
}

// Upsert writes a shortlist entry, refreshing score/is_ghost on (profile, job)
// conflict without clobbering the user's chosen status.
func (r *ShortlistRepo) Upsert(ctx context.Context, e *models.ShortlistEntry) (*models.ShortlistEntry, error) {
	row := r.db.QueryRowContext(ctx,
		`INSERT INTO shortlist (id, profile_id, job_id, verification_id, discovery_run_id, score, is_ghost, apply_url, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'new')
		 ON CONFLICT (profile_id, job_id) DO UPDATE SET
		   score = EXCLUDED.score,
		   is_ghost = EXCLUDED.is_ghost,
		   apply_url = EXCLUDED.apply_url,
		   verification_id = EXCLUDED.verification_id,
		   discovery_run_id = EXCLUDED.discovery_run_id
		 RETURNING id, profile_id, job_id, verification_id, discovery_run_id, score, is_ghost, apply_url, status, discovered_at`,
		e.ID, e.ProfileID, e.JobID, nullStr(e.VerificationID), nullStr(e.DiscoveryRunID), e.Score, e.IsGhost, e.ApplyURL)
	var out models.ShortlistEntry
	var verID, runID sql.NullString
	if err := row.Scan(&out.ID, &out.ProfileID, &out.JobID, &verID, &runID, &out.Score,
		&out.IsGhost, &out.ApplyURL, &out.Status, &out.DiscoveredAt); err != nil {
		return nil, err
	}
	out.VerificationID = verID.String
	out.DiscoveryRunID = runID.String
	return &out, nil
}

// Get returns a single shortlist entry by id.
func (r *ShortlistRepo) Get(ctx context.Context, id string) (*models.ShortlistEntry, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, profile_id, job_id, verification_id, discovery_run_id, score, is_ghost, apply_url, status, discovered_at
		 FROM shortlist WHERE id = $1`, id)
	var out models.ShortlistEntry
	var verID, runID sql.NullString
	err := row.Scan(&out.ID, &out.ProfileID, &out.JobID, &verID, &runID, &out.Score,
		&out.IsGhost, &out.ApplyURL, &out.Status, &out.DiscoveredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.VerificationID = verID.String
	out.DiscoveryRunID = runID.String
	return &out, nil
}

// UpdateAts stores the ATS match score + details for one shortlist entry.
func (r *ShortlistRepo) UpdateAts(ctx context.Context, id string, score int, details json.RawMessage) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE shortlist SET ats_score = $2, ats_details = $3 WHERE id = $1`,
		id, score, nullJSON(details))
	return err
}

// List returns cockpit rows (shortlist joined with jobs), newest/highest first.
func (r *ShortlistRepo) List(ctx context.Context, f ShortlistFilter) ([]models.ShortlistRow, error) {
	var where []string
	var args []any
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, strings.Replace(clause, "?", "$"+itoa(len(args)), 1))
	}
	add("s.profile_id = ?", f.ProfileID)
	if f.Status != "" {
		add("s.status = ?", f.Status)
	}
	if !f.IncludeGhost {
		where = append(where, "s.is_ghost = FALSE")
	}
	add("s.score >= ?", f.MinScore)

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)
	limClause := "$" + itoa(len(args))
	args = append(args, f.Offset)
	offClause := "$" + itoa(len(args))

	q := `SELECT s.id, s.profile_id, s.job_id, s.verification_id, s.discovery_run_id, s.score, s.is_ghost, s.apply_url, s.status, s.discovered_at,
	              j.title, j.company, j.location, j.posted_at, j.platform,
	              COALESCE(r.started_at, s.discovered_at) as run_started_at,
	              v.details, s.ats_score, s.ats_details
	       FROM shortlist s JOIN jobs j ON j.id = s.job_id
	       LEFT JOIN discovery_runs r ON r.id = s.discovery_run_id
	       LEFT JOIN company_verifications v ON v.id = s.verification_id
	       WHERE ` + strings.Join(where, " AND ") + `
	       ORDER BY s.score DESC, j.posted_at DESC NULLS LAST
	       LIMIT ` + limClause + ` OFFSET ` + offClause

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.ShortlistRow
	for rows.Next() {
		var row models.ShortlistRow
		var verID, runID, location sql.NullString
		var postedAt sql.NullTime
		var details, atsDetails []byte
		var atsScore sql.NullInt64
		if err := rows.Scan(&row.ID, &row.ProfileID, &row.JobID, &verID, &runID, &row.Score, &row.IsGhost,
			&row.ApplyURL, &row.Status, &row.DiscoveredAt, &row.Title, &row.Company, &location, &postedAt,
			&row.Platform, &row.RunStartedAt, &details, &atsScore, &atsDetails); err != nil {
			return nil, err
		}
		row.PostedAt = postedAt.Time
		row.VerificationID = verID.String
		row.DiscoveryRunID = runID.String
		row.Location = location.String
		row.Signals = details
		if atsScore.Valid {
			s := new(int)
			*s = int(atsScore.Int64)
			row.AtsScore = s
			row.AtsDetails = atsDetails
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateStatus flips the user-managed status for one entry.
func (r *ShortlistRepo) UpdateStatus(ctx context.Context, id, status string) (*models.ShortlistEntry, error) {
	row := r.db.QueryRowContext(ctx,
		`UPDATE shortlist SET status = $2 WHERE id = $1
		 RETURNING id, profile_id, job_id, verification_id, discovery_run_id, score, is_ghost, apply_url, status, discovered_at`,
		id, status)
	var out models.ShortlistEntry
	var verID, runID sql.NullString
	err := row.Scan(&out.ID, &out.ProfileID, &out.JobID, &verID, &runID, &out.Score,
		&out.IsGhost, &out.ApplyURL, &out.Status, &out.DiscoveredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.VerificationID = verID.String
	out.DiscoveryRunID = runID.String
	return &out, nil
}

// DeleteByProfile removes all shortlist entries for a profile ("start over").
// Jobs/verifications are kept (shared, cached); only this profile's cockpit is
// cleared. Returns the number of rows removed.
func (r *ShortlistRepo) DeleteByProfile(ctx context.Context, profileID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM shortlist WHERE profile_id = $1`, profileID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Stats returns per-status counts for a profile, including a ghost tally.
func (r *ShortlistRepo) Stats(ctx context.Context, profileID string) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM shortlist WHERE profile_id = $1 GROUP BY status`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{
		models.StatusNew: 0, models.StatusSaved: 0, models.StatusDismissed: 0, models.StatusApplied: 0, "ghost": 0,
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var ghost int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM shortlist WHERE profile_id = $1 AND is_ghost = TRUE`, profileID).Scan(&ghost); err != nil {
		return nil, err
	}
	out["ghost"] = ghost
	return out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// scaffold:inject

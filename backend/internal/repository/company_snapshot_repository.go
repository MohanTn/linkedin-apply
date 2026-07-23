package repository

import (
	"context"
	"database/sql"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// CompanySnapshotRepo persists employee-count observations per company.
type CompanySnapshotRepo struct {
	db *sql.DB
}

func NewCompanySnapshotRepo(db *sql.DB) *CompanySnapshotRepo {
	return &CompanySnapshotRepo{db: db}
}

func (r *CompanySnapshotRepo) Insert(ctx context.Context, s *models.CompanySnapshot) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO company_snapshots (id, company, employee_count, source)
		 VALUES ($1, $2, $3, $4)`,
		s.ID, s.Company, s.EmployeeCount, s.Source)
	return err
}

// Recent returns up to limit snapshots for a company, newest first.
func (r *CompanySnapshotRepo) Recent(ctx context.Context, company string, limit int) ([]models.CompanySnapshot, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, company, employee_count, source, captured_at
		 FROM company_snapshots WHERE company = $1
		 ORDER BY captured_at DESC LIMIT $2`, company, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.CompanySnapshot
	for rows.Next() {
		var s models.CompanySnapshot
		var count sql.NullInt64
		if err := rows.Scan(&s.ID, &s.Company, &count, &s.Source, &s.CapturedAt); err != nil {
			return nil, err
		}
		s.EmployeeCount = int(count.Int64)
		out = append(out, s)
	}
	return out, rows.Err()
}

// scaffold:inject

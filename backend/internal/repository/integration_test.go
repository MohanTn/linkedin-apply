package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mohan/linkedin-apply-backend/internal/database"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// openTestDB connects to DATABASE_URL and applies the schema, skipping the test
// when no database is configured.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestIntegration_DiscoveryFlow(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	profiles := NewProfileRepo(db)
	jobs := NewJobRepo(db)
	vers := NewCompanyVerificationRepo(db)
	shortlist := NewShortlistRepo(db)
	sessions := NewSessionRepo(db)

	pid := "profile-" + uuid.NewString()[:8]
	// Unique company names keep the test hermetic across reruns (company is a
	// natural key with a UNIQUE constraint).
	acme := "Acme-" + uuid.NewString()[:8]
	ghostco := "GhostCo-" + uuid.NewString()[:8]
	oldco := "OldCo-" + uuid.NewString()[:8]

	// Profile upsert + fetch
	if err := profiles.Upsert(ctx, &models.Profile{ID: pid, Name: "T", LinkedinEmail: "t@x.com"}); err != nil {
		t.Fatal(err)
	}
	if got, err := profiles.GetByID(ctx, pid); err != nil || got.Name != "T" {
		t.Fatalf("profile get: %v %+v", err, got)
	}

	// Job upsert preserves identity + first_seen_at on conflict
	ext := "li_" + uuid.NewString()
	j1, err := jobs.Upsert(ctx, &models.Job{ID: uuid.NewString(), ExternalJobID: ext, Title: "Go Dev", Company: acme, ApplyURL: "http://a/1", Platform: "linkedin", PostedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	j2, err := jobs.Upsert(ctx, &models.Job{ID: uuid.NewString(), ExternalJobID: ext, Title: "Go Dev (updated)", Company: acme, ApplyURL: "http://a/1", Platform: "linkedin", PostedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if j1.ID != j2.ID {
		t.Fatalf("upsert changed id on conflict: %s vs %s", j1.ID, j2.ID)
	}
	if !j2.FirstSeenAt.Equal(j1.FirstSeenAt) {
		t.Fatalf("first_seen_at not preserved")
	}

	// Verification cache TTL. Upsert writes the canonical id back into cv.
	cv := &models.CompanyVerification{ID: uuid.NewString(), Company: acme, IsGhostJob: false, Score: 80, ExpiresAt: time.Now().Add(time.Hour)}
	if err := vers.Upsert(ctx, cv); err != nil {
		t.Fatal(err)
	}
	cvID := cv.ID
	if got, err := vers.GetByCompany(ctx, acme); err != nil || got.Score != 80 || got.ID != cvID {
		t.Fatalf("verification get: %v %+v", err, got)
	}
	// Expired verification is not returned
	if err := vers.Upsert(ctx, &models.CompanyVerification{ID: uuid.NewString(), Company: oldco, Score: 10, ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := vers.GetByCompany(ctx, oldco); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for expired, got %v", err)
	}

	// Shortlist upsert (real + ghost), list filters, status update, stats
	real := &models.ShortlistEntry{ID: uuid.NewString(), ProfileID: pid, JobID: j2.ID, VerificationID: cvID, Score: 80, IsGhost: false, ApplyURL: "http://a/1"}
	if _, err := shortlist.Upsert(ctx, real); err != nil {
		t.Fatal(err)
	}
	// a ghost job needs its own job row (unique profile_id, job_id)
	gj, _ := jobs.Upsert(ctx, &models.Job{ID: uuid.NewString(), ExternalJobID: "li_" + uuid.NewString(), Title: "Ghost", Company: ghostco, ApplyURL: "http://a/2", Platform: "linkedin", PostedAt: time.Now()})
	ghost := &models.ShortlistEntry{ID: uuid.NewString(), ProfileID: pid, JobID: gj.ID, Score: 15, IsGhost: true, ApplyURL: "http://a/2"}
	if _, err := shortlist.Upsert(ctx, ghost); err != nil {
		t.Fatal(err)
	}

	// Default (exclude ghost, minScore 0) -> only the real one
	rows, err := shortlist.List(ctx, ShortlistFilter{ProfileID: pid, MinScore: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].IsGhost {
		t.Fatalf("expected 1 non-ghost row, got %d %+v", len(rows), rows)
	}
	if rows[0].Company != acme || rows[0].Title == "" {
		t.Fatalf("join did not populate job fields: %+v", rows[0])
	}
	// Include ghost -> both
	rows, _ = shortlist.List(ctx, ShortlistFilter{ProfileID: pid, IncludeGhost: true})
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows with ghost, got %d", len(rows))
	}

	// Status update
	upd, err := shortlist.UpdateStatus(ctx, real.ID, models.StatusApplied)
	if err != nil || upd.Status != models.StatusApplied {
		t.Fatalf("update status: %v %+v", err, upd)
	}

	// Stats
	stats, err := shortlist.Stats(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if stats[models.StatusApplied] != 1 || stats["ghost"] != 1 || stats[models.StatusNew] != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	// Session upsert/get
	sid := uuid.NewString()
	if err := sessions.Upsert(ctx, &models.BrowserSession{ID: sid, ProfileID: pid, Platform: "linkedin", Status: models.SessionActive, LastLogin: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if s, err := sessions.Get(ctx, pid, "linkedin"); err != nil || s.Status != models.SessionActive {
		t.Fatalf("session get: %v %+v", err, s)
	}
}

package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// JobScraperService pulls recent listings via the authenticated browser session.
type JobScraperService struct {
	auth    *AuthSessionService
	browser browser.Browser
	jobs    JobStore
}

func NewJobScraperService(auth *AuthSessionService, b browser.Browser, jobs JobStore) *JobScraperService {
	return &JobScraperService{auth: auth, browser: b, jobs: jobs}
}

// ScrapeRecent logs in (or reuses a session), scrapes listings for the platform,
// discards anything older than sinceHours or from an excluded company, and
// upserts the survivors. Returns the stored jobs.
func (s *JobScraperService) ScrapeRecent(ctx context.Context, profileID, platform string, prefs models.SearchPrefs, sinceHours int) ([]models.Job, error) {
	sess, err := s.auth.EnsureSession(ctx, profileID, platform)
	if err != nil {
		return nil, err // ErrInvalidCreds / ErrNeeds2FA bubble up
	}

	q := browser.ScrapeQuery{
		Keywords:         prefs.Keywords,
		Locations:        prefs.Locations,
		RemoteOnly:       prefs.RemoteOnly,
		ExperienceLevels: prefs.ExperienceLevels,
		SinceHours:       sinceHours,
	}
	scraped, err := s.browser.ScrapeRecent(ctx, platform, sess.Cookies, q)
	// Even on a partial error we keep whatever was gathered.
	cutoff := time.Now().Add(-time.Duration(sinceHours) * time.Hour)
	excluded := lowerSet(prefs.ExcludeCompanies)

	var out []models.Job
	for _, sj := range scraped {
		if !sj.PostedAt.IsZero() && sj.PostedAt.Before(cutoff) {
			continue // belt-and-suspenders on the platform date filter
		}
		if excluded[strings.ToLower(strings.TrimSpace(sj.Company))] {
			continue
		}
		job := &models.Job{
			ID:            uuid.NewString(),
			ExternalJobID: sj.ExternalJobID,
			Title:         sj.Title,
			Company:       strings.TrimSpace(sj.Company),
			ApplyURL:      sj.ApplyURL,
			Platform:      platform,
			PostedAt:      sj.PostedAt,
			Location:      sj.Location,
			Salary:        sj.Salary,
			RawData:       sj.Raw,
		}
		stored, uerr := s.jobs.Upsert(ctx, job)
		if uerr != nil {
			return out, fmt.Errorf("upsert job %s: %w", sj.ExternalJobID, uerr)
		}
		out = append(out, *stored)
	}
	return out, err
}

func lowerSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[strings.ToLower(strings.TrimSpace(it))] = true
	}
	return m
}

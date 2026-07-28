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

// PortalAPI is an API-based job source (no browser, no login), e.g. the
// official Arbeitsagentur jobsuche REST API.
type PortalAPI interface {
	ScrapeRecent(ctx context.Context, q browser.ScrapeQuery) ([]browser.ScrapedJob, error)
}

// JobScraperService pulls recent listings via the authenticated browser
// session, a login-free public scrape, or a portal API — per platform.
type JobScraperService struct {
	auth    *AuthSessionService
	browser browser.Browser
	jobs    JobStore
	apis    map[string]PortalAPI // platform -> API source
}

func NewJobScraperService(auth *AuthSessionService, b browser.Browser, jobs JobStore, apis map[string]PortalAPI) *JobScraperService {
	return &JobScraperService{auth: auth, browser: b, jobs: jobs, apis: apis}
}

// ScrapeRecent gathers listings for the platform (API source, public scrape, or
// authenticated session), discards anything older than sinceHours or from an
// excluded company, and upserts the survivors. Returns the stored jobs.
func (s *JobScraperService) ScrapeRecent(ctx context.Context, profileID, platform string, prefs models.SearchPrefs, sinceHours int) ([]models.Job, error) {
	q := browser.ScrapeQuery{
		Keywords:         prefs.Keywords,
		Locations:        prefs.Locations,
		RemoteOnly:       prefs.RemoteOnly,
		ExperienceLevels: prefs.ExperienceLevels,
		SinceHours:       sinceHours,
	}

	var scraped []browser.ScrapedJob
	var err error
	switch {
	case s.apis[platform] != nil:
		scraped, err = s.apis[platform].ScrapeRecent(ctx, q)
	case browser.Public(platform):
		scraped, err = s.browser.ScrapeRecent(ctx, platform, nil, q)
	default:
		// Use the stored session only. A missing one means "not signed in yet",
		// which the caller reports and skips — it must never trigger an
		// interactive login in the middle of a background run.
		sess, aerr := s.auth.ActiveSession(ctx, profileID, platform)
		if aerr != nil {
			return nil, aerr
		}
		scraped, err = s.browser.ScrapeRecent(ctx, platform, sess.Cookies, q)
	}
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

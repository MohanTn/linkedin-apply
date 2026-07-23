package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// Discovery run phases.
const (
	PhaseLogin     = "login"
	PhaseScraping  = "scraping"
	PhaseVerifying = "verifying"
	PhaseDone      = "done"
	PhaseError     = "error"
)

// DiscoveryProgress is emitted as a run advances.
type DiscoveryProgress struct {
	Phase       string `json:"phase"`
	Platform    string `json:"platform,omitempty"` // platform currently being searched
	Message     string `json:"message,omitempty"`  // human-readable live step
	Found       int    `json:"found"`
	Verified    int    `json:"verified"`
	Shortlisted int    `json:"shortlisted"`
	Ghost       int    `json:"ghost"`
	Error       string `json:"error,omitempty"`
}

// platformHost maps a platform id to the site name shown in progress messages.
func platformHost(platform string) string {
	switch platform {
	case "linkedin":
		return "linkedin.com"
	case "glassdoor":
		return "glassdoor.com"
	case "xing":
		return "xing.com"
	case "stepstone":
		return "stepstone.de"
	case "indeed":
		return "de.indeed.com"
	case "arbeitsagentur":
		return "arbeitsagentur.de"
	}
	return platform
}

// DiscoveryService orchestrates scrape -> verify -> shortlist. It never applies,
// and no longer runs ATS matching — that is triggered on demand per selection.
type DiscoveryService struct {
	profiles     *ProfileService
	scraper      *JobScraperService
	verification *CompanyVerificationService
	shortlist    ShortlistStore
}

func NewDiscoveryService(p *ProfileService, sc *JobScraperService, v *CompanyVerificationService, sl ShortlistStore) *DiscoveryService {
	return &DiscoveryService{profiles: p, scraper: sc, verification: v, shortlist: sl}
}

// Discover gathers recent jobs across platforms, verifies each company, and
// upserts a shortlist entry per job (ghosts flagged, not dropped). progress may
// be nil. Returns the number of shortlisted jobs.
func (s *DiscoveryService) Discover(ctx context.Context, profileID string, platforms []string, sinceHours int, progress func(DiscoveryProgress)) (int, error) {
	return s.DiscoverWithRunID(ctx, profileID, platforms, sinceHours, "", progress)
}

// DiscoverWithRunID is like Discover but associates jobs with a discovery run.
func (s *DiscoveryService) DiscoverWithRunID(ctx context.Context, profileID string, platforms []string, sinceHours int, runID string, progress func(DiscoveryProgress)) (int, error) {
	emit := func(p DiscoveryProgress) {
		if progress != nil {
			progress(p)
		}
	}
	prog := DiscoveryProgress{Phase: PhaseLogin}
	emit(prog)

	prefs, err := s.profiles.GetSearchPrefs(ctx, profileID)
	if err != nil {
		emit(DiscoveryProgress{Phase: PhaseError, Error: err.Error()})
		return 0, err
	}

	// ---- scrape ----
	prog.Phase = PhaseScraping
	emit(prog)
	var jobs []models.Job
	for _, platform := range platforms {
		prog.Platform = platform
		prog.Message = "Searching " + platformHost(platform) + "…"
		emit(prog)
		got, serr := s.scraper.ScrapeRecent(ctx, profileID, platform, prefs, sinceHours)
		jobs = append(jobs, got...)
		prog.Found = len(jobs)
		prog.Message = fmt.Sprintf("%s: found %d jobs matching", platformHost(platform), len(got))
		emit(prog)
		if serr != nil {
			// Login failures are fatal for that platform; keep going to others
			// but remember partial results. A hard auth error aborts the run.
			if serr == ErrInvalidCreds || serr == ErrNeeds2FA {
				emit(DiscoveryProgress{Phase: PhaseError, Found: len(jobs), Error: serr.Error()})
				return 0, serr
			}
		}
	}
	jobs = dedupeJobs(jobs)
	prog.Found = len(jobs)
	prog.Platform = ""
	prog.Message = fmt.Sprintf("%d unique jobs after de-duplication", len(jobs))
	emit(prog)

	// ---- verify + shortlist ----
	prog.Phase = PhaseVerifying
	prog.Message = "Doing company research…"
	emit(prog)
	verCache := map[string]*models.CompanyVerification{}
	shortlisted, ghost := 0, 0
	for _, job := range jobs {
		key := strings.ToLower(strings.TrimSpace(job.Company))
		cv := verCache[key]
		if cv == nil {
			prog.Message = fmt.Sprintf("Researching %s…", job.Company)
			emit(prog)
			cv, err = s.verification.Verify(ctx, job.Company)
			if err != nil {
				return shortlisted, err
			}
			verCache[key] = cv
			prog.Verified = len(verCache)
		}

		entry := &models.ShortlistEntry{
			ID:             uuid.NewString(),
			ProfileID:      profileID,
			JobID:          job.ID,
			VerificationID: cv.ID,
			DiscoveryRunID: runID,
			Score:          cv.Score,
			IsGhost:        cv.IsGhostJob,
			ApplyURL:       job.ApplyURL,
		}
		if _, err := s.shortlist.Upsert(ctx, entry); err != nil {
			return shortlisted, err
		}
		shortlisted++
		if cv.IsGhostJob {
			ghost++
		}
		prog.Shortlisted = shortlisted
		prog.Ghost = ghost
		prog.Message = fmt.Sprintf("Shortlisting %d/%d jobs", shortlisted, len(jobs))
		emit(prog)
	}

	// ATS matching is no longer run automatically here — it is expensive (each JD
	// fetch launches a browser). The user selects jobs in the cockpit and triggers
	// AtsRunService on just that selection instead.

	prog.Phase = PhaseDone
	prog.Message = fmt.Sprintf("Done — %d shortlisted, %d possible ghosts", shortlisted, ghost)
	emit(prog)
	return shortlisted, nil
}

// dedupeJobs collapses the same role seen on multiple platforms by company+title,
// preferring an entry that has an apply URL.
func dedupeJobs(jobs []models.Job) []models.Job {
	seen := map[string]int{} // key -> index in out
	var out []models.Job
	for _, j := range jobs {
		key := strings.ToLower(strings.TrimSpace(j.Company)) + "|" + strings.ToLower(strings.TrimSpace(j.Title))
		if idx, ok := seen[key]; ok {
			if out[idx].ApplyURL == "" && j.ApplyURL != "" {
				out[idx] = j
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, j)
	}
	return out
}

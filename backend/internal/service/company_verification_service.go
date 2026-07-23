package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// CompanySignals are the raw inputs the scorer combines.
type CompanySignals struct {
	Found             bool    // probe succeeded; if false the company is "unverified"
	HasLinkedInPage   bool    `json:"hasLinkedInPage"`
	LinkedInFollowers int     `json:"linkedInFollowers"`
	GlassdoorReviews  int     `json:"glassdoorReviews"`
	GlassdoorRating   float64 `json:"glassdoorRating"`
	Reposts           int     `json:"reposts"` // times the posting was reposted (ghost signal)
}

// CompanyProbe gathers signals about a company. Real implementations hit
// LinkedIn/Glassdoor; tests use a fake.
type CompanyProbe interface {
	Probe(ctx context.Context, company string) (CompanySignals, error)
}

// CompanyVerificationService scores companies and caches results with a TTL.
type CompanyVerificationService struct {
	store VerificationStore
	probe CompanyProbe
	ttl   time.Duration
}

func NewCompanyVerificationService(store VerificationStore, probe CompanyProbe) *CompanyVerificationService {
	return &CompanyVerificationService{store: store, probe: probe, ttl: 7 * 24 * time.Hour}
}

// Verify returns a cached or freshly computed verification. A probe failure
// yields an "unverified" result (neutral score, not a ghost) rather than an
// error, so one flaky company never fails a whole discovery run.
func (s *CompanyVerificationService) Verify(ctx context.Context, company string) (*models.CompanyVerification, error) {
	if cv, err := s.store.GetByCompany(ctx, company); err == nil {
		return cv, nil
	} else if !isNotFound(err) {
		return nil, err
	}

	var sig CompanySignals
	if s.probe != nil {
		if got, err := s.probe.Probe(ctx, company); err == nil {
			// Respect the probe's own Found flag: if it could not confirm the
			// company (bot-blocked, 404, network error), treat it as unverified
			// (neutral score) rather than as a ghost with no page.
			sig = got
		}
	}

	score := scoreCompany(sig)
	details, _ := json.Marshal(sig)
	methods, _ := json.Marshal(verificationMethods(sig))

	cv := &models.CompanyVerification{
		ID:                  uuid.NewString(),
		Company:             company,
		IsGhostJob:          sig.Found && score < models.GhostThreshold,
		Score:               score,
		VerificationMethods: methods,
		Details:             details,
		ExpiresAt:           time.Now().Add(s.ttl),
	}
	if err := s.store.Upsert(ctx, cv); err != nil {
		return nil, err
	}
	return cv, nil
}

// scoreCompany is a pure 0-100 scorer. It starts from a neutral 50 and moves up
// or down on evidence, so a company is only flagged as a ghost (score < 40) when
// there is a genuine negative signal — a missing company page or frequent
// reposts — rather than merely a lack of rich data. This avoids hiding
// legitimate jobs when the (best-effort, unauthenticated) probe can only confirm
// a company page exists.
func scoreCompany(sig CompanySignals) int {
	if !sig.Found {
		return 50 // unverified — surface it, don't hide it
	}
	score := 50
	if sig.HasLinkedInPage {
		score += 25 // a real company presence is a positive signal
	} else {
		score -= 25 // no discoverable company page is a ghost signal
	}
	score += clampInt(sig.LinkedInFollowers/1000, 0, 15)
	score += clampInt(sig.GlassdoorReviews/10, 0, 20)
	score += int(sig.GlassdoorRating / 5.0 * 15.0)
	if sig.Reposts > 1 {
		score -= clampInt((sig.Reposts-1)*10, 0, 40) // frequent reposts = ghost signal
	}
	return clampInt(score, 0, 100)
}

func verificationMethods(sig CompanySignals) []string {
	if !sig.Found {
		return []string{"unverified"}
	}
	return []string{"linkedin_presence", "glassdoor_reviews", "repost_frequency"}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

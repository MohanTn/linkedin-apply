package service

import (
	"context"
	"testing"
	"time"

	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

func TestScoreCompany(t *testing.T) {
	cases := []struct {
		name    string
		sig     CompanySignals
		wantLo  int
		wantHi  int
		isGhost bool
	}{
		{"unverified is neutral", CompanySignals{Found: false}, 50, 50, false},
		{"has a company page, no rich data, is NOT ghost", CompanySignals{Found: true, HasLinkedInPage: true}, 70, 80, false},
		{"strong company", CompanySignals{Found: true, HasLinkedInPage: true, LinkedInFollowers: 20000, GlassdoorReviews: 500, GlassdoorRating: 4.5}, 70, 100, false},
		{"ghost spam", CompanySignals{Found: true, HasLinkedInPage: false, GlassdoorReviews: 0, Reposts: 5}, 0, 39, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scoreCompany(c.sig)
			if got < c.wantLo || got > c.wantHi {
				t.Fatalf("score=%d, want [%d,%d]", got, c.wantLo, c.wantHi)
			}
			ghost := c.sig.Found && got < models.GhostThreshold
			if ghost != c.isGhost {
				t.Fatalf("isGhost=%v, want %v (score=%d)", ghost, c.isGhost, got)
			}
		})
	}
}

func TestProfileService_LoadProfiles(t *testing.T) {
	store := newFakeProfileStore()
	svc := NewProfileService(store, t.TempDir())
	env := map[string]string{
		"PROFILE_1_LINKEDIN_EMAIL":    "a@x.com",
		"PROFILE_1_LINKEDIN_PASSWORD": "pw",
	}
	svc.getenv = func(k string) string { return env[k] }

	got, err := svc.LoadProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "profile-1" || got[0].LinkedinEmail != "a@x.com" {
		t.Fatalf("unexpected profiles: %+v", got)
	}
	email, pw, err := svc.GetCredentials("profile-1", "linkedin")
	if err != nil || email != "a@x.com" || pw != "pw" {
		t.Fatalf("creds=%s/%s err=%v", email, pw, err)
	}
	if _, _, err := svc.GetCredentials("profile-1", "glassdoor"); err == nil {
		t.Fatal("expected missing glassdoor creds error")
	}
}

// buildDiscovery wires real services around fakes.
func buildDiscovery(t *testing.T, fb *fakeBrowser, probe CompanyProbe, prefs models.SearchPrefs) (*DiscoveryService, *fakeShortlistStore) {
	t.Helper()
	pstore := newFakeProfileStore()
	psvc := NewProfileService(pstore, t.TempDir())
	psvc.getenv = func(k string) string {
		if k == "PROFILE_1_LINKEDIN_EMAIL" {
			return "a@x.com"
		}
		if k == "PROFILE_1_LINKEDIN_PASSWORD" {
			return "pw"
		}
		return ""
	}
	if _, err := psvc.LoadProfiles(context.Background()); err != nil {
		t.Fatal(err)
	}
	psvc.prefs["profile-1"] = prefs

	auth := NewAuthSessionService(psvc, newFakeSessionStore(), fb)
	scraper := NewJobScraperService(auth, fb, newFakeJobStore())
	verify := NewCompanyVerificationService(newFakeVerificationStore(), probe)
	sl := &fakeShortlistStore{}
	return NewDiscoveryService(psvc, scraper, verify, sl), sl
}

func TestDiscovery_ShortlistsAndFlagsGhost(t *testing.T) {
	now := time.Now()
	fb := &fakeBrowser{
		outcome: browser.OutcomeActive,
		scraped: map[string][]browser.ScrapedJob{
			"linkedin": {
				{ExternalJobID: "li_1", Title: "Go Dev", Company: "Acme", ApplyURL: "http://a/1", PostedAt: now},
				{ExternalJobID: "li_2", Title: "Go Dev", Company: "GhostCo", ApplyURL: "http://a/2", PostedAt: now},
			},
		},
	}
	probe := &fakeProbe{sig: map[string]CompanySignals{
		"Acme":    {Found: true, HasLinkedInPage: true, LinkedInFollowers: 20000, GlassdoorReviews: 400, GlassdoorRating: 4.5},
		"GhostCo": {Found: true, Reposts: 6},
	}}
	disc, sl := buildDiscovery(t, fb, probe, models.SearchPrefs{Keywords: []string{"Go"}})

	var last DiscoveryProgress
	n, err := disc.Discover(context.Background(), "profile-1", []string{"linkedin"}, 24, func(p DiscoveryProgress) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("shortlisted=%d, want 2", n)
	}
	if len(sl.entries) != 2 {
		t.Fatalf("entries=%d, want 2", len(sl.entries))
	}
	// Ghost is flagged, not dropped.
	var ghosts int
	for _, e := range sl.entries {
		if e.IsGhost {
			ghosts++
		}
	}
	if ghosts != 1 {
		t.Fatalf("ghost entries=%d, want 1", ghosts)
	}
	if last.Phase != PhaseDone || last.Ghost != 1 {
		t.Fatalf("final progress=%+v", last)
	}
}

func TestDiscovery_AbortsOnInvalidCreds(t *testing.T) {
	fb := &fakeBrowser{outcome: browser.OutcomeInvalidCreds}
	disc, _ := buildDiscovery(t, fb, &fakeProbe{}, models.SearchPrefs{})
	_, err := disc.Discover(context.Background(), "profile-1", []string{"linkedin"}, 24, nil)
	if err != ErrInvalidCreds {
		t.Fatalf("err=%v, want ErrInvalidCreds", err)
	}
}

func TestScraper_FiltersOldAndExcluded(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	fb := &fakeBrowser{
		outcome: browser.OutcomeActive,
		scraped: map[string][]browser.ScrapedJob{
			"linkedin": {
				{ExternalJobID: "li_1", Title: "Fresh", Company: "Acme", ApplyURL: "http://a/1", PostedAt: now},
				{ExternalJobID: "li_2", Title: "Stale", Company: "Acme", ApplyURL: "http://a/2", PostedAt: old},
				{ExternalJobID: "li_3", Title: "Blocked", Company: "BadCo", ApplyURL: "http://a/3", PostedAt: now},
			},
		},
	}
	psvc := NewProfileService(newFakeProfileStore(), t.TempDir())
	psvc.getenv = func(k string) string {
		switch k {
		case "PROFILE_1_LINKEDIN_EMAIL":
			return "a@x.com"
		case "PROFILE_1_LINKEDIN_PASSWORD":
			return "pw"
		}
		return ""
	}
	auth := NewAuthSessionService(psvc, newFakeSessionStore(), fb)
	scraper := NewJobScraperService(auth, fb, newFakeJobStore())

	prefs := models.SearchPrefs{ExcludeCompanies: []string{"BadCo"}}
	jobs, err := scraper.ScrapeRecent(context.Background(), "profile-1", "linkedin", prefs, 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Title != "Fresh" {
		t.Fatalf("jobs=%+v, want only Fresh", jobs)
	}
}

func TestDedupeJobs(t *testing.T) {
	jobs := []models.Job{
		{Company: "Acme", Title: "Go Dev", ApplyURL: ""},
		{Company: "acme ", Title: "go dev", ApplyURL: "http://a/1"}, // same role, has URL
		{Company: "Other", Title: "PM", ApplyURL: "http://a/2"},
	}
	got := dedupeJobs(jobs)
	if len(got) != 2 {
		t.Fatalf("deduped=%d, want 2", len(got))
	}
	for _, j := range got {
		if j.Company == "Acme" && j.ApplyURL == "" {
			t.Fatal("dedupe should prefer the entry with an apply URL")
		}
	}
}

func TestAuth_Needs2FA(t *testing.T) {
	fb := &fakeBrowser{outcome: browser.OutcomeNeeds2FA}
	psvc := NewProfileService(newFakeProfileStore(), t.TempDir())
	psvc.getenv = func(k string) string {
		switch k {
		case "PROFILE_1_LINKEDIN_EMAIL":
			return "a@x.com"
		case "PROFILE_1_LINKEDIN_PASSWORD":
			return "pw"
		}
		return ""
	}
	sessions := newFakeSessionStore()
	auth := NewAuthSessionService(psvc, sessions, fb)
	_, err := auth.Login(context.Background(), "profile-1", "linkedin")
	if err != ErrNeeds2FA {
		t.Fatalf("err=%v, want ErrNeeds2FA", err)
	}
	got, _ := sessions.Get(context.Background(), "profile-1", "linkedin")
	if got == nil || got.Status != models.SessionNeeds2FA {
		t.Fatalf("session status=%v, want needs_2fa", got)
	}
}

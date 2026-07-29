package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

func nowPlusDay() time.Time { return time.Now().Add(24 * time.Hour) }

// buildSkipDiscovery wires discovery over a browser that scrapes fine, but with
// no stored sessions — so login platforms are skipped and public ones are not.
func buildSkipDiscovery(t *testing.T, sessions *fakeSessionStore) (*DiscoveryService, *fakeShortlistStore) {
	t.Helper()
	fb := &fakeBrowser{scraped: map[string][]browser.ScrapedJob{
		"linkedin":  {{ExternalJobID: "li1", Title: "QA Engineer", Company: "Acme", ApplyURL: "http://a/1"}},
		"glassdoor": {{ExternalJobID: "gd1", Title: "QA Lead", Company: "Beta", ApplyURL: "http://a/2"}},
		"xing":      {{ExternalJobID: "xi1", Title: "Tester", Company: "Gamma", ApplyURL: "http://a/3"}},
	}}
	psvc := NewProfileService(newFakeProfileStore(), t.TempDir())
	psvc.getenv = func(string) string { return "" }
	psvc.prefs["profile-1"] = models.SearchPrefs{}
	auth := NewAuthSessionService(psvc, sessions, fb)
	scraper := NewJobScraperService(auth, fb, newFakeJobStore(), nil)
	ver := NewCompanyVerificationService(newFakeVerificationStore(),
		&fakeProbe{sig: map[string]CompanySignals{}}, newFakeJobStore(), &fakeSnapshotStore{})
	sl := &fakeShortlistStore{}
	return NewDiscoveryService(psvc, scraper, ver, sl), sl
}

// activeSession stores a usable session so that platform is scraped.
func activeSession(t *testing.T, s *fakeSessionStore, profileID, platform string) {
	t.Helper()
	if err := s.Upsert(context.Background(), &models.BrowserSession{
		ID: platform, ProfileID: profileID, Platform: platform,
		Status: models.SessionActive, Cookies: []byte(`[{"name":"c"}]`),
		ExpiresAt: nowPlusDay(),
	}); err != nil {
		t.Fatal(err)
	}
}

// The reported failure: LinkedIn and Glassdoor return jobs, Xing has no session.
// The run must keep the jobs it already has instead of discarding everything.
func TestDiscovery_UnsignedPlatformDoesNotDiscardOtherResults(t *testing.T) {
	sessions := newFakeSessionStore()
	activeSession(t, sessions, "profile-1", "linkedin")
	activeSession(t, sessions, "profile-1", "glassdoor")
	// xing deliberately has no session.

	svc, sl := buildSkipDiscovery(t, sessions)

	var last DiscoveryProgress
	shortlisted, err := svc.Discover(context.Background(), "profile-1",
		[]string{"linkedin", "glassdoor", "xing"}, 24, func(p DiscoveryProgress) { last = p })
	if err != nil {
		t.Fatalf("run failed instead of skipping the unsigned platform: %v", err)
	}
	if shortlisted != 2 {
		t.Fatalf("shortlisted=%d, want 2 (linkedin + glassdoor)", shortlisted)
	}
	if len(sl.entries) != 2 {
		t.Fatalf("stored %d shortlist entries, want 2", len(sl.entries))
	}
	if last.Phase != PhaseDone {
		t.Fatalf("phase=%q, want %q", last.Phase, PhaseDone)
	}
	// The user must be told why xing produced nothing.
	joined := strings.Join(last.Warnings, " | ")
	if !strings.Contains(joined, "xing.com") || !strings.Contains(joined, "not signed in") {
		t.Fatalf("warnings=%q, want an explanation for xing", joined)
	}
}

// Scraping must never trigger an interactive login: a platform without a session
// is skipped, and the browser's Login is not called at all.
func TestDiscovery_NeverOpensSignInWindow(t *testing.T) {
	sessions := newFakeSessionStore()
	activeSession(t, sessions, "profile-1", "linkedin")

	fb := &fakeBrowser{scraped: map[string][]browser.ScrapedJob{
		"linkedin": {{ExternalJobID: "li1", Title: "QA", Company: "Acme", ApplyURL: "http://a/1"}},
	}}
	psvc := NewProfileService(newFakeProfileStore(), t.TempDir())
	psvc.getenv = func(string) string { return "" }
	psvc.prefs["profile-1"] = models.SearchPrefs{}
	auth := NewAuthSessionService(psvc, sessions, fb)
	scraper := NewJobScraperService(auth, fb, newFakeJobStore(), nil)
	ver := NewCompanyVerificationService(newFakeVerificationStore(),
		&fakeProbe{sig: map[string]CompanySignals{}}, newFakeJobStore(), &fakeSnapshotStore{})
	svc := NewDiscoveryService(psvc, scraper, ver, &fakeShortlistStore{})

	if _, err := svc.Discover(context.Background(), "profile-1",
		[]string{"linkedin", "xing"}, 24, nil); err != nil {
		t.Fatal(err)
	}
	if fb.loginN != 0 {
		t.Fatalf("browser.Login called %d times during discovery; it must never open a sign-in window", fb.loginN)
	}
}

// When nothing at all could be gathered the run is a genuine failure, and the
// message says which platforms were skipped.
func TestDiscovery_AllPlatformsSkippedIsAnError(t *testing.T) {
	svc, _ := buildSkipDiscovery(t, newFakeSessionStore()) // no sessions at all

	var last DiscoveryProgress
	_, err := svc.Discover(context.Background(), "profile-1",
		[]string{"linkedin", "xing"}, 24, func(p DiscoveryProgress) { last = p })
	if err == nil {
		t.Fatal("expected an error when every platform was skipped")
	}
	if last.Phase != PhaseError {
		t.Fatalf("phase=%q, want %q", last.Phase, PhaseError)
	}
	for _, want := range []string{"linkedin.com", "xing.com"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s", err, want)
		}
	}
}

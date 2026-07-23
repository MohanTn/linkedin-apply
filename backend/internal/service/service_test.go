package service

import (
	"context"
	"encoding/json"
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
	scraper := NewJobScraperService(auth, fb, newFakeJobStore(), nil)
	verify := NewCompanyVerificationService(newFakeVerificationStore(), probe, nil, nil)
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
	scraper := NewJobScraperService(auth, fb, newFakeJobStore(), nil)

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

// TestDiscoverViaPortalAPI: an API platform yields shortlist entries with zero
// browser logins (acceptance criterion 1).
func TestDiscoverViaPortalAPI(t *testing.T) {
	now := time.Now()
	fb := &fakeBrowser{outcome: browser.OutcomeActive}
	api := &fakePortalAPI{jobs: []browser.ScrapedJob{
		{ExternalJobID: "ba_1", Title: "Go Dev", Company: "Acme", ApplyURL: "http://a/1", PostedAt: now},
		{ExternalJobID: "ba_2", Title: "Java Dev", Company: "Beta", ApplyURL: "http://a/2", PostedAt: now},
	}}

	pstore := newFakeProfileStore()
	psvc := NewProfileService(pstore, t.TempDir())
	psvc.getenv = func(k string) string {
		if k == "PROFILE_1_LINKEDIN_EMAIL" {
			return "a@x.com"
		}
		return ""
	}
	if _, err := psvc.LoadProfiles(context.Background()); err != nil {
		t.Fatal(err)
	}

	auth := NewAuthSessionService(psvc, newFakeSessionStore(), fb)
	scraper := NewJobScraperService(auth, fb, newFakeJobStore(), map[string]PortalAPI{"arbeitsagentur": api})
	verify := NewCompanyVerificationService(newFakeVerificationStore(), &fakeProbe{}, nil, nil)
	sl := &fakeShortlistStore{}
	disc := NewDiscoveryService(psvc, scraper, verify, sl)

	n, err := disc.Discover(context.Background(), "profile-1", []string{"arbeitsagentur"}, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(sl.entries) != 2 {
		t.Fatalf("shortlisted=%d entries=%d, want 2/2", n, len(sl.entries))
	}
	if fb.loginN != 0 {
		t.Fatalf("loginN=%d, want 0 (API platform must not log in)", fb.loginN)
	}
	if api.callN != 1 || api.lastQ.SinceHours != 24 {
		t.Fatalf("api calls=%d lastQ=%+v", api.callN, api.lastQ)
	}
}

// TestVerifyCountsReposts: a reopened posting lowers the score below the
// non-repost baseline (acceptance criterion 3).
func TestVerifyCountsReposts(t *testing.T) {
	probe := &fakeProbe{sig: map[string]CompanySignals{
		"Acme": {Found: true, HasLinkedInPage: true},
	}}

	baseline := NewCompanyVerificationService(newFakeVerificationStore(), probe, nil, nil)
	cvBase, err := baseline.Verify(context.Background(), "Acme")
	if err != nil {
		t.Fatal(err)
	}

	jobs := newFakeJobStore()
	jobs.reposts = map[string]int{"Acme": 3}
	svc := NewCompanyVerificationService(newFakeVerificationStore(), probe, jobs, nil)
	cv, err := svc.Verify(context.Background(), "Acme")
	if err != nil {
		t.Fatal(err)
	}

	var sig CompanySignals
	if err := json.Unmarshal(cv.Details, &sig); err != nil {
		t.Fatal(err)
	}
	if sig.Reposts < 1 {
		t.Fatalf("reposts=%d, want >= 1", sig.Reposts)
	}
	if cv.Score >= cvBase.Score {
		t.Fatalf("score=%d, want < baseline %d", cv.Score, cvBase.Score)
	}
}

// TestVerifyEmployeeGrowth: growth appears only with 2+ snapshots (acceptance
// criterion 4).
func TestVerifyEmployeeGrowth(t *testing.T) {
	probe := &fakeProbe{sig: map[string]CompanySignals{
		"Acme": {Found: true, HasLinkedInPage: true, EmployeeCount: 120},
	}}

	// One pre-existing snapshot + the one Verify inserts = growth computed.
	snaps := &fakeSnapshotStore{rows: []models.CompanySnapshot{
		{Company: "Acme", EmployeeCount: 100, Source: "linkedin_bucket"},
	}}
	svc := NewCompanyVerificationService(newFakeVerificationStore(), probe, nil, snaps)
	cv, err := svc.Verify(context.Background(), "Acme")
	if err != nil {
		t.Fatal(err)
	}
	var sig CompanySignals
	if err := json.Unmarshal(cv.Details, &sig); err != nil {
		t.Fatal(err)
	}
	if sig.EmployeeGrowthPct <= 19 || sig.EmployeeGrowthPct >= 21 {
		t.Fatalf("growth=%v, want ~20", sig.EmployeeGrowthPct)
	}

	// First-ever snapshot: growth unknown, score unaffected.
	fresh := &fakeSnapshotStore{}
	svc2 := NewCompanyVerificationService(newFakeVerificationStore(), probe, nil, fresh)
	cv2, err := svc2.Verify(context.Background(), "Acme")
	if err != nil {
		t.Fatal(err)
	}
	var sig2 CompanySignals
	if err := json.Unmarshal(cv2.Details, &sig2); err != nil {
		t.Fatal(err)
	}
	if sig2.EmployeeGrowthPct != 0 {
		t.Fatalf("growth=%v, want 0 with a single snapshot", sig2.EmployeeGrowthPct)
	}
	noGrowth := scoreCompany(CompanySignals{Found: true, HasLinkedInPage: true, EmployeeCount: 120})
	if cv2.Score != noGrowth {
		t.Fatalf("score=%d, want %d (unknown growth must not move the score)", cv2.Score, noGrowth)
	}
}

func TestScoreCompanyGrowthSignals(t *testing.T) {
	base := CompanySignals{Found: true, HasLinkedInPage: true}
	grow, shrink := base, base
	grow.EmployeeGrowthPct = 12
	shrink.EmployeeGrowthPct = -12
	if scoreCompany(grow) <= scoreCompany(base) {
		t.Fatal("growth should raise the score")
	}
	if scoreCompany(shrink) >= scoreCompany(base) {
		t.Fatal("shrinkage should lower the score")
	}
	kununu := base
	kununu.KununuReviews = 300
	kununu.KununuRating = 4.5
	if scoreCompany(kununu) <= scoreCompany(base) {
		t.Fatal("kununu reviews should raise the score")
	}
}

func TestCompanySlugGerman(t *testing.T) {
	cases := map[string]string{
		"Müller GmbH & Co. KG": "mueller",
		"Mueller GmbH":         "mueller",
		"Straßenbau AG":        "strassenbau",
		"Acme":                 "acme",
	}
	for in, want := range cases {
		if got := companySlug(in); got != want {
			t.Errorf("companySlug(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestAppendLogCoalescesCounters(t *testing.T) {
	var log []string
	for _, msg := range []string{
		"Searching stepstone.de…",
		"stepstone.de: found 10 jobs matching",
		"Doing company research…",
		"Researching Acme…",
		"Researching Beta…", // same step, overwrites
		"Shortlisting 1/10 jobs",
		"Shortlisting 2/10 jobs", // same step, overwrites
		"Shortlisting 2/10 jobs", // identical, ignored
		"Done — 2 shortlisted, 0 possible ghosts",
	} {
		log = appendLog(log, msg)
	}
	want := []string{
		"Searching stepstone.de…",
		"stepstone.de: found 10 jobs matching",
		"Doing company research…",
		"Researching Beta…",
		"Shortlisting 2/10 jobs",
		"Done — 2 shortlisted, 0 possible ghosts",
	}
	if len(log) != len(want) {
		t.Fatalf("log=%q, want %q", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("log[%d]=%q, want %q", i, log[i], want[i])
		}
	}
}

func TestExtractText(t *testing.T) {
	if got, err := extractText("cv.txt", []byte("hello world")); err != nil || got != "hello world" {
		t.Fatalf("txt: got %q err %v", got, err)
	}
	if got, err := extractText("cv.md", []byte("# CV\nGo dev")); err != nil || got != "# CV\nGo dev" {
		t.Fatalf("md: got %q err %v", got, err)
	}
	if _, err := extractText("cv.docx", []byte("x")); err != ErrUnsupportedResume {
		t.Fatalf("docx: want ErrUnsupportedResume, got %v", err)
	}
}

func TestResumeServiceRejectsEmptyText(t *testing.T) {
	svc := NewResumeService(&fakeResumeStore{})
	if _, err := svc.Upload(context.Background(), "profile-1", "cv.txt", []byte("  ")); err != ErrNoResumeText {
		t.Fatalf("want ErrNoResumeText, got %v", err)
	}
}

func TestScoreResumeKeywordCoverage(t *testing.T) {
	jd := "We are looking for a backend engineer with strong Go and Kubernetes experience. " +
		"You will build microservices in Go, deploy on Kubernetes, and work with Postgres and Docker. " +
		"Knowledge of gRPC and AWS is a plus. Go is used daily across the platform."
	resume := "Backend engineer building Go microservices deployed on Kubernetes. " +
		"Daily experience with Go, Postgres, Docker, gRPC and AWS across the platform. " +
		"I deploy services and build strong backend systems using Go and Kubernetes."
	full := ScoreResume(resume, jd)
	if full.Score < 60 {
		t.Fatalf("well-matched resume score=%d, want >= 60", full.Score)
	}
	if len(full.Matched) == 0 {
		t.Fatal("expected matched keywords")
	}
	poor := ScoreResume("Graphic designer skilled in Photoshop and Illustrator.", jd)
	if poor.Score >= full.Score {
		t.Fatalf("poor=%d should score below full=%d", poor.Score, full.Score)
	}
	if len(poor.Missing) == 0 {
		t.Fatal("expected missing keywords for a poor match")
	}
	// Too-short JD yields an empty (skip) result.
	if r := ScoreResume("anything", "too short"); len(r.Matched)+len(r.Missing) != 0 {
		t.Fatalf("short JD should produce empty result, got %+v", r)
	}
}

// goJD is a job description long enough to pass the minJDChars threshold and
// rich in the resume's skills.
const goJD = "Backend engineer role using Go, Kubernetes and Postgres. You build Go microservices, " +
	"run Kubernetes clusters, manage Postgres databases and Docker images, with daily Go work across the platform teams."

// buildAtsRun wires an AtsRunService over fakes: two shortlist entries (Acme,
// Beta) with the given resume text and per-company JD text.
func buildAtsRun(resumeText string, jd map[string]string) (*AtsRunService, *fakeAtsShortlist, []string) {
	jobs := newFakeJobStore()
	sl := newFakeAtsShortlist()
	var ids []string
	for _, c := range []string{"Acme", "Beta"} {
		jobID := "job_" + c
		jobs.byExt[jobID] = &models.Job{ID: jobID, Company: c, Platform: "linkedin", ApplyURL: "http://a/" + c}
		entryID := "e_" + c
		sl.entries[entryID] = &models.ShortlistEntry{ID: entryID, ProfileID: "profile-1", JobID: jobID}
		ids = append(ids, entryID)
	}
	resumes := &fakeResumeStore{}
	if resumeText != "" {
		_ = resumes.Upsert(context.Background(), &models.Resume{ProfileID: "profile-1", Text: resumeText})
	}
	return NewAtsRunService(sl, jobs, resumes, &fakeJDFetcher{byCompany: jd}), sl, ids
}

func TestAtsRun_ScoresSelection(t *testing.T) {
	svc, sl, ids := buildAtsRun("Go Kubernetes Postgres Docker developer",
		map[string]string{"Acme": goJD, "Beta": goJD})

	results, err := svc.Run(context.Background(), "profile-1", ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || len(sl.ats) != 2 {
		t.Fatalf("results=%d ats=%d, want 2/2", len(results), len(sl.ats))
	}
	for _, r := range results {
		if !r.Scored || r.Score <= 0 || r.Score > 100 {
			t.Fatalf("bad result %+v", r)
		}
	}
}

func TestAtsRun_OnlySelectedScored(t *testing.T) {
	svc, sl, ids := buildAtsRun("Go Kubernetes Postgres", map[string]string{"Acme": goJD, "Beta": goJD})
	// Score just the first entry.
	if _, err := svc.Run(context.Background(), "profile-1", ids[:1]); err != nil {
		t.Fatal(err)
	}
	if len(sl.ats) != 1 {
		t.Fatalf("expected 1 scored entry, got %d", len(sl.ats))
	}
	if _, ok := sl.ats[ids[0]]; !ok {
		t.Fatalf("wrong entry scored: %v", sl.ats)
	}
}

func TestAtsRun_NoResumeIsError(t *testing.T) {
	svc, _, ids := buildAtsRun("", map[string]string{"Acme": goJD, "Beta": goJD})
	if _, err := svc.Run(context.Background(), "profile-1", ids); err != ErrNoResume {
		t.Fatalf("err=%v, want ErrNoResume", err)
	}
}

func TestAtsRun_UnfetchableJDSkipsEntry(t *testing.T) {
	// Beta's JD is unfetchable ("") -> only Acme scored, run still succeeds.
	svc, sl, ids := buildAtsRun("Go Kubernetes Postgres", map[string]string{"Acme": goJD, "Beta": ""})
	results, err := svc.Run(context.Background(), "profile-1", ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(sl.ats) != 1 {
		t.Fatalf("expected 1 scored entry, got %d", len(sl.ats))
	}
	var scored, unscored int
	for _, r := range results {
		if r.Scored {
			scored++
		} else {
			unscored++
		}
	}
	if scored != 1 || unscored != 1 {
		t.Fatalf("scored=%d unscored=%d, want 1/1", scored, unscored)
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

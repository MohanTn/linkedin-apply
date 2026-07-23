package service

import (
	"context"
	"encoding/json"

	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/models"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
)

// ---- in-memory stores ----

type fakeProfileStore struct{ m map[string]*models.Profile }

func newFakeProfileStore() *fakeProfileStore {
	return &fakeProfileStore{m: map[string]*models.Profile{}}
}
func (f *fakeProfileStore) GetByID(_ context.Context, id string) (*models.Profile, error) {
	if p, ok := f.m[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, repository.ErrNotFound
}
func (f *fakeProfileStore) GetAll(context.Context) ([]models.Profile, error) {
	var out []models.Profile
	for _, p := range f.m {
		out = append(out, *p)
	}
	return out, nil
}
func (f *fakeProfileStore) Upsert(_ context.Context, p *models.Profile) error {
	cp := *p
	f.m[p.ID] = &cp
	return nil
}

type fakeJobStore struct {
	byExt map[string]*models.Job
	seq   int
}

func newFakeJobStore() *fakeJobStore { return &fakeJobStore{byExt: map[string]*models.Job{}} }
func (f *fakeJobStore) Upsert(_ context.Context, j *models.Job) (*models.Job, error) {
	if existing, ok := f.byExt[j.ExternalJobID]; ok {
		j.ID = existing.ID // preserve identity on conflict
	}
	cp := *j
	f.byExt[j.ExternalJobID] = &cp
	return &cp, nil
}

type fakeVerificationStore struct {
	m map[string]*models.CompanyVerification
}

func newFakeVerificationStore() *fakeVerificationStore {
	return &fakeVerificationStore{m: map[string]*models.CompanyVerification{}}
}
func (f *fakeVerificationStore) GetByCompany(_ context.Context, c string) (*models.CompanyVerification, error) {
	if v, ok := f.m[c]; ok {
		return v, nil
	}
	return nil, repository.ErrNotFound
}
func (f *fakeVerificationStore) Upsert(_ context.Context, cv *models.CompanyVerification) error {
	f.m[cv.Company] = cv
	return nil
}

type fakeShortlistStore struct{ entries []models.ShortlistEntry }

func (f *fakeShortlistStore) Upsert(_ context.Context, e *models.ShortlistEntry) (*models.ShortlistEntry, error) {
	f.entries = append(f.entries, *e)
	return e, nil
}

type fakeSessionStore struct {
	m map[string]*models.BrowserSession
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{m: map[string]*models.BrowserSession{}}
}
func (f *fakeSessionStore) Get(_ context.Context, profileID, platform string) (*models.BrowserSession, error) {
	if s, ok := f.m[profileID+"|"+platform]; ok {
		return s, nil
	}
	return nil, repository.ErrNotFound
}
func (f *fakeSessionStore) Upsert(_ context.Context, s *models.BrowserSession) error {
	f.m[s.ProfileID+"|"+s.Platform] = s
	return nil
}

// ---- fake browser ----

type fakeBrowser struct {
	outcome string
	scraped map[string][]browser.ScrapedJob // platform -> jobs
	checkOK bool
	loginN  int
}

func (b *fakeBrowser) Login(context.Context, string, string, string) (browser.LoginResult, error) {
	b.loginN++
	out := b.outcome
	if out == "" {
		out = browser.OutcomeActive
	}
	res := browser.LoginResult{Outcome: out}
	if out == browser.OutcomeActive {
		res.Cookies = json.RawMessage(`[{"name":"li_at","value":"x"}]`)
	}
	return res, nil
}
func (b *fakeBrowser) CheckSession(context.Context, string, json.RawMessage) (bool, error) {
	return b.checkOK, nil
}
func (b *fakeBrowser) ScrapeRecent(_ context.Context, platform string, _ json.RawMessage, _ browser.ScrapeQuery) ([]browser.ScrapedJob, error) {
	return b.scraped[platform], nil
}

// ---- fake company probe ----

type fakeProbe struct{ sig map[string]CompanySignals }

func (p *fakeProbe) Probe(_ context.Context, company string) (CompanySignals, error) {
	return p.sig[company], nil
}

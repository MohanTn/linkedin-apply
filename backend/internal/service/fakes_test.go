package service

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/models"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
)

// ---- in-memory stores ----

type fakeProfileStore struct {
	m       map[string]*models.Profile
	prefs   map[string][]byte
	deleted map[string]bool // tombstones, mirroring the soft-deleted rows
}

func newFakeProfileStore() *fakeProfileStore {
	return &fakeProfileStore{
		m:       map[string]*models.Profile{},
		prefs:   map[string][]byte{},
		deleted: map[string]bool{},
	}
}
func (f *fakeProfileStore) GetPrefs(_ context.Context, id string) ([]byte, error) {
	return f.prefs[id], nil
}
func (f *fakeProfileStore) SetPrefs(_ context.Context, id string, prefs []byte) error {
	f.prefs[id] = prefs
	return nil
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

// SeedIfAbsent mirrors the repo: insert only when the id is unknown. Tombstoned
// ids stay known, so a deleted env profile is not re-seeded.
func (f *fakeProfileStore) SeedIfAbsent(_ context.Context, p *models.Profile) error {
	if _, known := f.m[p.ID]; known || f.deleted[p.ID] {
		return nil
	}
	cp := *p
	f.m[p.ID] = &cp
	return nil
}
func (f *fakeProfileStore) Delete(_ context.Context, id string) error {
	if _, ok := f.m[id]; !ok {
		return repository.ErrNotFound
	}
	delete(f.m, id)
	f.deleted[id] = true
	return nil
}

type fakeJobStore struct {
	byExt   map[string]*models.Job
	seq     int
	reposts map[string]int // company -> repost count
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
func (f *fakeJobStore) CountReposts(_ context.Context, company string, _ int) (int, error) {
	return f.reposts[company], nil
}
func (f *fakeJobStore) GetByID(_ context.Context, id string) (*models.Job, error) {
	for _, j := range f.byExt {
		if j.ID == id {
			return j, nil
		}
	}
	return nil, repository.ErrNotFound
}

// fakeAtsShortlist backs AtsRunService: read entries, record ATS writes.
type fakeAtsShortlist struct {
	mu      sync.Mutex
	entries map[string]*models.ShortlistEntry
	ats     map[string]int
}

func newFakeAtsShortlist() *fakeAtsShortlist {
	return &fakeAtsShortlist{entries: map[string]*models.ShortlistEntry{}, ats: map[string]int{}}
}
func (f *fakeAtsShortlist) Get(_ context.Context, id string) (*models.ShortlistEntry, error) {
	if e, ok := f.entries[id]; ok {
		return e, nil
	}
	return nil, repository.ErrNotFound
}
func (f *fakeAtsShortlist) UpdateAts(_ context.Context, id string, score int, _ json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ats[id] = score
	return nil
}

type fakeSnapshotStore struct{ rows []models.CompanySnapshot }

func (f *fakeSnapshotStore) Insert(_ context.Context, s *models.CompanySnapshot) error {
	// Prepend: Recent() returns newest first.
	f.rows = append([]models.CompanySnapshot{*s}, f.rows...)
	return nil
}
func (f *fakeSnapshotStore) Recent(_ context.Context, company string, limit int) ([]models.CompanySnapshot, error) {
	var out []models.CompanySnapshot
	for _, r := range f.rows {
		if r.Company == company && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
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

type fakeResumeStore struct{ m map[string]*models.Resume }

func (f *fakeResumeStore) Upsert(_ context.Context, r *models.Resume) error {
	if f.m == nil {
		f.m = map[string]*models.Resume{}
	}
	f.m[r.ProfileID] = r
	return nil
}
func (f *fakeResumeStore) GetByProfile(_ context.Context, profileID string) (*models.Resume, error) {
	if r, ok := f.m[profileID]; ok {
		return r, nil
	}
	return nil, repository.ErrNotFound
}

// fakeJDFetcher returns canned JD text keyed by company ("" -> unfetchable).
type fakeJDFetcher struct{ byCompany map[string]string }

func (f *fakeJDFetcher) FetchJD(_ context.Context, _ string, job models.Job) (string, error) {
	return f.byCompany[job.Company], nil
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
func (b *fakeBrowser) FetchJD(_ context.Context, _ string, _ json.RawMessage, _ string) (string, error) {
	return "", nil
}

// ---- fake portal API ----

type fakePortalAPI struct {
	jobs  []browser.ScrapedJob
	callN int
	lastQ browser.ScrapeQuery
}

func (f *fakePortalAPI) ScrapeRecent(_ context.Context, q browser.ScrapeQuery) ([]browser.ScrapedJob, error) {
	f.callN++
	f.lastQ = q
	return f.jobs, nil
}

// ---- fake company probe ----

type fakeProbe struct{ sig map[string]CompanySignals }

func (p *fakeProbe) Probe(_ context.Context, company string) (CompanySignals, error) {
	return p.sig[company], nil
}

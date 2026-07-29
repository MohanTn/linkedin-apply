package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/models"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
	"github.com/mohan/linkedin-apply-backend/internal/service"
)

// --- in-memory stores -------------------------------------------------------

type memProfileStore struct {
	m       map[string]*models.Profile
	deleted map[string]bool
}

func (s *memProfileStore) GetByID(_ context.Context, id string) (*models.Profile, error) {
	if p, ok := s.m[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, repository.ErrNotFound
}
func (s *memProfileStore) GetAll(context.Context) ([]models.Profile, error) {
	var out []models.Profile
	for _, p := range s.m {
		out = append(out, *p)
	}
	return out, nil
}
func (s *memProfileStore) Upsert(_ context.Context, p *models.Profile) error {
	cp := *p
	s.m[p.ID] = &cp
	return nil
}
func (s *memProfileStore) SeedIfAbsent(_ context.Context, p *models.Profile) error {
	if _, known := s.m[p.ID]; known || s.deleted[p.ID] {
		return nil
	}
	cp := *p
	s.m[p.ID] = &cp
	return nil
}
func (s *memProfileStore) Delete(_ context.Context, id string) error {
	if _, ok := s.m[id]; !ok {
		return repository.ErrNotFound
	}
	delete(s.m, id)
	s.deleted[id] = true
	return nil
}
func (s *memProfileStore) GetPrefs(context.Context, string) ([]byte, error) { return nil, nil }
func (s *memProfileStore) SetPrefs(context.Context, string, []byte) error   { return nil }

type memSessionStore struct{ m map[string]*models.BrowserSession }

func (s *memSessionStore) Get(_ context.Context, profileID, platform string) (*models.BrowserSession, error) {
	if v, ok := s.m[profileID+"|"+platform]; ok {
		return v, nil
	}
	return nil, repository.ErrNotFound
}
func (s *memSessionStore) Upsert(_ context.Context, v *models.BrowserSession) error {
	s.m[v.ProfileID+"|"+v.Platform] = v
	return nil
}

// newProfileRouter wires the real handler + services over in-memory stores.
func newProfileRouter(t *testing.T) (*gin.Engine, *memSessionStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pstore := &memProfileStore{m: map[string]*models.Profile{}, deleted: map[string]bool{}}
	sstore := &memSessionStore{m: map[string]*models.BrowserSession{}}
	psvc := service.NewProfileService(pstore, t.TempDir())
	auth := service.NewAuthSessionService(psvc, sstore, nil)
	h := NewProfileHandler(psvc, auth)

	r := gin.New()
	r.GET("/api/profiles", h.GetProfiles)
	r.POST("/api/profiles", h.Create)
	r.PUT("/api/profiles/:id", h.Update)
	r.DELETE("/api/profiles/:id", h.Delete)
	return r, sstore
}

func do(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// --- tests ------------------------------------------------------------------

func TestProfileHandler_CRUDRoundTrip(t *testing.T) {
	r, _ := newProfileRouter(t)

	// A profile is just a name — no credential fields are accepted or required.
	w := do(t, r, http.MethodPost, "/api/profiles", `{"name":"Mohan"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}

	var created models.Profile
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Source != models.ProfileSourceDB {
		t.Fatalf("unexpected profile: %+v", created)
	}
	// Every login platform is reported, all disconnected until the user signs in.
	if len(created.Sessions) != len(service.LoginPlatforms) {
		t.Fatalf("sessions=%+v, want %d entries", created.Sessions, len(service.LoginPlatforms))
	}
	for _, ps := range created.Sessions {
		if ps.Status != "none" {
			t.Fatalf("%s status=%q, want none", ps.Platform, ps.Status)
		}
	}

	// List shows it.
	w = do(t, r, http.MethodGet, "/api/profiles", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Mohan") {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}

	// Rename.
	w = do(t, r, http.MethodPut, "/api/profiles/"+created.ID, `{"name":"Mohan T"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Mohan T") {
		t.Fatalf("rename not reflected: %s", w.Body.String())
	}

	// Delete.
	w = do(t, r, http.MethodDelete, "/api/profiles/"+created.ID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
	w = do(t, r, http.MethodGet, "/api/profiles", "")
	if strings.Contains(w.Body.String(), "Mohan") {
		t.Fatalf("profile still listed after delete: %s", w.Body.String())
	}
}

func TestProfileHandler_CreateRejectsBlankName(t *testing.T) {
	r, _ := newProfileRouter(t)
	w := do(t, r, http.MethodPost, "/api/profiles", `{"name":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (body=%s)", w.Code, w.Body.String())
	}
}

func TestProfileHandler_EnvProfileIsEditableAndDeletable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pstore := &memProfileStore{
		m: map[string]*models.Profile{
			"profile-1": {ID: "profile-1", Name: "Env", Source: models.ProfileSourceEnv},
		},
		deleted: map[string]bool{},
	}
	psvc := service.NewProfileService(pstore, t.TempDir())
	auth := service.NewAuthSessionService(psvc, &memSessionStore{m: map[string]*models.BrowserSession{}}, nil)
	h := NewProfileHandler(psvc, auth)
	r := gin.New()
	r.PUT("/api/profiles/:id", h.Update)
	r.DELETE("/api/profiles/:id", h.Delete)

	w := do(t, r, http.MethodPut, "/api/profiles/profile-1", `{"name":"Renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w.Code, w.Body.String())
	}
	if pstore.m["profile-1"].Name != "Renamed" {
		t.Fatalf("rename not persisted: %+v", pstore.m["profile-1"])
	}
	if w := do(t, r, http.MethodDelete, "/api/profiles/profile-1", ""); w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProfileHandler_UnknownProfileIs404(t *testing.T) {
	r, _ := newProfileRouter(t)
	if w := do(t, r, http.MethodPut, "/api/profiles/nope", `{"name":"x"}`); w.Code != http.StatusNotFound {
		t.Fatalf("update status=%d, want 404", w.Code)
	}
	if w := do(t, r, http.MethodDelete, "/api/profiles/nope", ""); w.Code != http.StatusNotFound {
		t.Fatalf("delete status=%d, want 404", w.Code)
	}
}

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
)

type fakeShortlist struct {
	rows    []models.ShortlistRow
	updated map[string]string
	cleared string
}

func (f *fakeShortlist) DeleteByProfile(_ context.Context, profileID string) (int64, error) {
	f.cleared = profileID
	n := int64(len(f.rows))
	f.rows = nil
	return n, nil
}

func (f *fakeShortlist) List(_ context.Context, ff repository.ShortlistFilter) ([]models.ShortlistRow, error) {
	var out []models.ShortlistRow
	for _, r := range f.rows {
		if !ff.IncludeGhost && r.IsGhost {
			continue
		}
		if r.Score < ff.MinScore {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
func (f *fakeShortlist) UpdateStatus(_ context.Context, id, status string) (*models.ShortlistEntry, error) {
	if id != "sl-1" {
		return nil, repository.ErrNotFound
	}
	if f.updated == nil {
		f.updated = map[string]string{}
	}
	f.updated[id] = status
	return &models.ShortlistEntry{ID: id, Status: status}, nil
}
func (f *fakeShortlist) Stats(context.Context, string) (map[string]int, error) {
	return map[string]int{"new": 2, "ghost": 1}, nil
}

func newRouter(store ShortlistReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewShortlistHandler(store)
	r.GET("/api/shortlist", h.GetShortlist)
	r.PATCH("/api/shortlist/:id", h.UpdateStatus)
	return r
}

func TestGetShortlist_FiltersGhostByDefault(t *testing.T) {
	store := &fakeShortlist{rows: []models.ShortlistRow{
		{ShortlistEntry: models.ShortlistEntry{ID: "a", Score: 80, IsGhost: false}},
		{ShortlistEntry: models.ShortlistEntry{ID: "b", Score: 20, IsGhost: true}},
	}}
	r := newRouter(store)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/shortlist?profileId=p1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	var resp struct {
		Items []models.ShortlistRow `json:"items"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "a" {
		t.Fatalf("expected only non-ghost row, got %+v", resp)
	}
}

func TestGetShortlist_RequiresProfileID(t *testing.T) {
	r := newRouter(&fakeShortlist{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/shortlist", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
}

func TestUpdateStatus_ValidatesStatus(t *testing.T) {
	store := &fakeShortlist{}
	r := newRouter(store)

	// invalid status rejected
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/api/shortlist/sl-1", strings.NewReader(`{"status":"bogus"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status code=%d, want 400", w.Code)
	}

	// valid status applied
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/api/shortlist/sl-1", strings.NewReader(`{"status":"applied"}`)))
	if w.Code != http.StatusOK || store.updated["sl-1"] != "applied" {
		t.Fatalf("code=%d updated=%v", w.Code, store.updated)
	}
}

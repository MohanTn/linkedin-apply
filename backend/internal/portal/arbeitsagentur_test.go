package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mohan/linkedin-apply-backend/internal/browser"
)

func TestArbeitsagenturScrapeRecent(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		gotKey = r.Header.Get("X-API-Key")
		w.Write([]byte(`{"stellenangebote":[
			{"refnr":"10001-1","titel":"Go Dev","arbeitgeber":"Acme GmbH","arbeitsort":{"ort":"Berlin","region":"Berlin"},"aktuelleVeroeffentlichungsdatum":"2026-07-22"},
			{"refnr":"10001-1","titel":"Dup","arbeitgeber":"Acme GmbH","arbeitsort":{},"aktuelleVeroeffentlichungsdatum":""},
			{"refnr":"","titel":"NoRef","arbeitgeber":"X","arbeitsort":{},"aktuelleVeroeffentlichungsdatum":""}
		]}`))
	}))
	defer srv.Close()

	c := NewArbeitsagenturClient()
	c.baseURL = srv.URL
	jobs, err := c.ScrapeRecent(context.Background(), browser.ScrapeQuery{
		Keywords:   []string{"golang"},
		Locations:  []string{"Berlin"},
		SinceHours: 48,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 { // dedupe by refnr, drop empty refnr
		t.Fatalf("jobs=%d, want 1", len(jobs))
	}
	j := jobs[0]
	if j.ExternalJobID != "ba_10001-1" || j.Company != "Acme GmbH" || j.Location != "Berlin" {
		t.Fatalf("unexpected job: %+v", j)
	}
	if j.ApplyURL != "https://www.arbeitsagentur.de/jobsuche/jobdetail/10001-1" {
		t.Fatalf("applyURL=%s", j.ApplyURL)
	}
	if gotKey != "jobboerse-jobsuche" {
		t.Fatalf("X-API-Key=%q", gotKey)
	}
	// 48h -> veroeffentlichtseit=2 days.
	if want := "veroeffentlichtseit=2"; !strings.Contains(gotPath, want) || !strings.Contains(gotPath, "was=golang") {
		t.Fatalf("path=%s, want %s and was=golang", gotPath, want)
	}
}

func TestArbeitsagenturErrorKeepsPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewArbeitsagenturClient()
	c.baseURL = srv.URL
	jobs, err := c.ScrapeRecent(context.Background(), browser.ScrapeQuery{Keywords: []string{"go"}})
	if err == nil {
		t.Fatal("want error on 429")
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs=%d, want 0", len(jobs))
	}
}

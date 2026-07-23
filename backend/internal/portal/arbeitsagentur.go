// Package portal holds API-based job sources that need no browser at all.
package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mohan/linkedin-apply-backend/internal/browser"
)

// ArbeitsagenturClient queries the official Bundesagentur für Arbeit jobsuche
// REST API (jobsuche.api.bund.dev). Free, no account; only a static API key
// header is required.
type ArbeitsagenturClient struct {
	client  *http.Client
	baseURL string
}

func NewArbeitsagenturClient() *ArbeitsagenturClient {
	return &ArbeitsagenturClient{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://rest.arbeitsagentur.de/jobboerse/jobsuche-service",
	}
}

// baJob is the subset of the API's stellenangebot we consume.
type baJob struct {
	Refnr       string `json:"refnr"`
	Titel       string `json:"titel"`
	Arbeitgeber string `json:"arbeitgeber"`
	Arbeitsort  struct {
		Ort    string `json:"ort"`
		Region string `json:"region"`
	} `json:"arbeitsort"`
	VeroeffentlichtSeit string `json:"aktuelleVeroeffentlichungsdatum"`
}

type baResponse struct {
	Stellenangebote []baJob `json:"stellenangebote"`
}

// ScrapeRecent runs one API search per keyword and returns normalized jobs.
// A failed request returns whatever was gathered plus the error; the caller
// (DiscoveryService) treats that as a partial result, not an abort.
func (c *ArbeitsagenturClient) ScrapeRecent(ctx context.Context, q browser.ScrapeQuery) ([]browser.ScrapedJob, error) {
	days := q.SinceHours / 24
	if days < 1 {
		days = 1
	}
	keywords := q.Keywords
	if len(keywords) == 0 {
		keywords = []string{""}
	}

	var all []browser.ScrapedJob
	seen := map[string]bool{}
	for _, kw := range keywords {
		v := url.Values{}
		if kw != "" {
			v.Set("was", kw)
		}
		if len(q.Locations) > 0 {
			v.Set("wo", q.Locations[0])
		}
		v.Set("veroeffentlichtseit", strconv.Itoa(days))
		v.Set("size", "100")
		if q.RemoteOnly {
			v.Set("arbeitszeit", "ho") // Homeoffice
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			c.baseURL+"/pc/v4/jobs?"+v.Encode(), nil)
		if err != nil {
			return all, err
		}
		req.Header.Set("X-API-Key", "jobboerse-jobsuche")
		req.Header.Set("Accept", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			return all, fmt.Errorf("arbeitsagentur %q: %w", kw, err)
		}
		var body baResponse
		derr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return all, fmt.Errorf("arbeitsagentur %q: status %d", kw, resp.StatusCode)
		}
		if derr != nil {
			continue // tolerate a bad payload, keep what we have
		}

		for _, j := range body.Stellenangebote {
			if j.Refnr == "" || seen[j.Refnr] {
				continue
			}
			seen[j.Refnr] = true
			all = append(all, toScraped(j))
		}
	}
	return all, nil
}

func toScraped(j baJob) browser.ScrapedJob {
	var posted time.Time // zero = publish date unknown
	if j.VeroeffentlichtSeit != "" {
		if t, err := time.Parse("2006-01-02", j.VeroeffentlichtSeit); err == nil {
			posted = t
		}
	}
	loc := j.Arbeitsort.Ort
	if loc == "" {
		loc = j.Arbeitsort.Region
	}
	raw, _ := json.Marshal(j)
	return browser.ScrapedJob{
		ExternalJobID: "ba_" + j.Refnr,
		Title:         j.Titel,
		Company:       j.Arbeitgeber,
		ApplyURL:      "https://www.arbeitsagentur.de/jobsuche/jobdetail/" + url.PathEscape(j.Refnr),
		Location:      loc,
		PostedAt:      posted,
		Raw:           raw,
	}
}

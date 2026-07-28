package service

import (
	"context"
	"encoding/json"

	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// JDFetcher retrieves a job's full description text for ATS scoring.
type JDFetcher interface {
	FetchJD(ctx context.Context, profileID string, job models.Job) (string, error)
}

// BrowserJDFetcher opens each job's apply URL in the headless browser and
// extracts the page text. Login platforms (linkedin/glassdoor/xing) reuse the
// profile's stored session so gated detail pages render; public portals and the
// Arbeitsagentur SPA are fetched without cookies. Any failure returns an empty
// string, never an error, so one unreachable JD never fails the run — the same
// degradation contract as HTTPProbe.
//
// NOTE: the official Arbeitsagentur jobdetails REST endpoint is not usable
// (403 for the public keys), so its Angular detail page is rendered in-browser
// like every other platform instead.
type BrowserJDFetcher struct {
	auth    *AuthSessionService
	browser browser.Browser
}

func NewBrowserJDFetcher(auth *AuthSessionService, b browser.Browser) *BrowserJDFetcher {
	return &BrowserJDFetcher{auth: auth, browser: b}
}

func (f *BrowserJDFetcher) FetchJD(ctx context.Context, profileID string, job models.Job) (string, error) {
	if job.ApplyURL == "" {
		return "", nil
	}
	var cookies json.RawMessage
	if browser.NeedsLogin(job.Platform) {
		// Stored session only — an ATS run must never open a sign-in window.
		sess, err := f.auth.ActiveSession(ctx, profileID, job.Platform)
		if err != nil {
			return "", nil // no session -> best-effort skip, not a run failure
		}
		cookies = sess.Cookies
	}
	text, err := f.browser.FetchJD(ctx, job.Platform, cookies, job.ApplyURL)
	if err != nil {
		return "", nil
	}
	return text, nil
}

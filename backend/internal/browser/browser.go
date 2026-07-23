// Package browser abstracts the logged-in headless browser used for login and
// scraping, so services can be unit-tested against a fake and driven by real
// chromedp in production.
package browser

import (
	"context"
	"encoding/json"
	"time"
)

// Login outcomes surfaced to the user.
const (
	OutcomeActive       = "active"
	OutcomeInvalidCreds = "invalid_creds"
	OutcomeNeeds2FA     = "needs_2fa"
)

// LoginResult is the outcome of a credentialed login attempt.
type LoginResult struct {
	Outcome string          // active | invalid_creds | needs_2fa
	Cookies json.RawMessage // serialized cookie jar (only on active)
}

// ScrapeQuery describes what recent listings to gather.
type ScrapeQuery struct {
	Keywords         []string
	Locations        []string
	RemoteOnly       bool
	ExperienceLevels []string
	SinceHours       int
}

// ScrapedJob is a raw listing extracted from a results page.
type ScrapedJob struct {
	ExternalJobID string
	Title         string
	Company       string
	ApplyURL      string
	Location      string
	Salary        string
	PostedAt      time.Time
	Raw           json.RawMessage
}

// Browser is the port the services depend on.
type Browser interface {
	// Login authenticates and returns cookies or a non-active outcome.
	Login(ctx context.Context, platform, email, password string) (LoginResult, error)
	// CheckSession reports whether the given cookies still yield a logged-in state.
	CheckSession(ctx context.Context, platform string, cookies json.RawMessage) (bool, error)
	// ScrapeRecent returns listings matching q using the authenticated cookies.
	ScrapeRecent(ctx context.Context, platform string, cookies json.RawMessage, q ScrapeQuery) ([]ScrapedJob, error)
}

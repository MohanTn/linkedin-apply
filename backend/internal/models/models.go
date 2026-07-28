// Package models holds the domain structs shared across repositories and services.
package models

import (
	"encoding/json"
	"time"
)

// Profile sources: env-seeded profiles are read-only in the cockpit, db ones are
// created by the user and fully editable.
const (
	ProfileSourceEnv = "env"
	ProfileSourceDB  = "db"
)

// Profile is a job-seeker identity with its own credentials and search preferences.
type Profile struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	LinkedinEmail   string    `json:"linkedinEmail"`
	GlassdoorEmail  string    `json:"glassdoorEmail"`
	Source          string    `json:"source"` // env | db
	ProfileDataPath string    `json:"-"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	// Sessions reports this profile's browser session per login platform.
	// Populated on read, ignored on write.
	Sessions []PlatformSession `json:"sessions,omitempty"`
}

// PlatformSession is the cockpit's view of one portal's login state. No
// credentials are involved: the user signs in through a real browser window and
// only the resulting session is persisted.
type PlatformSession struct {
	Platform string `json:"platform"`
	// Status is the persisted session state: active | expired | needs_2fa | none.
	Status string `json:"status"`
	// ExpiresAt is when the stored session stops being reused. A pointer because
	// omitempty does not skip a zero time.Time, and "no session" must not
	// serialize as a date in year 1.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// SearchPrefs comes from data_profileN.json and drives what the scraper looks for.
type SearchPrefs struct {
	Keywords         []string `json:"keywords"`
	Locations        []string `json:"locations"`
	RemoteOnly       bool     `json:"remoteOnly"`
	ExperienceLevels []string `json:"experienceLevels"`
	ExcludeCompanies []string `json:"excludeCompanies"`
	MinCompanyScore  int      `json:"minCompanyScore"`
}

// ProfileFile is the on-disk shape of data_profileN.json.
type ProfileFile struct {
	Name   string      `json:"name"`
	Search SearchPrefs `json:"search"`
}

// Job is a single scraped listing.
type Job struct {
	ID            string          `json:"id"`
	ExternalJobID string          `json:"externalJobId"`
	Title         string          `json:"title"`
	Company       string          `json:"company"`
	ApplyURL      string          `json:"applyUrl"`
	Platform      string          `json:"platform"` // "linkedin" | "glassdoor"
	PostedAt      time.Time       `json:"postedAt"`
	FirstSeenAt   time.Time       `json:"firstSeenAt"`
	Location      string          `json:"location"`
	Salary        string          `json:"salary,omitempty"`
	RawData       json.RawMessage `json:"-"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// Verification statuses / scores.
const (
	GhostThreshold = 40 // score below this is flagged as a likely ghost job
)

// CompanyVerification is the cached ghost-job check result for a company.
type CompanyVerification struct {
	ID                  string          `json:"id"`
	Company             string          `json:"company"`
	IsGhostJob          bool            `json:"isGhostJob"`
	Score               int             `json:"score"` // 0-100
	VerificationMethods json.RawMessage `json:"verificationMethods,omitempty"`
	Details             json.RawMessage `json:"details,omitempty"`
	ExpiresAt           time.Time       `json:"expiresAt"`
	CreatedAt           time.Time       `json:"createdAt"`
}

// CompanySnapshot is one employee-count observation for a company; growth is
// computed from the delta between snapshots taken across discovery runs.
type CompanySnapshot struct {
	ID            string    `json:"id"`
	Company       string    `json:"company"`
	EmployeeCount int       `json:"employeeCount"`
	Source        string    `json:"source"` // linkedin_bucket | kununu
	CapturedAt    time.Time `json:"capturedAt"`
}

// Resume is a profile's uploaded resume plus its extracted plain text.
type Resume struct {
	ID         string    `json:"id"`
	ProfileID  string    `json:"profileId"`
	Filename   string    `json:"filename"`
	Content    []byte    `json:"-"`
	Text       string    `json:"-"`
	UploadedAt time.Time `json:"uploadedAt"`
}

// Shortlist entry statuses (user-managed).
const (
	StatusNew       = "new"
	StatusSaved     = "saved"
	StatusDismissed = "dismissed"
	StatusApplied   = "applied"
)

// ShortlistEntry is a discovered job + its verification, with a manual user status.
type ShortlistEntry struct {
	ID             string    `json:"id"`
	ProfileID      string    `json:"profileId"`
	JobID          string    `json:"jobId"`
	VerificationID string    `json:"verificationId,omitempty"`
	DiscoveryRunID string    `json:"discoveryRunId,omitempty"`
	Score          int       `json:"score"`
	IsGhost        bool      `json:"isGhost"`
	ApplyURL       string    `json:"applyUrl"`
	Status         string    `json:"status"`
	DiscoveredAt   time.Time `json:"discoveredAt"`
	// ATS match of the profile's resume against the job description. Nil until a
	// resume is uploaded and the matching phase runs.
	AtsScore   *int            `json:"atsScore,omitempty"`
	AtsDetails json.RawMessage `json:"atsDetails,omitempty"`
}

// DiscoveryRun tracks a discovery run's start time.
type DiscoveryRun struct {
	ID          string    `json:"id"`
	ProfileID   string    `json:"profileId"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt,omitempty"`
}

// ShortlistRow is a ShortlistEntry joined with its job fields and run info, for the cockpit.
type ShortlistRow struct {
	ShortlistEntry
	Title        string          `json:"title"`
	Company      string          `json:"company"`
	Location     string          `json:"location"`
	PostedAt     time.Time       `json:"postedAt"`
	RunStartedAt time.Time       `json:"runStartedAt,omitempty"`
	Platform     string          `json:"platform,omitempty"`
	Signals      json.RawMessage `json:"signals,omitempty"` // company_verifications.details
}

// Session statuses.
const (
	SessionActive       = "active"
	SessionExpired      = "expired"
	SessionNeeds2FA     = "needs_2fa"
	SessionInvalidCreds = "invalid_creds"
)

// BrowserSession is a persisted logged-in browser session per profile/platform.
type BrowserSession struct {
	ID        string          `json:"id"`
	ProfileID string          `json:"profileId"`
	Platform  string          `json:"platform"`
	Cookies   json.RawMessage `json:"-"`
	Status    string          `json:"status"`
	LastLogin time.Time       `json:"lastLogin"`
	ExpiresAt time.Time       `json:"expiresAt"`
}

// IsValidStatus reports whether s is a user-settable shortlist status.
func IsValidStatus(s string) bool {
	switch s {
	case StatusNew, StatusSaved, StatusDismissed, StatusApplied:
		return true
	}
	return false
}

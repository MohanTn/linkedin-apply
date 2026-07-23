package service

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPProbe is a best-effort CompanyProbe. It checks whether a company has a
// reachable LinkedIn company page. Real review/rating scraping is intentionally
// omitted because LinkedIn/Glassdoor gate that behind auth and bot checks; when
// a signal cannot be obtained the probe reports Found=false and the scorer falls
// back to a neutral (unverified) score rather than a ghost flag.
type HTTPProbe struct {
	client *http.Client
}

func NewHTTPProbe() *HTTPProbe {
	return &HTTPProbe{client: &http.Client{Timeout: 8 * time.Second}}
}

func (p *HTTPProbe) Probe(ctx context.Context, company string) (CompanySignals, error) {
	slug := companySlug(company)
	if slug == "" {
		return CompanySignals{}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.linkedin.com/company/"+url.PathEscape(slug), nil)
	if err != nil {
		return CompanySignals{}, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JobDiscoveryBot/1.0)")
	resp, err := p.client.Do(req)
	if err != nil {
		return CompanySignals{}, nil // unreachable -> unverified, not an error
	}
	defer resp.Body.Close()

	sig := CompanySignals{}
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		sig.Found = true
		sig.HasLinkedInPage = true
	}
	return sig, nil
}

func companySlug(company string) string {
	s := strings.ToLower(strings.TrimSpace(company))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-' || r == '_':
			return '-'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

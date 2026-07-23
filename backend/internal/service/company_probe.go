package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// HTTPProbe is a best-effort CompanyProbe. It checks whether a company has a
// reachable LinkedIn company page and a public Kununu (German Glassdoor)
// profile, and opportunistically parses an employee count / rating out of
// whatever public HTML is readable. LinkedIn/Glassdoor gate rich data behind
// auth and bot checks; when a signal cannot be obtained the probe reports it as
// zero and the scorer treats it as unknown, never as a ghost flag.
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

	sig := CompanySignals{}
	if body, ok := p.fetch(ctx, "https://www.linkedin.com/company/"+url.PathEscape(slug)); ok {
		sig.Found = true
		sig.HasLinkedInPage = true
		sig.EmployeeCount = parseStaffCount(body)
	}

	if body, ok := p.fetch(ctx, "https://www.kununu.com/de/"+url.PathEscape(slug)); ok {
		sig.Found = true
		sig.KununuReviews, sig.KununuRating = parseKununu(body)
	}
	return sig, nil
}

// fetch GETs the URL and returns up to 512KB of body on a 2xx/3xx response.
// Any failure is reported as not-ok, never as an error (unverified, not ghost).
func (p *HTTPProbe) fetch(ctx context.Context, u string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JobDiscoveryBot/1.0)")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	return string(body), true
}

var (
	staffCountRe   = regexp.MustCompile(`"staffCount"\s*:\s*(\d+)`)
	kununuCountRe  = regexp.MustCompile(`"(?:ratingCount|reviewCount)"\s*:\s*"?(\d+)"?`)
	kununuRatingRe = regexp.MustCompile(`"ratingValue"\s*:\s*"?([0-9.]+)"?`)
	legalSuffixRe  = regexp.MustCompile(`(?i)\s+(gmbh\s*&\s*co\.?\s*kg|gmbh|ag|se|kg|ug|ohg|e\.?\s?v\.?|mbh|inc\.?|ltd\.?|llc)\s*$`)
)

// parseStaffCount pulls an employee count out of a public LinkedIn page, when
// one is embedded. 0 = unknown.
func parseStaffCount(body string) int {
	if m := staffCountRe.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// parseKununu pulls the aggregate review count and rating (JSON-LD) out of a
// public Kununu profile page. Kununu rates 1-5, same scale as Glassdoor.
func parseKununu(body string) (reviews int, rating float64) {
	if m := kununuCountRe.FindStringSubmatch(body); m != nil {
		reviews, _ = strconv.Atoi(m[1])
	}
	if m := kununuRatingRe.FindStringSubmatch(body); m != nil {
		rating, _ = strconv.ParseFloat(m[1], 64)
	}
	return reviews, rating
}

// companySlug normalizes a (often German) company name into a URL slug:
// legal suffixes are stripped ("Müller GmbH & Co. KG" -> "Müller") and umlauts
// are transliterated (ü -> ue) instead of dropped, so "Müller" becomes
// "mueller", not "mller".
func companySlug(company string) string {
	s := strings.ToLower(strings.TrimSpace(company))
	s = legalSuffixRe.ReplaceAllString(s, "")
	replacer := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
	s = replacer.Replace(s)
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

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Chromedp is the production Browser backed by headless Chrome.
//
// NOTE: the login flow and CSS selectors below target LinkedIn/Glassdoor as of
// mid-2026 and are best-effort — these sites change frequently and actively
// detect automation. Tune the selector constants against the live DOM. Logging
// in with stored credentials also violates both platforms' Terms of Service; use
// at your own risk on accounts you control.
type Chromedp struct {
	headless  bool
	timeout   time.Duration // overall per-operation budget
	loginWait time.Duration // how long to wait for a terminal login state
}

// New returns a Chromedp browser. In headful mode (headless=false) a real
// Chromium window opens and the login wait is generous so the user can complete
// a checkpoint / 2FA / CAPTCHA by hand before we capture the session cookie.
func New(headless bool) *Chromedp {
	if headless {
		return &Chromedp{headless: true, timeout: 90 * time.Second, loginWait: 25 * time.Second}
	}
	return &Chromedp{headless: false, timeout: 8 * time.Minute, loginWait: 6 * time.Minute}
}

// storedCookie is our JSON-serializable cookie shape (decoupled from cdproto).
type storedCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
}

func (b *Chromedp) newContext(parent context.Context) (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", b.headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"),
	)
	// Running headless as root inside a container requires --no-sandbox and a
	// large /dev/shm workaround. CHROME_BIN pins the Chromium binary when the
	// default lookup would miss it (e.g. the Docker image).
	if b.headless {
		opts = append(opts, chromedp.NoSandbox, chromedp.Flag("disable-dev-shm-usage", true))
	}
	if bin := os.Getenv("CHROME_BIN"); bin != "" {
		opts = append(opts, chromedp.ExecPath(bin))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parent, opts...)
	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	return taskCtx, func() { cancelTask(); cancelAlloc() }
}

// ---- Login ----------------------------------------------------------------

type platformSpec struct {
	loginURL      string
	userSel       string
	passSel       string
	submitSel     string
	authCookie    string // presence of this cookie == authenticated (most reliable signal)
	loggedInProbe string // JS returning true when authenticated (fallback signal)
	errorProbe    string // JS returning true when an invalid-credentials error is shown
	challengeHint string // substring in URL indicating 2FA/checkpoint
	searchURL     func(q ScrapeQuery) string
	extractJS     string // JS returning JSON array of raw jobs
}

var specs = map[string]platformSpec{
	"linkedin": {
		loginURL:      "https://www.linkedin.com/login",
		userSel:       "#username",
		passSel:       "#password",
		submitSel:     "button[type=submit]",
		authCookie:    "li_at",
		loggedInProbe: `(function(){try{return !!document.querySelector('.global-nav, .scaffold-layout, .feed-identity-module, [data-control-name="nav.settings"]')||location.pathname.startsWith('/feed');}catch(e){return false;}})()`,
		errorProbe:    `(function(){try{var t=(document.body&&document.body.innerText)||'';return !!document.querySelector('#error-for-password, #error-for-username, .form__label--error, [role="alert"]')||/wrong|not the right password|couldn.?t find|isn.?t connected to/i.test(t);}catch(e){return false;}})()`,
		challengeHint: "checkpoint",
		searchURL:     linkedinSearchURL,
		extractJS:     linkedinExtractJS,
	},
	"glassdoor": {
		loginURL:      "https://www.glassdoor.com/profile/login_input.htm",
		userSel:       "#inlineUserEmail",
		passSel:       "#inlineUserPassword",
		submitSel:     "button[type=submit]",
		authCookie:    "gdId",
		loggedInProbe: `(function(){try{return !document.querySelector('#inlineUserPassword');}catch(e){return false;}})()`,
		errorProbe:    `(function(){try{return !!document.querySelector('[data-test="error"], .error, [role="alert"]');}catch(e){return false;}})()`,
		challengeHint: "verify",
		searchURL:     glassdoorSearchURL,
		extractJS:     glassdoorExtractJS,
	},
}

func (b *Chromedp) Login(ctx context.Context, platform, email, password string) (LoginResult, error) {
	spec, ok := specs[platform]
	if !ok {
		return LoginResult{}, fmt.Errorf("unknown platform %q", platform)
	}
	tctx, cancel := b.newContext(ctx)
	defer cancel()
	tctx, tcancel := context.WithTimeout(tctx, b.timeout)
	defer tcancel()

	// Navigate to the login page (this must succeed).
	if err := chromedp.Run(tctx, network.Enable(), chromedp.Navigate(spec.loginURL)); err != nil {
		return LoginResult{}, fmt.Errorf("navigate: %w", err)
	}

	// Best-effort auto-fill + submit with a short budget. In headful mode a
	// failure here is fine — the user can sign in by hand in the open window and
	// we still detect success via the auth cookie. In headless mode auto-fill is
	// the only way in, so a failure is fatal.
	fctx, fcancel := context.WithTimeout(tctx, 20*time.Second)
	fillErr := chromedp.Run(fctx,
		chromedp.WaitVisible(spec.userSel, chromedp.ByQuery),
		chromedp.SendKeys(spec.userSel, email, chromedp.ByQuery),
		chromedp.SendKeys(spec.passSel, password, chromedp.ByQuery),
		chromedp.Click(spec.submitSel, chromedp.ByQuery),
	)
	fcancel()
	if fillErr != nil {
		if b.headless {
			return LoginResult{}, fmt.Errorf("login submit: %w", fillErr)
		}
		log.Printf("[login] %s: auto-fill skipped (%v) — sign in manually in the open window", platform, fillErr)
	}

	// Poll for a terminal state: authentication (the auth cookie appears), an
	// explicit credential error, or (headless only) a challenge URL.
	if !b.headless {
		log.Printf("[login] %s: window is open — sign in and complete any checkpoint/2FA/CAPTCHA; waiting up to %s (do NOT close the window)", platform, b.loginWait)
	}

	var finalURL, title string
	var loggedIn, hasError bool
	deadline := time.Now().Add(b.loginWait)
	lastLog := time.Now()
	consecErr := 0
	for time.Now().Before(deadline) {
		// The auth cookie is the ground truth and this check is cheap + robust.
		if b.hasAuthCookie(tctx, spec.authCookie) {
			loggedIn = true
			break
		}
		err := chromedp.Run(tctx,
			chromedp.Sleep(1500*time.Millisecond),
			chromedp.Location(&finalURL),
			chromedp.Title(&title),
			chromedp.Evaluate(spec.loggedInProbe, &loggedIn),
			chromedp.Evaluate(spec.errorProbe, &hasError),
		)
		if err != nil {
			// A single error is usually a transient navigation race; only give up
			// after several in a row (the window was actually closed/crashed).
			consecErr++
			if consecErr >= 4 {
				return LoginResult{}, fmt.Errorf("login wait interrupted (window closed?): %w", err)
			}
			continue
		}
		consecErr = 0
		if loggedIn {
			break
		}
		if hasError {
			break
		}
		if b.headless && hasChallenge(finalURL) {
			break
		}
		if !b.headless && time.Since(lastLog) > 15*time.Second {
			log.Printf("[login] %s: still waiting for sign-in… (url=%s)", platform, finalURL)
			lastLog = time.Now()
		}
	}

	cookies, _ := b.dumpCookies(tctx)
	// The presence of the auth cookie is the ground truth for "logged in".
	if loggedIn || cookiesContain(cookies, spec.authCookie) {
		log.Printf("[login] %s: success url=%s", platform, finalURL)
		return LoginResult{Outcome: OutcomeActive, Cookies: cookies}, nil
	}

	// Not authenticated — capture what the page actually showed so the failure
	// is diagnosable (screenshot + HTML + logged reason).
	shot := b.debugCapture(tctx, platform)
	switch {
	case hasChallenge(finalURL):
		log.Printf("[login] %s: 2FA/checkpoint required — url=%s title=%q screenshot=%s", platform, finalURL, title, shot)
		return LoginResult{Outcome: OutcomeNeeds2FA}, nil
	case hasError:
		log.Printf("[login] %s: credentials rejected by the site — url=%s screenshot=%s", platform, finalURL, shot)
		return LoginResult{Outcome: OutcomeInvalidCreds}, nil
	default:
		// No explicit error and no login: almost always a bot/anti-automation
		// block on headless Chrome. The credentials may be perfectly valid, so
		// do NOT report them as invalid — surface it as needs_2fa/blocked.
		log.Printf("[login] %s: blocked or ambiguous (likely anti-bot check on headless Chrome; try HEADLESS=false) — url=%s title=%q screenshot=%s",
			platform, finalURL, title, shot)
		return LoginResult{Outcome: OutcomeNeeds2FA}, nil
	}
}

// hasAuthCookie reports whether the named cookie is currently set with a value.
func (b *Chromedp) hasAuthCookie(ctx context.Context, name string) bool {
	if name == "" {
		return false
	}
	found := false
	_ = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cs, err := network.GetCookies().Do(ctx)
		if err != nil {
			return err
		}
		for _, c := range cs {
			if c.Name == name && c.Value != "" {
				found = true
			}
		}
		return nil
	}))
	return found
}

func cookiesContain(raw json.RawMessage, name string) bool {
	if name == "" || len(raw) == 0 {
		return false
	}
	var cs []storedCookie
	if json.Unmarshal(raw, &cs) != nil {
		return false
	}
	for _, c := range cs {
		if c.Name == name && c.Value != "" {
			return true
		}
	}
	return false
}

// debugCapture saves a screenshot and the page HTML to DEBUG_DIR (default a
// temp dir) and returns the screenshot path. Best-effort; errors are ignored.
func (b *Chromedp) debugCapture(ctx context.Context, platform string) string {
	dir := os.Getenv("DEBUG_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "discovery-debug")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	base := filepath.Join(dir, fmt.Sprintf("%s-login-%s", platform, time.Now().Format("20060102-150405")))
	var png []byte
	var html string
	_ = chromedp.Run(ctx,
		chromedp.CaptureScreenshot(&png),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	if len(png) > 0 {
		_ = os.WriteFile(base+".png", png, 0o644)
	}
	if html != "" {
		_ = os.WriteFile(base+".html", []byte(html), 0o644)
	}
	return base + ".png"
}

func hasChallenge(u string) bool {
	for _, hint := range []string{"checkpoint", "challenge", "captcha", "verify", "two-step"} {
		if strings.Contains(u, hint) {
			return true
		}
	}
	return false
}

func (b *Chromedp) dumpCookies(ctx context.Context) (json.RawMessage, error) {
	var out []storedCookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cookies, err := network.GetCookies().Do(ctx)
		if err != nil {
			return err
		}
		for _, c := range cookies {
			out = append(out, storedCookie{
				Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
				Expires: c.Expires, HTTPOnly: c.HTTPOnly, Secure: c.Secure,
			})
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func setCookiesAction(raw json.RawMessage) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var cookies []storedCookie
		if err := json.Unmarshal(raw, &cookies); err != nil {
			return err
		}
		for _, c := range cookies {
			exp := cdp.TimeSinceEpoch(time.Unix(int64(c.Expires), 0))
			p := network.SetCookie(c.Name, c.Value).
				WithDomain(c.Domain).WithPath(c.Path).
				WithHTTPOnly(c.HTTPOnly).WithSecure(c.Secure)
			if c.Expires > 0 {
				p = p.WithExpires(&exp)
			}
			if err := p.Do(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- Session check --------------------------------------------------------

func (b *Chromedp) CheckSession(ctx context.Context, platform string, cookies json.RawMessage) (bool, error) {
	spec, ok := specs[platform]
	if !ok {
		return false, fmt.Errorf("unknown platform %q", platform)
	}
	tctx, cancel := b.newContext(ctx)
	defer cancel()
	tctx, tcancel := context.WithTimeout(tctx, b.timeout)
	defer tcancel()

	var loggedIn bool
	err := chromedp.Run(tctx,
		network.Enable(),
		setCookiesAction(cookies),
		chromedp.Navigate(spec.loginURL),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(spec.loggedInProbe, &loggedIn),
	)
	return loggedIn, err
}

// ---- Scrape ---------------------------------------------------------------

func (b *Chromedp) ScrapeRecent(ctx context.Context, platform string, cookies json.RawMessage, q ScrapeQuery) ([]ScrapedJob, error) {
	spec, ok := specs[platform]
	if !ok {
		return nil, fmt.Errorf("unknown platform %q", platform)
	}
	tctx, cancel := b.newContext(ctx)
	defer cancel()
	tctx, tcancel := context.WithTimeout(tctx, b.timeout+time.Duration(len(q.Keywords))*time.Minute)
	defer tcancel()

	var all []ScrapedJob
	seen := map[string]bool{}
	// One search per keyword keeps the query URLs simple and platform-friendly.
	keywords := q.Keywords
	if len(keywords) == 0 {
		keywords = []string{""}
	}
	for _, kw := range keywords {
		sub := q
		sub.Keywords = []string{kw}
		var raw string
		err := chromedp.Run(tctx,
			network.Enable(),
			setCookiesAction(cookies),
			chromedp.Navigate(spec.searchURL(sub)),
			chromedp.Sleep(4*time.Second),
			scrollToLoad(),
			chromedp.Evaluate(spec.extractJS, &raw),
		)
		if err != nil {
			return all, fmt.Errorf("scrape %q: %w", kw, err)
		}
		var jobs []rawJob
		if err := json.Unmarshal([]byte(raw), &jobs); err != nil {
			continue // tolerate a bad page, keep what we have
		}
		for _, rj := range jobs {
			if rj.ExternalJobID == "" || seen[rj.ExternalJobID] {
				continue
			}
			seen[rj.ExternalJobID] = true
			all = append(all, rj.toScraped())
		}
	}
	return all, nil
}

func scrollToLoad() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for i := 0; i < 5; i++ {
			_ = chromedp.Run(ctx,
				chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil),
				chromedp.Sleep(1500*time.Millisecond),
			)
		}
		return nil
	})
}

type rawJob struct {
	ExternalJobID string `json:"externalJobId"`
	Title         string `json:"title"`
	Company       string `json:"company"`
	ApplyURL      string `json:"applyUrl"`
	Location      string `json:"location"`
	Salary        string `json:"salary"`
	PostedISO     string `json:"postedAt"`
}

func (rj rawJob) toScraped() ScrapedJob {
	posted := time.Now()
	if rj.PostedISO != "" {
		if t, err := time.Parse(time.RFC3339, rj.PostedISO); err == nil {
			posted = t
		}
	}
	raw, _ := json.Marshal(rj)
	return ScrapedJob{
		ExternalJobID: rj.ExternalJobID,
		Title:         rj.Title,
		Company:       rj.Company,
		ApplyURL:      rj.ApplyURL,
		Location:      rj.Location,
		Salary:        rj.Salary,
		PostedAt:      posted,
		Raw:           raw,
	}
}

// ---- Platform URL + extraction JS ----------------------------------------

func linkedinSearchURL(q ScrapeQuery) string {
	v := url.Values{}
	if len(q.Keywords) > 0 {
		v.Set("keywords", q.Keywords[0])
	}
	if len(q.Locations) > 0 {
		v.Set("location", q.Locations[0])
	}
	v.Set("f_TPR", "r86400") // past 24 hours
	if q.RemoteOnly {
		v.Set("f_WT", "2")
	}
	return "https://www.linkedin.com/jobs/search/?" + v.Encode()
}

func glassdoorSearchURL(q ScrapeQuery) string {
	v := url.Values{}
	if len(q.Keywords) > 0 {
		v.Set("sc.keyword", q.Keywords[0])
	}
	v.Set("fromAge", "1") // last day
	if len(q.Locations) > 0 {
		v.Set("locKeyword", q.Locations[0])
	}
	return "https://www.glassdoor.com/Job/jobs.htm?" + v.Encode()
}

// linkedinExtractJS walks visible job cards and returns a JSON array. Returns a
// JSON string (chromedp Evaluate marshals the JS return value).
const linkedinExtractJS = `JSON.stringify(Array.from(document.querySelectorAll('div.job-card-container, li.jobs-search-results__list-item, div[data-job-id]')).map(function(el){
  var id = el.getAttribute('data-job-id') || (el.querySelector('a[data-job-id]') && el.querySelector('a[data-job-id]').getAttribute('data-job-id')) || '';
  var a = el.querySelector('a.job-card-container__link, a.job-card-list__title, a[href*="/jobs/view/"]');
  var href = a ? a.href : '';
  if (!id && href) { var m = href.match(/\/jobs\/view\/(\d+)/); if (m) id = m[1]; }
  var title = a ? a.innerText.trim().split('\n')[0] : '';
  var company = (el.querySelector('.job-card-container__primary-description, .artdeco-entity-lockup__subtitle, .job-card-container__company-name') || {}).innerText || '';
  var loc = (el.querySelector('.job-card-container__metadata-item, .artdeco-entity-lockup__caption') || {}).innerText || '';
  var time = el.querySelector('time');
  return { externalJobId: 'li_' + id, title: title, company: company.trim(), applyUrl: href.split('?')[0], location: loc.trim(), salary: '', postedAt: time ? (time.getAttribute('datetime') || '') : '' };
}).filter(function(j){ return j.title && j.applyUrl; }))`

const glassdoorExtractJS = `JSON.stringify(Array.from(document.querySelectorAll('li[data-test="jobListing"], article[data-test="job-card"]')).map(function(el){
  var a = el.querySelector('a[data-test="job-link"], a[href*="/job-listing/"], a[href*="partner/jobListing"]');
  var href = a ? a.href : '';
  var id = el.getAttribute('data-id') || el.getAttribute('data-jobid') || (href.match(/jobListingId=(\d+)/) ? href.match(/jobListingId=(\d+)/)[1] : '');
  var title = (el.querySelector('[data-test="job-title"]') || a || {}).innerText || '';
  var company = (el.querySelector('[data-test="employer-short-name"], .EmployerProfile_compactEmployerName__9MGcV') || {}).innerText || '';
  var loc = (el.querySelector('[data-test="emp-location"]') || {}).innerText || '';
  var salary = (el.querySelector('[data-test="detailSalary"]') || {}).innerText || '';
  return { externalJobId: 'gd_' + id, title: (title||'').trim(), company: company.trim(), applyUrl: href.split('?')[0], location: loc.trim(), salary: salary.trim(), postedAt: '' };
}).filter(function(j){ return j.title && j.applyUrl; }))`

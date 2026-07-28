package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	// Chrome refuses to run as root without --no-sandbox, headless or not, and the
	// container's default 64MB /dev/shm is too small either way. Keyed on the uid
	// rather than on headless, so the headful sign-in window works in Docker too.
	if os.Geteuid() == 0 {
		opts = append(opts, chromedp.NoSandbox, chromedp.Flag("disable-dev-shm-usage", true))
	}
	// CHROME_BIN pins the Chromium binary when the default lookup would miss it.
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
	public        bool   // no login required; ScrapeRecent works without cookies
}

// Public reports whether the platform can be scraped without a login session.
func Public(platform string) bool {
	return specs[platform].public
}

// NeedsLogin reports whether the platform is a known login platform (has a spec
// and is not public), i.e. JD fetching should attach a session.
func NeedsLogin(platform string) bool {
	s, ok := specs[platform]
	return ok && !s.public
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
	"xing": {
		loginURL:      "https://login.xing.com/",
		userSel:       "input[name=username]",
		passSel:       "input[name=password]",
		submitSel:     "button[type=submit]",
		authCookie:    "login",
		loggedInProbe: `(function(){try{return !document.querySelector('input[name=password]')&&!location.host.startsWith('login.');}catch(e){return false;}})()`,
		errorProbe:    `(function(){try{return !!document.querySelector('[role="alert"], [data-qa="error"]');}catch(e){return false;}})()`,
		challengeHint: "two-factor",
		searchURL:     xingSearchURL,
		extractJS:     xingExtractJS,
	},
	// Openly searchable German portals — no account needed.
	"stepstone": {
		public:    true,
		searchURL: stepstoneSearchURL,
		extractJS: stepstoneExtractJS,
	},
	"indeed": {
		public:    true,
		searchURL: indeedDeSearchURL,
		extractJS: indeedExtractJS,
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

	// A sign-in is driven by the user in a real window, so a headless browser has
	// no way to complete it. Fail early with the actual fix rather than timing out.
	if b.headless && (email == "" || password == "") {
		return LoginResult{}, fmt.Errorf(
			"cannot sign in to %s headlessly: signing in opens a browser window for you to "+
				"log in, which needs HEADLESS=false and a display", platform)
	}

	// Navigate to the login page (this must succeed).
	if err := chromedp.Run(tctx, network.Enable(), chromedp.Navigate(spec.loginURL)); err != nil {
		return LoginResult{}, explainDisplayError(err)
	}

	// Prefill from PROFILE_<N>_* env vars when they happen to be set, purely as a
	// convenience — the user still completes the sign-in in the window. With no
	// credentials we leave the form untouched and simply wait for them.
	if email != "" && password != "" {
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
	} else {
		log.Printf("[login] %s: no stored credentials (by design) — sign in in the open window", platform)
	}

	// Poll for a terminal state: authentication (the auth cookie appears), an
	// explicit credential error, or (headless only) a challenge URL.
	if !b.headless {
		log.Printf("[login] %s: window is open at %s — sign in there and complete any checkpoint/2FA/CAPTCHA; waiting up to %s",
			platform, ViewerURL(), b.loginWait)
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

// ---- JD fetch -------------------------------------------------------------

// FetchJD navigates to a job URL (optionally authenticated) and returns just the
// job-description text, collapsed to single spaces. It targets the platform's
// description container so nav/header/footer chrome ("apply", "skip", "premium",
// …) never pollutes the ATS keywords; if no container matches it falls back to
// the body with nav/header/footer/aside stripped. Best-effort: an error yields
// empty text so ATS matching can skip the job rather than fail the run.
func (b *Chromedp) FetchJD(ctx context.Context, platform string, cookies json.RawMessage, url string) (string, error) {
	tctx, cancel := b.newContext(ctx)
	defer cancel()
	tctx, tcancel := context.WithTimeout(tctx, b.timeout)
	defer tcancel()

	actions := []chromedp.Action{network.Enable()}
	if len(cookies) > 0 {
		actions = append(actions, setCookiesAction(cookies))
	}
	var text string
	actions = append(actions,
		chromedp.Navigate(url),
		chromedp.Sleep(3*time.Second),
		chromedp.Evaluate(jdExtractJS(platform), &text),
	)
	if err := chromedp.Run(tctx, actions...); err != nil {
		return "", err
	}
	return strings.Join(strings.Fields(text), " "), nil
}

// jdDescSelectors maps a platform to the CSS selectors that wrap the actual job
// description (tried in order, first match wins).
var jdDescSelectors = map[string][]string{
	"linkedin":       {".jobs-description__content", ".jobs-box__html-content", "#job-details", ".show-more-less-html__markup", ".description__text"},
	"glassdoor":      {".JobDetails_jobDescription__uW_fK", "#JobDescriptionContainer", "[data-test='jobDescriptionContent']"},
	"xing":           {"[data-testid='expandable-content']", "[data-testid='job-description']", ".job-description"},
	"stepstone":      {"[data-at='job-ad-content']", ".js-app-ld-ContentBlock", ".listing-content"},
	"indeed":         {"#jobDescriptionText", ".jobsearch-JobComponent-description"},
	"arbeitsagentur": {"#jobdetails-beschreibung", "jb-jobdetail-beschreibung", "[id*='beschreibung']"},
}

// jdExtractJS builds JS that returns the description container's innerText, or —
// when none of the platform selectors match — the body innerText with structural
// chrome (nav/header/footer/aside/script/style/button) removed.
func jdExtractJS(platform string) string {
	sels, _ := json.Marshal(jdDescSelectors[platform])
	return `(function(){
  try {
    var sels = ` + string(sels) + ` || [];
    for (var i = 0; i < sels.length; i++) {
      var el = document.querySelector(sels[i]);
      if (el && el.innerText && el.innerText.trim().length > 100) return el.innerText;
    }
    var main = document.querySelector('main, article, [role=main]') || document.body;
    if (!main) return '';
    var clone = main.cloneNode(true);
    clone.querySelectorAll('nav, header, footer, aside, script, style, button, form, [role=navigation]').forEach(function(n){ n.remove(); });
    return clone.innerText || '';
  } catch (e) { return (document.body && document.body.innerText) || ''; }
})()`
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
		actions := []chromedp.Action{network.Enable()}
		if len(cookies) > 0 { // public platforms scrape without a session
			actions = append(actions, setCookiesAction(cookies))
		}
		actions = append(actions,
			chromedp.Navigate(spec.searchURL(sub)),
			chromedp.Sleep(4*time.Second),
			scrollToLoad(),
			chromedp.Evaluate(spec.extractJS, &raw),
		)
		err := chromedp.Run(tctx, actions...)
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
	// No date from the portal = unknown (zero), never "now" — the Posted column
	// must reflect when the portal published the job, not when we scraped it.
	var posted time.Time
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if rj.PostedISO == "" {
			break
		}
		if t, err := time.Parse(layout, rj.PostedISO); err == nil {
			posted = t
			break
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

func xingSearchURL(q ScrapeQuery) string {
	v := url.Values{}
	if len(q.Keywords) > 0 {
		v.Set("keywords", q.Keywords[0])
	}
	if len(q.Locations) > 0 {
		v.Set("location", q.Locations[0])
	}
	v.Set("sort", "date")
	if q.RemoteOnly {
		v.Set("remoteOption", "FULL_REMOTE.ffce20")
	}
	return "https://www.xing.com/jobs/search?" + v.Encode()
}

func stepstoneSearchURL(q ScrapeQuery) string {
	kw := "jobs"
	if len(q.Keywords) > 0 && q.Keywords[0] != "" {
		kw = strings.ReplaceAll(strings.TrimSpace(q.Keywords[0]), " ", "-")
	}
	u := "https://www.stepstone.de/jobs/" + url.PathEscape(kw)
	if len(q.Locations) > 0 && q.Locations[0] != "" {
		loc := strings.ReplaceAll(strings.TrimSpace(q.Locations[0]), " ", "-")
		u += "/in-" + url.PathEscape(loc)
	}
	days := q.SinceHours / 24
	if days < 1 {
		days = 1
	}
	v := url.Values{}
	v.Set("ag", "age_"+strconv.Itoa(days))
	if q.RemoteOnly {
		v.Set("wfh", "1")
	}
	return u + "?" + v.Encode()
}

func indeedDeSearchURL(q ScrapeQuery) string {
	v := url.Values{}
	if len(q.Keywords) > 0 {
		v.Set("q", q.Keywords[0])
	}
	if len(q.Locations) > 0 {
		v.Set("l", q.Locations[0])
	}
	days := q.SinceHours / 24
	if days < 1 {
		days = 1
	}
	v.Set("fromage", strconv.Itoa(days))
	v.Set("sort", "date")
	return "https://de.indeed.com/jobs?" + v.Encode()
}

// explainDisplayError rewrites Chrome's X11 startup noise into the fix. Chrome
// prints several lines here; the actionable one is almost always that the X
// server refused the container's connection.
func explainDisplayError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Authorization required"),
		strings.Contains(msg, "authorization protocol"):
		return fmt.Errorf(
			"the X server refused the connection, so the sign-in window could not open. "+
				"The container needs your X cookie: run  ./scripts/allow-x11.sh  on the host, "+
				"then  docker compose up -d --force-recreate backend. "+
				"(xhost alone is often not enough — the container's hostname differs from the "+
				"host's, so the cookie has to be rewritten to the wildcard family, which that "+
				"script does.) Original error: %w", err)
	case strings.Contains(msg, "Missing X server"),
		strings.Contains(msg, "$DISPLAY"),
		strings.Contains(msg, "failed to initialize"):
		return fmt.Errorf(
			"no usable X display, so the sign-in window could not open. Check that DISPLAY is "+
				"set on the host, /tmp/.X11-unix is mounted into the container, and you ran "+
				"`xhost +local:root`. Original error: %w", err)
	}
	return fmt.Errorf("navigate: %w", err)
}

// x11SocketDir is where local X displays expose their unix sockets. A variable
// so tests can point it at a temp dir instead of depending on the real host.
var x11SocketDir = "/tmp/.X11-unix"

// DisplayInfo summarises which X display will be used and which ones are
// actually reachable. Logged at startup because "Chromium runs but I see no
// window" is almost always the container drawing on a different display than
// the monitor — invisible unless the two are printed side by side.
func DisplayInfo() string {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = "(unset)"
	}
	entries, err := os.ReadDir(x11SocketDir)
	if err != nil {
		return fmt.Sprintf("DISPLAY=%s, but %s is not readable (is it mounted?)", display, x11SocketDir)
	}
	var found []string
	for _, e := range entries {
		if name := e.Name(); strings.HasPrefix(name, "X") {
			found = append(found, ":"+strings.TrimPrefix(name, "X"))
		}
	}
	sort.Strings(found)
	if len(found) == 0 {
		return fmt.Sprintf("DISPLAY=%s, but no X displays are visible in %s", display, x11SocketDir)
	}
	msg := fmt.Sprintf("DISPLAY=%s; displays reachable from this container: %s",
		display, strings.Join(found, " "))
	if len(found) > 1 {
		msg += " — if the sign-in window does not appear on your screen, the wrong one is selected;" +
			" set DISPLAY to the one your monitor uses and recreate the container"
	}
	return msg
}

// CheckDisplay reports a human-readable problem with the X setup, or "" when it
// looks usable. Called at startup in headful mode so a misconfigured display is
// visible in the logs immediately rather than on the first Sign in click.
func CheckDisplay() string {
	display := os.Getenv("DISPLAY")
	if display == "" {
		return "DISPLAY is not set, so the sign-in window cannot open. In Docker, pass " +
			"DISPLAY through and mount /tmp/.X11-unix (see docker-compose.yml)."
	}
	// A local display (":0", "host:0") is served by a unix socket under
	// /tmp/.X11-unix; a remote one (TCP) is not, so only check the local case.
	host, num, found := strings.Cut(display, ":")
	if !found || (host != "" && host != "localhost" && host != "unix") {
		return ""
	}
	screen, _, _ := strings.Cut(num, ".")
	sock := filepath.Join(x11SocketDir, "X"+screen)
	if _, err := os.Stat(sock); err != nil {
		return fmt.Sprintf("DISPLAY=%s but %s is missing, so the sign-in window cannot open. "+
			"In Docker, mount /tmp/.X11-unix into the container (see docker-compose.yml).", display, sock)
	}
	// The X server will reject us without a cookie. A missing/empty XAUTHORITY —
	// or the directory docker silently creates for an absent bind-mount source —
	// is the usual cause, and it fails only when the user first clicks Sign in.
	if xa := os.Getenv("XAUTHORITY"); xa != "" {
		switch st, err := os.Stat(xa); {
		case err != nil:
			return fmt.Sprintf("XAUTHORITY=%s does not exist, so the X server will refuse the "+
				"connection. Run ./scripts/allow-x11.sh on the host, then recreate this container.", xa)
		case st.IsDir():
			return fmt.Sprintf("XAUTHORITY=%s is a directory (docker created it because the file "+
				"was missing), so the X server will refuse the connection. Run "+
				"./scripts/allow-x11.sh on the host, then recreate this container.", xa)
		case st.Size() == 0:
			return fmt.Sprintf("XAUTHORITY=%s is empty, so the X server will refuse the "+
				"connection. Run ./scripts/allow-x11.sh on the host, then recreate this container.", xa)
		}
	}
	return ""
}

// ViewerURL is where the user watches and drives the sign-in window. The
// container runs its own X server and exports it over noVNC, so this is a page
// in their normal browser rather than a window on their desktop.
func ViewerURL() string {
	if u := os.Getenv("SIGNIN_VIEWER_URL"); u != "" {
		return u
	}
	port := os.Getenv("NOVNC_PORT")
	if port == "" {
		port = "7900"
	}
	return "http://localhost:" + port + "/vnc.html"
}

// scaffold:inject

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

const xingExtractJS = `JSON.stringify(Array.from(document.querySelectorAll('article[data-testid="job-search-result"], li[data-testid="search-result"] article, a[href*="/jobs/"][data-testid]')).map(function(el){
  var a = el.tagName === 'A' ? el : el.querySelector('a[href*="/jobs/"]');
  var href = a ? a.href : '';
  var m = href.match(/jobs\/[^?]*?(\d{6,})/);
  var id = m ? m[1] : href.split('?')[0].split('/').pop();
  var title = (el.querySelector('h2, h3, [data-testid="job-title"]') || {}).innerText || '';
  var company = (el.querySelector('[data-testid="company-name"], [class*="company"]') || {}).innerText || '';
  var loc = (el.querySelector('[data-testid="job-location"], [class*="location"]') || {}).innerText || '';
  var salary = (el.querySelector('[data-testid="job-salary"]') || {}).innerText || '';
  var t = el.querySelector('time[datetime]');
  var posted = t ? (t.getAttribute('datetime') || '') : '';
  return { externalJobId: 'xi_' + id, title: title.trim(), company: company.trim(), applyUrl: href.split('?')[0], location: loc.trim(), salary: salary.trim(), postedAt: posted };
}).filter(function(j){ return j.title && j.applyUrl; }))`

const stepstoneExtractJS = `JSON.stringify(Array.from(document.querySelectorAll('article[data-at="job-item"], article[data-testid="job-item"]')).map(function(el){
  var a = el.querySelector('a[data-at="job-item-title"], a[href*="/stellenangebote--"]');
  var href = a ? a.href : '';
  var m = href.match(/--(\d+)-inline\.html/) || href.match(/--(\d+)\b/);
  var id = m ? m[1] : '';
  var title = a ? a.innerText.trim() : '';
  var company = (el.querySelector('[data-at="job-item-company-name"]') || {}).innerText || '';
  var loc = (el.querySelector('[data-at="job-item-location"]') || {}).innerText || '';
  var salary = (el.querySelector('[data-at="job-item-salary-info"]') || {}).innerText || '';
  var t = el.querySelector('time[datetime], [data-at="job-item-timestamp"] time');
  var posted = t ? (t.getAttribute('datetime') || '') : '';
  return { externalJobId: 'st_' + id, title: title, company: company.trim(), applyUrl: href.split('?')[0], location: loc.trim(), salary: salary.trim(), postedAt: posted };
}).filter(function(j){ return j.title && j.applyUrl && j.externalJobId !== 'st_'; }))`

const indeedExtractJS = `JSON.stringify(Array.from(document.querySelectorAll('div.job_seen_beacon, li div[class*="cardOutline"]')).map(function(el){
  var a = el.querySelector('a[data-jk], h2.jobTitle a, a[href*="/rc/clk"], a[href*="/viewjob"]');
  var id = a ? (a.getAttribute('data-jk') || ((a.href.match(/jk=([0-9a-f]+)/)||[])[1] || '')) : '';
  var title = (el.querySelector('h2.jobTitle span[title], h2.jobTitle') || a || {}).innerText || '';
  var company = (el.querySelector('[data-testid="company-name"]') || {}).innerText || '';
  var loc = (el.querySelector('[data-testid="text-location"]') || {}).innerText || '';
  var salary = (el.querySelector('[data-testid="attribute_snippet_testid"], .salary-snippet-container') || {}).innerText || '';
  var dtxt = ((el.querySelector('[data-testid="myJobsStateDate"], .date, span.css-qvloho') || {}).innerText || '');
  var posted = '';
  var rel = dtxt.match(/vor\s+(\d+)\s+Tag/i) || dtxt.match(/(\d+)\s+days?\s+ago/i);
  if (rel) { posted = new Date(Date.now() - parseInt(rel[1], 10) * 864e5).toISOString(); }
  else if (/heute|gerade|today|just posted/i.test(dtxt)) { posted = new Date().toISOString(); }
  return { externalJobId: 'in_' + id, title: title.trim(), company: company.trim(), applyUrl: id ? 'https://de.indeed.com/viewjob?jk=' + id : '', location: loc.trim(), salary: salary.trim(), postedAt: posted };
}).filter(function(j){ return j.title && j.applyUrl; }))`

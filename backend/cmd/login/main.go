// Command login opens a REAL (visible) Chromium window so you can log in to
// LinkedIn/Glassdoor by hand — solving any checkpoint, 2FA, or CAPTCHA — and
// saves the resulting session cookies to Postgres. The headless server/discovery
// then reuses that session (cookie reuse is rarely bot-blocked).
//
// Usage (from the backend/ directory, with the Postgres container running):
//
//	go run ./cmd/login --profile profile-1 --platform linkedin
//
// Credentials and DB config are read from backend/.env by default.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/database"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
	"github.com/mohan/linkedin-apply-backend/internal/service"
)

func main() {
	profile := flag.String("profile", "profile-1", "profile id to log in")
	platform := flag.String("platform", "linkedin", "platform: linkedin | glassdoor")
	envFile := flag.String("env-file", ".env", "env file with credentials + DATABASE_URL")
	flag.Parse()

	loadEnvFile(*envFile)
	pickChromeBinary()

	ctx := context.Background()
	dsn := env("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/linkedin_apply?sslmode=disable")
	db, err := database.Open(ctx, dsn)
	if err != nil {
		log.Fatalf("db: %v (is the Postgres container up? `docker compose up -d db`)", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	profileRepo := repository.NewProfileRepo(db)
	sessionRepo := repository.NewSessionRepo(db)
	profileSvc := service.NewProfileService(profileRepo, env("DATA_DIR", ".."))
	// Sync profiles from env + data_profileN.json so the profile row exists (the
	// session FK needs it) and credentials are available.
	if _, err := profileSvc.LoadProfiles(ctx); err != nil {
		log.Fatalf("load profiles: %v", err)
	}

	br := browser.New(false) // headful: a real window
	auth := service.NewAuthSessionService(profileSvc, sessionRepo, br)

	log.Printf("opening a real Chromium window for %s / %s …", *profile, *platform)
	sess, err := auth.Login(ctx, *profile, *platform)
	switch {
	case err == nil:
		log.Printf("✅ logged in — session saved (status=%s). The headless stack will now reuse it.", sess.Status)
	case errors.Is(err, service.ErrNeeds2FA):
		log.Printf("⚠️  still not authenticated (status=needs_2fa). If a challenge was shown, finish it and re-run. Check debug/ for a screenshot.")
		os.Exit(1)
	case errors.Is(err, service.ErrInvalidCreds):
		log.Printf("❌ the site rejected the credentials. Fix %s and retry.", *envFile)
		os.Exit(1)
	default:
		log.Fatalf("login error: %v", err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// loadEnvFile loads KEY=VALUE lines into the process env (without overwriting
// values already set). Missing file is ignored.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
}

// pickChromeBinary prefers a real (non-snap) Chrome/Chromium for chromedp when
// CHROME_BIN is not already set, since the snap wrapper can misbehave under
// automation.
func pickChromeBinary() {
	if os.Getenv("CHROME_BIN") != "" {
		return
	}
	for _, p := range []string{"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			_ = os.Setenv("CHROME_BIN", p)
			return
		}
	}
}

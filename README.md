# Job Discovery with Company Verification

A **discovery-only** job aggregator. It logs in with your own LinkedIn/Glassdoor
credentials (Selenium-style, via headless Chrome), gathers every open position
posted in the **last 24 hours**, runs a **ghost-job company check** on each, and
gives you a curated, ranked **shortlist with direct apply links**.

**It never applies for you.** Every shortlist row is a link you open and submit
yourself. Status (`new` / `saved` / `dismissed` / `applied`) is manual bookkeeping.

> ⚠️ Automating logins to LinkedIn/Glassdoor with stored credentials violates
> their Terms of Service and can get accounts restricted. Use only on accounts
> you control, at your own risk. The scraper selectors are best-effort and will
> need tuning as the sites change.

## Architecture

```
backend/  Go (gin, chromedp, database/sql + Postgres)
  internal/models        domain structs
  internal/database      connection + schema migration
  internal/repository    Postgres repos (scaffold-generated skeletons)
  internal/browser       Browser port + chromedp login/scrape impl
  internal/service       profile, auth-session, scraper, verification, discovery
  internal/handler       gin HTTP handlers
  cmd/main.go            wiring + router
  sql/schema.sql         DDL (embedded)
frontend/ Next.js (App Router, TypeScript, TanStack Query) cockpit
```

Flow: **login → scrape last 24h → verify companies → upsert shortlist**. Ghost
jobs are *flagged, not dropped* (hidden behind an "include ghost" toggle).

## Try it with Docker Compose

Starts Postgres, the Go backend (headless Chromium baked in), and the Next.js
cockpit.

**1. Configure credentials**

```bash
cp backend/.env.example backend/.env    # set PROFILE_1_LINKEDIN_EMAIL / _PASSWORD
```

**2. Start the stack**

```bash
docker compose up --build -d
```

Services: `db` (5433 on host, 5432 in-network), `backend` (8080), `frontend` (3000). `data_profileN.json`
at the repo root is mounted read-only at `/data`.

**3. Log in once (real browser, on your host)**

The containers are **headless**, and LinkedIn blocks headless logins even with
valid credentials. So do the first sign-in in a real browser window — it saves
the session to the same Postgres, and the containers reuse it:

```bash
./scripts/login.sh profile-1 linkedin
```

A Chrome/Chromium window opens; sign in fully (solve any checkpoint/2FA) and
**don't close it** — it closes itself once the session is saved. (Requires Go +
a desktop session on the host. Login failures drop a screenshot in `debug/`.)

**4. Use the cockpit**

Open http://localhost:3000 → pick the profile → **Gather open positions (last
24h)** → open the apply links. Applying is manual, by design.

Stop with `docker compose down` (add `-v` to also drop the Postgres volume).

> Why the split? A visible browser can't run inside the container (no display),
> and headless LinkedIn logins get bot-blocked. The one-time host login is the
> reliable way to seed a session the headless stack can reuse.

## Run the backend (without Docker)

Requires Go 1.26+, Postgres, and Chrome/Chromium (for real scraping).

```bash
cd backend
cp .env.example .env    # fill in PROFILE_1_* credentials + DATABASE_URL
set -a; . ./.env; set +a
go run ./cmd            # migrates the schema, listens on :8080
```

Profiles are discovered from `PROFILE_<N>_LINKEDIN_EMAIL/PASSWORD` (and
`_GLASSDOOR_`) env vars; search preferences come from `data_profile<N>.json`.

## Run the frontend

```bash
cd frontend
npm install
npm run dev            # proxies /api/* to http://localhost:8080
```

Open http://localhost:3000, pick a profile (this logs in), click **Gather open
positions (last 24h)**, then open the apply links in the shortlist.

## API

| Method | Path | Purpose |
|--------|------|---------|
| GET    | `/api/profiles` | list profiles (env + JSON) |
| POST   | `/api/profiles/:id/login` | log in; `{status: active\|invalid_creds\|needs_2fa}` |
| POST   | `/api/discovery/run` | start a background run `{profileId, platforms, sinceHours}` |
| GET    | `/api/discovery/:runId/status` | run progress `{phase, found, verified, shortlisted, ghost}` |
| GET    | `/api/shortlist` | curated jobs `?profileId&status&includeGhost&minScore` |
| PATCH  | `/api/shortlist/:id` | set manual status `{status}` |
| GET    | `/api/shortlist/stats/:profileId` | per-status counts |

There is deliberately **no `/api/apply`** route.

## Test

```bash
cd backend
go test ./...                                  # unit tests (no DB needed)
DATABASE_URL=postgres://... go test ./...      # + repository integration tests
```

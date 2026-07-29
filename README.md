# Job Discovery with Company Verification

A **discovery-only** job aggregator. You sign in to LinkedIn/Glassdoor/Xing once
in a real browser window (no passwords are stored), it gathers every open position
posted in the **last 24 hours**, runs a **ghost-job company check** on each, and
gives you a curated, ranked **shortlist with direct apply links**.

**It never applies for you.** Every shortlist row is a link you open and submit
yourself. Status (`new` / `saved` / `dismissed` / `applied`) is manual bookkeeping.

> ⚠️ Automated scraping of LinkedIn/Glassdoor violates their Terms of Service and
> can get accounts restricted. Use only on accounts you control, at your own
> risk. The scraper selectors are best-effort and will need tuning as the sites
> change.

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

**1. Configure (optional)**

```bash
cp backend/.env.example backend/.env    # optional: seeds a starter profile
```

Nothing else to set up — the sign-in window runs inside the container.

**2. Start the stack**

```bash
docker compose up --build -d
```

Services: `db` (5433 on host, 5432 in-network), `backend` (8080 API + 7900
sign-in window), `frontend` (3000). `data_profileN.json` at the repo root is
mounted read-only at `/data`.

**3. Connect a portal (sign in once)**

Open http://localhost:3000 → **Profiles & portal sessions** → **Add profile** →
**Sign in** next to LinkedIn (or Glassdoor/Xing).

A browser tab opens on **http://localhost:7900/vnc.html** showing a real Chromium
window running inside the container. Log in there — including any
checkpoint/2FA/CAPTCHA. The session is saved to Postgres and the headless
scraping reuses it until it expires (7 days); the cockpit shows the time left.

**No passwords are ever stored.** Only the session cookies are kept.

Why a window inside the container? A visible browser needs a display, and
borrowing the host's X server means cookie/`xhost` wrangling that breaks on
Wayland, macOS, and remote hosts. The container brings its own display and
streams it to your browser, so this works the same everywhere. Port 7900 is
bound to `127.0.0.1` on purpose: it is an unauthenticated view of a browser you
are logging in to, so it must not be reachable from the network.

If you would rather use the host's X server, set `USE_HOST_DISPLAY=true` and
pass `DISPLAY`/`XAUTHORITY` in (run `./scripts/allow-x11.sh` first). The
host-side script still works too:

```bash
./scripts/login.sh profile-1 linkedin
```

**4. Use the cockpit**

Open http://localhost:3000 → pick the profile → **Gather open positions (last
24h)** → open the apply links. Applying is manual, by design.

Stop with `docker compose down` (add `-v` to also drop the Postgres volume).

> Why sign in by hand? LinkedIn bot-blocks headless logins even with valid
> credentials, and 2FA/CAPTCHA needs a human anyway. Signing in yourself once
> sidesteps both — and means the app never has to hold your password.

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
| POST   | `/api/profiles` | create a profile `{name}` |
| PUT    | `/api/profiles/:id` | rename a profile `{name}` |
| DELETE | `/api/profiles/:id` | delete a profile and its sessions/shortlist |
| POST   | `/api/profiles/:id/signin` | open the sign-in window; stores the session |
| GET    | `/api/signin-viewer` | where to watch/complete the sign-in |
| POST   | `/api/profiles/:id/login` | session status; `{status: active\|expired\|needs_2fa\|none}` |
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

#!/usr/bin/env bash
# One-time interactive login. Opens a REAL browser window on your host so you can
# sign in (and solve any checkpoint/2FA/CAPTCHA) once; the session is saved to the
# Postgres started by docker compose, and the headless containers then reuse it.
#
# Usage:  ./scripts/login.sh [profile-id] [platform]
#         ./scripts/login.sh profile-1 linkedin
#
# Prereqs: `docker compose up -d db` (Postgres reachable on localhost:5433),
# Go installed on the host, a desktop session (DISPLAY), and credentials in
# backend/.env.
set -euo pipefail

cd "$(dirname "$0")/.."
export PATH="$HOME/.nix-profile/bin:$PATH"

: "${DISPLAY:=:0}"; export DISPLAY
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:5433/linkedin_apply?sslmode=disable}"
export DATA_DIR="${DATA_DIR:-$PWD}"

PROFILE="${1:-profile-1}"
PLATFORM="${2:-linkedin}"

echo "→ A browser window will open. Sign in fully (do NOT close it); it saves the"
echo "  session and closes itself on success. Waiting up to ~6 minutes."
cd backend
exec go run ./cmd/login --profile "$PROFILE" --platform "$PLATFORM" --env-file .env

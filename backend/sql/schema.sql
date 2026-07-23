-- Schema for the Job Discovery engine. Discovery + verification only; no applications table.

CREATE TABLE IF NOT EXISTS profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  linkedin_email TEXT,
  glassdoor_email TEXT,
  profile_data_path TEXT,
  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  external_job_id TEXT UNIQUE,
  title TEXT NOT NULL,
  company TEXT NOT NULL,
  apply_url TEXT NOT NULL,
  platform TEXT NOT NULL,
  posted_at TIMESTAMPTZ,
  first_seen_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  location TEXT,
  salary TEXT,
  raw_data JSONB,
  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_jobs_platform ON jobs(platform);
CREATE INDEX IF NOT EXISTS idx_jobs_company ON jobs(company);
CREATE INDEX IF NOT EXISTS idx_jobs_posted_at ON jobs(posted_at);

CREATE TABLE IF NOT EXISTS company_verifications (
  id TEXT PRIMARY KEY,
  company TEXT UNIQUE NOT NULL,
  is_ghost_job BOOLEAN NOT NULL,
  score INTEGER NOT NULL,
  verification_methods JSONB,
  details JSONB,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_company_verifications_company ON company_verifications(company);

CREATE TABLE IF NOT EXISTS discovery_runs (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  started_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_discovery_runs_profile_started ON discovery_runs(profile_id, started_at DESC);

CREATE TABLE IF NOT EXISTS shortlist (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  job_id TEXT NOT NULL REFERENCES jobs(id),
  verification_id TEXT REFERENCES company_verifications(id),
  discovery_run_id TEXT REFERENCES discovery_runs(id),
  score INTEGER NOT NULL DEFAULT 0,
  is_ghost BOOLEAN NOT NULL DEFAULT FALSE,
  apply_url TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'new', -- new | saved | dismissed | applied
  discovered_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
-- Databases created before discovery_runs existed keep their old shortlist table,
-- since CREATE TABLE IF NOT EXISTS above is a no-op for them.
ALTER TABLE shortlist ADD COLUMN IF NOT EXISTS discovery_run_id TEXT REFERENCES discovery_runs(id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_shortlist_profile_job ON shortlist(profile_id, job_id);
CREATE INDEX IF NOT EXISTS idx_shortlist_status ON shortlist(status);
CREATE INDEX IF NOT EXISTS idx_shortlist_discovered_at ON shortlist(discovered_at);
CREATE INDEX IF NOT EXISTS idx_shortlist_run_id ON shortlist(discovery_run_id);

CREATE TABLE IF NOT EXISTS browser_sessions (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  platform TEXT NOT NULL,
  cookies JSONB,
  status TEXT NOT NULL,
  last_login TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  UNIQUE(profile_id, platform)
);

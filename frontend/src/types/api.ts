export interface Profile {
  id: string;
  name: string;
  linkedinEmail: string;
  glassdoorEmail: string;
}

export type ShortlistStatus = 'new' | 'saved' | 'dismissed' | 'applied';

// Editable search preferences (mirrors backend models.SearchPrefs).
export interface SearchPrefs {
  keywords: string[];
  locations: string[];
  remoteOnly: boolean;
  experienceLevels: string[];
  excludeCompanies: string[];
  minCompanyScore: number;
}

// Company research signals from company_verifications.details. All fields are
// optional: rows verified before a signal existed simply lack it.
export interface ResearchSignals {
  reposts?: number;
  employeeCount?: number;
  employeeGrowthPct?: number;
  kununuReviews?: number;
  kununuRating?: number;
  hasLinkedInPage?: boolean;
}

// Resume-vs-JD ATS match, from the matching phase.
export interface AtsDetails {
  score: number;
  matched: string[];
  missing: string[];
}

export interface ShortlistItem {
  id: string;
  profileId: string;
  jobId: string;
  title: string;
  company: string;
  location: string;
  applyUrl: string; // user opens this and applies themselves
  score: number; // company verification score 0-100
  isGhost: boolean;
  postedAt: string;
  status: ShortlistStatus;
  discoveredAt: string;
  discoveryRunId?: string;
  runStartedAt?: string;
  platform?: string;
  signals?: ResearchSignals;
  atsScore?: number; // resume match 0-100, absent until scored
  atsDetails?: AtsDetails;
}

// Resume metadata returned by the resume endpoints.
export interface ResumeInfo {
  filename: string;
  uploadedAt: string;
  textLen: number;
}

export type DiscoveryPhase = 'login' | 'scraping' | 'verifying' | 'matching' | 'done' | 'error';

export interface DiscoveryStatus {
  runId: string;
  phase: DiscoveryPhase;
  platform?: string; // platform currently being searched
  message?: string; // live human-readable step
  log?: string[]; // step-by-step activity feed
  found: number;
  verified: number;
  shortlisted: number;
  ghost: number;
  error?: string;
}

export type LoginStatus = 'active' | 'invalid_creds' | 'needs_2fa' | 'none';

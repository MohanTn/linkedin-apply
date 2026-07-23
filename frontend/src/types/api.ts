export interface Profile {
  id: string;
  name: string;
  linkedinEmail: string;
  glassdoorEmail: string;
}

export type ShortlistStatus = 'new' | 'saved' | 'dismissed' | 'applied';

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
  discoveryRunId?: string;
  runStartedAt?: string;
}

export type DiscoveryPhase = 'login' | 'scraping' | 'verifying' | 'done' | 'error';

export interface DiscoveryStatus {
  runId: string;
  phase: DiscoveryPhase;
  found: number;
  verified: number;
  shortlisted: number;
  ghost: number;
  error?: string;
}

export type LoginStatus = 'active' | 'invalid_creds' | 'needs_2fa' | 'none';

import type {
  DiscoveryStatus,
  LoginStatus,
  Profile,
  ResumeInfo,
  SearchPrefs,
  ShortlistItem,
  ShortlistStatus,
} from '@/types/api';

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json() as Promise<T>;
}

export const apiClient = {
  getProfiles: () =>
    fetch('/api/profiles').then((r) => json<{ profiles: Profile[] }>(r)),

  login: (profileId: string, platform: string) =>
    fetch(`/api/profiles/${profileId}/login`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ platform }),
    }).then((r) => json<{ ok: boolean; status: LoginStatus }>(r)),

  // Actively launches the browser login and persists the session cookies. Slow:
  // in headful mode it waits for the user to finish signing in.
  signIn: (profileId: string, platform: string) =>
    fetch(`/api/profiles/${profileId}/signin`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ platform }),
    }).then((r) => json<{ ok: boolean; status: LoginStatus }>(r)),

  startDiscovery: (profileId: string, platforms: string[], sinceHours = 24) =>
    fetch('/api/discovery/run', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ profileId, platforms, sinceHours }),
    }).then((r) => json<{ runId: string }>(r)),

  getDiscoveryStatus: (runId: string) =>
    fetch(`/api/discovery/${runId}/status`).then((r) => json<DiscoveryStatus>(r)),

  getShortlist: (
    profileId: string,
    opts: { status?: string; includeGhost?: boolean; minScore?: number; limit?: number } = {},
  ) => {
    const params = new URLSearchParams({ profileId });
    if (opts.status) params.set('status', opts.status);
    if (opts.includeGhost) params.set('includeGhost', 'true');
    if (opts.minScore != null) params.set('minScore', String(opts.minScore));
    // Load the whole shortlist; filtering + pagination happen client-side.
    params.set('limit', String(opts.limit ?? 1000));
    return fetch(`/api/shortlist?${params}`).then((r) =>
      json<{ items: ShortlistItem[]; total: number }>(r),
    );
  },

  updateShortlistStatus: (id: string, status: ShortlistStatus) =>
    fetch(`/api/shortlist/${id}`, {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ status }),
    }).then((r) => json<{ id: string; status: ShortlistStatus }>(r)),

  uploadResume: (profileId: string, file: File) => {
    const fd = new FormData();
    fd.append('file', file);
    return fetch(`/api/profiles/${profileId}/resume`, { method: 'POST', body: fd }).then(
      (r) => json<ResumeInfo>(r),
    );
  },

  // Returns null when no resume is uploaded (404), rather than throwing.
  getResume: (profileId: string) =>
    fetch(`/api/profiles/${profileId}/resume`).then((r) =>
      r.status === 404 ? null : json<ResumeInfo>(r),
    ),

  getPrefs: (profileId: string) =>
    fetch(`/api/profiles/${profileId}/prefs`).then((r) => json<SearchPrefs>(r)),

  savePrefs: (profileId: string, prefs: SearchPrefs) =>
    fetch(`/api/profiles/${profileId}/prefs`, {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(prefs),
    }).then((r) => json<SearchPrefs>(r)),

  clearShortlist: (profileId: string) =>
    fetch(`/api/shortlist?profileId=${encodeURIComponent(profileId)}`, {
      method: 'DELETE',
    }).then((r) => json<{ deleted: number }>(r)),

  // Runs the ATS scan on demand for the selected shortlist entries.
  runAts: (profileId: string, ids: string[]) =>
    fetch('/api/shortlist/ats', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ profileId, ids }),
    }).then((r) =>
      json<{ results: { entryId: string; score: number; scored: boolean }[] }>(r),
    ),
};

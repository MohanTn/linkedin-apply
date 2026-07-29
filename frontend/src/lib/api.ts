import type {
  DiscoveryStatus,
  LoginStatus,
  Profile,
  ProfileInput,
  ResumeInfo,
  SearchPrefs,
  ShortlistItem,
  ShortlistStatus,
} from '@/types/api';

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) throw new Error(await errorMessage(res));
  return res.json() as Promise<T>;
}

// The API reports failures as {"error": "..."}; surface that instead of a bare
// status code so the cockpit can show why a profile save was rejected.
async function errorMessage(res: Response): Promise<string> {
  try {
    const body = await res.json();
    if (body?.error) return body.error as string;
  } catch {
    /* not JSON */
  }
  return `${res.status} ${res.statusText}`;
}

export const apiClient = {
  getProfiles: () =>
    fetch('/api/profiles').then((r) => json<{ profiles: Profile[] }>(r)),

  createProfile: (input: ProfileInput) =>
    fetch('/api/profiles', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(input),
    }).then((r) => json<Profile>(r)),

  updateProfile: (profileId: string, input: ProfileInput) =>
    fetch(`/api/profiles/${profileId}`, {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(input),
    }).then((r) => json<Profile>(r)),

  deleteProfile: (profileId: string) =>
    fetch(`/api/profiles/${profileId}`, { method: 'DELETE' }).then((r) =>
      json<{ deleted: string }>(r),
    ),

  login: (profileId: string, platform: string) =>
    fetch(`/api/profiles/${profileId}/login`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ platform }),
    }).then((r) => json<{ ok: boolean; status: LoginStatus }>(r)),

  // Where the sign-in window can be watched and driven. The window runs inside
  // the backend container, so it is a page rather than a desktop window.
  getSignInViewer: () =>
    fetch('/api/signin-viewer').then((r) => json<{ viewerUrl: string }>(r)),

  // Opens the sign-in window and persists the resulting session. Slow by design:
  // it waits for the user to finish logging in inside that window.
  signIn: (profileId: string, platform: string) =>
    fetch(`/api/profiles/${profileId}/signin`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ platform }),
    }).then((r) => json<{ ok: boolean; status: LoginStatus; viewerUrl?: string }>(r)),

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

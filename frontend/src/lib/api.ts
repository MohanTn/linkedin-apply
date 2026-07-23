import type {
  DiscoveryStatus,
  LoginStatus,
  Profile,
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
    opts: { status?: string; includeGhost?: boolean; minScore?: number } = {},
  ) => {
    const params = new URLSearchParams({ profileId });
    if (opts.status) params.set('status', opts.status);
    if (opts.includeGhost) params.set('includeGhost', 'true');
    if (opts.minScore != null) params.set('minScore', String(opts.minScore));
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
};

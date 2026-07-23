'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api';
import type { ShortlistStatus } from '@/types/api';
import { ProfileSelector } from '@/components/ProfileSelector';
import { DiscoveryProgressBar } from '@/components/DiscoveryProgressBar';
import { ShortlistTable } from '@/components/ShortlistTable';

export default function Dashboard() {
  const qc = useQueryClient();
  const [profile, setProfile] = useState<string | null>(null);
  const [runId, setRunId] = useState<string | null>(null);
  const [includeGhost, setIncludeGhost] = useState(false);

  const profilesQuery = useQuery({
    queryKey: ['profiles'],
    queryFn: apiClient.getProfiles,
  });

  const runStatusQuery = useQuery({
    queryKey: ['run', runId],
    queryFn: () => apiClient.getDiscoveryStatus(runId as string),
    enabled: !!runId,
    refetchInterval: (q) =>
      q.state.data && ['done', 'error'].includes(q.state.data.phase) ? false : 2000,
  });

  const phase = runStatusQuery.data?.phase;
  const shortlistQuery = useQuery({
    queryKey: ['shortlist', profile, includeGhost, phase],
    queryFn: () =>
      apiClient.getShortlist(profile as string, { includeGhost }),
    enabled: !!profile,
  });

  const gather = useMutation({
    mutationFn: () =>
      apiClient.startDiscovery(profile as string, ['linkedin', 'glassdoor'], 24),
    onSuccess: (res) => setRunId(res.runId),
  });

  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: ShortlistStatus }) =>
      apiClient.updateShortlistStatus(id, status),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['shortlist'] }),
  });

  return (
    <main>
      <h1>🛰️ Job Discovery Cockpit</h1>
      <p className="muted">
        Gather open positions from the last 24 hours, run the company check, then
        apply yourself using the links. This app never applies for you.
      </p>

      <div className="card">
        <ProfileSelector profiles={profilesQuery.data?.profiles} onSelect={setProfile} />
        <div className="row" style={{ marginTop: '1rem' }}>
          <button
            onClick={() => gather.mutate()}
            disabled={!profile || gather.isPending || phase === 'scraping' || phase === 'verifying'}
          >
            Gather open positions (last 24h)
          </button>
          <label className="row" style={{ gap: '0.35rem' }}>
            <input
              type="checkbox"
              checked={includeGhost}
              onChange={(e) => setIncludeGhost(e.target.checked)}
            />
            show possible ghost jobs
          </label>
        </div>
      </div>

      {runId && <DiscoveryProgressBar status={runStatusQuery.data} />}

      <div className="card">
        <ShortlistTable
          items={shortlistQuery.data?.items}
          onStatus={(id, status) => setStatus.mutate({ id, status })}
        />
      </div>
    </main>
  );
}

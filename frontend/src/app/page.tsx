'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api';
import type { ShortlistStatus } from '@/types/api';
import { ProfileManager } from '@/components/ProfileManager';
import { ProfileSelector } from '@/components/ProfileSelector';
import { SearchPrefsEditor } from '@/components/SearchPrefsEditor';
import { DiscoveryProgressBar } from '@/components/DiscoveryProgressBar';
import { ShortlistTable } from '@/components/ShortlistTable';

const ALL_PLATFORMS = [
  'linkedin',
  'glassdoor',
  'xing',
  'stepstone',
  'indeed',
  'arbeitsagentur',
];

export default function Dashboard() {
  const qc = useQueryClient();
  const [profile, setProfile] = useState<string | null>(null);
  const [runId, setRunId] = useState<string | null>(null);
  const [includeGhost, setIncludeGhost] = useState(false);
  const [platforms, setPlatforms] = useState<string[]>(ALL_PLATFORMS);

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

  const resumeQuery = useQuery({
    queryKey: ['resume', profile],
    queryFn: () => apiClient.getResume(profile as string),
    enabled: !!profile,
  });

  const uploadResume = useMutation({
    mutationFn: (file: File) => apiClient.uploadResume(profile as string, file),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['resume', profile] }),
  });

  const gather = useMutation({
    mutationFn: () => apiClient.startDiscovery(profile as string, platforms, 24),
    onSuccess: (res) => setRunId(res.runId),
  });

  const togglePlatform = (p: string) =>
    setPlatforms((cur) => (cur.includes(p) ? cur.filter((x) => x !== p) : [...cur, p]));

  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: ShortlistStatus }) =>
      apiClient.updateShortlistStatus(id, status),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['shortlist'] }),
  });

  const clearShortlist = useMutation({
    mutationFn: () => apiClient.clearShortlist(profile as string),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['shortlist'] }),
  });

  const runAts = useMutation({
    mutationFn: (ids: string[]) => apiClient.runAts(profile as string, ids),
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
        <ProfileManager profiles={profilesQuery.data?.profiles} />
      </div>

      <div className="card">
        <ProfileSelector profiles={profilesQuery.data?.profiles} onSelect={setProfile} />
        <div className="row" style={{ marginTop: '0.75rem' }}>
          {ALL_PLATFORMS.map((p) => (
            <label key={p} className="row" style={{ gap: '0.3rem' }}>
              <input
                type="checkbox"
                checked={platforms.includes(p)}
                onChange={() => togglePlatform(p)}
              />
              {p}
            </label>
          ))}
        </div>
        <div className="row" style={{ marginTop: '0.75rem', gap: '0.5rem' }}>
          <label className="row" style={{ gap: '0.4rem' }}>
            📄 Resume:
            <input
              type="file"
              accept=".pdf,.txt,.md"
              disabled={!profile || uploadResume.isPending}
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) uploadResume.mutate(f);
              }}
            />
          </label>
          {uploadResume.isPending && <span className="muted">uploading…</span>}
          {uploadResume.isError && (
            <span className="err">{(uploadResume.error as Error).message}</span>
          )}
          {resumeQuery.data && !uploadResume.isPending && (
            <span className="muted">✅ {resumeQuery.data.filename}</span>
          )}
          {profile && !resumeQuery.data && !uploadResume.isPending && (
            <span className="muted">none — upload to enable ATS match scoring</span>
          )}
        </div>
        <div className="row" style={{ marginTop: '1rem' }}>
          <button
            onClick={() => gather.mutate()}
            disabled={
              !profile ||
              platforms.length === 0 ||
              gather.isPending ||
              phase === 'scraping' ||
              phase === 'verifying' ||
              phase === 'matching'
            }
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

      {profile && (
        <div className="card">
          <SearchPrefsEditor profileId={profile} />
        </div>
      )}

      {(runId || gather.isPending) && (
        <DiscoveryProgressBar status={runStatusQuery.data} starting={gather.isPending} />
      )}

      <div className="card">
        <div className="row" style={{ justifyContent: 'space-between', marginBottom: '0.75rem' }}>
          <h2 style={{ margin: 0, fontSize: '1.1rem' }}>Shortlist</h2>
          {profile && shortlistQuery.data?.items?.length ? (
            <button
              className="btn-danger"
              disabled={clearShortlist.isPending}
              onClick={() => {
                if (
                  window.confirm(
                    'Clear the entire shortlist for this profile and start over? This cannot be undone.',
                  )
                ) {
                  clearShortlist.mutate();
                }
              }}
            >
              {clearShortlist.isPending ? 'Clearing…' : '🗑 Clear all & start over'}
            </button>
          ) : null}
        </div>
        <ShortlistTable
          items={shortlistQuery.data?.items}
          onStatus={(id, status) => setStatus.mutate({ id, status })}
          onRunAts={(ids) => runAts.mutate(ids)}
          atsRunning={runAts.isPending}
        />
        {runAts.isError && <p className="err">{(runAts.error as Error).message}</p>}
      </div>
    </main>
  );
}

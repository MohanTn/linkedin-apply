'use client';

import type { DiscoveryStatus } from '@/types/api';

const PHASE_LABEL: Record<string, string> = {
  login: 'Logging in…',
  scraping: 'Gathering jobs…',
  verifying: 'Researching companies…',
  matching: 'Running ATS scan…',
  done: 'Done',
  error: 'Error',
};

// Percent complete: scraping is open-ended (indeterminate), verifying is
// shortlisted/found, done is 100.
function percent(status?: DiscoveryStatus): number | null {
  if (!status) return null;
  if (status.phase === 'done') return 100;
  if (status.phase === 'verifying' && status.found > 0) {
    return Math.round((status.shortlisted / status.found) * 100);
  }
  return null; // indeterminate
}

export function DiscoveryProgressBar({
  status,
  starting,
}: {
  status?: DiscoveryStatus;
  starting?: boolean;
}) {
  if (!status && !starting) return null;

  const phase = status?.phase;
  const running = starting || (phase !== undefined && !['done', 'error'].includes(phase));
  const pct = percent(status);

  return (
    <div className="card">
      <strong>
        {running && <span className="spinner" aria-label="running" />}
        {status ? PHASE_LABEL[status.phase] ?? status.phase : 'Starting run…'}
      </strong>

      <div className="progress-track" style={{ marginTop: '0.6rem' }}>
        <div
          className={pct === null && running ? 'progress-fill indeterminate' : 'progress-fill'}
          style={pct !== null ? { width: `${pct}%` } : undefined}
        />
      </div>

      {status && (
        <div className="muted" style={{ marginTop: '0.4rem' }}>
          Found {status.found} · Verified {status.verified} · Shortlisted{' '}
          {status.shortlisted} · Ghost {status.ghost}
          {pct !== null && ` · ${pct}%`}
        </div>
      )}

      {status?.log && status.log.length > 0 && (
        <ul className="run-log">
          {status.log.map((line, i) => (
            <li key={i} className={i === status.log!.length - 1 && running ? 'run-log-live' : undefined}>
              {line}
            </li>
          ))}
        </ul>
      )}

      {phase === 'error' && <div className="err">{status?.error}</div>}
    </div>
  );
}

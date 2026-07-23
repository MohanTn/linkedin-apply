'use client';

import type { DiscoveryStatus } from '@/types/api';

export function DiscoveryProgressBar({ status }: { status?: DiscoveryStatus }) {
  if (!status) return null;
  return (
    <div className="card">
      <strong>Phase: {status.phase}</strong>
      <div className="muted">
        Found {status.found} · Verified {status.verified} · Shortlisted{' '}
        {status.shortlisted} · Ghost {status.ghost}
      </div>
      {status.phase === 'error' && <div className="err">{status.error}</div>}
    </div>
  );
}

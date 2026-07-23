'use client';

import { useState } from 'react';
import { apiClient } from '@/lib/api';
import type { LoginStatus, Profile } from '@/types/api';

export function ProfileSelector({
  profiles,
  onSelect,
}: {
  profiles: Profile[] | undefined;
  onSelect: (id: string | null) => void;
}) {
  const [status, setStatus] = useState<LoginStatus | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSelect(id: string) {
    if (!id) {
      onSelect(null);
      setStatus(null);
      return;
    }
    setBusy(true);
    try {
      const res = await apiClient.login(id, 'linkedin');
      setStatus(res.status);
      onSelect(res.status === 'active' ? id : null);
    } catch {
      setStatus('invalid_creds');
      onSelect(null);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="row">
      <label htmlFor="profile">Profile:</label>
      <select
        id="profile"
        disabled={busy}
        onChange={(e) => handleSelect(e.target.value)}
        defaultValue=""
      >
        <option value="">Select profile…</option>
        {profiles?.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </select>
      {busy && <span className="muted">checking session…</span>}
      {status === 'active' && <span className="muted">✅ session active</span>}
      {status !== null && status !== 'active' && (
        <span className="warn">
          No active session — run <code>./scripts/login.sh {'{'}profile{'}'} linkedin</code> to sign in, then reselect
        </span>
      )}
    </div>
  );
}

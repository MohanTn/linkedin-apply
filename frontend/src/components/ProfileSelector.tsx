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
  const [selected, setSelected] = useState<string>('');
  const [status, setStatus] = useState<LoginStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [signingIn, setSigningIn] = useState(false);

  async function handleSelect(id: string) {
    setSelected(id);
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

  async function handleSignIn() {
    if (!selected) return;
    setSigningIn(true);
    try {
      const res = await apiClient.signIn(selected, 'linkedin');
      setStatus(res.status);
      onSelect(res.status === 'active' ? selected : null);
    } catch {
      setStatus('invalid_creds');
      onSelect(null);
    } finally {
      setSigningIn(false);
    }
  }

  return (
    <div className="row">
      <label htmlFor="profile">Profile:</label>
      <select
        id="profile"
        disabled={busy || signingIn}
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

      {selected && !busy && status !== null && status !== 'active' && (
        <>
          <button onClick={handleSignIn} disabled={signingIn}>
            🔐 Sign in to LinkedIn
          </button>
          {signingIn ? (
            <span className="muted">a browser window is opening — complete sign-in there…</span>
          ) : (
            <span className="warn">
              No active session — click Sign in to launch the browser login
            </span>
          )}
        </>
      )}
    </div>
  );
}

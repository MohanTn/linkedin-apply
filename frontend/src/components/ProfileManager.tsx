'use client';

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api';
import type { LoginStatus, PlatformSession, Profile } from '@/types/api';

const STATUS_LABEL: Record<LoginStatus, string> = {
  active: '✅ connected',
  expired: '⌛ expired',
  needs_2fa: '⚠️ sign-in incomplete',
  invalid_creds: '❌ sign-in failed',
  none: '— not connected',
};

// "in 6 days" / "in 3 hours", so an active session says how long it has left.
function expiresIn(iso?: string): string {
  if (!iso) return '';
  const ms = new Date(iso).getTime() - Date.now();
  if (!Number.isFinite(ms) || ms <= 0) return '';
  const hours = Math.round(ms / 3_600_000);
  if (hours < 24) return `expires in ${hours}h`;
  return `expires in ${Math.round(hours / 24)}d`;
}

export function ProfileManager({ profiles }: { profiles: Profile[] | undefined }) {
  const qc = useQueryClient();
  // null = form closed, '' = adding a new profile, otherwise the id being edited.
  const [editing, setEditing] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [signingIn, setSigningIn] = useState<string | null>(null);

  const refresh = () => qc.invalidateQueries({ queryKey: ['profiles'] });

  // The sign-in window runs inside the backend container, so it is watched in a
  // browser tab rather than appearing on the desktop.
  const viewerQuery = useQuery({
    queryKey: ['signin-viewer'],
    queryFn: apiClient.getSignInViewer,
    staleTime: Infinity,
  });
  const viewerUrl = viewerQuery.data?.viewerUrl;

  const save = useMutation({
    mutationFn: (n: string) =>
      editing ? apiClient.updateProfile(editing, { name: n }) : apiClient.createProfile({ name: n }),
    onSuccess: () => {
      refresh();
      setEditing(null);
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => apiClient.deleteProfile(id),
    onSuccess: refresh,
  });

  // Opens a real browser window on the backend's display. The user signs in
  // there; the request resolves once the session has been stored.
  const signIn = useMutation({
    mutationFn: ({ id, platform }: { id: string; platform: string }) =>
      apiClient.signIn(id, platform),
    onMutate: () => {
      // Open the viewer immediately: the window is already waiting for input,
      // and the request itself does not resolve until sign-in finishes.
      if (viewerUrl) window.open(viewerUrl, 'signin-window', 'width=1280,height=900');
    },
    onSettled: () => {
      setSigningIn(null);
      refresh();
    },
  });

  const openForm = (p?: Profile) => {
    setEditing(p ? p.id : '');
    setName(p ? p.name : '');
    save.reset();
  };

  const renderSession = (p: Profile, s: PlatformSession) => {
    const key = `${p.id}:${s.platform}`;
    const busy = signingIn === key;
    return (
      <span key={s.platform} className="row" style={{ gap: '0.35rem' }}>
        <strong>{s.platform}</strong>
        <span className={s.status === 'active' ? 'muted' : 'warn'}>
          {STATUS_LABEL[s.status] ?? s.status}
        </span>
        {s.status === 'active' && s.expiresAt && (
          <span className="muted">({expiresIn(s.expiresAt)})</span>
        )}
        <button
          className="link-btn"
          disabled={signIn.isPending}
          onClick={() => {
            setSigningIn(key);
            signIn.mutate({ id: p.id, platform: s.platform });
          }}
        >
          {busy
            ? 'waiting — sign in in the window…'
            : s.status === 'active'
              ? 'Sign in again'
              : '🔐 Sign in'}
        </button>
        {busy && viewerUrl && (
          <a href={viewerUrl} target="signin-window" rel="noreferrer">
            open the window
          </a>
        )}
      </span>
    );
  };

  return (
    <details className="prefs" open={!profiles?.length}>
      <summary>👤 Profiles &amp; portal sessions</summary>
      <p className="muted" style={{ marginTop: '0.5rem' }}>
        No passwords are stored. Sign in opens a browser window{' '}
        {viewerUrl ? (
          <>
            at <a href={viewerUrl} target="signin-window" rel="noreferrer">{viewerUrl}</a>
          </>
        ) : null}{' '}
        — log in there (including any 2FA) and the session is saved and reused until it expires.
      </p>

      <table style={{ marginTop: '0.75rem' }}>
        <thead>
          <tr>
            <th>Profile</th>
            <th>Portal sessions</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {profiles?.map((p) => (
            <tr key={p.id}>
              <td>
                {p.name}
                {/* Origin only — env-seeded profiles are just as editable. */}
                {p.source === 'env' && <span className="badge badge-portal"> from .env</span>}
              </td>
              <td>
                <div className="row" style={{ gap: '0.9rem' }}>
                  {p.sessions?.length ? (
                    p.sessions.map((s) => renderSession(p, s))
                  ) : (
                    <span className="muted">no portals</span>
                  )}
                </div>
              </td>
              <td>
                <div className="row" style={{ gap: '0.5rem' }}>
                  <button className="link-btn" onClick={() => openForm(p)}>
                    Rename
                  </button>
                  <button
                    className="link-btn"
                    disabled={remove.isPending}
                    onClick={() => {
                      if (
                        window.confirm(
                          `Delete “${p.name}”? Its sessions, resume, and shortlist are removed too.`,
                        )
                      ) {
                        remove.mutate(p.id);
                      }
                    }}
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {remove.isError && <p className="err">{(remove.error as Error).message}</p>}
      {signIn.isError && <p className="err">{(signIn.error as Error).message}</p>}

      {editing === null ? (
        <button style={{ marginTop: '0.75rem' }} onClick={() => openForm()}>
          ➕ Add profile
        </button>
      ) : (
        <div className="row" style={{ marginTop: '0.75rem', gap: '0.6rem' }}>
          <label className="row" style={{ gap: '0.4rem' }}>
            Profile name
            <input
              value={name}
              autoFocus
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && name.trim()) save.mutate(name);
              }}
              placeholder="e.g. Mohan — QA roles"
            />
          </label>
          <button onClick={() => save.mutate(name)} disabled={save.isPending || !name.trim()}>
            {save.isPending ? 'Saving…' : editing ? 'Save' : 'Create profile'}
          </button>
          <button className="link-btn" onClick={() => setEditing(null)}>
            Cancel
          </button>
          {save.isError && <span className="err">{(save.error as Error).message}</span>}
        </div>
      )}
    </details>
  );
}

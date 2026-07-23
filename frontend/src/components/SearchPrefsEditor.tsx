'use client';

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/lib/api';
import type { SearchPrefs } from '@/types/api';

const EXPERIENCE = ['entry', 'mid', 'senior', 'lead'];

const csv = (s: string) =>
  s
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean);

export function SearchPrefsEditor({ profileId }: { profileId: string }) {
  const qc = useQueryClient();
  const prefsQuery = useQuery({
    queryKey: ['prefs', profileId],
    queryFn: () => apiClient.getPrefs(profileId),
  });

  const [draft, setDraft] = useState<SearchPrefs | null>(null);
  useEffect(() => {
    if (prefsQuery.data) setDraft(prefsQuery.data);
  }, [prefsQuery.data]);

  const save = useMutation({
    mutationFn: (p: SearchPrefs) => apiClient.savePrefs(profileId, p),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['prefs', profileId] }),
  });

  if (!draft) return <p className="muted">Loading search preferences…</p>;

  const patch = (p: Partial<SearchPrefs>) => setDraft({ ...draft, ...p });

  return (
    <details className="prefs" open>
      <summary>🔍 Search preferences (edit &amp; save, then rerun)</summary>
      <div className="prefs-grid">
        <label>
          Keywords (comma-separated)
          <input
            value={draft.keywords.join(', ')}
            onChange={(e) => patch({ keywords: csv(e.target.value) })}
            placeholder="quality, office"
          />
        </label>
        <label>
          Locations (comma-separated)
          <input
            value={draft.locations.join(', ')}
            onChange={(e) => patch({ locations: csv(e.target.value) })}
            placeholder="Remote, Berlin, Germany"
          />
        </label>
        <label>
          Exclude companies (comma-separated)
          <input
            value={draft.excludeCompanies.join(', ')}
            onChange={(e) => patch({ excludeCompanies: csv(e.target.value) })}
            placeholder="e.g. some gmbh"
          />
        </label>
        <label>
          Min company score
          <input
            type="number"
            min={0}
            max={100}
            value={draft.minCompanyScore}
            onChange={(e) => patch({ minCompanyScore: Number(e.target.value) })}
          />
        </label>
        <fieldset>
          <legend>Experience</legend>
          <div className="row">
            {EXPERIENCE.map((lvl) => (
              <label key={lvl} className="row" style={{ gap: '0.3rem' }}>
                <input
                  type="checkbox"
                  checked={draft.experienceLevels.includes(lvl)}
                  onChange={(e) =>
                    patch({
                      experienceLevels: e.target.checked
                        ? [...draft.experienceLevels, lvl]
                        : draft.experienceLevels.filter((x) => x !== lvl),
                    })
                  }
                />
                {lvl}
              </label>
            ))}
          </div>
        </fieldset>
        <label className="row" style={{ gap: '0.4rem' }}>
          <input
            type="checkbox"
            checked={draft.remoteOnly}
            onChange={(e) => patch({ remoteOnly: e.target.checked })}
          />
          Remote only
        </label>
      </div>
      <div className="row" style={{ marginTop: '0.6rem', gap: '0.6rem' }}>
        <button onClick={() => save.mutate(draft)} disabled={save.isPending}>
          {save.isPending ? 'Saving…' : 'Save preferences'}
        </button>
        {save.isSuccess && <span className="muted">✅ saved — rerun to apply</span>}
        {save.isError && <span className="err">{(save.error as Error).message}</span>}
      </div>
    </details>
  );
}

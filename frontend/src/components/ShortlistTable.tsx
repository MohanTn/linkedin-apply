'use client';

import type { ShortlistItem, ShortlistStatus } from '@/types/api';

function scoreColor(score: number): string {
  if (score >= 70) return '#16a34a';
  if (score >= 40) return '#d97706';
  return '#dc2626';
}

function timeAgo(iso: string): string {
  const d = new Date(iso).getTime();
  if (Number.isNaN(d)) return '';
  const mins = Math.floor((Date.now() - d) / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  return `${hrs}h ago`;
}

function formatRunTime(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const day = String(d.getDate()).padStart(2, '0');
  const month = String(d.getMonth() + 1).padStart(2, '0');
  const year = d.getFullYear();
  const hours = String(d.getHours()).padStart(2, '0');
  const mins = String(d.getMinutes()).padStart(2, '0');
  const ampm = d.getHours() >= 12 ? 'PM' : 'AM';
  return `${day}/${month}/${year} ${hours}:${mins} ${ampm}`;
}

function groupByRun(items: ShortlistItem[]) {
  const grouped: Map<string, ShortlistItem[]> = new Map();
  for (const item of items) {
    const runId = item.discoveryRunId || 'old';
    if (!grouped.has(runId)) {
      grouped.set(runId, []);
    }
    grouped.get(runId)!.push(item);
  }
  return grouped;
}

export function ShortlistTable({
  items,
  onStatus,
}: {
  items: ShortlistItem[] | undefined;
  onStatus: (id: string, status: ShortlistStatus) => void;
}) {
  if (!items?.length) {
    return <p className="muted">No jobs yet — run a discovery to populate the shortlist.</p>;
  }

  const grouped = groupByRun(items);
  const runs = Array.from(grouped.entries()).sort((a, b) => {
    const timeA = a[1][0]?.runStartedAt;
    const timeB = b[1][0]?.runStartedAt;
    if (!timeA) return 1;
    if (!timeB) return -1;
    return new Date(timeB).getTime() - new Date(timeA).getTime();
  });

  return (
    <div style={{ overflowX: 'auto' }}>
      <table>
        <thead>
          <tr>
            <th>Company</th>
            <th>Title</th>
            <th>Location</th>
            <th>Posted</th>
            <th>Score</th>
            <th>Apply link</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {runs.flatMap(([runId, runItems]) => [
            <tr key={`${runId}-sep`} style={{ backgroundColor: 'rgba(200, 200, 200, 0.1)' }}>
              <td colSpan={7} style={{ padding: '0.5rem 0', textAlign: 'center', fontSize: '0.85rem', color: '#666' }}>
                {'.'.repeat(50)}{formatRunTime(runItems[0]?.runStartedAt)}
              </td>
            </tr>,
            ...runItems.map((it) => (
              <tr key={it.id} className={it.isGhost ? 'ghost' : undefined}>
                <td>
                  {it.company}
                  {it.isGhost && <span className="badge badge-ghost">possible ghost</span>}
                </td>
                <td>{it.title}</td>
                <td>{it.location}</td>
                <td>{timeAgo(it.postedAt)}</td>
                <td>
                  <span className="pill" style={{ background: scoreColor(it.score) }}>
                    {it.score}
                  </span>
                </td>
                <td>
                  <a href={it.applyUrl} target="_blank" rel="noopener noreferrer">
                    Open &amp; apply →
                  </a>
                </td>
                <td>
                  <select
                    value={it.status}
                    onChange={(e) => onStatus(it.id, e.target.value as ShortlistStatus)}
                  >
                    <option value="new">New</option>
                    <option value="saved">Saved</option>
                    <option value="dismissed">Dismissed</option>
                    <option value="applied">Applied</option>
                  </select>
                </td>
              </tr>
            )),
          ])}
        </tbody>
      </table>
    </div>
  );
}

'use client';

import { Fragment, useEffect, useMemo, useState } from 'react';
import type { ShortlistItem, ShortlistStatus } from '@/types/api';

function scoreColor(score: number): string {
  if (score >= 70) return '#16a34a';
  if (score >= 40) return '#d97706';
  return '#dc2626';
}

function timeAgo(iso: string): string {
  const d = new Date(iso).getTime();
  // Invalid or zero-value date = the portal did not publish a date.
  if (Number.isNaN(d) || d <= 0) return '—';
  const mins = Math.floor((Date.now() - d) / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 48) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

function formatFoundTime(it: ShortlistItem): string {
  const iso = it.runStartedAt || it.discoveredAt;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const day = String(d.getDate()).padStart(2, '0');
  const month = String(d.getMonth() + 1).padStart(2, '0');
  const hours = String(d.getHours()).padStart(2, '0');
  const mins = String(d.getMinutes()).padStart(2, '0');
  return `${day}/${month} ${hours}:${mins}`;
}

function growthLabel(pct: number): string {
  if (pct > 5) return `▲ growing +${pct.toFixed(1)}%`;
  if (pct < -5) return `▼ shrinking ${pct.toFixed(1)}%`;
  if (pct !== 0) return `→ stable (${pct.toFixed(1)}%)`;
  return '— unknown (needs more snapshots)';
}

function AtsDetailPanel({ it }: { it: ShortlistItem }) {
  if (it.atsScore == null || !it.atsDetails) return null;
  const { matched, missing } = it.atsDetails;
  return (
    <div style={{ padding: '0.5rem 0', fontSize: '0.9rem' }}>
      <div style={{ marginBottom: '0.35rem' }}>
        Resume match: <strong>{it.atsScore}%</strong> against the job description
      </div>
      <div style={{ display: 'flex', gap: '0.3rem', flexWrap: 'wrap', marginBottom: '0.35rem' }}>
        {matched.map((k) => (
          <span key={k} className="chip chip-hit">
            {k}
          </span>
        ))}
      </div>
      <div style={{ display: 'flex', gap: '0.3rem', flexWrap: 'wrap' }}>
        {missing.map((k) => (
          <span key={k} className="chip chip-miss">
            {k}
          </span>
        ))}
      </div>
    </div>
  );
}

function ResearchDetail({ it }: { it: ShortlistItem }) {
  if (!it.signals && it.atsScore == null) {
    return <p className="muted">No research data for this company yet.</p>;
  }
  if (!it.signals) {
    return <AtsDetailPanel it={it} />;
  }
  // Rows verified before a signal existed lack that field — default to 0/false.
  const {
    reposts = 0,
    employeeCount = 0,
    employeeGrowthPct = 0,
    kununuReviews = 0,
    kununuRating = 0,
    hasLinkedInPage = false,
  } = it.signals;
  return (
    <>
      <div style={{ display: 'flex', gap: '2rem', flexWrap: 'wrap', fontSize: '0.9rem', padding: '0.5rem 0' }}>
        <span>
          Reposts:{' '}
          <strong style={{ color: reposts > 0 ? '#dc2626' : undefined }}>
            {reposts > 0 ? `${reposts} — reopened before` : 'none'}
          </strong>
        </span>
        <span>
          Employees: <strong>{employeeCount > 0 ? employeeCount : '?'}</strong>{' '}
          {growthLabel(employeeGrowthPct)}
        </span>
        <span>
          Kununu:{' '}
          <strong>
            {kununuReviews > 0 ? `${kununuRating.toFixed(1)}★ (${kununuReviews} reviews)` : 'no profile found'}
          </strong>
        </span>
        <span>
          LinkedIn page: <strong>{hasLinkedInPage ? 'yes' : 'not found'}</strong>
        </span>
      </div>
      <AtsDetailPanel it={it} />
    </>
  );
}

// Per-column filter state. Text filters are case-insensitive substring matches.
interface Filters {
  company: string;
  title: string;
  portal: string; // '' = any
  location: string;
  postedWithinH: string; // '' = any, else hours
  foundRun: string; // '' = any, else formatted found time
  minScore: string;
  status: string; // '' = any
}

const NO_FILTERS: Filters = {
  company: '',
  title: '',
  portal: '',
  location: '',
  postedWithinH: '',
  foundRun: '',
  minScore: '',
  status: '',
};

function applyFilters(items: ShortlistItem[], f: Filters): ShortlistItem[] {
  const has = (hay: string | undefined, needle: string) =>
    !needle || (hay ?? '').toLowerCase().includes(needle.toLowerCase());
  return items.filter((it) => {
    if (!has(it.company, f.company)) return false;
    if (!has(it.title, f.title)) return false;
    if (f.portal && (it.platform ?? '') !== f.portal) return false;
    if (!has(it.location, f.location)) return false;
    if (f.postedWithinH) {
      const cutoff = Date.now() - Number(f.postedWithinH) * 3600_000;
      if (new Date(it.postedAt).getTime() < cutoff) return false;
    }
    if (f.foundRun && formatFoundTime(it) !== f.foundRun) return false;
    if (f.minScore && it.score < Number(f.minScore)) return false;
    if (f.status && it.status !== f.status) return false;
    return true;
  });
}

export function ShortlistTable({
  items,
  onStatus,
  onRunAts,
  atsRunning,
}: {
  items: ShortlistItem[] | undefined;
  onStatus: (id: string, status: ShortlistStatus) => void;
  onRunAts?: (ids: string[]) => void;
  atsRunning?: boolean;
}) {
  const [expanded, setExpanded] = useState<string | null>(null);
  const [filters, setFilters] = useState<Filters>(NO_FILTERS);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const filtered = useMemo(() => applyFilters(items ?? [], filters), [items, filters]);

  // Snap back to a valid page whenever filters, page size, or the data shrink
  // the result below the current page.
  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize));
  useEffect(() => {
    if (page > pageCount) setPage(pageCount);
  }, [page, pageCount]);

  const portals = useMemo(
    () => Array.from(new Set((items ?? []).map((it) => it.platform).filter(Boolean))) as string[],
    [items],
  );
  const foundRuns = useMemo(
    () => Array.from(new Set((items ?? []).map(formatFoundTime).filter(Boolean))).sort().reverse(),
    [items],
  );

  if (!items?.length) {
    return <p className="muted">No jobs yet — run a discovery to populate the shortlist.</p>;
  }

  // Filtering changes the result set, so return to the first page.
  const set = (patch: Partial<Filters>) => {
    setFilters({ ...filters, ...patch });
    setPage(1);
  };
  const active = filters !== NO_FILTERS && Object.values(filters).some((v) => v !== '');

  const safePage = Math.min(page, pageCount);
  const start = (safePage - 1) * pageSize;
  const pageRows = filtered.slice(start, start + pageSize);
  const colCount = onRunAts ? 10 : 9;

  const toggleOne = (id: string) => {
    const next = new Set(selected);
    next.has(id) ? next.delete(id) : next.add(id);
    setSelected(next);
  };
  const pageAllSelected = pageRows.length > 0 && pageRows.every((it) => selected.has(it.id));
  const togglePage = () => {
    const next = new Set(selected);
    if (pageAllSelected) pageRows.forEach((it) => next.delete(it.id));
    else pageRows.forEach((it) => next.add(it.id));
    setSelected(next);
  };

  return (
    <div>
      {onRunAts && (
        <div className="row" style={{ gap: '0.6rem', marginBottom: '0.6rem' }}>
          <button
            disabled={selected.size === 0 || atsRunning}
            onClick={() => onRunAts(Array.from(selected))}
            title="Fetch each selected job's description and score your resume against it"
          >
            {atsRunning ? 'Running ATS…' : `⚡ Run ATS on ${selected.size} selected`}
          </button>
          {selected.size > 0 && !atsRunning && (
            <button className="link-btn" onClick={() => setSelected(new Set())}>
              clear selection
            </button>
          )}
          <span className="muted">ATS is on-demand — pick jobs, then run the scan.</span>
        </div>
      )}
      <table className="shortlist">
        <colgroup>
          {onRunAts && <col style={{ width: '3%' }} />} {/* select */}
          <col style={{ width: '16%' }} /> {/* Company */}
          <col style={{ width: '18%' }} /> {/* Title */}
          <col style={{ width: '8%' }} /> {/* Portal */}
          <col style={{ width: '10%' }} /> {/* Location */}
          <col style={{ width: '7%' }} /> {/* Posted */}
          <col style={{ width: '7%' }} /> {/* Found */}
          <col style={{ width: '13%' }} /> {/* Score + CV */}
          <col style={{ width: '8%' }} /> {/* Apply */}
          <col style={{ width: '11%' }} /> {/* Status */}
        </colgroup>
        <thead>
          <tr>
            {onRunAts && (
              <th title="Select for ATS scan">
                <input type="checkbox" checked={pageAllSelected} onChange={togglePage} />
              </th>
            )}
            <th>Company</th>
            <th>Title</th>
            <th>Portal</th>
            <th>Location</th>
            <th>Posted</th>
            <th>Found</th>
            <th>Score · CV</th>
            <th>Apply link</th>
            <th>Status</th>
          </tr>
          <tr className="filter-row">
            {onRunAts && <th />}
            <th>
              <input
                placeholder="filter…"
                value={filters.company}
                onChange={(e) => set({ company: e.target.value })}
              />
            </th>
            <th>
              <input
                placeholder="filter…"
                value={filters.title}
                onChange={(e) => set({ title: e.target.value })}
              />
            </th>
            <th>
              <select value={filters.portal} onChange={(e) => set({ portal: e.target.value })}>
                <option value="">all</option>
                {portals.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </select>
            </th>
            <th>
              <input
                placeholder="filter…"
                value={filters.location}
                onChange={(e) => set({ location: e.target.value })}
              />
            </th>
            <th>
              <select
                value={filters.postedWithinH}
                onChange={(e) => set({ postedWithinH: e.target.value })}
              >
                <option value="">any</option>
                <option value="24">last 24h</option>
                <option value="72">last 3d</option>
                <option value="168">last 7d</option>
              </select>
            </th>
            <th>
              <select value={filters.foundRun} onChange={(e) => set({ foundRun: e.target.value })}>
                <option value="">all runs</option>
                {foundRuns.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            </th>
            <th>
              <input
                type="number"
                min={0}
                max={100}
                placeholder="min"
                value={filters.minScore}
                onChange={(e) => set({ minScore: e.target.value })}
              />
            </th>
            <th>
              {active && (
                <button
                  className="link-btn"
                  onClick={() => {
                    setFilters(NO_FILTERS);
                    setPage(1);
                  }}
                >
                  clear
                </button>
              )}
            </th>
            <th>
              <select value={filters.status} onChange={(e) => set({ status: e.target.value })}>
                <option value="">all</option>
                <option value="new">New</option>
                <option value="saved">Saved</option>
                <option value="dismissed">Dismissed</option>
                <option value="applied">Applied</option>
              </select>
            </th>
          </tr>
        </thead>
        <tbody>
          {filtered.length === 0 && (
            <tr>
              <td colSpan={colCount} className="muted" style={{ textAlign: 'center' }}>
                No jobs match the current filters.
              </td>
            </tr>
          )}
          {pageRows.map((it) => (
            <Fragment key={it.id}>
              <tr className={it.isGhost ? 'ghost' : undefined}>
                {onRunAts && (
                  <td style={{ textAlign: 'center' }}>
                    <input
                      type="checkbox"
                      checked={selected.has(it.id)}
                      onChange={() => toggleOne(it.id)}
                    />
                  </td>
                )}
                <td
                  onClick={() => setExpanded(expanded === it.id ? null : it.id)}
                  style={{ cursor: 'pointer' }}
                  title="Show company research"
                >
                  {expanded === it.id ? '▾ ' : '▸ '}
                  {it.company}
                  {it.isGhost && <span className="badge badge-ghost">possible ghost</span>}
                </td>
                <td>{it.title}</td>
                <td>
                  {it.platform && <span className="badge badge-portal">{it.platform}</span>}
                </td>
                <td>{it.location}</td>
                <td>{timeAgo(it.postedAt)}</td>
                <td className="muted">{formatFoundTime(it)}</td>
                <td>
                  <span className="row" style={{ gap: '0.3rem' }}>
                    <span className="pill" style={{ background: scoreColor(it.score) }} title="Company legitimacy score">
                      {it.score}
                    </span>
                    {it.atsScore != null && (
                      <span
                        className="pill"
                        style={{ background: scoreColor(it.atsScore) }}
                        title="Resume ATS match against the job description"
                      >
                        CV {it.atsScore}
                      </span>
                    )}
                  </span>
                </td>
                <td>
                  <a href={it.applyUrl} target="_blank" rel="noopener noreferrer">
                    Apply →
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
              {expanded === it.id && (
                <tr>
                  <td colSpan={colCount} style={{ backgroundColor: 'rgba(200, 200, 200, 0.08)' }}>
                    <ResearchDetail it={it} />
                  </td>
                </tr>
              )}
            </Fragment>
          ))}
        </tbody>
      </table>

      <div className="pager">
        <span className="muted">
          {filtered.length === 0
            ? '0 jobs'
            : `${start + 1}–${Math.min(start + pageSize, filtered.length)} of ${filtered.length}${
                active ? ' (filtered)' : ''
              }`}
        </span>
        <span className="row" style={{ gap: '0.4rem' }}>
          <button className="link-btn" disabled={safePage <= 1} onClick={() => setPage(1)}>
            « first
          </button>
          <button className="link-btn" disabled={safePage <= 1} onClick={() => setPage(safePage - 1)}>
            ‹ prev
          </button>
          <span>
            Page {safePage} / {pageCount}
          </span>
          <button
            className="link-btn"
            disabled={safePage >= pageCount}
            onClick={() => setPage(safePage + 1)}
          >
            next ›
          </button>
          <button
            className="link-btn"
            disabled={safePage >= pageCount}
            onClick={() => setPage(pageCount)}
          >
            last »
          </button>
        </span>
        <label className="row" style={{ gap: '0.35rem' }}>
          Per page:
          <select
            value={pageSize}
            onChange={(e) => {
              setPageSize(Number(e.target.value));
              setPage(1);
            }}
          >
            {[10, 25, 50, 100].map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </label>
      </div>
    </div>
  );
}

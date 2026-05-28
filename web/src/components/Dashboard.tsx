import { useEffect, useMemo, useState } from 'preact/hooks'
import { api } from '../api'
import type { StatsDayBucket, StatsResponse } from '../types'

// ── Date helpers ──────────────────────────────────────────────────────────────

function today(): string {
  const now = new Date()
  return [
    now.getFullYear(),
    String(now.getMonth() + 1).padStart(2, '0'),
    String(now.getDate()).padStart(2, '0'),
  ].join('-')
}

function addDays(date: string, n: number): string {
  const [y, m, d] = date.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d + n)).toISOString().slice(0, 10)
}

function startOfWeek(date: string): string {
  const [y, m, d] = date.split('-').map(Number)
  const dt = new Date(Date.UTC(y, m - 1, d))
  const day = dt.getUTCDay()
  dt.setUTCDate(dt.getUTCDate() - day + (day === 0 ? -6 : 1))
  return dt.toISOString().slice(0, 10)
}

function startOfMonth(date: string): string {
  return date.slice(0, 7) + '-01'
}

function startOfYear(date: string): string {
  return date.slice(0, 4) + '-01-01'
}

function formatDate(s: string): string {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' })
}

function formatMonth(s: string): string {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, { month: 'short', year: 'numeric', timeZone: 'UTC' })
}

// ── Preset definitions ────────────────────────────────────────────────────────

type Preset = 'week' | 'month' | 'year' | 'all'

function presetRange(p: Preset): { from: string; to: string } {
  const t = today()
  switch (p) {
    case 'week':  return { from: startOfWeek(t), to: t }
    case 'month': return { from: startOfMonth(t), to: t }
    case 'year':  return { from: startOfYear(t), to: t }
    case 'all':   return { from: '', to: '' }
  }
}

// ── Aggregation: daily → weekly / monthly ─────────────────────────────────────

interface Bucket {
  label: string
  tasks_done: number
  github: number
  jira: number
  confluence: number
  total: number
}

function aggregateTimeline(timeline: StatsDayBucket[], from: string, to: string): Bucket[] {
  if (timeline.length === 0) return []

  const first = from || timeline[0].date
  const last = to || timeline[timeline.length - 1].date
  const days = Math.round(
    (new Date(last + 'T00:00:00').getTime() - new Date(first + 'T00:00:00').getTime()) /
    86400000,
  ) + 1

  // Index timeline by date for O(1) lookup.
  const byDate: Record<string, StatsDayBucket> = {}
  for (const b of timeline) byDate[b.date] = b

  if (days <= 60) {
    // Daily — one bucket per calendar day in range
    const buckets: Bucket[] = []
    let cur = first
    while (cur <= last) {
      const b = byDate[cur]
      buckets.push({
        label: cur,
        tasks_done: b?.tasks_done ?? 0,
        github: b?.github ?? 0,
        jira: b?.jira ?? 0,
        confluence: b?.confluence ?? 0,
        total: (b?.tasks_done ?? 0) + (b?.github ?? 0) + (b?.jira ?? 0) + (b?.confluence ?? 0),
      })
      cur = addDays(cur, 1)
    }
    return buckets
  }

  if (days <= 365) {
    // Weekly — ISO week starting Monday
    const bucketMap: Record<string, Bucket> = {}
    let cur = first
    while (cur <= last) {
      const weekStart = startOfWeek(cur)
      if (!bucketMap[weekStart]) {
        bucketMap[weekStart] = { label: weekStart, tasks_done: 0, github: 0, jira: 0, confluence: 0, total: 0 }
      }
      const b = byDate[cur]
      if (b) {
        bucketMap[weekStart].tasks_done += b.tasks_done
        bucketMap[weekStart].github += b.github
        bucketMap[weekStart].jira += b.jira
        bucketMap[weekStart].confluence += b.confluence
        bucketMap[weekStart].total += b.tasks_done + b.github + b.jira + b.confluence
      }
      cur = addDays(cur, 1)
    }
    return Object.keys(bucketMap).sort().map(k => bucketMap[k])
  }

  // Monthly
  const bucketMap: Record<string, Bucket> = {}
  for (const b of timeline) {
    const month = b.date.slice(0, 7)
    if (!bucketMap[month]) {
      bucketMap[month] = { label: month + '-01', tasks_done: 0, github: 0, jira: 0, confluence: 0, total: 0 }
    }
    bucketMap[month].tasks_done += b.tasks_done
    bucketMap[month].github += b.github
    bucketMap[month].jira += b.jira
    bucketMap[month].confluence += b.confluence
    bucketMap[month].total += b.tasks_done + b.github + b.jira + b.confluence
  }
  return Object.keys(bucketMap).sort().map(k => bucketMap[k])
}

// ── Sub-components ────────────────────────────────────────────────────────────

function StatCard({ label, value, sub }: { label: string; value: number; sub?: string }) {
  return (
    <div class="stat-card">
      <div class="stat-card-value">{value}</div>
      <div class="stat-card-label">{label}</div>
      {sub && <div class="stat-card-sub">{sub}</div>}
    </div>
  )
}

function BarChart({ buckets, granularity }: { buckets: Bucket[]; granularity: 'day' | 'week' | 'month' }) {
  const max = Math.max(...buckets.map(b => b.total), 1)

  function labelFor(b: Bucket): string {
    if (granularity === 'month') return formatMonth(b.label)
    if (granularity === 'week')  return 'W/' + b.label.slice(5)
    return b.label.slice(5) // MM-DD
  }

  // Show at most ~30 labels to avoid crowding
  const labelInterval = Math.ceil(buckets.length / 30)

  return (
    <div class="barchart">
      <div class="barchart-bars">
        {buckets.map((b, i) => (
          <div key={b.label} class="barchart-col" title={`${formatDate(b.label)}: ${b.total} items`}>
            <div class="barchart-stack" style={{ height: '100%' }}>
              <div class="barchart-fill barchart-fill--github"    style={{ flex: b.github }} />
              <div class="barchart-fill barchart-fill--jira"      style={{ flex: b.jira }} />
              <div class="barchart-fill barchart-fill--confluence" style={{ flex: b.confluence }} />
              <div class="barchart-fill barchart-fill--tasks"     style={{ flex: b.tasks_done }} />
              <div class="barchart-fill barchart-fill--empty"     style={{ flex: max - b.total }} />
            </div>
            {i % labelInterval === 0 && (
              <div class="barchart-label">{labelFor(b)}</div>
            )}
          </div>
        ))}
      </div>
      <div class="barchart-legend">
        <span class="barchart-legend-item barchart-legend-item--github">GitHub</span>
        <span class="barchart-legend-item barchart-legend-item--jira">Jira</span>
        <span class="barchart-legend-item barchart-legend-item--confluence">Confluence</span>
        <span class="barchart-legend-item barchart-legend-item--tasks">Tasks done</span>
      </div>
    </div>
  )
}

function SourceBlock({ title, rows }: { title: string; rows: { label: string; value: number; highlight?: boolean }[] }) {
  const total = rows.reduce((s, r) => s + r.value, 0)
  if (total === 0) return null
  return (
    <div class="source-block">
      <div class="source-block-title">{title}</div>
      <div class="source-block-rows">
        {rows.filter(r => r.value > 0).map(r => (
          <div key={r.label} class={`source-block-row${r.highlight ? ' source-block-row--highlight' : ''}`}>
            <span class="source-block-row-label">{r.label}</span>
            <span class="source-block-row-value">{r.value}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

// ── Main component ────────────────────────────────────────────────────────────

export function Dashboard() {
  const [preset, setPreset] = useState<Preset>('month')
  const [customFrom, setCustomFrom] = useState('')
  const [customTo, setCustomTo] = useState('')
  const [usingCustom, setUsingCustom] = useState(false)
  const [data, setData] = useState<StatsResponse | null>(null)
  const [loading, setLoading] = useState(false)

  const { from, to } = usingCustom
    ? { from: customFrom, to: customTo }
    : presetRange(preset)

  useEffect(() => {
    setLoading(true)
    api.getStats(from || undefined, to || undefined)
      .then(setData)
      .catch(console.error)
      .finally(() => setLoading(false))
  }, [from, to])

  const buckets = useMemo(() => {
    if (!data) return []
    return aggregateTimeline(data.timeline, from, to)
  }, [data, from, to])

  const granularity: 'day' | 'week' | 'month' = useMemo(() => {
    if (buckets.length === 0) return 'day'
    const days = buckets.length
    if (days > 365 / 30) return 'month'
    if (days > 60) return 'week'
    return 'day'
  }, [buckets])

  const selectPreset = (p: Preset) => {
    setPreset(p)
    setUsingCustom(false)
  }

  const applyCustom = () => {
    if (customFrom && customTo && customFrom <= customTo) {
      setUsingCustom(true)
    }
  }

  const PRESETS: { key: Preset; label: string }[] = [
    { key: 'week', label: 'This week' },
    { key: 'month', label: 'This month' },
    { key: 'year', label: 'This year' },
    { key: 'all', label: 'All time' },
  ]

  return (
    <div class="dashboard">
      {/* Period selector */}
      <div class="dash-period-bar">
        <div class="dash-presets">
          {PRESETS.map(p => (
            <button
              key={p.key}
              class={`dash-preset-btn${!usingCustom && preset === p.key ? ' dash-preset-btn--active' : ''}`}
              onClick={() => selectPreset(p.key)}
            >
              {p.label}
            </button>
          ))}
        </div>
        <div class="dash-custom-range">
          <input
            type="date"
            class="dash-date-input"
            value={customFrom}
            onInput={e => setCustomFrom((e.target as HTMLInputElement).value)}
          />
          <span class="dash-range-sep">–</span>
          <input
            type="date"
            class="dash-date-input"
            value={customTo}
            onInput={e => setCustomTo((e.target as HTMLInputElement).value)}
          />
          <button
            class="dash-apply-btn"
            onClick={applyCustom}
            disabled={!customFrom || !customTo || customFrom > customTo}
          >
            Apply
          </button>
        </div>
      </div>

      {loading && <div class="dash-loading">Loading…</div>}

      {data && !loading && (
        <>
          {/* Summary cards */}
          <div class="dash-cards">
            <StatCard
              label="Tasks completed"
              value={data.summary.tasks_done}
              sub={data.summary.tasks_total > 0
                ? `${Math.round(data.summary.tasks_done / data.summary.tasks_total * 100)}% of ${data.summary.tasks_total}`
                : undefined}
            />
            <StatCard label="PRs authored" value={data.github.authored_total} sub={`${data.github.authored_merged} merged`} />
            <StatCard label="PRs reviewed" value={data.github.reviewed_total} sub={`${data.github.reviewed_merged} merged`} />
            <StatCard label="Jira tickets" value={data.jira.total} sub={`${data.jira.done} done`} />
            <StatCard label="Confluence pages" value={data.confluence.total} sub={`${data.confluence.created} created`} />
            <StatCard label="Active days" value={data.period.active_days} />
          </div>

          {/* Timeline bar chart */}
          {buckets.length > 0 && (
            <div class="dash-section">
              <div class="dash-section-title">Activity over time</div>
              <BarChart buckets={buckets} granularity={granularity} />
            </div>
          )}

          {/* Per-source breakdowns */}
          <div class="dash-section">
            <div class="dash-section-title">Breakdown by source</div>
            <div class="dash-breakdowns">
              <SourceBlock
                title="GitHub — Authored PRs"
                rows={[
                  { label: 'Merged', value: data.github.authored_merged, highlight: true },
                  { label: 'Open', value: data.github.authored_open },
                  { label: 'Approved', value: data.github.authored_approved },
                  { label: 'In review', value: data.github.authored_in_review },
                  { label: 'Changes requested', value: data.github.authored_changes_requested },
                  { label: 'Draft', value: data.github.authored_draft },
                  { label: 'Closed', value: data.github.authored_closed },
                ]}
              />
              <SourceBlock
                title="GitHub — Reviewed PRs"
                rows={[
                  { label: 'Merged', value: data.github.reviewed_merged, highlight: true },
                  { label: 'Open', value: data.github.reviewed_open },
                  { label: 'Draft', value: data.github.reviewed_draft },
                  { label: 'Closed', value: data.github.reviewed_closed },
                ]}
              />
              <SourceBlock
                title="Jira"
                rows={[
                  { label: 'Done', value: data.jira.done, highlight: true },
                  { label: 'In progress', value: data.jira.in_progress },
                  { label: 'To do', value: data.jira.todo },
                ]}
              />
              <SourceBlock
                title="Confluence"
                rows={[
                  { label: 'Pages created', value: data.confluence.created, highlight: true },
                  { label: 'Pages edited', value: data.confluence.edited },
                ]}
              />
            </div>
          </div>

          {/* Top busiest days */}
          {data.top_days.length > 0 && (
            <div class="dash-section">
              <div class="dash-section-title">Most active days</div>
              <div class="dash-top-days">
                {data.top_days.map((d, i) => (
                  <div key={d.date} class="dash-top-day">
                    <span class="dash-top-day-rank">#{i + 1}</span>
                    <span class="dash-top-day-date">{formatDate(d.date)}</span>
                    <span class="dash-top-day-count">{d.total} items</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {data.summary.activities_total === 0 && data.summary.tasks_total === 0 && (
            <div class="dash-empty">No data for this period.</div>
          )}
        </>
      )}
    </div>
  )
}

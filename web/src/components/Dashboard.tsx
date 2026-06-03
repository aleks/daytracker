import { useEffect, useMemo, useState } from 'preact/hooks'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { api } from '../api'
import type { SlowestItem, StatsDayBucket, StatsResponse, VelocityResponse } from '../types'

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
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC',
  })
}

function formatMonth(s: string): string {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    month: 'short', year: 'numeric', timeZone: 'UTC',
  })
}

function formatWeek(s: string): string {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    month: 'short', day: 'numeric', timeZone: 'UTC',
  })
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
}

function aggregateTimeline(timeline: StatsDayBucket[], from: string, to: string): Bucket[] {
  if (timeline.length === 0) return []

  const first = from || timeline[0].date
  const last = to || timeline[timeline.length - 1].date
  const [fy, fm, fd] = first.split('-').map(Number)
  const [ly, lm, ld] = last.split('-').map(Number)
  const days = Math.round(
    (Date.UTC(ly, lm - 1, ld) - Date.UTC(fy, fm - 1, fd)) / 86400000,
  ) + 1

  const byDate: Record<string, StatsDayBucket> = {}
  for (const b of timeline) byDate[b.date] = b

  if (days <= 60) {
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
      })
      cur = addDays(cur, 1)
    }
    return buckets
  }

  if (days <= 365) {
    const bucketMap: Record<string, Bucket> = {}
    let cur = first
    while (cur <= last) {
      const weekStart = startOfWeek(cur)
      if (!bucketMap[weekStart]) {
        bucketMap[weekStart] = { label: weekStart, tasks_done: 0, github: 0, jira: 0, confluence: 0 }
      }
      const b = byDate[cur]
      if (b) {
        bucketMap[weekStart].tasks_done += b.tasks_done
        bucketMap[weekStart].github += b.github
        bucketMap[weekStart].jira += b.jira
        bucketMap[weekStart].confluence += b.confluence
      }
      cur = addDays(cur, 1)
    }
    return Object.keys(bucketMap).sort().map(k => bucketMap[k])
  }

  const bucketMap: Record<string, Bucket> = {}
  for (const b of timeline) {
    const month = b.date.slice(0, 7)
    if (!bucketMap[month]) {
      bucketMap[month] = { label: month + '-01', tasks_done: 0, github: 0, jira: 0, confluence: 0 }
    }
    bucketMap[month].tasks_done += b.tasks_done
    bucketMap[month].github += b.github
    bucketMap[month].jira += b.jira
    bucketMap[month].confluence += b.confluence
  }
  return Object.keys(bucketMap).sort().map(k => bucketMap[k])
}

// ── Chart colors (match CSS variables — duplicated here for Recharts) ─────────

const COLORS = {
  github:     '#24292e',
  jira:       '#0052cc',
  confluence: '#0065ff',
  tasks:      '#2563eb',
}

const COLORS_DARK = {
  github:     '#c9d1d9',
  jira:       '#4c9aff',
  confluence: '#4c9aff',
  tasks:      '#3b82f6',
}

function useChartColors() {
  const dark = document.documentElement.getAttribute('data-theme') === 'dark'
  return dark ? COLORS_DARK : COLORS
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

function ActivityChart({ buckets, granularity }: { buckets: Bucket[]; granularity: 'day' | 'week' | 'month' }) {
  const colors = useChartColors()

  function tickFormatter(label: string): string {
    if (granularity === 'month') return formatMonth(label)
    if (granularity === 'week')  return formatWeek(label)
    return label.slice(5) // MM-DD
  }

  function tooltipLabel(label: unknown): string {
    const s = String(label)
    if (granularity === 'month') return formatMonth(s)
    if (granularity === 'week')  return `Week of ${formatDate(s)}`
    return formatDate(s)
  }

  // Thin out x-axis ticks to avoid crowding — at most ~12 visible labels.
  const interval = Math.max(0, Math.ceil(buckets.length / 12) - 1)

  return (
    <div class="barchart-wrap">
      <ResponsiveContainer width="100%" height={240}>
        <BarChart data={buckets} margin={{ top: 4, right: 8, left: -16, bottom: 0 }} barCategoryGap="20%">
          <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" vertical={false} />
          <XAxis
            dataKey="label"
            tickFormatter={tickFormatter}
            interval={interval}
            tick={{ fontSize: 11, fill: 'var(--color-text-muted)' }}
            axisLine={false}
            tickLine={false}
          />
          <YAxis
            allowDecimals={false}
            tick={{ fontSize: 11, fill: 'var(--color-text-muted)' }}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip
            labelFormatter={tooltipLabel}
            contentStyle={{
              background: 'var(--color-surface)',
              border: '1px solid var(--color-border)',
              borderRadius: '6px',
              fontSize: '12px',
            }}
            labelStyle={{ color: 'var(--color-text)', fontWeight: 600, marginBottom: 4 }}
            itemStyle={{ color: 'var(--color-text)' }}
            cursor={{ fill: 'var(--color-border)', opacity: 0.5 }}
          />
          <Legend
            wrapperStyle={{ fontSize: '12px', paddingTop: '12px', color: 'var(--color-text-muted)' }}
          />
          <Bar dataKey="github"     name="GitHub"     stackId="a" fill={colors.github}     radius={[0, 0, 0, 0]} />
          <Bar dataKey="jira"       name="Jira"       stackId="a" fill={colors.jira}       radius={[0, 0, 0, 0]} />
          <Bar dataKey="confluence" name="Confluence" stackId="a" fill={colors.confluence} radius={[0, 0, 0, 0]} />
          <Bar dataKey="tasks_done" name="Tasks done" stackId="a" fill={colors.tasks}      radius={[3, 3, 0, 0]} />
        </BarChart>
      </ResponsiveContainer>
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

// ── Velocity section ──────────────────────────────────────────────────────────

const SOURCE_LABELS: Record<string, string> = {
  github: 'GitHub PR',
  jira: 'Jira',
  task: 'Task',
}

function VelocitySection({ velocity }: { velocity: VelocityResponse }) {
  const metrics = [
    { label: 'Avg. days to merge PR', metric: velocity.github_authored },
    { label: 'Avg. days to close Jira ticket', metric: velocity.jira },
    { label: 'Avg. days to complete task', metric: velocity.tasks },
  ].filter(m => m.metric.sample_size > 0)

  const hasSlowest = velocity.slowest.length > 0

  if (metrics.length === 0 && !hasSlowest) return null

  return (
    <div class="dash-section">
      <div class="dash-section-title">Velocity</div>
      <p class="dash-velocity-note">
        Measures calendar days from first appearance to completion. Only includes items that were resolved within the selected period. Items synced before daytracker was set up may show shorter durations than reality.
      </p>

      {metrics.length > 0 && (
        <div class="dash-cards dash-cards--velocity">
          {metrics.map(({ label, metric }) => (
            <div key={label} class="stat-card">
              <div class="stat-card-value">{metric.avg_days.toFixed(1)}d</div>
              <div class="stat-card-label">{label}</div>
              <div class="stat-card-sub">{metric.sample_size} completed</div>
            </div>
          ))}
        </div>
      )}

      {hasSlowest && (
        <div class="dash-slowest">
          <div class="dash-slowest-title">Slowest 10 completed items</div>
          <div class="dash-slowest-list">
            {velocity.slowest.map((item, i) => (
              <SlowestRow key={item.external_id + item.source} rank={i + 1} item={item} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function SlowestRow({ rank, item }: { rank: number; item: SlowestItem }) {
  const label = SOURCE_LABELS[item.source] ?? item.source
  const days = item.days === 0 ? 'same day' : `${item.days}d`
  return (
    <div class="dash-slowest-row">
      <span class="dash-slowest-rank">#{rank}</span>
      <span class={`dash-slowest-badge dash-slowest-badge--${item.source}`}>{label}</span>
      <span class="dash-slowest-title-text">
        {item.url
          ? <a href={item.url} target="_blank" rel="noopener noreferrer">{item.title}</a>
          : item.title}
      </span>
      <span class="dash-slowest-days">{days}</span>
    </div>
  )
}

// ── Main component ────────────────────────────────────────────────────────────

export function Dashboard() {
  const [preset, setPreset] = useState<Preset>('month')
  const [customFrom, setCustomFrom] = useState('')
  const [customTo, setCustomTo] = useState('')
  const [usingCustom, setUsingCustom] = useState(false)
  const [mode, setMode] = useState<'unique' | 'raw'>('unique')
  const [data, setData] = useState<StatsResponse | null>(null)
  const [velocity, setVelocity] = useState<VelocityResponse | null>(null)
  const [loading, setLoading] = useState(false)

  const { from, to } = usingCustom
    ? { from: customFrom, to: customTo }
    : presetRange(preset)

  useEffect(() => {
    setLoading(true)
    Promise.all([
      api.getStats(from || undefined, to || undefined, mode),
      api.getVelocity(from || undefined, to || undefined),
    ])
      .then(([stats, vel]) => { setData(stats); setVelocity(vel) })
      .catch(console.error)
      .finally(() => setLoading(false))
  }, [from, to, mode])

  const buckets = useMemo(() => {
    if (!data) return []
    return aggregateTimeline(data.timeline, from, to)
  }, [data, from, to])

  const granularity: 'day' | 'week' | 'month' = useMemo(() => {
    if (buckets.length === 0) return 'day'
    if (buckets.length > 365 / 30) return 'month'
    if (buckets.length > 60) return 'week'
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
    { key: 'week',  label: 'This week' },
    { key: 'month', label: 'This month' },
    { key: 'year',  label: 'This year' },
    { key: 'all',   label: 'All time' },
  ]

  return (
    <div class="dashboard">
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
        <div class="dash-mode-toggle">
          <button
            class={`dash-mode-btn${mode === 'unique' ? ' dash-mode-btn--active' : ''}`}
            onClick={() => setMode('unique')}
            title="Count each item once, using its latest state"
          >
            Unique
          </button>
          <button
            class={`dash-mode-btn${mode === 'raw' ? ' dash-mode-btn--active' : ''}`}
            onClick={() => setMode('raw')}
            title="Count every row including carry-forward duplicates"
          >
            All appearances
          </button>
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
          <div class="dash-cards">
            <StatCard
              label="Tasks completed"
              value={data.summary.tasks_done}
              sub={data.summary.tasks_total > 0
                ? `${Math.round(data.summary.tasks_done / data.summary.tasks_total * 100)}% of ${data.summary.tasks_total}`
                : undefined}
            />
            <StatCard label="PRs authored"      value={data.github.authored_total}  sub={`${data.github.authored_merged} merged`} />
            <StatCard label="PRs reviewed"      value={data.github.reviewed_total}  sub={`${data.github.reviewed_merged} merged`} />
            <StatCard label="Jira tickets"      value={data.jira.total}             sub={`${data.jira.done} done`} />
            <StatCard label="Confluence pages"  value={data.confluence.total}       sub={`${data.confluence.created} created`} />
            <StatCard label="Active days"       value={data.period.active_days} />
          </div>

          {buckets.length > 0 && (
            <div class="dash-section">
              <div class="dash-section-title">Activity over time</div>
              <div class="dash-chart-card">
                <ActivityChart buckets={buckets} granularity={granularity} />
              </div>
            </div>
          )}

          <div class="dash-section">
            <div class="dash-section-title">Breakdown by source</div>
            <div class="dash-breakdowns">
              <SourceBlock
                title="GitHub — Authored PRs"
                rows={[
                  { label: 'Merged',            value: data.github.authored_merged,            highlight: true },
                  { label: 'Open',              value: data.github.authored_open },
                  { label: 'Approved',          value: data.github.authored_approved },
                  { label: 'In review',         value: data.github.authored_in_review },
                  { label: 'Changes requested', value: data.github.authored_changes_requested },
                  { label: 'Draft',             value: data.github.authored_draft },
                  { label: 'Closed',            value: data.github.authored_closed },
                ]}
              />
              <SourceBlock
                title="GitHub — Reviewed PRs"
                rows={[
                  { label: 'Merged', value: data.github.reviewed_merged, highlight: true },
                  { label: 'Open',   value: data.github.reviewed_open },
                  { label: 'Draft',  value: data.github.reviewed_draft },
                  { label: 'Closed', value: data.github.reviewed_closed },
                ]}
              />
              <SourceBlock
                title="Jira"
                rows={[
                  { label: 'Done',        value: data.jira.done,        highlight: true },
                  { label: 'In progress', value: data.jira.in_progress },
                  { label: 'To do',       value: data.jira.todo },
                ]}
              />
              <SourceBlock
                title="Confluence"
                rows={[
                  { label: 'Pages created', value: data.confluence.created, highlight: true },
                  { label: 'Pages edited',  value: data.confluence.edited },
                ]}
              />
            </div>
          </div>

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

          {velocity && <VelocitySection velocity={velocity} />}

          {data.summary.activities_total === 0 && data.summary.tasks_total === 0 && (
            <div class="dash-empty">No data for this period.</div>
          )}
        </>
      )}
    </div>
  )
}

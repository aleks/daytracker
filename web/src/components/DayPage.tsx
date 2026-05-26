import { useEffect, useLayoutEffect, useState } from 'preact/hooks'
import { api } from '../api'
import type { ActivityItem, DayDetail, Task } from '../types'
import { ActivityList } from './ActivityList'
import { TaskList } from './TaskList'

const SOURCE_ORDER = ['github', 'jira', 'confluence'] as const
const SOURCE_LABELS: Record<string, string> = {
  github: 'GitHub',
  jira: 'Jira',
  confluence: 'Confluence',
}

interface Props {
  date: string
  isToday?: boolean
  onTodayChanged?: () => void
  onNavigate?: (date: string) => void
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  return d.toLocaleDateString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })
}

function offsetDate(dateStr: string, days: number): string {
  const d = new Date(dateStr + 'T00:00:00')
  d.setDate(d.getDate() + days)
  const mm = (d.getMonth() + 1).toString().padStart(2, '0')
  const dd = d.getDate().toString().padStart(2, '0')
  return `${d.getFullYear()}-${mm}-${dd}`
}

export function DayPage({ date, isToday, onTodayChanged, onNavigate }: Props) {
  const [detail, setDetail] = useState<DayDetail | null>(null)
  const [error, setError] = useState<string | null>(null)

  useLayoutEffect(() => {
    document.title = `${formatDate(date)} — Daytracker`
  }, [date])

  useEffect(() => {
    api.getDay(date)
      .then(setDetail)
      .catch(err => setError(err.message))
  }, [date])

  const handleTasksChanged = (tasks: Task[]) => {
    if (!detail) return
    setDetail({ ...detail, tasks })
  }

  return (
    <section class="day-page">
      <div class="day-heading-row">
        <div class="day-nav-group">
          <button class="day-nav" onClick={() => onNavigate?.(offsetDate(date, -1))} aria-label="Previous day">‹</button>
          {!isToday && (
            <button class="day-nav day-nav--today" onClick={() => onNavigate?.(new Date().toISOString().slice(0, 10))}>today</button>
          )}
          <button class="day-nav" onClick={() => onNavigate?.(offsetDate(date, 1))} aria-label="Next day">›</button>
        </div>
        <h2 class={`day-heading${isToday ? ' day-heading--today' : ''}`}>{formatDate(date)}</h2>
      </div>

      {error && <p class="error-message">{error}</p>}

      {detail && (
        <div class="day-body">
          <div class="day-section">
            <h3 class="section-heading">Tasks</h3>
            <TaskList
              date={date}
              tasks={detail.tasks}
              onChanged={handleTasksChanged}
              onCopyToToday={!isToday ? async (title) => {
                const today = new Date().toISOString().slice(0, 10)
                await api.createTask(today, title)
                onTodayChanged?.()
              } : undefined}
            />
          </div>

          {(() => {
            const bySource = detail.activities.reduce<Record<string, ActivityItem[]>>((acc, item) => {
              ;(acc[item.source] ??= []).push(item)
              return acc
            }, {})
            const sources = [
              ...SOURCE_ORDER.filter(s => bySource[s]),
              ...Object.keys(bySource).filter(s => !SOURCE_ORDER.includes(s as typeof SOURCE_ORDER[number])),
            ]
            if (sources.length === 0) return null
            return sources.map(source => (
              <div key={source} class="day-section">
                <h3 class="section-heading">{SOURCE_LABELS[source] ?? source}</h3>
                <ActivityList activities={bySource[source]} source={source} />
              </div>
            ))
          })()}
        </div>
      )}
    </section>
  )
}

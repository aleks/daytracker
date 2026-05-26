import { useEffect, useState } from 'preact/hooks'
import { api } from '../api'
import type { DayDetail, Task } from '../types'
import { ActivityList } from './ActivityList'
import { TaskList } from './TaskList'

interface Props {
  date: string
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  return d.toLocaleDateString(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })
}

export function DayPage({ date }: Props) {
  const [detail, setDetail] = useState<DayDetail | null>(null)
  const [error, setError] = useState<string | null>(null)

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
      <h2 class="day-heading">{formatDate(date)}</h2>

      {error && <p class="error-message">{error}</p>}

      {detail && (
        <div class="day-body">
          <div class="day-section">
            <h3 class="section-heading">Tasks</h3>
            <TaskList date={date} tasks={detail.tasks} onChanged={handleTasksChanged} />
          </div>

          <div class="day-section">
            <h3 class="section-heading">Activity</h3>
            <ActivityList activities={detail.activities} />
          </div>
        </div>
      )}
    </section>
  )
}

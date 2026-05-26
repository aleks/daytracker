import { useEffect, useState } from 'preact/hooks'
import { api } from './api'
import { ConnectorStatus } from './components/ConnectorStatus'
import { DayPage } from './components/DayPage'

function todayStr(): string {
  return new Date().toISOString().slice(0, 10)
}

export function App() {
  const [dates, setDates] = useState<string[]>([todayStr()])
  const [todayRefresh, setTodayRefresh] = useState(0)

  useEffect(() => {
    api.listDays()
      .then(days => {
        const today = todayStr()
        const pastDates = days
          .map(d => d.date.slice(0, 10))
          .filter(d => d !== today)
        setDates([today, ...pastDates])
      })
      .catch(console.error)
  }, [])

  const today = todayStr()

  return (
    <div class="app">
      <header class="app-header">
        <h1 class="app-title">Daytracker</h1>
        <ConnectorStatus />
      </header>

      <main class="app-main">
        {dates.map(date => (
          <DayPage
            key={date === today ? `${date}-${todayRefresh}` : date}
            date={date}
            isToday={date === today}
            onTodayChanged={() => setTodayRefresh(r => r + 1)}
          />
        ))}
      </main>
    </div>
  )
}

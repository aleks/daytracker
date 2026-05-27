import { useEffect, useState } from 'preact/hooks'
import { api } from './api'
import { Calendar } from './components/Calendar'
import { ConnectorStatus } from './components/ConnectorStatus'
import { DayPage } from './components/DayPage'
import { localToday } from './date'

function initTheme(): 'light' | 'dark' {
  const stored = localStorage.getItem('theme')
  if (stored === 'dark' || stored === 'light') return stored
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function App() {
  const today = localToday()
  const [selectedDate, setSelectedDate] = useState(today)
  const [activeDates, setActiveDates] = useState<Set<string>>(new Set())
  const [refreshKey, setRefreshKey] = useState(0)
  const [theme, setTheme] = useState<'light' | 'dark'>(initTheme)

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    localStorage.setItem('theme', theme)
  }, [theme])

  const toggleTheme = () => setTheme(t => t === 'light' ? 'dark' : 'light')

  const initialYear = parseInt(today.slice(0, 4), 10)
  const initialMonth = parseInt(today.slice(5, 7), 10)
  const [calYear, setCalYear] = useState(initialYear)
  const [calMonth, setCalMonth] = useState(initialMonth)

  useEffect(() => {
    api.listDays()
      .then(days => {
        setActiveDates(new Set(days.map(d => d.date.slice(0, 10))))
      })
      .catch(console.error)
  }, [refreshKey])

  const handleSelectDate = (date: string) => {
    setSelectedDate(date)
    // Navigate calendar to the selected date's month
    const y = parseInt(date.slice(0, 4), 10)
    const m = parseInt(date.slice(5, 7), 10)
    setCalYear(y)
    setCalMonth(m)
  }

  const handleDayChanged = () => {
    setRefreshKey(k => k + 1)
  }

  return (
    <div class="app">
      <aside class="sidebar">
        <div class="sidebar-top">
          <span class="app-title">Daytracker</span>
          <button class="theme-toggle-btn" onClick={toggleTheme} title="Toggle theme">
            {theme === 'dark' ? '☀︎' : '☾'}
          </button>
        </div>

        <Calendar
          year={calYear}
          month={calMonth}
          activeDates={activeDates}
          selectedDate={selectedDate}
          onSelectDate={handleSelectDate}
          onMonthChange={(y, m) => { setCalYear(y); setCalMonth(m) }}
        />

        <div class="sidebar-connectors">
          <ConnectorStatus />
        </div>
      </aside>

      <main class="main-panel">
        <DayPage
          key={`${selectedDate}-${refreshKey}`}
          date={selectedDate}
          isToday={selectedDate === today}
          onTodayChanged={handleDayChanged}
          onNavigate={handleSelectDate}
        />
      </main>
    </div>
  )
}

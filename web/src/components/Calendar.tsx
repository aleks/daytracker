interface Props {
  year: number
  month: number // 1-12
  activeDates: Set<string>
  selectedDate: string
  onSelectDate: (date: string) => void
  onMonthChange: (year: number, month: number) => void
}

const DAY_NAMES = ['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su']

function padDate(n: number): string {
  return n.toString().padStart(2, '0')
}

function toDateStr(year: number, month: number, day: number): string {
  return `${year}-${padDate(month)}-${padDate(day)}`
}

function daysInMonth(year: number, month: number): number {
  return new Date(year, month, 0).getDate()
}

// Monday-based: Monday = 0, Sunday = 6
function firstWeekdayOfMonth(year: number, month: number): number {
  const day = new Date(year, month - 1, 1).getDay() // 0=Sun
  return (day + 6) % 7
}

function prevMonth(year: number, month: number): [number, number] {
  return month === 1 ? [year - 1, 12] : [year, month - 1]
}

function nextMonth(year: number, month: number): [number, number] {
  return month === 12 ? [year + 1, 1] : [year, month + 1]
}

const MONTH_NAMES = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December',
]

export function Calendar({ year, month, activeDates, selectedDate, onSelectDate, onMonthChange }: Props) {
  const totalDays = daysInMonth(year, month)
  const startOffset = firstWeekdayOfMonth(year, month)

  // Build a flat array of cells: null = empty padding, number = day of month
  const cells: (number | null)[] = [
    ...Array(startOffset).fill(null),
    ...Array.from({ length: totalDays }, (_, i) => i + 1),
  ]
  // Pad to complete the last row
  while (cells.length % 7 !== 0) cells.push(null)

  const today = new Date().toISOString().slice(0, 10)
  const todayYear = parseInt(today.slice(0, 4), 10)
  const todayMonth = parseInt(today.slice(5, 7), 10)
  const isCurrentMonth = year === todayYear && month === todayMonth

  const [py, pm] = prevMonth(year, month)
  const [ny, nm] = nextMonth(year, month)

  const goToToday = () => {
    onSelectDate(today)
    onMonthChange(todayYear, todayMonth)
  }

  return (
    <div class="cal">
      <div class="cal-header">
        <button class="cal-nav" onClick={() => onMonthChange(py, pm)} aria-label="Previous month">‹</button>
        <span class="cal-title">{MONTH_NAMES[month - 1]} {year}</span>
        <button class="cal-nav" onClick={() => onMonthChange(ny, nm)} aria-label="Next month">›</button>
      </div>

      <div class="cal-grid">
        {DAY_NAMES.map(d => (
          <span key={d} class="cal-weekday">{d}</span>
        ))}

        {cells.map((day, i) => {
          if (day === null) return <span key={`pad-${i}`} />

          const dateStr = toDateStr(year, month, day)
          const isSelected = dateStr === selectedDate
          const isToday = dateStr === today
          const hasData = activeDates.has(dateStr)

          return (
            <button
              key={dateStr}
              class={[
                'cal-day',
                isSelected ? 'cal-day--selected' : '',
                isToday ? 'cal-day--today' : '',
              ].join(' ').trim()}
              onClick={() => onSelectDate(dateStr)}
              aria-label={dateStr}
              aria-pressed={isSelected}
            >
              <span class="cal-day-num">{day}</span>
              {hasData && <span class="cal-dot" aria-hidden />}
            </button>
          )
        })}
      </div>

      {!isCurrentMonth && (
        <div class="cal-today-row">
          <button class="cal-today-btn" onClick={goToToday}>Jump to today</button>
        </div>
      )}
    </div>
  )
}

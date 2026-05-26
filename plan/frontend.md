# Daytracker — Frontend Design

## Stack

| Concern | Choice |
|---------|--------|
| Framework | Preact 10 |
| Build tool | Vite 5 |
| Language | TypeScript (strict mode) |
| Styling | Plain CSS with CSS custom properties |
| State | Preact hooks + Context API |
| HTTP | Native `fetch` wrapped in `src/api.ts` |

No component library. No CSS-in-JS. No state management library. The app is a single-user tool; simplicity wins over ecosystem completeness.

---

## Project Bootstrap

```
web/
├── index.html
├── vite.config.ts
├── tsconfig.json
├── package.json
└── src/
    ├── main.tsx
    ├── App.tsx
    ├── api.ts
    ├── types.ts
    ├── components/
    │   ├── DayPage.tsx
    │   ├── TaskList.tsx
    │   ├── ActivityList.tsx
    │   └── ConnectorStatus.tsx
    └── styles/
        └── main.css
```

`vite.config.ts` proxies `/api` to `http://localhost:8080` during development:

```ts
import { defineConfig } from 'vite'
import preact from '@preact/preset-vite'

export default defineConfig({
  plugins: [preact()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
```

---

## Type Definitions (`src/types.ts`)

```ts
export interface Task {
  id: number
  day_id: number
  title: string
  done: boolean
  created_at: string
}

export interface ActivityItem {
  id: number
  day_id: number
  source: string        // "github" | "jira" | "confluence"
  external_id: string
  kind: string          // "pr" | "review" | "jira_issue" | "confluence_page" | "confluence_comment"
  title: string
  url: string
  metadata: Record<string, unknown>
  fetched_at: string
}

export interface DaySummary {
  date: string          // "YYYY-MM-DD"
  task_count: number
  activity_count: number
}

export interface DayDetail {
  date: string
  tasks: Task[]
  activities: ActivityItem[]
}

export interface ConnectorInfo {
  name: string
  configured: boolean
  last_sync_at: string | null
  last_error: string
}
```

---

## API Client (`src/api.ts`)

Thin wrapper — no axios, no react-query. Each function throws on non-2xx responses.

```ts
const BASE = '/api'

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `HTTP ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  listDays: (limit = 30) =>
    request<DaySummary[]>(`/days?limit=${limit}`),

  getDay: (date: string) =>
    request<DayDetail>(`/days/${date}`),

  createTask: (date: string, title: string) =>
    request<Task>(`/days/${date}/tasks`, {
      method: 'POST',
      body: JSON.stringify({ title }),
    }),

  updateTask: (id: number, patch: Partial<Pick<Task, 'done'>>) =>
    request<Task>(`/tasks/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),

  deleteTask: (id: number) =>
    request<void>(`/tasks/${id}`, { method: 'DELETE' }),

  listConnectors: () =>
    request<ConnectorInfo[]>('/connectors'),

  triggerSync: (name: string) =>
    request<{ message: string }>(`/connectors/${name}/sync`, { method: 'POST' }),
}
```

---

## Component Tree

```
<App>
  <ConnectorStatus />
  <main class="day-feed">
    <DayPage date="2026-05-26" />
    <DayPage date="2026-05-25" />
    ...
  </main>
</App>
```

---

### `<App>` (`src/App.tsx`)

Responsibilities:
- On mount, fetches `GET /api/days` to get the list of dates with data.
- Always prepends today's date to the list (so today is always shown even with no data).
- Renders a vertically scrolling list of `<DayPage>` components.
- Provides a `ConnectorsContext` (list of `ConnectorInfo`) via Context so child components can read connector state without prop drilling.
- Polls `GET /api/days` every 60 seconds to pick up new days added by the worker.

State:
```ts
const [days, setDays] = useState<string[]>([])      // "YYYY-MM-DD" strings
const [loading, setLoading] = useState(true)
const [error, setError] = useState<string | null>(null)
```

---

### `<DayPage date>` (`src/components/DayPage.tsx`)

Responsibilities:
- Fetches `GET /api/days/:date` on mount.
- Passes `tasks` to `<TaskList>` and `activities` to `<ActivityList>`.
- Refreshes data every 30 seconds while visible (simple `setInterval`; no IntersectionObserver needed at this scale).
- Displays a date heading formatted as `Monday, 26 May 2026` using `Intl.DateTimeFormat`.

Props:
```ts
interface DayPageProps {
  date: string  // "YYYY-MM-DD"
}
```

State:
```ts
const [detail, setDetail] = useState<DayDetail | null>(null)
const [loading, setLoading] = useState(true)
```

---

### `<TaskList>` (`src/components/TaskList.tsx`)

Responsibilities:
- Renders an inline "add task" input at the top. On Enter or clicking the add button, calls `api.createTask(date, title)` and refreshes.
- Renders each task as a row with: checkbox (toggle done), title (strikethrough when done), delete button.
- Optimistic updates for toggle and delete — update local state immediately, revert on error.

Props:
```ts
interface TaskListProps {
  date: string
  tasks: Task[]
  onTasksChanged: () => void   // triggers parent re-fetch
}
```

Markup sketch:
```
[ + Add a task... ]
[✓] Buy coffee         [×]
[ ] Write API doc      [×]
[✓] Review PR #42      [×]
```

Done tasks appear below undone tasks (sort in the component, not on the server).

---

### `<ActivityList>` (`src/components/ActivityList.tsx`)

Responsibilities:
- Receives `activities: ActivityItem[]` as a prop.
- Groups items by `source` in this order: `github`, `jira`, `confluence`, then any unknown sources alphabetically.
- Renders each group with a heading (source name + count) and a colored left-border indicator per source.
- Each item is a card showing: icon (text emoji or SVG), title as a link (`<a href={url} target="_blank">`), kind badge (e.g. "PR", "Review"), and relative time from `fetched_at`.
- If `activities` is empty, renders a subtle "No activity yet" placeholder.

Source color mapping (CSS variables):
```
github      → --color-github:     #24292f
jira        → --color-jira:       #0052cc
confluence  → --color-confluence: #172b4d
```

Props:
```ts
interface ActivityListProps {
  activities: ActivityItem[]
}
```

---

### `<ConnectorStatus>` (`src/components/ConnectorStatus.tsx`)

Responsibilities:
- Reads `ConnectorInfo[]` from `ConnectorsContext`.
- Renders a compact status bar at the top of the page showing each configured connector, its last sync time, and a "Sync now" button.
- Clicking "Sync now" calls `api.triggerSync(name)` and shows a brief "syncing…" label.
- Unconfigured connectors are shown in a muted style with "not configured" text.

---

## State Management

No Redux, no Zustand, no Jotai. Two patterns cover all needs at this scale:

1. **Local state** — each component owns its own `useState` / `useReducer`. The `<DayPage>` owns its `DayDetail` and passes it down as props.
2. **Context** — a single `ConnectorsContext` at the `<App>` level holds `ConnectorInfo[]` so `<ConnectorStatus>` and any future component can read it without prop drilling.

```ts
// src/context.ts
import { createContext } from 'preact'
import { ConnectorInfo } from './types'

export const ConnectorsContext = createContext<ConnectorInfo[]>([])
```

---

## Styling (`src/styles/main.css`)

CSS custom properties defined on `:root`:

```css
:root {
  --font-sans: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  --color-bg: #ffffff;
  --color-surface: #f8f9fa;
  --color-border: #e1e4e8;
  --color-text: #24292f;
  --color-text-muted: #6e7781;
  --color-accent: #0969da;
  --color-done: #8c959f;
  --color-github: #24292f;
  --color-jira: #0052cc;
  --color-confluence: #172b4d;
  --radius: 6px;
  --spacing-sm: 8px;
  --spacing-md: 16px;
  --spacing-lg: 32px;
}
```

Layout rules:
- `body`: `font-family: var(--font-sans)`, `background: var(--color-bg)`, `color: var(--color-text)`, `max-width: 860px`, `margin: 0 auto`, `padding: var(--spacing-lg)`.
- Day section: separated by a `1px solid var(--color-border)` divider with `margin-bottom: var(--spacing-lg)`.
- Day heading (`h2`): `font-size: 1.1rem`, `color: var(--color-text-muted)`, `font-weight: 600`, `margin-bottom: var(--spacing-md)`.
- Activity card: `background: var(--color-surface)`, `border-left: 3px solid <source-color>`, `border-radius: var(--radius)`, `padding: var(--spacing-sm) var(--spacing-md)`, `margin-bottom: var(--spacing-sm)`.
- Task row: `display: flex`, `align-items: center`, `gap: var(--spacing-sm)`, `padding: 6px 0`.
- Done task title: `text-decoration: line-through`, `color: var(--color-done)`.

No mobile-specific breakpoints in the initial version — the single-column layout works on narrow screens naturally.

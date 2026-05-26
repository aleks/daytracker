# Daytracker — Agent Reference

## Overview

Daytracker is a single-binary Go server that embeds a Preact SPA. It syncs activity (PRs, Jira issues, Confluence pages) from external services into a local SQLite database and presents a day-by-day view alongside manually-created tasks.

## Repository layout

```
cmd/server/      — main entrypoint; wires db, connectors, worker, API
cmd/check/       — CLI health-check tool (reads .env, pings configured connectors)
internal/
  api/           — Gin HTTP handlers (days, tasks, connectors)
  backup/        — markdown snapshot writer
  connector/     — Connector interface + GitHub, Jira, Confluence implementations
  db/            — GORM models and database init
  worker/        — background sync loop
web/             — Preact + TypeScript frontend (Vite)
  src/
    components/  — ActivityList, Calendar, ConnectorStatus, DayPage, TaskList
    styles/      — main.css (single file, CSS custom properties)
    api.ts       — typed fetch wrapper
    types.ts     — shared TypeScript types
plan/            — design notes; useful for historical context but not authoritative
```

## Build and run

```bash
make build       # builds web/ then compiles Go binary → ./daytracker
make dev-api     # go run -tags dev ./cmd/server  (serves web/ via Vite proxy)
make dev-web     # cd web && npm run dev
make check       # go run -tags dev ./cmd/check   (connector health check)
go test ./...    # run all Go tests
```

The `-tags dev` build tag switches the file server to a reverse proxy to Vite (`web_dev.go`) instead of serving the embedded dist (`web.go`).

## Environment variables

Defined in `.env` (loaded by the server at startup). See `.env.example` for the full list.

| Variable | Purpose |
|---|---|
| `DAYTRACKER_GITHUB_TOKEN` | GitHub PAT (scopes: `repo`, `read:user`) |
| `DAYTRACKER_JIRA_BASE_URL` | e.g. `https://your-org.atlassian.net` |
| `DAYTRACKER_JIRA_EMAIL` | Atlassian account email |
| `DAYTRACKER_JIRA_TOKEN` | Atlassian API token |
| `DAYTRACKER_CONFLUENCE_BASE_URL` | e.g. `https://your-org.atlassian.net/wiki` |
| `DAYTRACKER_CONFLUENCE_EMAIL` | Atlassian account email |
| `DAYTRACKER_CONFLUENCE_TOKEN` | Atlassian API token |
| `DAYTRACKER_BACKUP_DIR` | Absolute path for markdown backups (optional) |
| `DAYTRACKER_SYNC_INTERVAL` | Connector sync cadence (default `15m`) |
| `DAYTRACKER_STATUS_REFRESH_INTERVAL` | PR status refresh cadence (default `5m`) |
| `DAYTRACKER_BACKFILL_DAYS` | Days to backfill on startup (default `14`) |

If no `.env` file is present the server reads from the process environment directly.

## Database models (`internal/db/models.go`)

- **`Day`** — one row per calendar date (`date` unique index).
- **`Task`** — belongs to a `Day`; has `title` and `done`.
- **`ActivityItem`** — a fetched event. Unique on `(source, external_id, day_id)` — same-day re-syncs update in place; cross-day syncs create a new row, preserving history.
- **`ConnectorState`** — tracks `last_sync_at` and `last_error` per connector name.

The composite unique index on `ActivityItem` is critical: do not change it to `(source, external_id)` as that would clobber cross-day history.

## Connector pattern (`internal/connector/`)

### Interface

```go
type Connector interface {
    Name() string
    IsConfigured() bool
    Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error)
}
```

- `Name()` returns the lowercase source string stored in `ActivityItem.Source` (e.g. `"github"`).
- `IsConfigured()` returns false if required env vars are missing; the worker skips unconfigured connectors silently.
- `Fetch()` returns items for the given UTC date. Items must have `Source`, `ExternalID`, `Kind`, and `Title` set. `URL` and `Metadata` are optional.

### Optional: `StatusRefresher`

```go
type StatusRefresher interface {
    RefreshStatuses(ctx context.Context, items []PRStatusItem) ([]PRStatusUpdate, error)
}
```

Implement this when a connector has stateful items whose state can change after the fetch date (e.g. a PR going from open → merged). The worker calls `RefreshStatuses` every `DAYTRACKER_STATUS_REFRESH_INTERVAL` for all items within the backfill window that are not in a terminal state.

The worker updates rows **by primary key** (`id`), not by `(source, external_id)`, so that only the specific day's row is mutated and other days' history is preserved.

### Adding a new connector

1. Create `internal/connector/<name>.go`.
2. Implement `Connector` (and optionally `StatusRefresher`).
3. Read credentials from env vars in the constructor (`NewXxx()`).
4. Resolve any lazy credentials (cloud IDs, user IDs) via `sync.Once` on first use.
5. Register it in `cmd/server/main.go`: `registry.Register(connector.NewXxx())`.
6. Add env vars to `.env.example`.
7. Add the source name to `SOURCE_ORDER` and `SOURCE_LABELS` in `web/src/components/DayPage.tsx`.

### Kind naming convention

Kinds are `<source>_<state>` strings stored in `ActivityItem.Kind`. They drive both the UI badge and the backup markdown label. Examples:

- GitHub: `authored_open`, `authored_merged`, `authored_closed`, `authored_draft`, `reviewed_open`, `reviewed_merged`, `reviewed_approved`, `reviewed_changes_requested`
- Jira: `jira_todo`, `jira_in_progress`, `jira_done`
- Confluence: `confluence_created`, `confluence_edited`

When adding a new kind, also update `kindLabel()` in `internal/backup/backup.go` and the badge mapping in `web/src/components/ActivityList.tsx`.

## Worker (`internal/worker/worker.go`)

The worker runs a set of tickers in a single goroutine:

| Ticker | Default | What it does |
|---|---|---|
| `syncTicker` | `15m` | Runs `syncAll` for today |
| `refreshTicker` | `5m` | Runs `refreshAllStatuses` across the backfill window |
| `backupTicker` | `2m` | Writes today's markdown backup (picks up task edits) |

On startup it runs `runBackfill` synchronously (fetches the last N days) before starting tickers. A sync can also be triggered immediately via `TriggerChan()` (used by the `POST /api/connectors/:name/sync` endpoint).

`syncOne` uses a GORM `OnConflict` upsert: matching on `(source, external_id, day_id)`, updating `title`, `url`, `kind`, `metadata`, `fetched_at`.

## Backup (`internal/backup/backup.go`)

`WriteDay(ctx, date)` writes `<DAYTRACKER_BACKUP_DIR>/YYYY/MM/DD.md`. It is idempotent — calling it multiple times overwrites with the latest state. Days with no tasks and no activity are skipped.

URLs embedded in task titles are extracted and rendered as `[Open link](url)` markdown links so raw URLs don't clutter the file.

## API (`internal/api/`)

REST endpoints registered in `router.go`:

| Method | Path | Handler |
|---|---|---|
| `GET` | `/api/days` | `DayHandler.List` — days with tasks or activity only |
| `GET` | `/api/days/:date` | `DayHandler.Get` — `FirstOrCreate` the day, return tasks + activities |
| `POST` | `/api/days/:date/tasks` | `TaskHandler.Create` |
| `PUT` | `/api/days/:date/tasks/:id` | `TaskHandler.Update` |
| `DELETE` | `/api/days/:date/tasks/:id` | `TaskHandler.Delete` |
| `GET` | `/api/connectors` | `ConnectorHandler.List` |
| `POST` | `/api/connectors/:name/sync` | `ConnectorHandler.Sync` — triggers worker |

All other routes serve the embedded SPA (`index.html` fallback for client-side routing).

## Frontend (`web/`)

- **Framework**: Preact with TypeScript (Vite build).
- **Styling**: Single CSS file at `web/src/styles/main.css` using CSS custom properties (`--color-*`, `--space-*`, `--font-*`). No CSS-in-JS, no utility classes. Add new styles to this file.
- **State**: No global state library. `app.tsx` owns top-level state (`selectedDate`, `activeDates`, calendar month). Components receive props and call callbacks.
- **API**: All server calls go through `web/src/api.ts` — add new endpoints there.
- **Types**: Shared TypeScript types in `web/src/types.ts` — keep in sync with Go structs.

### Component responsibilities

| Component | Responsibility |
|---|---|
| `app.tsx` | Layout, date selection, sidebar, day list fetch |
| `Calendar` | Month grid, day selection, activity dots, month navigation |
| `DayPage` | Day heading, day navigation, tasks + activity sections |
| `TaskList` | Task CRUD, URL extraction from titles, inline edit |
| `ActivityList` | Renders activity items for one source; GitHub subgroups |
| `ConnectorStatus` | Connector chips with sync trigger and last-sync time |

## Testing

### Go

Tests live next to the packages they test (`*_test.go`). Run with `go test ./...`.

**Connector tests** use an in-process `httptest.NewTLSServer` that serves scripted JSON responses. The connector's HTTP client is replaced with the test server's client. There is no real network traffic in tests.

Pattern (see `internal/connector/github_test.go`):
1. Start a TLS httptest server returning sequential JSON `data` payloads.
2. Construct the connector with `client` pointing at the test server.
3. Seed any lazily-resolved state (e.g. username) directly via `sync.Once`.
4. Call `Fetch` or `RefreshStatuses` and assert on the returned `[]ActivityItem`.

**API tests** use an in-memory SQLite database. Tests call handlers directly via Gin's test helpers — no separate server process.

**Worker tests** use an in-memory SQLite database and a stub registry.

**What not to mock**: Do not mock the database. Use `gorm.Open(sqlite.Open(":memory:"))` with `db.AutoMigrate()`. This ensures schema changes are caught by tests.

### Frontend

There are currently no frontend unit tests. When adding them, use Vitest (already in the Vite ecosystem). Focus on pure logic functions (`parseTaskTitle`, date helpers) — do not snapshot-test components.

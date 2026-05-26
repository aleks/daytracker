# Daytracker — High-Level Architecture

## Two-Process Dev, Single Binary Production

During development two processes run side by side:

| Process | Technology | Default Port |
|---------|-----------|--------------|
| API backend | Go, Gin, GORM, SQLite | 8080 |
| Frontend dev server | Preact, Vite, TypeScript | 5173 |

The Vite dev server proxies `/api/*` requests to `localhost:8080`.

**In production a single Go binary is the entire application.** The Vite build output (`web/dist/`) is embedded into the binary at compile time using `//go:embed`. The Go server:

1. Serves all API routes under `/api/`.
2. Serves the embedded frontend assets at `/` and all other paths (catch-all for client-side routing).

There is no separate frontend process, no separate static file server, and no need to ship any files alongside the binary — just `./daytracker` (and a writable path for the SQLite file).

### Build sequence

```bash
# 1. Build the frontend
cd web && npm run build   # outputs to web/dist/

# 2. Compile the Go binary (embed picks up web/dist/ automatically)
go build -o daytracker ./cmd/server
```

A `Makefile` target `make build` should run both steps in order.

---

## Data Store

A single SQLite file (`daytracker.db`) holds all data. GORM manages migrations with `AutoMigrate` on startup. The file path is configurable via `DB_PATH` env var (default: `./daytracker.db`).

No external database is required. SQLite is adequate for a single-user daily tool.

---

## Connector Pattern

A `Connector` is a Go interface that knows how to fetch a user's external activity for a given calendar date. Implementations exist for GitHub, Jira, and Confluence. Each connector:

- Is registered at startup in a connector registry.
- Reads its credentials from environment variables.
- Returns a slice of `ActivityItem` values for the requested date.
- Is idempotent: re-running it for the same date upserts rather than duplicates records, keyed on `(source, external_id)`.

See `plan/connectors.md` for the interface definition and per-connector details.

---

## Background Worker

A single goroutine (`internal/worker`) runs on a configurable ticker (default: 15 minutes). On each tick it calls every configured connector for today's date and upserts results into the database. On first startup it also backfills the past N days (default: 7).

The worker exposes a trigger channel so the API handler for `POST /api/connectors/:name/sync` can request an immediate out-of-cycle sync without blocking the HTTP response.

See `plan/worker.md` for details.

---

## Frontend ↔ API Communication

The frontend uses plain REST calls via a thin `fetch` wrapper in `src/api.ts`. There is no WebSocket or SSE layer in the initial implementation — the frontend polls `GET /api/days/:date` on a short interval (e.g. 30 seconds) when a day page is visible, or the user manually triggers a refresh. This is intentionally simple and avoids SSE complexity for a single-user tool.

If live push is desired later, the Go server can add an SSE endpoint at `GET /api/events` that broadcasts a ping whenever the worker writes new activity, and the frontend can subscribe to it.

---

## Folder Structure

```
daytracker/
├── cmd/
│   └── server/
│       └── main.go              # Entry point: wires DB, router, worker, starts HTTP server
├── internal/
│   ├── api/
│   │   ├── router.go            # Gin router setup, middleware registration
│   │   ├── days.go              # GET /api/days, GET /api/days/:date
│   │   ├── tasks.go             # POST/PATCH/DELETE task handlers
│   │   ├── activities.go        # GET /api/days/:date/activities
│   │   └── connectors.go        # GET /api/connectors, POST /api/connectors/:name/sync
│   ├── db/
│   │   ├── db.go                # Opens SQLite connection, runs AutoMigrate
│   │   └── models.go            # GORM model structs
│   ├── connector/
│   │   ├── connector.go         # Connector interface + registry
│   │   ├── github.go            # GitHub connector
│   │   ├── jira.go              # Jira connector
│   │   └── confluence.go        # Confluence connector
│   └── worker/
│       └── worker.go            # Background sync goroutine
├── web/                         # Vite + Preact frontend source
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── api.ts
│   │   ├── components/
│   │   │   ├── DayPage.tsx
│   │   │   ├── TaskList.tsx
│   │   │   ├── ActivityList.tsx
│   │   │   └── ConnectorStatus.tsx
│   │   └── styles/
│   │       └── main.css
│   ├── index.html
│   ├── vite.config.ts
│   ├── tsconfig.json
│   └── package.json
│   └── dist/                    # Built output — gitignored, embedded at compile time
├── internal/
│   └── assets/
│       └── assets.go            # //go:embed web/dist — exposes embedded FS to router
├── plan/                        # Architecture planning docs (this folder)
├── Makefile                     # `make build` runs npm build then go build
├── daytracker.db                # Created at runtime (gitignored)
├── go.mod
├── go.sum
└── .env.example                 # Documents all supported env vars
```

---

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `DB_PATH` | `./daytracker.db` | SQLite file path |
| `PORT` | `8080` | HTTP server port |
| `SYNC_INTERVAL` | `15m` | Worker poll interval |
| `BACKFILL_DAYS` | `7` | Days to backfill on first run |
| `GITHUB_TOKEN` | — | GitHub personal access token |
| `JIRA_BASE_URL` | — | e.g. `https://yourorg.atlassian.net` |
| `JIRA_TOKEN` | — | Jira API token |
| `JIRA_EMAIL` | — | Jira account email (used for basic auth) |
| `CONFLUENCE_BASE_URL` | — | Usually same host as Jira |
| `CONFLUENCE_TOKEN` | — | Confluence API token |
| `CONFLUENCE_EMAIL` | — | Confluence account email |

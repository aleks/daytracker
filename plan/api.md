# Daytracker — API Design

## Go Module Layout

```
module github.com/you/daytracker

cmd/server/main.go          — wires everything together, starts HTTP server
internal/api/               — HTTP handlers and router
internal/db/                — GORM models, DB connection, migration
internal/connector/         — Connector interface + implementations
internal/worker/            — Background sync goroutine
```

Dependencies (go.mod):
- `github.com/gin-gonic/gin` — HTTP router
- `gorm.io/gorm` — ORM
- `gorm.io/driver/sqlite` — SQLite driver (CGO; uses `modernc.org/sqlite` for CGO-free alternative)
- `golang.org/x/sync/errgroup` — parallel connector fetches in worker

---

## Embedding the Frontend

The compiled Vite output is embedded into the Go binary at build time using `//go:embed`.

**`internal/assets/assets.go`:**
```go
package assets

import "embed"

//go:embed all:web/dist
var FS embed.FS
```

Note: the embed directive lives in `internal/assets/` but the path `web/dist` is relative to the **module root**. Go resolves embed paths relative to the source file's directory, so this file must sit at the module root level or the path must be adjusted. The simplest approach is to place `assets.go` at the module root under a package (e.g. `package assets`) or keep the embed in `cmd/server/main.go` directly:

```go
//go:embed all:web/dist
var webFS embed.FS
```

**Router catch-all (`internal/api/router.go`):**
```go
func RegisterRoutes(r *gin.Engine, db *gorm.DB, webFS embed.FS) {
    // API routes
    api := r.Group("/api")
    { /* ... handlers ... */ }

    // Serve embedded frontend
    sub, _ := fs.Sub(webFS, "web/dist")
    fileServer := http.FileServer(http.FS(sub))

    r.NoRoute(func(c *gin.Context) {
        path := c.Request.URL.Path
        // If the path has a file extension and isn't found, return 404.
        // Otherwise serve index.html for client-side routing.
        if _, err := sub.(fs.ReadFileFS).Open(path); err == nil {
            fileServer.ServeHTTP(c.Writer, c.Request)
        } else {
            // SPA fallback
            c.FileFromFS("index.html", http.FS(sub))
        }
    })
}
```

In development (`ENV=dev`), skip the embedded file handler entirely — Vite's dev server handles the frontend and proxies `/api` to Go.

---

## GORM Models (`internal/db/models.go`)

### `Day`

Represents a calendar date that has at least one task or activity. Created lazily when a task is added or an activity is upserted for a date that has no existing row.

```go
type Day struct {
    ID        uint           `gorm:"primarykey"`
    Date      time.Time      `gorm:"uniqueIndex;not null"` // stored as date, no time component
    CreatedAt time.Time
    Tasks     []Task         `gorm:"foreignKey:DayID"`
    Activities []ActivityItem `gorm:"foreignKey:DayID"`
}
```

The `Date` field should always be stored and compared as UTC midnight (`time.Date(y, m, d, 0, 0, 0, 0, time.UTC)`).

### `Task`

A user-created todo item attached to a specific day.

```go
type Task struct {
    ID        uint      `gorm:"primarykey"`
    DayID     uint      `gorm:"not null;index"`
    Title     string    `gorm:"not null"`
    Done      bool      `gorm:"default:false"`
    CreatedAt time.Time
}
```

### `ActivityItem`

A single piece of activity fetched from an external source.

```go
type ActivityItem struct {
    ID         uint            `gorm:"primarykey"`
    DayID      uint            `gorm:"not null;index"`
    Source     string          `gorm:"not null"`          // "github", "jira", "confluence"
    ExternalID string          `gorm:"not null"`          // ID or key in the source system
    Kind       string          `gorm:"not null"`          // "pr", "review", "jira_issue", "confluence_page", "confluence_comment"
    Title      string          `gorm:"not null"`
    URL        string
    Metadata   datatypes.JSON  // arbitrary JSON blob; connector-specific fields
    FetchedAt  time.Time
}
```

Unique constraint on `(source, external_id)` so upserts are idempotent:
```go
// In AutoMigrate or a migration: add unique index
// gorm tag: `gorm:"uniqueIndex:idx_source_external"`
```

### `ConnectorState`

Tracks last sync time and last error per connector.

```go
type ConnectorState struct {
    ID         uint      `gorm:"primarykey"`
    Name       string    `gorm:"uniqueIndex;not null"` // matches Connector.Name()
    LastSyncAt *time.Time
    LastError  string    // empty string when last sync succeeded
    UpdatedAt  time.Time
}
```

---

## REST Endpoints

Base path: `/api`

All responses are JSON. Successful responses return the data directly (object or array). Errors use the shape:

```json
{ "error": "human-readable message" }
```

HTTP status codes follow REST conventions: `200 OK`, `201 Created`, `204 No Content`, `400 Bad Request`, `404 Not Found`, `500 Internal Server Error`.

---

### Days

#### `GET /api/days`

Returns the list of dates that have at least one task or activity, sorted descending (most recent first).

Query params:
- `limit` (int, default 30) — number of days to return
- `before` (date string `YYYY-MM-DD`, optional) — return days before this date (for pagination)

Response:
```json
[
  {
    "date": "2026-05-26",
    "task_count": 3,
    "activity_count": 12
  }
]
```

#### `GET /api/days/:date`

Returns a full summary for a specific date. The `:date` param is a `YYYY-MM-DD` string.

Creates the day row if it does not exist (so the frontend can load today even with no data yet).

Response:
```json
{
  "date": "2026-05-26",
  "tasks": [ /* array of Task objects */ ],
  "activities": [ /* array of ActivityItem objects */ ]
}
```

---

### Tasks

#### `POST /api/days/:date/tasks`

Creates a new task for the given date. Creates the `Day` row if needed.

Request body:
```json
{ "title": "Write API doc" }
```

Response: `201 Created` with the created `Task` object.

```json
{
  "id": 7,
  "day_id": 3,
  "title": "Write API doc",
  "done": false,
  "created_at": "2026-05-26T10:00:00Z"
}
```

Validation: `title` must be non-empty. Return `400` if missing or blank.

#### `PATCH /api/tasks/:id`

Updates a task. Currently only `done` is patchable (toggling completion). Keep the handler generic enough to accept any subset of updatable fields so it's easy to extend.

Request body:
```json
{ "done": true }
```

Response: `200 OK` with the updated `Task` object.

#### `DELETE /api/tasks/:id`

Hard-deletes the task. Returns `204 No Content`.

---

### Activities

#### `GET /api/days/:date/activities`

Returns all activity items for the given date, sorted by `fetched_at` descending.

Optional query param: `source` — filter to a specific connector name (e.g. `?source=github`).

Response: array of `ActivityItem` objects:
```json
[
  {
    "id": 42,
    "day_id": 3,
    "source": "github",
    "external_id": "PR#1234",
    "kind": "pr",
    "title": "feat: add connector pattern",
    "url": "https://github.com/org/repo/pull/1234",
    "metadata": { "repo": "org/repo", "state": "open", "reviews": 2 },
    "fetched_at": "2026-05-26T09:45:00Z"
  }
]
```

---

### Connectors

#### `GET /api/connectors`

Returns the list of all registered connectors with their configuration status and last sync state.

Response:
```json
[
  {
    "name": "github",
    "configured": true,
    "last_sync_at": "2026-05-26T09:45:00Z",
    "last_error": ""
  },
  {
    "name": "jira",
    "configured": false,
    "last_sync_at": null,
    "last_error": ""
  }
]
```

#### `POST /api/connectors/:name/sync`

Triggers an immediate out-of-cycle sync for the named connector. Sends a signal to the worker via its trigger channel and returns immediately — the sync happens asynchronously.

Response: `202 Accepted`
```json
{ "message": "sync triggered for github" }
```

Returns `404` if the connector name is unknown, `400` if the connector is not configured.

---

### Health

#### `GET /api/health`

Lightweight liveness check. Returns `200 OK`:
```json
{ "status": "ok" }
```

---

## Middleware

Registered in `internal/api/router.go` in this order:

1. **Request logger** — logs method, path, status, and latency using Gin's built-in `gin.Logger()`.
2. **Recovery** — `gin.Recovery()` catches panics and returns `500`.
3. **CORS** — for development, allow all origins. In production, restrict to the served origin. Implement as a simple custom middleware rather than pulling in a CORS library:
   ```go
   func corsMiddleware() gin.HandlerFunc {
       return func(c *gin.Context) {
           c.Header("Access-Control-Allow-Origin", "*")
           c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
           c.Header("Access-Control-Allow-Headers", "Content-Type")
           if c.Request.Method == http.MethodOptions {
               c.AbortWithStatus(http.StatusNoContent)
               return
           }
           c.Next()
       }
   }
   ```

---

## Date Handling

All date parameters in URLs are `YYYY-MM-DD` strings. Parse them with `time.Parse("2006-01-02", dateStr)` and normalize to UTC midnight before DB queries. Return dates in responses as `YYYY-MM-DD` strings (not full timestamps) by using a custom JSON marshaler or by storing/returning strings for the `date` field.

---

## Dependency Injection

`main.go` constructs a single `*gorm.DB` instance, passes it to the worker and the API handlers. Handlers receive the DB via closure or a struct receiver — avoid global state.

```go
// Example handler struct pattern
type TaskHandler struct {
    db *gorm.DB
}

func (h *TaskHandler) Create(c *gin.Context) { ... }
```

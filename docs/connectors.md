# Writing a Connector

Connectors are the bridge between daytracker and an external service. Each connector fetches activity for a given calendar date and returns a list of `ActivityItem` records that are stored in the local database. This document describes how to build one and how to submit it.

## How connectors work

On startup the worker runs a backfill: it calls every configured connector for each of the last N days (`DAYTRACKER_BACKFILL_DAYS`, default 14). After that it syncs today on a configurable interval (`DAYTRACKER_SYNC_INTERVAL`, default 15 minutes).

Each sync call goes through `syncOne`, which calls `Fetch` on the connector and upserts the returned items into the database. The upsert key is `(source, external_id, day_id)` — re-syncing the same day is safe and updates the title, URL, kind, and metadata in place without creating duplicates.

If your connector has items whose state can change after the fetch date (e.g. a ticket that moves from in-progress to done), implement the optional `StatusRefresher` interface. The worker calls `RefreshStatuses` every `DAYTRACKER_STATUS_REFRESH_INTERVAL` (default 5 minutes) for all non-terminal items within the backfill window.

## The `Connector` interface

```go
type Connector interface {
    // Name returns the lowercase source string stored in ActivityItem.Source.
    // Use a short, stable identifier: "github", "jira", "linear", etc.
    Name() string

    // IsConfigured returns false when required credentials are missing.
    // The worker silently skips unconfigured connectors — never return an
    // error from Fetch just because credentials are absent.
    IsConfigured() bool

    // Fetch returns activity items for the given UTC calendar date.
    // Source, ExternalID, Kind, and Title must be set on every item.
    // URL and Metadata are optional but shown in the UI and backup files.
    Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error)

    // KindLabel returns a human-readable label for a kind string.
    // Used in the UI badge and the markdown backup. Return kind unchanged
    // for any value you don't recognise.
    KindLabel(kind string) string

    // ShouldCarryForward reports whether an item with this kind should be
    // copied to the next day when the day rolls over. Return true for
    // incomplete states (open, in-progress) and false for terminal or
    // date-specific ones (merged, closed, created, commented).
    ShouldCarryForward(kind string) bool
}
```

## The `StatusRefresher` interface (optional)

Implement this when your connector has items whose state can change after the fetch date.

```go
type StatusRefresher interface {
    // IsTerminal reports whether a kind string can never change again.
    // The worker skips terminal items during refresh to avoid unnecessary
    // API calls. Be conservative — if in doubt, return false.
    IsTerminal(kind string) bool

    // RefreshStatuses accepts a batch of non-terminal items and returns
    // updated kind strings for any that have changed. Items whose kind
    // has not changed should be omitted from the returned slice.
    // CurrentKind is provided so you can preserve any role prefix
    // (e.g. "authored_open" → "authored_merged").
    RefreshStatuses(ctx context.Context, items []PRStatusItem) ([]PRStatusUpdate, error)
}
```

## Kind naming convention

Kinds are short strings stored in `ActivityItem.Kind`. They identify both the source and the state of the item. Use the format `<source>_<state>`:

```
github_authored_open
github_authored_merged
jira_todo
jira_in_progress
jira_done
confluence_created
confluence_edited
```

Every kind your connector produces must have:
- A `KindLabel` entry returning a human-readable string
- A `ShouldCarryForward` case
- An `IsTerminal` case (if you implement `StatusRefresher`)
- A badge variant in the frontend (see [Frontend wiring](#frontend-wiring) below)

## Anatomy of an `ActivityItem`

| Field | Required | Description |
|---|---|---|
| `Source` | yes | Must match `Name()` exactly |
| `ExternalID` | yes | Stable identifier for the item within the source (e.g. `"PROJ-123"`, `"owner/repo#42"`). Combined with `Source` and `DayID` to form the upsert key — must be unique per item per day |
| `Kind` | yes | `<source>_<state>` string (see above) |
| `Title` | yes | Display text shown in the UI and backup files |
| `URL` | no | Link opened when the user clicks the item |
| `Metadata` | no | Secondary text shown in the UI (e.g. repo name, issue type) |

Do not set `DayID`, `ID`, or `FetchedAt` — the worker fills those in.

## Step-by-step: adding a connector

### 1. Create `internal/connector/<name>.go`

```go
package connector

import (
    "context"
    "net/http"
    "os"
    "time"

    "github.com/aleksmaksimow/daytracker/internal/db"
)

type LinearConnector struct {
    token  string
    client *http.Client
}

func NewLinear() *LinearConnector {
    return &LinearConnector{
        token:  os.Getenv("DAYTRACKER_LINEAR_TOKEN"),
        client: &http.Client{Timeout: 30 * time.Second},
    }
}

func (l *LinearConnector) Name() string        { return "linear" }
func (l *LinearConnector) IsConfigured() bool  { return l.token != "" }

func (l *LinearConnector) KindLabel(kind string) string {
    switch kind {
    case "linear_todo":       return "todo"
    case "linear_in_progress": return "in progress"
    case "linear_done":       return "done"
    case "linear_cancelled":  return "cancelled"
    default:                  return kind
    }
}

func (l *LinearConnector) ShouldCarryForward(kind string) bool {
    return kind == "linear_todo" || kind == "linear_in_progress"
}

func (l *LinearConnector) Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error) {
    // ... call the Linear API and return items
}
```

### 2. Resolve credentials lazily with `sync.Once`

If your connector needs to resolve a user ID, cloud ID, or team ID from the API before it can make real requests, do it inside a `sync.Once` on the first `Fetch` call — not in the constructor. See `JiraConnector.apiBase` for the pattern.

### 3. Register the connector in `cmd/server/main.go`

```go
registry.Register(connector.NewLinear())
```

### 4. Add env vars to `.env.example`

```bash
# Linear connector
DAYTRACKER_LINEAR_TOKEN=
```

### 5. Wire up the frontend

In `web/src/components/DayPage.tsx`, add the source name to `SOURCE_ORDER` and `SOURCE_LABELS`:

```ts
const SOURCE_ORDER = ['github', 'jira', 'confluence', 'linear']
const SOURCE_LABELS: Record<string, string> = {
  github: 'GitHub',
  jira: 'Jira',
  confluence: 'Confluence',
  linear: 'Linear',
}
```

In `web/src/components/ActivityList.tsx`, add badge variants for your new kind strings.

In `web/src/styles/main.css`, add the corresponding `.activity-badge--*` styles (and dark mode overrides if needed).

### 6. Write tests

Tests live next to the connector in `internal/connector/<name>_test.go`. Use an in-process `httptest.NewTLSServer` — no real network calls in tests. See `internal/connector/github_test.go` for the full pattern.

At minimum, cover:
- `IsConfigured` with env vars set and missing
- `Fetch` returning the correct `ActivityItem` fields
- `Fetch` returning an empty slice on an HTTP error
- `KindLabel` for every kind your connector produces
- `ShouldCarryForward` for each kind
- `IsTerminal` and `RefreshStatuses` if you implement `StatusRefresher`

## Contributing

1. Fork the repository and create a branch: `git checkout -b connector/linear`
2. Implement the connector following the steps above
3. Run `make test` and confirm all tests pass
4. Open a pull request with a description of what the connector syncs and any setup instructions (API tokens, required scopes, etc.)

Please keep pull requests focused on a single connector. If you're adding a connector that requires a new env var prefix, add it to `.env.example` and the configuration table in `README.md`.

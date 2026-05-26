# Daytracker — Connector Pattern

## Interface

Defined in `internal/connector/connector.go`:

```go
package connector

import (
    "context"
    "time"

    "github.com/you/daytracker/internal/db"
)

// Connector fetches activity for a single calendar date from one external source.
type Connector interface {
    // Name returns the stable identifier for this connector (e.g. "github", "jira").
    // Must match the Name stored in ConnectorState rows.
    Name() string

    // Fetch retrieves all activity items for the given date.
    // date is always UTC midnight. Implementations must scope their query to
    // [date, date+24h). Returns an empty slice (not nil) when there is no activity.
    Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error)

    // IsConfigured returns true when the connector has the credentials it needs.
    // A false return means Fetch will always fail; the worker skips unconfigured connectors.
    IsConfigured() bool
}
```

---

## Registry

Also in `internal/connector/connector.go`:

```go
// Registry holds all known connectors, keyed by name.
type Registry struct {
    connectors map[string]Connector
}

func NewRegistry(connectors ...Connector) *Registry {
    r := &Registry{connectors: make(map[string]Connector)}
    for _, c := range connectors {
        r.connectors[c.Name()] = c
    }
    return r
}

func (r *Registry) All() []Connector {
    result := make([]Connector, 0, len(r.connectors))
    for _, c := range r.connectors {
        result = append(result, c)
    }
    return result
}

func (r *Registry) Get(name string) (Connector, bool) {
    c, ok := r.connectors[name]
    return c, ok
}
```

Registered at startup in `cmd/server/main.go`:

```go
registry := connector.NewRegistry(
    connector.NewGitHub(),
    connector.NewJira(),
    connector.NewConfluence(),
)
```

---

## Upsert Strategy

All connectors upsert into `activity_items` using `(source, external_id)` as the unique key. This makes every sync run idempotent — re-running a connector for a date already in the DB updates existing records rather than creating duplicates.

GORM upsert pattern:
```go
func upsertActivity(gdb *gorm.DB, item db.ActivityItem) error {
    return gdb.
        Where(db.ActivityItem{Source: item.Source, ExternalID: item.ExternalID}).
        Assign(db.ActivityItem{
            DayID:     item.DayID,
            Kind:      item.Kind,
            Title:     item.Title,
            URL:       item.URL,
            Metadata:  item.Metadata,
            FetchedAt: item.FetchedAt,
        }).
        FirstOrCreate(&item).Error
}
```

`FirstOrCreate` inserts on first call; subsequent calls with the same `(source, external_id)` update the Assign fields.

---

## GitHub Connector (`internal/connector/github.go`)

### Configuration

| Env var | Required | Purpose |
|---------|----------|---------|
| `GITHUB_TOKEN` | Yes | Personal access token with `repo` scope |
| `GITHUB_USERNAME` | No | Override the username; defaults to the token owner |

`IsConfigured()` returns true when `GITHUB_TOKEN` is non-empty.

### What it fetches

| Kind | Query strategy |
|------|---------------|
| `pr` | PRs authored by the current user where `created_at` date matches |
| `review` | PRs where the user submitted a review on the given date |

### Implementation

Uses the GitHub REST API via `net/http` (not the `gh` CLI). The `gh` CLI adds process-spawning complexity that is unnecessary when a token is available.

**PRs created on date:**

```
GET https://api.github.com/search/issues
  ?q=type:pr+author:{username}+created:{date}
  &per_page=100
Authorization: Bearer {GITHUB_TOKEN}
```

**Reviews submitted on date:**

GitHub does not expose a direct "reviews by user on date" search. Strategy:
1. Search for PRs the user reviewed: `type:pr+reviewed-by:{username}+updated:{date}` (over-fetches).
2. For each PR in the result, fetch `GET /repos/{owner}/{repo}/pulls/{number}/reviews` and filter to reviews where `user.login == username` and `submitted_at` falls on the target date.

This is slow for users with many reviews but is acceptable for a 15-minute polling interval. Cache the PR list per date in the `metadata` JSON field to avoid re-fetching.

### Mapping to `ActivityItem`

```go
// PR
ActivityItem{
    Source:     "github",
    ExternalID: fmt.Sprintf("pr:%s:%d", repoFullName, prNumber),
    Kind:       "pr",
    Title:      pr.Title,
    URL:        pr.HTMLURL,
    Metadata:   toJSON(map[string]any{
        "repo":   repoFullName,
        "number": prNumber,
        "state":  pr.State,
    }),
    FetchedAt: time.Now().UTC(),
}

// Review
ActivityItem{
    Source:     "github",
    ExternalID: fmt.Sprintf("review:%s:%d:%d", repoFullName, prNumber, reviewID),
    Kind:       "review",
    Title:      fmt.Sprintf("Reviewed: %s", pr.Title),
    URL:        pr.HTMLURL,
    Metadata:   toJSON(map[string]any{
        "repo":         repoFullName,
        "pr_number":    prNumber,
        "review_state": review.State,  // "APPROVED", "CHANGES_REQUESTED", "COMMENTED"
    }),
    FetchedAt: time.Now().UTC(),
}
```

---

## Jira Connector (`internal/connector/jira.go`)

### Configuration

| Env var | Required | Purpose |
|---------|----------|---------|
| `JIRA_BASE_URL` | Yes | e.g. `https://yourorg.atlassian.net` |
| `JIRA_EMAIL` | Yes | Atlassian account email |
| `JIRA_TOKEN` | Yes | Atlassian API token |

`IsConfigured()` returns true when all three are non-empty.

Authentication: HTTP Basic Auth with `{JIRA_EMAIL}:{JIRA_TOKEN}` base64-encoded.

### What it fetches

Issues updated by (or assigned to) the current user on the given date.

### Query

```
POST {JIRA_BASE_URL}/rest/api/3/search
Content-Type: application/json

{
  "jql": "assignee = currentUser() AND updated >= '{date}' AND updated < '{date+1}'",
  "fields": ["summary", "status", "issuetype", "priority", "assignee"],
  "maxResults": 100
}
```

The `currentUser()` function in JQL resolves to the authenticated user, so no need to look up the user ID separately.

### Mapping to `ActivityItem`

```go
ActivityItem{
    Source:     "jira",
    ExternalID: issue.Key,              // e.g. "PROJ-123"
    Kind:       "jira_issue",
    Title:      fmt.Sprintf("[%s] %s", issue.Key, issue.Fields.Summary),
    URL:        fmt.Sprintf("%s/browse/%s", baseURL, issue.Key),
    Metadata:   toJSON(map[string]any{
        "status":     issue.Fields.Status.Name,
        "issue_type": issue.Fields.IssueType.Name,
        "priority":   issue.Fields.Priority.Name,
    }),
    FetchedAt: time.Now().UTC(),
}
```

---

## Confluence Connector (`internal/connector/confluence.go`)

### Configuration

| Env var | Required | Purpose |
|---------|----------|---------|
| `CONFLUENCE_BASE_URL` | Yes | Usually same host as Jira |
| `CONFLUENCE_EMAIL` | Yes | Atlassian account email |
| `CONFLUENCE_TOKEN` | Yes | Atlassian API token (shared with Jira) |

`IsConfigured()` returns true when all three are non-empty.

Authentication: same HTTP Basic Auth pattern as Jira.

### What it fetches

| Kind | Query |
|------|-------|
| `confluence_page` | Pages created or modified by current user on the given date |
| `confluence_comment` | Inline or footer comments created by current user on the given date |

### Page query (CQL)

```
GET {CONFLUENCE_BASE_URL}/wiki/rest/api/search
  ?cql=creator=currentUser() AND created >= "{date}" AND created < "{date+1}" AND type=page
  &limit=50
```

For updated (not just created) pages:
```
cql=contributor=currentUser() AND lastModified >= "{date}" AND lastModified < "{date+1}" AND type=page
```

Run both queries and deduplicate by page ID — a page created and modified on the same day should appear once.

### Comment query (CQL)

```
cql=creator=currentUser() AND created >= "{date}" AND created < "{date+1}" AND (type=comment)
```

### Mapping to `ActivityItem`

```go
// Page
ActivityItem{
    Source:     "confluence",
    ExternalID: page.ID,
    Kind:       "confluence_page",
    Title:      page.Title,
    URL:        page.Links.WebUI,       // prepend base URL
    Metadata:   toJSON(map[string]any{
        "space": page.Space.Key,
        "version": page.Version.Number,
    }),
    FetchedAt: time.Now().UTC(),
}

// Comment
ActivityItem{
    Source:     "confluence",
    ExternalID: comment.ID,
    Kind:       "confluence_comment",
    Title:      fmt.Sprintf("Comment on: %s", comment.Container.Title),
    URL:        comment.Links.WebUI,
    Metadata:   toJSON(map[string]any{
        "page_id":    comment.Container.ID,
        "page_title": comment.Container.Title,
    }),
    FetchedAt: time.Now().UTC(),
}
```

---

## Connector State Table

`connector_states` is updated by the worker after each sync attempt (success or failure):

```go
type ConnectorState struct {
    ID         uint       `gorm:"primarykey"`
    Name       string     `gorm:"uniqueIndex;not null"`
    LastSyncAt *time.Time
    LastError  string
    UpdatedAt  time.Time
}
```

The `GET /api/connectors` handler joins the registry (for `IsConfigured()`) with `ConnectorState` rows (for `LastSyncAt` and `LastError`) to build its response.

---

## Error Handling

- A connector that returns an error is logged at `WARN` level. The worker continues with the remaining connectors.
- The error string is stored in `ConnectorState.LastError` so it's visible via `GET /api/connectors`.
- A successful sync clears `LastError` by setting it to `""`.
- HTTP errors (non-2xx from the external API) are wrapped with the status code: `"jira: unexpected status 429: rate limited"`.
- Connectors should respect `ctx.Done()` and return early when the context is cancelled (graceful shutdown).

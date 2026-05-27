# daytracker

A single-binary daily work tracker. It embeds a Preact frontend and syncs activity from external sources (GitHub, Jira, Confluence) into a local SQLite database.

![daytracker](daytracker.png)

## Features

- **Day-by-day activity log** — browse any date and see everything you did across all connected sources in one place
- **GitHub** — pull requests you authored, reviewed, or commented on; PR statuses (draft → open → in review → approved → merged) kept fresh in the background
- **Jira** — issues assigned to you that were updated on the day
- **Confluence** — pages you created, edited, or commented on
- **Background sync** — a worker fetches fresh activity on a configurable interval and backfills recent history on startup
- **Single binary** — the Preact frontend is embedded; no separate web server or database process required
- **Markdown backup** — optionally mirrors every day to a `YYYY/MM/DD.md` file tree (tasks + activity, with links) that you can commit to a notes repo or open in any editor
- **Local-first** — all data is stored in a single SQLite file on your machine; no cloud account needed

## Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [Node.js 18+](https://nodejs.org/) and npm (only needed to build the frontend)

## Install

### Option 1: `go install` (no frontend build needed)

> Only works once a release binary is published. For now, clone and build.

### Option 2: Clone and build

```bash
git clone https://github.com/aleksmaksimow/daytracker.git
cd daytracker

# Install frontend dependencies and build the embedded UI
cd web && npm install && cd ..

# Build the single binary (frontend is embedded)
make build
```

This produces a `./daytracker` binary with the frontend baked in — no separate web server needed.

### Option 3: Run without building (development)

```bash
git clone https://github.com/aleksmaksimow/daytracker.git
cd daytracker
cd web && npm install && cd ..

# Terminal 1 — Go API server
make dev-api

# Terminal 2 — Vite dev server with HMR (proxies /api to :8080)
make dev-web
```

Open `http://localhost:5173`.

## Running

```bash
# Copy and fill in your credentials
cp .env.example .env
$EDITOR .env

# Start the server
./daytracker
```

Open `http://localhost:8080`.

## Verify connector credentials

```bash
make check
```

Reads your `.env` and pings each configured connector. Useful for confirming credentials before starting the server.

## Configuration

All configuration is via environment variables prefixed with `DAYTRACKER_`.

| Variable | Default | Description |
|---|---|---|
| `DAYTRACKER_PORT` | `8080` | HTTP port the server listens on |
| `DAYTRACKER_DB_PATH` | `./daytracker.db` | Path to the SQLite database file |
| `DAYTRACKER_SYNC_INTERVAL` | `15m` | How often the worker fetches fresh activity from all connectors |
| `DAYTRACKER_STATUS_REFRESH_INTERVAL` | `5m` | How often open PR statuses are re-checked |
| `DAYTRACKER_BACKFILL_DAYS` | `14` | Number of past days to sync on startup and to keep refreshing statuses for |
| `DAYTRACKER_BACKUP_DIR` | _(unset)_ | Directory to write daily `YYYY/MM/DD.md` snapshots; backup is disabled when unset |

## Connectors

Connectors are enabled automatically when their required variables are set. Unconfigured connectors are silently skipped.

### GitHub

Uses the [GitHub GraphQL API](https://docs.github.com/en/graphql) with a personal access token.

**Required variables:**

| Variable | Description |
|---|---|
| `DAYTRACKER_GITHUB_TOKEN` | A GitHub personal access token |

**How to create a token:**
1. Go to <https://github.com/settings/tokens> → **Generate new token (classic)**
2. Select scopes: `repo`, `read:user`
3. Copy the token and set it as `DAYTRACKER_GITHUB_TOKEN`

**What it syncs:**
- Pull requests you authored, created on the target date
- Pull requests you reviewed or commented on, updated on the target date (your own PRs are excluded from this list)

PR statuses (open, draft, in review, approved, changes requested, merged, closed) are refreshed every `DAYTRACKER_STATUS_REFRESH_INTERVAL` for PRs within the `DAYTRACKER_BACKFILL_DAYS` window.

---

### Jira

Uses the [Jira REST API v3](https://developer.atlassian.com/cloud/jira/platform/rest/v3/) with HTTP Basic Auth.

**Required variables:**

| Variable | Description |
|---|---|
| `DAYTRACKER_JIRA_BASE_URL` | Your Atlassian cloud base URL, e.g. `https://your-org.atlassian.net` |
| `DAYTRACKER_JIRA_EMAIL` | The email address of the Atlassian account |
| `DAYTRACKER_JIRA_TOKEN` | An Atlassian API token |

**How to create an API token:**
1. Go to <https://id.atlassian.com/manage-profile/security/api-tokens>
2. Click **Create API token**, give it a name, copy the value
3. Set it as `DAYTRACKER_JIRA_TOKEN`

The same token works for both Jira and Confluence — it is scoped to your Atlassian account, not to a specific product.

**What it syncs:**
- Issues assigned to you that were updated on the target date

---

### Confluence

Uses the [Confluence REST API v1](https://developer.atlassian.com/cloud/confluence/rest/v1/) with HTTP Basic Auth. The same Atlassian API token used for Jira works here.

**Required variables:**

| Variable | Description |
|---|---|
| `DAYTRACKER_CONFLUENCE_BASE_URL` | Your Atlassian cloud base URL, e.g. `https://your-org.atlassian.net` |
| `DAYTRACKER_CONFLUENCE_EMAIL` | The email address of the Atlassian account |
| `DAYTRACKER_CONFLUENCE_TOKEN` | An Atlassian API token (same token as Jira) |

See the [Jira connector](#jira) section for instructions on creating an API token.

**What it syncs:**
- Pages you created or edited on the target date (`contributor = currentUser()`)
- Pages you commented on — multiple comments on the same page are grouped into one activity item

---

## Markdown backup

Set `DAYTRACKER_BACKUP_DIR` to any directory and daytracker will write a snapshot for each synced day:

```
<backup-dir>/
  2025/
    05/
      27.md
      28.md
```

Each file contains your tasks (as a checklist) and your activity grouped by source, with titles linked to their original URLs. Files are overwritten on every sync and also refreshed every 2 minutes so task completions land quickly.

The directory is plain text — you can commit it to a notes repo, open it in Obsidian or any Markdown editor, or feed individual day files directly to an AI assistant to answer questions like "what did I work on last Tuesday?" or "summarise my Jira activity this week".

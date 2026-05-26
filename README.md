# daytracker

A single-binary daily work tracker. It embeds a Preact frontend and syncs activity from external sources (GitHub, Jira, Confluence) into a local SQLite database.

## Running

```bash
# Copy and fill in your config
cp .env.example .env

# Build the binary (also builds the frontend)
make build

# Start
./daytracker
```

Open `http://localhost:8080`.

### Development

```bash
make dev-api   # Go server (no embed, tags=dev)
make dev-web   # Vite dev server with HMR (proxies /api to :8080)
```

## Configuration

All configuration is via environment variables prefixed with `DAYTRACKER_`.

| Variable | Default | Description |
|---|---|---|
| `DAYTRACKER_PORT` | `8080` | HTTP port the server listens on |
| `DAYTRACKER_DB_PATH` | `./daytracker.db` | Path to the SQLite database file |
| `DAYTRACKER_SYNC_INTERVAL` | `15m` | How often the worker fetches fresh activity from all connectors |
| `DAYTRACKER_STATUS_REFRESH_INTERVAL` | `5m` | How often open PR statuses are re-checked |
| `DAYTRACKER_BACKFILL_DAYS` | `14` | Number of past days to sync on startup and to keep refreshing statuses for |

## Connectors

Connectors are enabled automatically when their required variables are set. Unconfigured connectors are silently skipped.

### GitHub

Uses the [`gh` CLI](https://cli.github.com) — no token configuration needed in daytracker itself.

**Requirements:**
1. Install the `gh` CLI: `brew install gh`
2. Authenticate: `gh auth login`

That's it. daytracker calls `gh` as a subprocess and inherits whatever account is logged in.

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

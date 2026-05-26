# Daytracker — Claude Instructions

Read `AGENTS.md` before making any changes to this repository. It describes the architecture, connector pattern, worker loop, database models, testing approach, and conventions used throughout the codebase.

## Key rules

- **Tests are required.** Any new connector, API handler, or worker behaviour must have a corresponding test. See `AGENTS.md § Testing` for the patterns to follow.
- **No database mocks.** Use `gorm.Open(sqlite.Open(":memory:"))` + `db.AutoMigrate()` in tests.
- **Connector credentials come from env vars only.** Never hardcode tokens. Constructor reads env, returns a struct with `IsConfigured() bool`.
- **New connectors need frontend wiring.** Add the source name to `SOURCE_ORDER` and `SOURCE_LABELS` in `web/src/components/DayPage.tsx`, and add a badge variant to `web/src/components/ActivityList.tsx` and `web/src/styles/main.css` for any new kind strings.
- **All styles go in `web/src/styles/main.css`.** No inline styles, no separate CSS files, no CSS modules.
- **Do not change the `ActivityItem` unique index** from `(source, external_id, day_id)`. It is intentionally day-scoped to preserve cross-day history.
- **Do not co-author commits.** Do not add yourself as a co-author in commit messages.

---
# daytracker-ru2i
title: 'Refactor + tests: github connector injectable runGH for Fetch and RefreshStatuses'
status: completed
type: task
priority: normal
created_at: 2026-05-26T16:52:19Z
updated_at: 2026-05-26T16:56:20Z
parent: daytracker-9aet
---

Replace the package-level runGH call with an injected field (func(ctx, ...string) ([]byte, error)) on GitHubConnector. Add tests for Fetch (authored/reviewed dedup, own-PR exclusion) and RefreshStatuses (role preservation, state mapping) using fake runGH output.
---
# daytracker-iuz1
title: 'Tests: internal/worker — syncOne, syncAll, backfill logic'
status: completed
type: task
priority: normal
created_at: 2026-05-26T16:35:47Z
updated_at: 2026-05-26T16:46:02Z
parent: daytracker-9aet
---

Unit tests for worker syncOne (upsert, dedup, error handling), syncAll parallel execution, terminalKinds filtering. Use in-memory SQLite and a stub connector.
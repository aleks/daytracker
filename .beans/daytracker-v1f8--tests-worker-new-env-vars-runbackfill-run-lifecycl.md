---
# daytracker-v1f8
title: 'Tests: worker New (env vars), runBackfill, Run lifecycle'
status: completed
type: task
priority: normal
created_at: 2026-05-26T16:52:19Z
updated_at: 2026-05-26T16:59:21Z
parent: daytracker-9aet
---

Test worker.New reads DAYTRACKER_SYNC_INTERVAL, DAYTRACKER_STATUS_REFRESH_INTERVAL, DAYTRACKER_BACKFILL_DAYS from env. Test runBackfill calls syncAll for each day in the window. Test Run exits cleanly on context cancellation.
---
# daytracker-qowe
title: Wire Confluence connector into server, frontend and docs
status: completed
type: task
priority: normal
created_at: 2026-05-26T15:40:14Z
updated_at: 2026-05-26T16:02:00Z
parent: daytracker-03cp
---

Register NewConfluence() in cmd/server/main.go. Add DAYTRACKER_CONFLUENCE_* vars to .env.example. Add confluence_created, confluence_edited, confluence_commented kinds to ActivityList.tsx KIND_META. Update README connector section. Add confluence check to cmd/check/main.go.
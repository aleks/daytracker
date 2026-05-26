---
# daytracker-vmoy
title: 'Tests: api/router serveEmbedded + serveFile SPA routing'
status: completed
type: task
priority: normal
created_at: 2026-05-26T16:52:19Z
updated_at: 2026-05-26T16:53:15Z
parent: daytracker-9aet
---

Test serveEmbedded: known file served, unknown path falls back to index.html, directory path falls back to index.html. Test serveFile: file not found returns 404. Use testing/fstest.MapFS as the embedded FS.
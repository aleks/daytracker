---
# daytracker-d0l7
title: 'Tests: api handler DB error paths (500 responses)'
status: completed
type: task
priority: normal
created_at: 2026-05-26T16:52:19Z
updated_at: 2026-05-26T16:54:02Z
parent: daytracker-9aet
---

Test that List/Get/Create/Update/Delete all return 500 when the database returns an error. Use a closed *sql.DB or a gorm.DB backed by a nil/broken connection to force errors.
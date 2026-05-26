---
# daytracker-cko2
title: 'Tests: internal/connector — jira and confluence HTTP logic'
status: completed
type: task
priority: normal
created_at: 2026-05-26T16:35:47Z
updated_at: 2026-05-26T16:41:43Z
parent: daytracker-9aet
---

Table-driven tests for JiraConnector.Fetch and ConfluenceConnector.Fetch using httptest.NewServer to stub the Atlassian API. Tests for cloud ID resolution, response parsing, date filtering, kind classification.
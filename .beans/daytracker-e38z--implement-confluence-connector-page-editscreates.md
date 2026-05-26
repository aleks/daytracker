---
# daytracker-e38z
title: 'Implement Confluence connector: page edits/creates'
status: completed
type: task
priority: normal
created_at: 2026-05-26T15:40:14Z
updated_at: 2026-05-26T16:02:00Z
parent: daytracker-03cp
---

CQL: `contributor = currentUser() AND type = page AND lastmodified >= "{date}" AND lastmodified < "{date+1}"`. ExternalID: `page:{id}`. Kind: `confluence_created` if history.createdBy.accountId matches current user and created date is today, else `confluence_edited`. Title from content.title. URL: baseURL + /wiki + content._links.webui. Auth via Basic Auth same as Jira. Cloud ID resolved via /_edge/tenant_info.
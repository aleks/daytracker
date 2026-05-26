---
# daytracker-sz8x
title: 'Implement Confluence connector: comments grouped by page'
status: scrapped
type: task
priority: normal
created_at: 2026-05-26T15:40:14Z
updated_at: 2026-05-26T16:02:00Z
parent: daytracker-03cp
---

CQL: `creator = currentUser() AND type = comment AND created >= "{date}" AND created < "{date+1}"`. Group multiple comments on the same page into one ActivityItem. ExternalID: `comment:page:{pageId}` where pageId is extracted from _expandable.container path. Kind: `confluence_commented`. Title: 'Commented on: {pageTitle}' (strip 'Re: ' prefix from comment title). URL: page URL (strip focusedCommentId query param).
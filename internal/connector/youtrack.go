package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// YouTrackConnector fetches issue activities (creation, edits, work items,
// resolution) for the authenticated user from a YouTrack instance.
type YouTrackConnector struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewYouTrack() *YouTrackConnector {
	return &YouTrackConnector{
		baseURL: strings.TrimRight(os.Getenv("DAYTRACKER_YOUTRACK_BASE_URL"), "/"),
		token:   os.Getenv("DAYTRACKER_YOUTRACK_TOKEN"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (y *YouTrackConnector) Name() string { return "youtrack" }

func (y *YouTrackConnector) IsConfigured() bool {
	return y.baseURL != "" && y.token != ""
}

func (y *YouTrackConnector) ShouldCarryForward(_ string) bool { return false }

func (y *YouTrackConnector) KindLabel(kind string) string {
	switch kind {
	case "youtrack_created":
		return "created"
	case "youtrack_edited":
		return "edited"
	case "youtrack_work":
		return "time logged"
	case "youtrack_resolved":
		return "resolved"
	default:
		return kind
	}
}

// ── API response types ────────────────────────────────────────────────────────

type ytActivityPage struct {
	Activities  []ytActivity `json:"activities"`
	AfterCursor string       `json:"afterCursor"`
	HasAfter    bool         `json:"hasAfter"`
}

type ytActivity struct {
	ID        string          `json:"id"`
	Timestamp int64           `json:"timestamp"`
	Added     json.RawMessage `json:"added"`
	Removed   json.RawMessage `json:"removed"`
	Type      string          `json:"$type"`
	Target    ytTarget        `json:"target"`
}

type ytNamed struct {
	Name string `json:"name"`
}

type ytTarget struct {
	IDReadable string   `json:"idReadable"`
	Summary    string   `json:"summary"`
	Project    *ytNamed `json:"project"`
	Issue      *ytIssueRef `json:"issue"`
}

type ytIssueRef struct {
	IDReadable string   `json:"idReadable"`
	Summary    string   `json:"summary"`
	Project    *ytNamed `json:"project"`
}

type ytWorkItem struct {
	Duration struct {
		Minutes      int    `json:"minutes"`
		Presentation string `json:"presentation"`
	} `json:"duration"`
	Text string `json:"text"`
}

// ── Fetch ─────────────────────────────────────────────────────────────────────

func (y *YouTrackConnector) Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error) {
	start := date.UTC()
	end := start.AddDate(0, 0, 1)

	params := url.Values{
		"fields":     {"activities(id,timestamp,added,removed,$type,target(idReadable,summary,project(name),issue(idReadable,summary,project(name))))"},
		"categories": {"IssueCreatedCategory", "WorkItemCategory", "SummaryCategory", "DescriptionCategory", "IssueResolvedCategory"},
		"author":     {"me"},
		"start":      {fmt.Sprintf("%d", start.UnixMilli())},
		"end":        {fmt.Sprintf("%d", end.UnixMilli())},
		"reverse":    {"false"},
		"$top":       {"100"},
	}

	const pageSize = 100
	var items []db.ActivityItem
	cursor := ""

	for {
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		body, err := y.get(ctx, y.baseURL+"/api/activitiesPage?"+params.Encode())
		if err != nil {
			return nil, fmt.Errorf("youtrack: fetch activities: %w", err)
		}

		var page ytActivityPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("youtrack: parse activities: %w", err)
		}

		for _, act := range page.Activities {
			if item := y.activityToItem(act); item != nil {
				items = append(items, *item)
			}
		}

		if !page.HasAfter || page.AfterCursor == "" {
			break
		}
		cursor = page.AfterCursor

		// If we got fewer items than requested, we're done regardless of hasAfter.
		if len(page.Activities) < pageSize {
			break
		}
	}

	return items, nil
}

func (y *YouTrackConnector) activityToItem(act ytActivity) *db.ActivityItem {
	issueID := act.Target.IDReadable
	summary := act.Target.Summary
	project := act.Target.Project

	// WorkItemActivityItem has a nested target.issue instead of direct fields.
	if issueID == "" && act.Target.Issue != nil {
		issueID = act.Target.Issue.IDReadable
		summary = act.Target.Issue.Summary
		project = act.Target.Issue.Project
	}
	if issueID == "" {
		return nil
	}

	issueURL := fmt.Sprintf("%s/issue/%s", y.baseURL, issueID)
	metadata := ""
	if project != nil {
		metadata = project.Name
	}

	switch act.Type {
	case "IssueCreatedActivityItem":
		return &db.ActivityItem{
			Source:     "youtrack",
			ExternalID: fmt.Sprintf("%s:created", issueID),
			Kind:       "youtrack_created",
			Title:      fmt.Sprintf("[%s] %s", issueID, summary),
			URL:        issueURL,
			Metadata:   metadata,
		}
	case "IssueResolvedActivityItem":
		return &db.ActivityItem{
			Source:     "youtrack",
			ExternalID: fmt.Sprintf("%s:resolved", issueID),
			Kind:       "youtrack_resolved",
			Title:      fmt.Sprintf("[%s] %s", issueID, summary),
			URL:        issueURL,
			Metadata:   metadata,
		}
	case "WorkItemActivityItem":
		return y.workItemToItem(act, issueID, summary, issueURL, metadata)
	case "SimpleValueActivityItem", "TextMarkupActivityItem":
		return &db.ActivityItem{
			Source:     "youtrack",
			ExternalID: fmt.Sprintf("%s:edited", issueID),
			Kind:       "youtrack_edited",
			Title:      fmt.Sprintf("[%s] %s", issueID, summary),
			URL:        issueURL,
			Metadata:   metadata,
		}
	}

	return nil
}

func (y *YouTrackConnector) workItemToItem(act ytActivity, issueID, summary, issueURL, metadata string) *db.ActivityItem {
	var added []ytWorkItem
	if err := json.Unmarshal(act.Added, &added); err != nil || len(added) == 0 {
		return nil
	}

	wi := added[0]
	title := fmt.Sprintf("[%s] %s", issueID, summary)

	if wi.Text != "" {
		title += " · " + wi.Text
	}
	if wi.Duration.Presentation != "" {
		title += " (" + wi.Duration.Presentation + ")"
	}

	return &db.ActivityItem{
		Source:     "youtrack",
		ExternalID: fmt.Sprintf("%s:work:%s", issueID, act.ID),
		Kind:       "youtrack_work",
		Title:      title,
		URL:        issueURL,
		Metadata:   metadata,
	}
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (y *YouTrackConnector) get(ctx context.Context, urlStr string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+y.token)
	req.Header.Set("Accept", "application/json")

	resp, err := y.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusBadGateway || !strings.HasPrefix(ct, "application/json") {
			return nil, fmt.Errorf("youtrack: server returned %d (server may be unavailable)", resp.StatusCode)
		}
		return nil, fmt.Errorf("youtrack: status %d: %s", resp.StatusCode, string(body))
	}
	if !strings.HasPrefix(ct, "application/json") {
		return nil, fmt.Errorf("youtrack: unexpected content type %q (server may be returning an error page)", ct)
	}
	return body, nil
}
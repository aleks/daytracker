package connector

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

type JiraConnector struct {
	baseURL string // e.g. https://your-org.atlassian.net — used for browse links
	email   string
	token   string
	client  *http.Client

	cloudIDOnce sync.Once
	cloudID     string
	cloudIDErr  error
}

func NewJira() *JiraConnector {
	return &JiraConnector{
		baseURL: strings.TrimRight(os.Getenv("DAYTRACKER_JIRA_BASE_URL"), "/"),
		email:   os.Getenv("DAYTRACKER_JIRA_EMAIL"),
		token:   os.Getenv("DAYTRACKER_JIRA_TOKEN"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (j *JiraConnector) Name() string { return "jira" }

func (j *JiraConnector) IsConfigured() bool {
	return j.baseURL != "" && j.email != "" && j.token != ""
}

func (j *JiraConnector) ShouldCarryForward(kind string) bool {
	return kind == "jira_todo" || kind == "jira_in_progress"
}

func (j *JiraConnector) KindLabel(kind string) string {
	switch kind {
	case "jira_todo":
		return "to do"
	case "jira_in_progress":
		return "in progress"
	case "jira_done":
		return "done"
	default:
		return kind
	}
}

// apiBase returns the Atlassian API gateway base URL for this cloud instance.
// Atlassian requires routing through api.atlassian.com rather than the tenant
// hostname when using API tokens.
func (j *JiraConnector) apiBase(ctx context.Context) (string, error) {
	j.cloudIDOnce.Do(func() {
		j.cloudID, j.cloudIDErr = j.resolveCloudID(ctx)
	})
	if j.cloudIDErr != nil {
		return "", j.cloudIDErr
	}
	return "https://api.atlassian.com/ex/jira/" + j.cloudID, nil
}

func (j *JiraConnector) resolveCloudID(ctx context.Context) (string, error) {
	// /_edge/tenant_info is an unauthenticated endpoint that returns the cloud ID
	// for any Atlassian cloud site.
	body, err := j.get(ctx, j.baseURL+"/_edge/tenant_info")
	if err != nil {
		return "", fmt.Errorf("jira: resolve cloud ID: %w", err)
	}

	var info struct {
		CloudID string `json:"cloudId"`
	}
	if err := json.Unmarshal(body, &info); err != nil || info.CloudID == "" {
		return "", fmt.Errorf("jira: parse tenant_info: %w", err)
	}

	return info.CloudID, nil
}

// jiraSearchResponse mirrors the Jira REST API v3 /search/jql response.
type jiraSearchResponse struct {
	Issues []jiraIssue `json:"issues"`
}

type jiraIssue struct {
	Key    string     `json:"key"`
	Fields jiraFields `json:"fields"`
}

type jiraFields struct {
	Summary   string      `json:"summary"`
	Status    jiraStatus  `json:"status"`
	IssueType jiraNameVal `json:"issuetype"`
}

type jiraStatus struct {
	Name           string         `json:"name"`
	StatusCategory jiraNameKeyVal `json:"statusCategory"`
}

type jiraNameVal struct {
	Name string `json:"name"`
}

type jiraNameKeyVal struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (j *JiraConnector) Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error) {
	apiBase, err := j.apiBase(ctx)
	if err != nil {
		return nil, err
	}

	dateStr := date.Format("2006-01-02")
	nextDate := date.AddDate(0, 0, 1).Format("2006-01-02")

	payload, _ := json.Marshal(map[string]any{
		"jql":        fmt.Sprintf(`assignee = currentUser() AND updated >= "%s" AND updated < "%s"`, dateStr, nextDate),
		"fields":     []string{"summary", "status", "issuetype"},
		"maxResults": 100,
	})

	respBody, err := j.post(ctx, apiBase+"/rest/api/3/search/jql", payload)
	if err != nil {
		return nil, fmt.Errorf("jira: search: %w", err)
	}

	var result jiraSearchResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("jira: parse response: %w", err)
	}

	items := make([]db.ActivityItem, 0, len(result.Issues))
	for _, issue := range result.Issues {
		items = append(items, db.ActivityItem{
			Source:     "jira",
			ExternalID: issue.Key,
			Kind:       jiraKind(issue.Fields.Status.StatusCategory.Key),
			Title:      fmt.Sprintf("[%s] %s", issue.Key, issue.Fields.Summary),
			URL:        fmt.Sprintf("%s/browse/%s", j.baseURL, issue.Key),
			Metadata:   issue.Fields.IssueType.Name,
		})
	}

	return items, nil
}

func (j *JiraConnector) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	j.setHeaders(req)
	return j.do(req)
}

func (j *JiraConnector) post(ctx context.Context, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	j.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	return j.do(req)
}

func (j *JiraConnector) setHeaders(req *http.Request) {
	creds := base64.StdEncoding.EncodeToString([]byte(j.email + ":" + j.token))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Accept", "application/json")
}

func (j *JiraConnector) do(req *http.Request) ([]byte, error) {
	resp, err := j.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// jiraKind maps a Jira statusCategory key to an ActivityItem kind.
func jiraKind(categoryKey string) string {
	switch categoryKey {
	case "done":
		return "jira_done"
	case "indeterminate":
		return "jira_in_progress"
	default:
		return "jira_todo"
	}
}

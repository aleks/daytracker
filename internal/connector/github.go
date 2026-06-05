package connector

import (
	"bytes"
	"context"
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

const githubGraphQLEndpoint = "https://api.github.com/graphql"

// GitHubConnector fetches PRs authored or reviewed by the current user via the GitHub GraphQL API.
type GitHubConnector struct {
	token  string
	client *http.Client

	usernameOnce sync.Once
	username     string
	usernameErr  error
}

func NewGitHub() *GitHubConnector {
	return &GitHubConnector{
		token:  os.Getenv("DAYTRACKER_GITHUB_TOKEN"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *GitHubConnector) Name() string { return "github" }

func (g *GitHubConnector) IsConfigured() bool { return g.token != "" }

func (g *GitHubConnector) KindLabel(kind string) string {
	switch kind {
	case "authored_open":
		return "open"
	case "authored_merged":
		return "merged"
	case "authored_closed":
		return "closed"
	case "authored_draft":
		return "draft"
	case "authored_approved":
		return "approved"
	case "authored_in_review":
		return "in review"
	case "authored_changes_requested":
		return "changes requested"
	case "reviewed_open":
		return "reviewed · open"
	case "reviewed_merged":
		return "reviewed · merged"
	case "reviewed_closed":
		return "reviewed · closed"
	case "reviewed_draft":
		return "reviewed · draft"
	case "reviewed_approved":
		return "reviewed · approved"
	case "reviewed_changes_requested":
		return "reviewed · changes requested"
	case "reviewed_in_review":
		return "reviewed · in review"
	default:
		return kind
	}
}

// ShouldCarryForward carries over authored PRs that are still open or draft.
// Reviewed PRs are excluded — reviews are date-specific events.
func (g *GitHubConnector) ShouldCarryForward(kind string) bool {
	switch kind {
	case "authored_open", "authored_draft", "authored_in_review", "authored_approved", "authored_changes_requested":
		return true
	}
	return false
}

func (g *GitHubConnector) IsTerminal(kind string) bool {
	switch kind {
	case "authored_merged", "authored_closed", "reviewed_merged", "reviewed_closed":
		return true
	}
	return false
}

func (g *GitHubConnector) resolveUsername(ctx context.Context) (string, error) {
	g.usernameOnce.Do(func() {
		data, err := g.graphql(ctx, `query { viewer { login } }`, nil)
		if err != nil {
			g.usernameErr = fmt.Errorf("github: resolve username: %w", err)
			return
		}
		var resp struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			g.usernameErr = fmt.Errorf("github: parse viewer: %w", err)
			return
		}
		g.username = resp.Viewer.Login
	})
	return g.username, g.usernameErr
}

// ghPRNode is the shape of a PullRequest node returned from a GraphQL search.
type ghPRNode struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"`
	Author         struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

const prSearchQuery = `
query($q: String!) {
  search(query: $q, type: ISSUE, first: 100) {
    nodes {
      ... on PullRequest {
        number
        title
        url
        state
        isDraft
        reviewDecision
        author { login }
        repository { nameWithOwner }
      }
    }
  }
}`

func (g *GitHubConnector) Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error) {
	username, err := g.resolveUsername(ctx)
	if err != nil {
		return nil, err
	}

	dateStr := date.Format("2006-01-02")
	fetchingToday := isToday(date)

	authored, err := g.searchPRs(ctx, fmt.Sprintf("is:pr author:%s created:%s", username, dateStr))
	if err != nil {
		return nil, fmt.Errorf("github fetch authored: %w", err)
	}

	reviewed, err := g.searchPRs(ctx, fmt.Sprintf("is:pr reviewed-by:%s updated:%s", username, dateStr))
	if err != nil {
		return nil, fmt.Errorf("github fetch reviewed: %w", err)
	}

	// When fetching today, also pull all currently open authored PRs so that
	// PRs with no recent activity still appear (rather than silently falling
	// out of carry-forward if they were never fetched).
	if fetchingToday {
		openAuthored, err := g.searchPRs(ctx, fmt.Sprintf("is:pr author:%s is:open", username))
		if err != nil {
			return nil, fmt.Errorf("github fetch open authored: %w", err)
		}
		authored = mergePRs(authored, openAuthored)
	}

	var items []db.ActivityItem

	authoredIDs := make(map[string]bool, len(authored))
	for _, pr := range authored {
		id := fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number)
		authoredIDs[id] = true
		items = append(items, db.ActivityItem{
			Source:     "github",
			ExternalID: id,
			Kind:       "authored_" + prStateFromDetail(pr.State, pr.IsDraft, pr.ReviewDecision),
			Title:      pr.Title,
			URL:        pr.URL,
			Metadata:   pr.Repository.NameWithOwner,
		})
	}

	for _, pr := range reviewed {
		if strings.EqualFold(pr.Author.Login, username) {
			continue
		}
		id := fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number)
		if authoredIDs[id] {
			continue
		}
		items = append(items, db.ActivityItem{
			Source:     "github",
			ExternalID: id,
			Kind:       "reviewed_" + prState(pr.State, pr.IsDraft),
			Title:      pr.Title,
			URL:        pr.URL,
			Metadata:   pr.Repository.NameWithOwner,
		})
	}

	return items, nil
}

// mergePRs appends PRs from extra that are not already present in base
// (deduplicated by repository+number).
func mergePRs(base, extra []ghPRNode) []ghPRNode {
	seen := make(map[string]bool, len(base))
	for _, pr := range base {
		seen[fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number)] = true
	}
	for _, pr := range extra {
		key := fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number)
		if !seen[key] {
			base = append(base, pr)
		}
	}
	return base
}


func (g *GitHubConnector) searchPRs(ctx context.Context, q string) ([]ghPRNode, error) {
	data, err := g.graphql(ctx, prSearchQuery, map[string]any{"q": q})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Search struct {
			Nodes []ghPRNode `json:"nodes"`
		} `json:"search"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("github: parse search: %w", err)
	}

	// Nodes that don't match the PullRequest inline fragment come back with zero
	// values; filter them out.
	var prs []ghPRNode
	for _, n := range resp.Search.Nodes {
		if n.Number != 0 {
			prs = append(prs, n)
		}
	}
	return prs, nil
}

func prState(state string, isDraft bool) string {
	switch strings.ToUpper(state) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}
	if isDraft {
		return "draft"
	}
	return "open"
}

// ── Status refresh ────────────────────────────────────────────────────────────

// RefreshStatuses fetches live PR state for all items in a single batched GraphQL
// request using indexed aliases, rather than one round-trip per PR.
func (g *GitHubConnector) RefreshStatuses(ctx context.Context, items []PRStatusItem) ([]PRStatusUpdate, error) {
	if len(items) == 0 {
		return nil, nil
	}

	// Build a dynamic query: one alias per PR.
	var sb strings.Builder
	sb.WriteString("query {")
	var valid []PRStatusItem
	for _, item := range items {
		repo, numStr, ok := parseExternalID(item.ExternalID)
		if !ok {
			continue
		}
		owner, repoName, ok := splitRepo(repo)
		if !ok {
			continue
		}
		var num int
		if _, err := fmt.Sscan(numStr, &num); err != nil {
			continue
		}
		fmt.Fprintf(&sb,
			"\n  pr_%d: repository(owner: %q, name: %q) { pullRequest(number: %d) { state isDraft reviewDecision } }",
			len(valid), owner, repoName, num,
		)
		valid = append(valid, item)
	}
	sb.WriteString("\n}")

	if len(valid) == 0 {
		return nil, nil
	}

	data, err := g.graphql(ctx, sb.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("github: refresh statuses: %w", err)
	}

	var rawData map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawData); err != nil {
		return nil, fmt.Errorf("github: parse refresh response: %w", err)
	}

	type prResult struct {
		PullRequest *struct {
			State          string `json:"state"`
			IsDraft        bool   `json:"isDraft"`
			ReviewDecision string `json:"reviewDecision"`
		} `json:"pullRequest"`
	}

	updates := make([]PRStatusUpdate, 0, len(valid))
	for i, item := range valid {
		raw, ok := rawData[fmt.Sprintf("pr_%d", i)]
		if !ok {
			continue
		}
		var repo prResult
		if err := json.Unmarshal(raw, &repo); err != nil || repo.PullRequest == nil {
			continue
		}
		pr := repo.PullRequest
		role := roleFromKind(item.CurrentKind)
		updates = append(updates, PRStatusUpdate{
			ExternalID: item.ExternalID,
			Kind:       role + "_" + prStateFromDetail(pr.State, pr.IsDraft, pr.ReviewDecision),
		})
	}

	return updates, nil
}

func roleFromKind(kind string) string {
	if idx := strings.Index(kind, "_"); idx >= 0 {
		return kind[:idx]
	}
	return "authored"
}

func prStateFromDetail(state string, isDraft bool, reviewDecision string) string {
	switch strings.ToUpper(state) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}
	if isDraft {
		return "draft"
	}
	switch reviewDecision {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes_requested"
	case "REVIEW_REQUIRED":
		return "in_review"
	default:
		return "open"
	}
}

// parseExternalID splits "owner/repo#123" into ("owner/repo", "123", true).
func parseExternalID(id string) (repo, number string, ok bool) {
	idx := strings.LastIndex(id, "#")
	if idx < 0 {
		return "", "", false
	}
	return id[:idx], id[idx+1:], true
}

// splitRepo splits "owner/repo" into ("owner", "repo", true).
func splitRepo(nameWithOwner string) (owner, name string, ok bool) {
	parts := strings.SplitN(nameWithOwner, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// graphql executes a GraphQL query and returns the data field of the response.
func (g *GitHubConnector) graphql(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github graphql: status %d: %s", resp.StatusCode, string(body))
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("github graphql: parse response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, len(envelope.Errors))
		for i, e := range envelope.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("github graphql errors: %s", strings.Join(msgs, "; "))
	}

	return envelope.Data, nil
}

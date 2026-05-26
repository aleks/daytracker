package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// GitHubConnector fetches PRs authored or reviewed by the current user.
type GitHubConnector struct{}

func NewGitHub() *GitHubConnector { return &GitHubConnector{} }

func (g *GitHubConnector) Name() string { return "github" }

func (g *GitHubConnector) IsConfigured() bool {
	// gh CLI handles auth itself; we just check it's present and logged in.
	err := exec.Command("gh", "auth", "status").Run()
	return err == nil
}

// ghSearchPR is the shape returned by gh search prs --json.
type ghSearchPR struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"` // "open" | "closed" | "merged"
	IsDraft    bool   `json:"isDraft"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func (g *GitHubConnector) Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error) {
	dateStr := date.Format("2006-01-02")

	authored, err := searchPRs(ctx, "--author", "@me", "--created", dateStr)
	if err != nil {
		return nil, fmt.Errorf("github fetch authored: %w", err)
	}

	reviewed, err := searchPRs(ctx, "--reviewed-by", "@me", "--updated", dateStr)
	if err != nil {
		return nil, fmt.Errorf("github fetch reviewed: %w", err)
	}

	var items []db.ActivityItem

	for _, pr := range authored {
		items = append(items, db.ActivityItem{
			Source:     "github",
			ExternalID: fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number),
			Kind:       prKindFromSearch(pr, "authored"),
			Title:      pr.Title,
			URL:        pr.URL,
			Metadata:   pr.Repository.NameWithOwner,
		})
	}

	// Deduplicate reviewed: skip PRs the user also authored (already included above).
	authoredIDs := make(map[string]bool, len(authored))
	for _, pr := range authored {
		authoredIDs[fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number)] = true
	}
	for _, pr := range reviewed {
		id := fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number)
		if authoredIDs[id] {
			continue
		}
		items = append(items, db.ActivityItem{
			Source:     "github",
			ExternalID: id,
			Kind:       prKindFromSearch(pr, "reviewed"),
			Title:      pr.Title,
			URL:        pr.URL,
			Metadata:   pr.Repository.NameWithOwner,
		})
	}

	return items, nil
}

func searchPRs(ctx context.Context, extraArgs ...string) ([]ghSearchPR, error) {
	args := append([]string{"search", "prs",
		"--json", "number,title,url,state,isDraft,repository",
		"--limit", "100",
	}, extraArgs...)

	out, err := runGH(ctx, args...)
	if err != nil {
		return nil, err
	}

	var prs []ghSearchPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return prs, nil
}

// prKindFromSearch derives the initial kind from a search result.
// role is either "authored" or "reviewed".
func prKindFromSearch(pr ghSearchPR, role string) string {
	switch {
	case strings.EqualFold(pr.State, "merged"):
		return "pr_merged"
	case strings.EqualFold(pr.State, "closed"):
		return "pr_closed"
	case pr.IsDraft:
		return "pr_draft"
	case role == "reviewed":
		return "pr_reviewed"
	default:
		return "pr_open"
	}
}

// ── Status refresh ────────────────────────────────────────────────────────────

// ghPRDetail is the shape returned by gh pr view --json.
type ghPRDetail struct {
	Number         int    `json:"number"`
	State          string `json:"state"` // "OPEN" | "CLOSED" | "MERGED"
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"` // "APPROVED" | "CHANGES_REQUESTED" | "REVIEW_REQUIRED" | ""
	MergedAt       string `json:"mergedAt"`
}

// RefreshStatuses fetches current status for each supplied external ID.
// ExternalIDs are in the form "owner/repo#number".
func (g *GitHubConnector) RefreshStatuses(ctx context.Context, externalIDs []string) ([]PRStatusUpdate, error) {
	updates := make([]PRStatusUpdate, 0, len(externalIDs))

	for _, id := range externalIDs {
		owner, number, ok := parseExternalID(id)
		if !ok {
			continue
		}

		out, err := runGH(ctx, "pr", "view", number,
			"--repo", owner,
			"--json", "number,state,isDraft,reviewDecision,mergedAt",
		)
		if err != nil {
			// PR may have been deleted or repo access lost; skip silently.
			continue
		}

		var detail ghPRDetail
		if err := json.Unmarshal(out, &detail); err != nil {
			continue
		}

		updates = append(updates, PRStatusUpdate{
			ExternalID: id,
			Kind:       prKindFromDetail(detail),
		})
	}

	return updates, nil
}

// prKindFromDetail derives a kind from a live pr view result.
func prKindFromDetail(d ghPRDetail) string {
	switch strings.ToUpper(d.State) {
	case "MERGED":
		return "pr_merged"
	case "CLOSED":
		return "pr_closed"
	}
	// OPEN
	if d.IsDraft {
		return "pr_draft"
	}
	switch d.ReviewDecision {
	case "APPROVED":
		return "pr_approved"
	case "CHANGES_REQUESTED":
		return "pr_changes_requested"
	case "REVIEW_REQUIRED":
		return "pr_in_review"
	default:
		return "pr_open"
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

func runGH(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w — %s", strings.Join(args, " "), err, stderr.String())
	}
	return out, nil
}

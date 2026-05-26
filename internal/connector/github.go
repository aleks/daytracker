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
type GitHubConnector struct {
	username string // resolved lazily on first Fetch
	ghRunner func(ctx context.Context, args ...string) ([]byte, error)
}

func NewGitHub() *GitHubConnector {
	return &GitHubConnector{ghRunner: runGH}
}

func (g *GitHubConnector) resolveUsername(ctx context.Context) error {
	if g.username != "" {
		return nil
	}
	out, err := g.ghRunner(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return fmt.Errorf("github: resolve username: %w", err)
	}
	g.username = strings.TrimSpace(string(out))
	return nil
}

func (g *GitHubConnector) Name() string { return "github" }

func (g *GitHubConnector) IsConfigured() bool {
	// gh CLI handles auth itself; we just check it's present and logged in.
	err := exec.Command("gh", "auth", "status").Run()
	return err == nil
}

// ghSearchPR is the shape returned by gh search prs --json.
type ghSearchPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	State   string `json:"state"` // "open" | "closed" | "merged"
	IsDraft bool   `json:"isDraft"`
	Author  struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func (g *GitHubConnector) Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error) {
	if err := g.resolveUsername(ctx); err != nil {
		return nil, err
	}

	dateStr := date.Format("2006-01-02")

	authored, err := g.searchPRs(ctx, "--author", "@me", "--created", dateStr)
	if err != nil {
		return nil, fmt.Errorf("github fetch authored: %w", err)
	}

	reviewed, err := g.searchPRs(ctx, "--reviewed-by", "@me", "--updated", dateStr)
	if err != nil {
		return nil, fmt.Errorf("github fetch reviewed: %w", err)
	}

	var items []db.ActivityItem

	// Build authored set — used both for items and for dedup below.
	authoredIDs := make(map[string]bool, len(authored))
	for _, pr := range authored {
		id := fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number)
		authoredIDs[id] = true
		items = append(items, db.ActivityItem{
			Source:     "github",
			ExternalID: id,
			Kind:       prKindFromSearch(pr, "authored"),
			Title:      pr.Title,
			URL:        pr.URL,
			Metadata:   pr.Repository.NameWithOwner,
		})
	}

	// Reviewed list: exclude any PR whose author is the current user,
	// regardless of whether it appeared in the authored query for this date.
	for _, pr := range reviewed {
		if strings.EqualFold(pr.Author.Login, g.username) {
			continue
		}
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

func (g *GitHubConnector) searchPRs(ctx context.Context, extraArgs ...string) ([]ghSearchPR, error) {
	args := append([]string{"search", "prs",
		"--json", "number,title,url,state,isDraft,author,repository",
		"--limit", "100",
	}, extraArgs...)

	out, err := g.ghRunner(ctx, args...)
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
// Kinds are prefixed with the role: "authored_*" or "reviewed_*".
func prKindFromSearch(pr ghSearchPR, role string) string {
	return role + "_" + prState(pr.State, pr.IsDraft)
}

// prState maps raw state + isDraft to the state suffix shared by both roles.
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

// ghPRDetail is the shape returned by gh pr view --json.
type ghPRDetail struct {
	Number         int    `json:"number"`
	State          string `json:"state"` // "OPEN" | "CLOSED" | "MERGED"
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"` // "APPROVED" | "CHANGES_REQUESTED" | "REVIEW_REQUIRED" | ""
	MergedAt       string `json:"mergedAt"`
}

// RefreshStatuses fetches live state for each item and returns updated kinds.
// The role prefix ("authored" / "reviewed") is preserved from CurrentKind.
func (g *GitHubConnector) RefreshStatuses(ctx context.Context, items []PRStatusItem) ([]PRStatusUpdate, error) {
	updates := make([]PRStatusUpdate, 0, len(items))

	for _, item := range items {
		owner, number, ok := parseExternalID(item.ExternalID)
		if !ok {
			continue
		}

		out, err := g.ghRunner(ctx, "pr", "view", number,
			"--repo", owner,
			"--json", "number,state,isDraft,reviewDecision,mergedAt",
		)
		if err != nil {
			// PR deleted or repo access lost — skip silently.
			continue
		}

		var detail ghPRDetail
		if err := json.Unmarshal(out, &detail); err != nil {
			continue
		}

		role := roleFromKind(item.CurrentKind)
		updates = append(updates, PRStatusUpdate{
			ExternalID: item.ExternalID,
			Kind:       role + "_" + prStateFromDetail(detail),
		})
	}

	return updates, nil
}

// roleFromKind extracts "authored" or "reviewed" from a kind like "authored_open".
// Falls back to "authored" if the kind has no underscore.
func roleFromKind(kind string) string {
	if idx := strings.Index(kind, "_"); idx >= 0 {
		return kind[:idx]
	}
	return "authored"
}

// prStateFromDetail maps a live pr view result to a state suffix.
func prStateFromDetail(d ghPRDetail) string {
	switch strings.ToUpper(d.State) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}
	if d.IsDraft {
		return "draft"
	}
	switch d.ReviewDecision {
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

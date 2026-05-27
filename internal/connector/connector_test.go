package connector

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// ── Registry ──────────────────────────────────────────────────────────────────

type stubConnector struct{ name string }

func (s *stubConnector) Name() string                         { return s.name }
func (s *stubConnector) IsConfigured() bool                   { return true }
func (s *stubConnector) KindLabel(kind string) string         { return kind }
func (s *stubConnector) ShouldCarryForward(_ string) bool     { return false }
func (s *stubConnector) Fetch(_ context.Context, _ time.Time) ([]db.ActivityItem, error) {
	return nil, nil
}

func TestRegistry_RegisterAndAll(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubConnector{"github"})
	r.Register(&stubConnector{"jira"})
	all := r.All()
	require.Len(t, all, 2)
	assert.Equal(t, "github", all[0].Name())
	assert.Equal(t, "jira", all[1].Name())
}

func TestRegistry_Get_Found(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubConnector{"github"})
	c, ok := r.Get("github")
	require.True(t, ok)
	assert.Equal(t, "github", c.Name())
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("missing")
	assert.False(t, ok)
}

func TestRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	assert.Empty(t, r.All())
}

// ── GitHub: prState ───────────────────────────────────────────────────────────

func TestPRState(t *testing.T) {
	tests := []struct {
		state   string
		isDraft bool
		want    string
	}{
		{"MERGED", false, "merged"},
		{"merged", false, "merged"},
		{"CLOSED", false, "closed"},
		{"closed", false, "closed"},
		{"OPEN", true, "draft"},
		{"open", true, "draft"},
		{"OPEN", false, "open"},
		{"open", false, "open"},
		{"", false, "open"},
	}
	for _, tc := range tests {
		t.Run(tc.state+"_draft="+boolStr(tc.isDraft), func(t *testing.T) {
			assert.Equal(t, tc.want, prState(tc.state, tc.isDraft))
		})
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ── GitHub: prStateFromDetail ─────────────────────────────────────────────────

func TestPRStateFromDetail(t *testing.T) {
	tests := []struct {
		name           string
		state          string
		isDraft        bool
		reviewDecision string
		want           string
	}{
		{"merged", "MERGED", false, "", "merged"},
		{"closed", "CLOSED", false, "", "closed"},
		{"draft", "OPEN", true, "", "draft"},
		{"approved", "OPEN", false, "APPROVED", "approved"},
		{"changes_requested", "OPEN", false, "CHANGES_REQUESTED", "changes_requested"},
		{"in_review", "OPEN", false, "REVIEW_REQUIRED", "in_review"},
		{"open_no_review", "OPEN", false, "", "open"},
		{"open_unknown_review", "OPEN", false, "SOMETHING_ELSE", "open"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, prStateFromDetail(tc.state, tc.isDraft, tc.reviewDecision))
		})
	}
}

// ── GitHub: roleFromKind ──────────────────────────────────────────────────────

func TestRoleFromKind(t *testing.T) {
	assert.Equal(t, "authored", roleFromKind("authored_open"))
	assert.Equal(t, "reviewed", roleFromKind("reviewed_merged"))
	assert.Equal(t, "authored", roleFromKind("nounderscore"))
	assert.Equal(t, "authored", roleFromKind(""))
}

// ── GitHub: parseExternalID ───────────────────────────────────────────────────

func TestParseExternalID(t *testing.T) {
	repo, num, ok := parseExternalID("owner/repo#42")
	require.True(t, ok)
	assert.Equal(t, "owner/repo", repo)
	assert.Equal(t, "42", num)
}

func TestParseExternalID_NoHash(t *testing.T) {
	_, _, ok := parseExternalID("owner/repo")
	assert.False(t, ok)
}

func TestParseExternalID_RepoWithHash(t *testing.T) {
	// Last # is the separator — repo name can't contain # but safety check.
	repo, num, ok := parseExternalID("org/repo#123")
	require.True(t, ok)
	assert.Equal(t, "org/repo", repo)
	assert.Equal(t, "123", num)
}

// ── Jira: jiraKind ────────────────────────────────────────────────────────────

func TestJiraKind(t *testing.T) {
	assert.Equal(t, "jira_done", jiraKind("done"))
	assert.Equal(t, "jira_in_progress", jiraKind("indeterminate"))
	assert.Equal(t, "jira_todo", jiraKind("new"))
	assert.Equal(t, "jira_todo", jiraKind(""))
	assert.Equal(t, "jira_todo", jiraKind("unknown"))
}

// ── KindLabel: GitHub ─────────────────────────────────────────────────────────

func TestGitHub_KindLabel(t *testing.T) {
	g := NewGitHub()
	tests := []struct {
		kind string
		want string
	}{
		{"authored_open", "open"},
		{"authored_merged", "merged"},
		{"authored_closed", "closed"},
		{"authored_draft", "draft"},
		{"reviewed_open", "reviewed · open"},
		{"reviewed_approved", "reviewed · approved"},
		{"unknown_kind", "unknown_kind"},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			assert.Equal(t, tc.want, g.KindLabel(tc.kind))
		})
	}
}

// ── KindLabel: Jira ───────────────────────────────────────────────────────────

func TestJira_KindLabel(t *testing.T) {
	j := NewJira()
	tests := []struct {
		kind string
		want string
	}{
		{"jira_todo", "to do"},
		{"jira_in_progress", "in progress"},
		{"jira_done", "done"},
		{"unknown_kind", "unknown_kind"},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			assert.Equal(t, tc.want, j.KindLabel(tc.kind))
		})
	}
}

// ── KindLabel: Confluence ─────────────────────────────────────────────────────

func TestConfluence_KindLabel(t *testing.T) {
	c := NewConfluence()
	tests := []struct {
		kind string
		want string
	}{
		{"confluence_created", "created"},
		{"confluence_edited", "edited"},
		{"confluence_commented", "commented"},
		{"unknown_kind", "unknown_kind"},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			assert.Equal(t, tc.want, c.KindLabel(tc.kind))
		})
	}
}

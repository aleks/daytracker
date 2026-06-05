package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ghFakeServer creates a TLS test server that serves pre-canned GraphQL data values in
// sequence. Each POST consumes the next entry; pass nil to simulate an HTTP 500.
func ghFakeServer(t *testing.T, dataValues ...any) *GitHubConnector {
	t.Helper()
	i := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i >= len(dataValues) {
			http.Error(w, "unexpected graphql call", http.StatusInternalServerError)
			return
		}
		v := dataValues[i]
		i++
		if v == nil {
			http.Error(w, "simulated server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": v})
	}))
	t.Cleanup(srv.Close)
	return &GitHubConnector{token: "fake-token", client: clientFor(srv)}
}

// newTestGitHub creates a GitHubConnector with a pre-seeded username,
// bypassing the viewer query.
func newTestGitHub(t *testing.T, username string, dataValues ...any) *GitHubConnector {
	t.Helper()
	g := ghFakeServer(t, dataValues...)
	g.usernameOnce.Do(func() { g.username = username })
	return g
}

// searchData wraps PR nodes in the GraphQL search envelope expected by searchPRs.
func searchData(nodes []ghPRNode) any {
	return map[string]any{"search": map[string]any{"nodes": nodes}}
}

// makePRNode constructs a ghPRNode for use in tests.
func makePRNode(number int, title, url, state string, isDraft bool, authorLogin, repo string) ghPRNode {
	n := ghPRNode{Number: number, Title: title, URL: url, State: state, IsDraft: isDraft}
	n.Author.Login = authorLogin
	n.Repository.NameWithOwner = repo
	return n
}

// prEntry constructs a single alias entry for a batched RefreshStatuses response.
func prEntry(state string, isDraft bool, reviewDecision string) map[string]any {
	return map[string]any{
		"pullRequest": map[string]any{
			"state":          state,
			"isDraft":        isDraft,
			"reviewDecision": reviewDecision,
		},
	}
}

// ── Fetch: authored PRs ───────────────────────────────────────────────────────

func TestGitHub_Fetch_AuthoredPR(t *testing.T) {
	authored := []ghPRNode{makePRNode(1, "My PR", "https://github.com/org/repo/pull/1", "open", false, "alice", "org/repo")}
	g := newTestGitHub(t, "alice",
		searchData(authored),
		searchData([]ghPRNode{}),
	)

	items, err := g.Fetch(context.Background(), time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "github", items[0].Source)
	assert.Equal(t, "org/repo#1", items[0].ExternalID)
	assert.Equal(t, "authored_open", items[0].Kind)
	assert.Equal(t, "My PR", items[0].Title)
}

func TestGitHub_Fetch_ReviewedPR(t *testing.T) {
	reviewed := []ghPRNode{makePRNode(42, "Someone's PR", "https://github.com/org/repo/pull/42", "open", false, "bob", "org/repo")}
	g := newTestGitHub(t, "alice",
		searchData([]ghPRNode{}), // authored (date-scoped)
		searchData(reviewed),     // reviewed (date-scoped)
		searchData([]ghPRNode{}), // open authored (today-only)
	)

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "reviewed_open", items[0].Kind)
	assert.Equal(t, "org/repo#42", items[0].ExternalID)
}

func TestGitHub_Fetch_OwnPRExcludedFromReviewed(t *testing.T) {
	reviewed := []ghPRNode{makePRNode(7, "Alice's own PR", "", "", false, "alice", "org/repo")}
	g := newTestGitHub(t, "alice",
		searchData([]ghPRNode{}), // authored
		searchData(reviewed),     // reviewed
		searchData([]ghPRNode{}), // open authored
	)

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestGitHub_Fetch_AlreadyAuthoredNotDuplicated(t *testing.T) {
	pr := makePRNode(5, "Dual PR", "", "open", false, "alice", "org/repo")
	g := newTestGitHub(t, "alice",
		searchData([]ghPRNode{pr}), // authored date-scoped
		searchData([]ghPRNode{pr}), // reviewed
		searchData([]ghPRNode{pr}), // open authored — same PR, must not duplicate
	)

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "authored_open", items[0].Kind)
}

func TestGitHub_Fetch_AuthoredQueryError(t *testing.T) {
	g := newTestGitHub(t, "alice", nil) // first HTTP call returns 500
	_, err := g.Fetch(context.Background(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authored")
}

func TestGitHub_Fetch_ReviewedQueryError(t *testing.T) {
	g := newTestGitHub(t, "alice",
		searchData([]ghPRNode{}), // authored ok
		nil,                     // reviewed errors
	)
	_, err := g.Fetch(context.Background(), time.Now())
	require.Error(t, err)
}

func TestGitHub_Fetch_DraftPR(t *testing.T) {
	authored := []ghPRNode{makePRNode(3, "Draft PR", "", "open", true, "alice", "org/repo")}
	g := newTestGitHub(t, "alice",
		searchData(authored),
		searchData([]ghPRNode{}),
		searchData([]ghPRNode{}), // open authored
	)

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "authored_draft", items[0].Kind)
}

func TestGitHub_Fetch_MergedPR(t *testing.T) {
	authored := []ghPRNode{makePRNode(9, "Merged PR", "", "merged", false, "alice", "org/repo")}
	g := newTestGitHub(t, "alice",
		searchData(authored),
		searchData([]ghPRNode{}),
		searchData([]ghPRNode{}), // open authored
	)

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "authored_merged", items[0].Kind)
}

// ── Fetch: open-authored PRs (today only) ─────────────────────────────────────

func TestGitHub_Fetch_OpenAuthoredIncludedForToday(t *testing.T) {
	// A PR that was not created today (not in date-scoped authored) but is
	// currently open — should appear when fetching today.
	silentPR := makePRNode(99, "Silent PR", "https://github.com/org/repo/pull/99", "open", false, "alice", "org/repo")
	g := newTestGitHub(t, "alice",
		searchData([]ghPRNode{}),      // authored date-scoped: nothing today
		searchData([]ghPRNode{}),      // reviewed date-scoped
		searchData([]ghPRNode{silentPR}), // open authored: picks it up
	)

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "org/repo#99", items[0].ExternalID)
	assert.Equal(t, "authored_open", items[0].Kind)
}

func TestGitHub_Fetch_OpenAuthoredNotFetchedForPastDate(t *testing.T) {
	// For a past date, only two GraphQL calls should be made (no open-PR query).
	pastDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	authored := []ghPRNode{makePRNode(1, "Old PR", "", "open", false, "alice", "org/repo")}
	g := newTestGitHub(t, "alice",
		searchData(authored),
		searchData([]ghPRNode{}),
		// No third slot — server returns 500 if a third call is made.
	)

	items, err := g.Fetch(context.Background(), pastDate)
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestGitHub_Fetch_OpenAuthoredQueryError(t *testing.T) {
	g := newTestGitHub(t, "alice",
		searchData([]ghPRNode{}), // authored ok
		searchData([]ghPRNode{}), // reviewed ok
		nil,                     // open authored errors
	)
	_, err := g.Fetch(context.Background(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open authored")
}

// ── resolveUsername ───────────────────────────────────────────────────────────

func TestGitHub_ResolveUsername(t *testing.T) {
	g := ghFakeServer(t, map[string]any{"viewer": map[string]any{"login": "alice"}})
	username, err := g.resolveUsername(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "alice", username)
}

func TestGitHub_ResolveUsername_Cached(t *testing.T) {
	// Pre-seed via usernameOnce; no HTTP calls should be made.
	g := ghFakeServer(t) // no responses registered
	g.usernameOnce.Do(func() { g.username = "cached" })
	username, err := g.resolveUsername(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "cached", username)
}

func TestGitHub_ResolveUsername_Error(t *testing.T) {
	g := ghFakeServer(t, nil) // HTTP 500
	_, err := g.resolveUsername(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve username")
}

// ── RefreshStatuses ───────────────────────────────────────────────────────────

func TestGitHub_RefreshStatuses_UpdatesKind(t *testing.T) {
	g := newTestGitHub(t, "alice", map[string]any{
		"pr_0": prEntry("OPEN", false, "APPROVED"),
	})

	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "org/repo#1", CurrentKind: "authored_open"},
	})
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.Equal(t, "org/repo#1", updates[0].ExternalID)
	assert.Equal(t, "authored_approved", updates[0].Kind)
}

func TestGitHub_RefreshStatuses_PreservesRole(t *testing.T) {
	g := newTestGitHub(t, "alice", map[string]any{
		"pr_0": prEntry("MERGED", false, ""),
	})

	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "org/repo#2", CurrentKind: "reviewed_open"},
	})
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.Equal(t, "reviewed_merged", updates[0].Kind)
}

func TestGitHub_RefreshStatuses_SkipsInvalidID(t *testing.T) {
	g := newTestGitHub(t, "alice") // no HTTP calls expected
	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "no-hash-here", CurrentKind: "authored_open"},
	})
	require.NoError(t, err)
	assert.Empty(t, updates)
}

func TestGitHub_RefreshStatuses_NullPRSkipped(t *testing.T) {
	// A null pullRequest in the response means the PR was deleted or is inaccessible;
	// it must be silently skipped rather than causing an error.
	g := newTestGitHub(t, "alice", map[string]any{
		"pr_0": map[string]any{"pullRequest": nil},
	})
	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "org/repo#1", CurrentKind: "authored_open"},
	})
	require.NoError(t, err)
	assert.Empty(t, updates)
}

func TestGitHub_RefreshStatuses_BatchedInOneRequest(t *testing.T) {
	// Two items must be sent in a single GraphQL request (one HTTP call, two aliases).
	g := newTestGitHub(t, "alice", map[string]any{
		"pr_0": prEntry("OPEN", false, ""),
		"pr_1": prEntry("MERGED", false, ""),
	})

	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "org/repo#1", CurrentKind: "authored_open"},
		{ExternalID: "org/repo#2", CurrentKind: "authored_open"},
	})
	require.NoError(t, err)
	require.Len(t, updates, 2)
	assert.Equal(t, "authored_open", updates[0].Kind)
	assert.Equal(t, "authored_merged", updates[1].Kind)
}

// ── Fetch: PR review decision kinds ──────────────────────────────────────────

func TestGitHub_Fetch_InReviewPR(t *testing.T) {
	authored := []ghPRNode{makePRNode(10, "In Review PR", "https://github.com/org/repo/pull/10", "open", false, "alice", "org/repo")}
	authored[0].ReviewDecision = "REVIEW_REQUIRED"
	g := newTestGitHub(t, "alice",
		searchData(authored),
		searchData([]ghPRNode{}),
		searchData([]ghPRNode{}), // open authored
	)

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "authored_in_review", items[0].Kind)
}

func TestGitHub_Fetch_ApprovedPR(t *testing.T) {
	authored := []ghPRNode{makePRNode(11, "Approved PR", "https://github.com/org/repo/pull/11", "open", false, "alice", "org/repo")}
	authored[0].ReviewDecision = "APPROVED"
	g := newTestGitHub(t, "alice",
		searchData(authored),
		searchData([]ghPRNode{}),
		searchData([]ghPRNode{}), // open authored
	)

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "authored_approved", items[0].Kind)
}

// ── ShouldCarryForward ────────────────────────────────────────────────────────

func TestGitHub_ShouldCarryForward(t *testing.T) {
	g := &GitHubConnector{}

	cases := []struct {
		kind     string
		expected bool
	}{
		{"authored_open", true},
		{"authored_draft", true},
		{"authored_in_review", true},
		{"authored_approved", true},
		{"authored_changes_requested", true},
		{"authored_merged", false},
		{"authored_closed", false},
		{"reviewed_open", false},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			assert.Equal(t, tc.expected, g.ShouldCarryForward(tc.kind))
		})
	}
}

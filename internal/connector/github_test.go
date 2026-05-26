package connector

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ghFakeRunner returns a runner that replies with pre-canned JSON for each
// successive call. Calls beyond the slice length return an error.
func ghFakeRunner(responses ...[]byte) func(context.Context, ...string) ([]byte, error) {
	i := 0
	return func(_ context.Context, _ ...string) ([]byte, error) {
		if i >= len(responses) {
			return nil, errors.New("unexpected gh call")
		}
		resp := responses[i]
		i++
		if resp == nil {
			return nil, errors.New("gh error")
		}
		return resp, nil
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func makeSearchPRs(prs []ghSearchPR) []byte {
	return mustJSON(prs)
}

// newTestGitHub creates a GitHubConnector with a pre-seeded username and the
// given fake runner, bypassing the real gh CLI entirely.
func newTestGitHub(username string, runner func(context.Context, ...string) ([]byte, error)) *GitHubConnector {
	return &GitHubConnector{username: username, ghRunner: runner}
}

// ── Fetch: authored PRs ───────────────────────────────────────────────────────

func TestGitHub_Fetch_AuthoredPR(t *testing.T) {
	authored := []ghSearchPR{
		{Number: 1, Title: "My PR", URL: "https://github.com/org/repo/pull/1",
			State: "open", IsDraft: false,
			Author:     struct{ Login string `json:"login"` }{Login: "alice"},
			Repository: struct{ NameWithOwner string `json:"nameWithOwner"` }{NameWithOwner: "org/repo"}},
	}
	g := newTestGitHub("alice", ghFakeRunner(
		makeSearchPRs(authored),
		makeSearchPRs([]ghSearchPR{}),
	))

	items, err := g.Fetch(context.Background(), time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "github", items[0].Source)
	assert.Equal(t, "org/repo#1", items[0].ExternalID)
	assert.Equal(t, "authored_open", items[0].Kind)
	assert.Equal(t, "My PR", items[0].Title)
}

func TestGitHub_Fetch_ReviewedPR(t *testing.T) {
	reviewed := []ghSearchPR{
		{Number: 42, Title: "Someone's PR", URL: "https://github.com/org/repo/pull/42",
			State:      "open",
			Author:     struct{ Login string `json:"login"` }{Login: "bob"},
			Repository: struct{ NameWithOwner string `json:"nameWithOwner"` }{NameWithOwner: "org/repo"}},
	}
	g := newTestGitHub("alice", ghFakeRunner(
		makeSearchPRs([]ghSearchPR{}), // authored — empty
		makeSearchPRs(reviewed),       // reviewed
	))

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "reviewed_open", items[0].Kind)
	assert.Equal(t, "org/repo#42", items[0].ExternalID)
}

func TestGitHub_Fetch_OwnPRExcludedFromReviewed(t *testing.T) {
	// alice's own PR appears in reviewed list — must be excluded.
	reviewed := []ghSearchPR{
		{Number: 7, Title: "Alice's own PR",
			Author:     struct{ Login string `json:"login"` }{Login: "alice"},
			Repository: struct{ NameWithOwner string `json:"nameWithOwner"` }{NameWithOwner: "org/repo"}},
	}
	g := newTestGitHub("alice", ghFakeRunner(
		makeSearchPRs([]ghSearchPR{}),
		makeSearchPRs(reviewed),
	))

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestGitHub_Fetch_AlreadyAuthoredNotDuplicated(t *testing.T) {
	// PR appears in both authored and reviewed — should only appear once as authored.
	pr := ghSearchPR{
		Number: 5, Title: "Dual PR",
		Author:     struct{ Login string `json:"login"` }{Login: "alice"},
		Repository: struct{ NameWithOwner string `json:"nameWithOwner"` }{NameWithOwner: "org/repo"},
	}
	g := newTestGitHub("alice", ghFakeRunner(
		makeSearchPRs([]ghSearchPR{pr}),
		makeSearchPRs([]ghSearchPR{pr}),
	))

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	// alice's own PR is excluded from reviewed; no duplicate.
	require.Len(t, items, 1)
	assert.Equal(t, "authored_open", items[0].Kind)
}

func TestGitHub_Fetch_AuthoredQueryError(t *testing.T) {
	g := newTestGitHub("alice", ghFakeRunner(nil)) // first call errors
	_, err := g.Fetch(context.Background(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authored")
}

func TestGitHub_Fetch_ReviewedQueryError(t *testing.T) {
	g := newTestGitHub("alice", ghFakeRunner(
		makeSearchPRs([]ghSearchPR{}), // authored ok
		nil,                           // reviewed errors
	))
	_, err := g.Fetch(context.Background(), time.Now())
	require.Error(t, err)
}

func TestGitHub_Fetch_DraftPR(t *testing.T) {
	authored := []ghSearchPR{
		{Number: 3, Title: "Draft PR", State: "open", IsDraft: true,
			Author:     struct{ Login string `json:"login"` }{Login: "alice"},
			Repository: struct{ NameWithOwner string `json:"nameWithOwner"` }{NameWithOwner: "org/repo"}},
	}
	g := newTestGitHub("alice", ghFakeRunner(
		makeSearchPRs(authored),
		makeSearchPRs([]ghSearchPR{}),
	))

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "authored_draft", items[0].Kind)
}

func TestGitHub_Fetch_MergedPR(t *testing.T) {
	authored := []ghSearchPR{
		{Number: 9, Title: "Merged PR", State: "merged",
			Author:     struct{ Login string `json:"login"` }{Login: "alice"},
			Repository: struct{ NameWithOwner string `json:"nameWithOwner"` }{NameWithOwner: "org/repo"}},
	}
	g := newTestGitHub("alice", ghFakeRunner(
		makeSearchPRs(authored),
		makeSearchPRs([]ghSearchPR{}),
	))

	items, err := g.Fetch(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "authored_merged", items[0].Kind)
}

// ── resolveUsername ───────────────────────────────────────────────────────────

func TestGitHub_ResolveUsername(t *testing.T) {
	g := &GitHubConnector{
		ghRunner: ghFakeRunner([]byte("alice\n")),
	}
	require.NoError(t, g.resolveUsername(context.Background()))
	assert.Equal(t, "alice", g.username)
}

func TestGitHub_ResolveUsername_Cached(t *testing.T) {
	calls := 0
	g := &GitHubConnector{
		username: "cached",
		ghRunner: func(_ context.Context, _ ...string) ([]byte, error) {
			calls++
			return []byte("other"), nil
		},
	}
	require.NoError(t, g.resolveUsername(context.Background()))
	assert.Equal(t, "cached", g.username)
	assert.Equal(t, 0, calls, "runner must not be called when username is already set")
}

func TestGitHub_ResolveUsername_Error(t *testing.T) {
	g := &GitHubConnector{ghRunner: ghFakeRunner(nil)}
	err := g.resolveUsername(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve username")
}

// ── RefreshStatuses ───────────────────────────────────────────────────────────

func TestGitHub_RefreshStatuses_UpdatesKind(t *testing.T) {
	detail := ghPRDetail{State: "OPEN", ReviewDecision: "APPROVED"}
	g := newTestGitHub("alice", ghFakeRunner(mustJSON(detail)))

	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "org/repo#1", CurrentKind: "authored_open"},
	})
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.Equal(t, "org/repo#1", updates[0].ExternalID)
	assert.Equal(t, "authored_approved", updates[0].Kind)
}

func TestGitHub_RefreshStatuses_PreservesRole(t *testing.T) {
	detail := ghPRDetail{State: "MERGED"}
	g := newTestGitHub("alice", ghFakeRunner(mustJSON(detail)))

	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "org/repo#2", CurrentKind: "reviewed_open"},
	})
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.Equal(t, "reviewed_merged", updates[0].Kind)
}

func TestGitHub_RefreshStatuses_SkipsInvalidID(t *testing.T) {
	g := newTestGitHub("alice", ghFakeRunner()) // no calls expected
	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "no-hash-here", CurrentKind: "authored_open"},
	})
	require.NoError(t, err)
	assert.Empty(t, updates)
}

func TestGitHub_RefreshStatuses_SkipsOnGHError(t *testing.T) {
	g := newTestGitHub("alice", ghFakeRunner(nil)) // gh errors for the PR
	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "org/repo#1", CurrentKind: "authored_open"},
	})
	require.NoError(t, err)
	assert.Empty(t, updates, "failed PR lookup must be silently skipped")
}

func TestGitHub_RefreshStatuses_MultipleItems(t *testing.T) {
	open := ghPRDetail{State: "OPEN"}
	merged := ghPRDetail{State: "MERGED"}
	g := newTestGitHub("alice", ghFakeRunner(mustJSON(open), mustJSON(merged)))

	updates, err := g.RefreshStatuses(context.Background(), []PRStatusItem{
		{ExternalID: "org/repo#1", CurrentKind: "authored_open"},
		{ExternalID: "org/repo#2", CurrentKind: "authored_open"},
	})
	require.NoError(t, err)
	require.Len(t, updates, 2)
	assert.Equal(t, "authored_open", updates[0].Kind)
	assert.Equal(t, "authored_merged", updates[1].Kind)
}

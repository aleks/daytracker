package connector

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── JiraConnector.IsTerminal ──────────────────────────────────────────────────

func TestJira_IsTerminal(t *testing.T) {
	j := NewJira()
	assert.True(t, j.IsTerminal("jira_done"))
	assert.False(t, j.IsTerminal("jira_todo"))
	assert.False(t, j.IsTerminal("jira_in_progress"))
}

// ── JiraConnector.RefreshStatuses ─────────────────────────────────────────────

func TestJira_RefreshStatuses_UpdatesKind(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/jira/x/rest/api/3/issue/ABC-1": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{
				"fields": map[string]any{
					"status": map[string]any{
						"statusCategory": map[string]any{"key": "done"},
					},
				},
			})
		},
	})

	j := newJiraConnector(t, srv)
	updates, err := j.RefreshStatuses(t.Context(), []PRStatusItem{
		{ExternalID: "ABC-1", CurrentKind: "jira_in_progress"},
	})
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.Equal(t, "ABC-1", updates[0].ExternalID)
	assert.Equal(t, "jira_done", updates[0].Kind)
}

func TestJira_RefreshStatuses_NoChangeSkipped(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/jira/x/rest/api/3/issue/ABC-2": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{
				"fields": map[string]any{
					"status": map[string]any{
						"statusCategory": map[string]any{"key": "indeterminate"},
					},
				},
			})
		},
	})

	j := newJiraConnector(t, srv)
	updates, err := j.RefreshStatuses(t.Context(), []PRStatusItem{
		{ExternalID: "ABC-2", CurrentKind: "jira_in_progress"},
	})
	require.NoError(t, err)
	assert.Empty(t, updates, "no update should be returned when kind has not changed")
}

func TestJira_RefreshStatuses_HTTPError(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/jira/x/rest/api/3/issue/ABC-3": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		},
	})

	j := newJiraConnector(t, srv)
	_, err := j.RefreshStatuses(t.Context(), []PRStatusItem{
		{ExternalID: "ABC-3", CurrentKind: "jira_todo"},
	})
	require.Error(t, err)
}

func TestJira_RefreshStatuses_Empty(t *testing.T) {
	// No HTTP calls expected when the list is empty.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	j := newJiraConnector(t, srv)
	updates, err := j.RefreshStatuses(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, updates)
}

// ── JiraConnector.ShouldCarryForward ─────────────────────────────────────────

func TestJira_ShouldCarryForward(t *testing.T) {
	j := NewJira()
	cases := []struct {
		kind     string
		expected bool
	}{
		{"jira_todo", true},
		{"jira_in_progress", true},
		{"jira_done", false},
		{"unknown_kind", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			assert.Equal(t, tc.expected, j.ShouldCarryForward(tc.kind))
		})
	}
}

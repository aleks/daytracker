package connector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── YouTrackConnector: Name / IsConfigured ────────────────────────────────────

func TestYouTrackName(t *testing.T) {
	assert.Equal(t, "youtrack", NewYouTrack().Name())
}

func TestYouTrackIsConfigured_AllSet(t *testing.T) {
	t.Setenv("DAYTRACKER_YOUTRACK_BASE_URL", "https://example.youtrack.cloud")
	t.Setenv("DAYTRACKER_YOUTRACK_TOKEN", "perm:abc123")
	assert.True(t, NewYouTrack().IsConfigured())
}

func TestYouTrackIsConfigured_MissingURL(t *testing.T) {
	t.Setenv("DAYTRACKER_YOUTRACK_BASE_URL", "")
	t.Setenv("DAYTRACKER_YOUTRACK_TOKEN", "perm:abc123")
	assert.False(t, NewYouTrack().IsConfigured())
}

func TestYouTrackIsConfigured_MissingToken(t *testing.T) {
	t.Setenv("DAYTRACKER_YOUTRACK_BASE_URL", "https://example.youtrack.cloud")
	t.Setenv("DAYTRACKER_YOUTRACK_TOKEN", "")
	assert.False(t, NewYouTrack().IsConfigured())
}

func TestYouTrackConstructor_TrimsTrailingSlash(t *testing.T) {
	t.Setenv("DAYTRACKER_YOUTRACK_BASE_URL", "https://example.youtrack.cloud/")
	y := NewYouTrack()
	assert.Equal(t, "https://example.youtrack.cloud", y.baseURL)
}

// ── YouTrackConnector.ShouldCarryForward ──────────────────────────────────────

func TestYouTrack_ShouldCarryForward(t *testing.T) {
	y := NewYouTrack()
	cases := []struct {
		kind     string
		expected bool
	}{
		{"youtrack_created", false},
		{"youtrack_edited", false},
		{"youtrack_work", false},
		{"youtrack_resolved", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			assert.Equal(t, tc.expected, y.ShouldCarryForward(tc.kind))
		})
	}
}

// ── YouTrackConnector.KindLabel ───────────────────────────────────────────────

func TestYouTrack_KindLabel(t *testing.T) {
	y := NewYouTrack()
	assert.Equal(t, "created", y.KindLabel("youtrack_created"))
	assert.Equal(t, "edited", y.KindLabel("youtrack_edited"))
	assert.Equal(t, "time logged", y.KindLabel("youtrack_work"))
	assert.Equal(t, "resolved", y.KindLabel("youtrack_resolved"))
	assert.Equal(t, "unknown", y.KindLabel("unknown"))
}

// ── YouTrackConnector.Fetch ───────────────────────────────────────────────────

func newYouTrackConnector(t *testing.T, srv *httptest.Server) *YouTrackConnector {
	t.Helper()
	return &YouTrackConnector{
		baseURL: srv.URL,
		token:   "perm:test",
		client:  clientFor(srv),
	}
}

func ytActivityPageJSON(activities []map[string]any) []byte {
	body, _ := json.Marshal(map[string]any{
		"activities":  activities,
		"afterCursor": "",
		"hasAfter":    false,
	})
	return body
}

func ytIssueTarget(issueID, summary string) map[string]any {
	return map[string]any{
		"idReadable": issueID,
		"summary":    summary,
		"project":    map[string]any{"name": "Dev"},
	}
}

func TestYouTrack_Fetch_CreatedIssue(t *testing.T) {
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/api/activitiesPage": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer perm:test", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			w.Write(ytActivityPageJSON([]map[string]any{
				{
					"id":        "1-1",
					"timestamp": date.UnixMilli(),
					"$type":     "IssueCreatedActivityItem",
					"target":    ytIssueTarget("YT-15", "Fix the thing"),
				},
			}))
		},
	})

	y := newYouTrackConnector(t, srv)
	items, err := y.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "youtrack", items[0].Source)
	assert.Equal(t, "YT-15:created", items[0].ExternalID)
	assert.Equal(t, "youtrack_created", items[0].Kind)
	assert.Equal(t, "[YT-15] Fix the thing", items[0].Title)
	assert.Equal(t, "Dev", items[0].Metadata)
	assert.Contains(t, items[0].URL, "/issue/YT-15")
}

func TestYouTrack_Fetch_ResolvedIssue(t *testing.T) {
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/api/activitiesPage": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(ytActivityPageJSON([]map[string]any{
				{
					"id":        "1-2",
					"timestamp": date.UnixMilli(),
					"$type":     "IssueResolvedActivityItem",
					"target":    ytIssueTarget("YT-20", "Done task"),
				},
			}))
		},
	})

	y := newYouTrackConnector(t, srv)
	items, err := y.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "youtrack_resolved", items[0].Kind)
	assert.Equal(t, "YT-20:resolved", items[0].ExternalID)
}

func TestYouTrack_Fetch_EditedIssue(t *testing.T) {
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/api/activitiesPage": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(ytActivityPageJSON([]map[string]any{
				{
					"id":        "1-3",
					"timestamp": date.UnixMilli(),
					"$type":     "SimpleValueActivityItem",
					"target":    ytIssueTarget("YT-30", "Updated summary"),
				},
			}))
		},
	})

	y := newYouTrackConnector(t, srv)
	items, err := y.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "youtrack_edited", items[0].Kind)
	assert.Equal(t, "YT-30:edited", items[0].ExternalID)
}

func TestYouTrack_Fetch_WorkItem(t *testing.T) {
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	added, _ := json.Marshal([]map[string]any{
		{
			"duration": map[string]any{
				"minutes":      90,
				"presentation": "1h 30m",
			},
			"text": "Fixing tests",
		},
	})

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/api/activitiesPage": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(ytActivityPageJSON([]map[string]any{
				{
					"id":        "1-4",
					"timestamp": date.UnixMilli(),
					"$type":     "WorkItemActivityItem",
					"target": map[string]any{
						"issue": ytIssueTarget("YT-40", "Add connector"),
					},
					"added": json.RawMessage(added),
				},
			}))
		},
	})

	y := newYouTrackConnector(t, srv)
	items, err := y.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "youtrack_work", items[0].Kind)
	assert.Equal(t, "YT-40:work:1-4", items[0].ExternalID)
	assert.Contains(t, items[0].Title, "Fixing tests")
	assert.Contains(t, items[0].Title, "1h 30m")
}

func TestYouTrack_Fetch_Empty(t *testing.T) {
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/api/activitiesPage": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(ytActivityPageJSON([]map[string]any{}))
		},
	})

	y := newYouTrackConnector(t, srv)
	items, err := y.Fetch(t.Context(), date)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestYouTrack_Fetch_HTTPError(t *testing.T) {
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/api/activitiesPage": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		},
	})

	y := newYouTrackConnector(t, srv)
	_, err := y.Fetch(t.Context(), date)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestYouTrack_Fetch_Pagination(t *testing.T) {
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	callCount := 0
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/api/activitiesPage": func(w http.ResponseWriter, r *http.Request) {
			callCount++
			cursor := r.URL.Query().Get("cursor")
			w.Header().Set("Content-Type", "application/json")
			if cursor == "" {
				// First page: full page, has more.
				activities := make([]map[string]any, 100)
				for i := range activities {
					activities[i] = map[string]any{
						"id":        fmt.Sprintf("1-%d", i),
						"timestamp": date.UnixMilli(),
						"$type":     "IssueCreatedActivityItem",
						"target":    ytIssueTarget(fmt.Sprintf("YT-%d", i), fmt.Sprintf("Issue %d", i)),
					}
				}
				body, _ := json.Marshal(map[string]any{
					"activities":  activities,
					"afterCursor": "cursor-page-2",
					"hasAfter":    true,
				})
				w.Write(body)
			} else {
				// Second page: partial, no more.
				body, _ := json.Marshal(map[string]any{
					"activities": []map[string]any{
						{
							"id":        "1-200",
							"timestamp": date.UnixMilli(),
							"$type":     "IssueCreatedActivityItem",
							"target":    ytIssueTarget("YT-200", "Last issue"),
						},
					},
					"afterCursor": "",
					"hasAfter":    false,
				})
				w.Write(body)
			}
		},
	})

	y := newYouTrackConnector(t, srv)
	items, err := y.Fetch(t.Context(), date)
	require.NoError(t, err)
	assert.Equal(t, 101, len(items))
	assert.Equal(t, 2, callCount)
}

func TestYouTrack_Fetch_SkipsUnknownType(t *testing.T) {
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/api/activitiesPage": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(ytActivityPageJSON([]map[string]any{
				{
					"id":        "1-5",
					"timestamp": date.UnixMilli(),
					"$type":     "CommentActivityItem",
					"target":    ytIssueTarget("YT-50", "No comment"),
				},
			}))
		},
	})

	y := newYouTrackConnector(t, srv)
	items, err := y.Fetch(t.Context(), date)
	require.NoError(t, err)
	assert.Empty(t, items)
}
package connector

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientFor returns an *http.Client that always dials srv regardless of the
// request URL host. TLS cert verification is relaxed to allow the httptest
// self-signed certificate to be used for any hostname (e.g. api.atlassian.com).
func clientFor(srv *httptest.Server) *http.Client {
	addr := srv.Listener.Addr().String()
	// Copy the test server's TLS config (which contains the trusted root CA),
	// then disable hostname verification so the cert is accepted for any host.
	baseTLS := srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	baseTLS.InsecureSkipVerify = true //nolint:gosec // test-only transport
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: baseTLS,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}
}

// allPathsServer builds a single httptest.Server whose mux handles the given paths.
func allPathsServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ── Jira: cloud ID resolution ─────────────────────────────────────────────────

func TestJira_CloudID_OK(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/_edge/tenant_info": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]string{"cloudId": "jira-cid"})
		},
	})

	j := &JiraConnector{baseURL: srv.URL, email: "u@e.com", token: "tok", client: clientFor(srv)}
	base, err := j.apiBase(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://api.atlassian.com/ex/jira/jira-cid", base)
}

func TestJira_CloudID_ServerError(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/_edge/tenant_info": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "down", http.StatusServiceUnavailable)
		},
	})

	j := &JiraConnector{baseURL: srv.URL, email: "u@e.com", token: "tok", client: clientFor(srv)}
	_, err := j.apiBase(t.Context())
	require.Error(t, err)
}

func TestJira_CloudID_EmptyCloudID(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/_edge/tenant_info": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]string{"cloudId": ""})
		},
	})

	j := &JiraConnector{baseURL: srv.URL, email: "u@e.com", token: "tok", client: clientFor(srv)}
	_, err := j.apiBase(t.Context())
	require.Error(t, err)
}

// ── Jira: Fetch ───────────────────────────────────────────────────────────────

// newJiraConnector sets up a JiraConnector that routes all HTTP calls to srv.
// The cloudID is pre-seeded so apiBase() returns the gateway path for srv.
func newJiraConnector(t *testing.T, srv *httptest.Server) *JiraConnector {
	t.Helper()
	j := &JiraConnector{
		baseURL: "https://org.atlassian.net",
		email:   "u@e.com",
		token:   "tok",
		client:  clientFor(srv),
	}
	j.cloudIDOnce.Do(func() { j.cloudID = "x" })
	return j
}

func TestJira_Fetch_ItemFields(t *testing.T) {
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/jira/x/rest/api/3/search/jql": func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			jsonResponse(w, map[string]any{
				"issues": []any{map[string]any{
					"key": "ABC-99",
					"fields": map[string]any{
						"summary":   "Fix the bug",
						"issuetype": map[string]any{"name": "Bug"},
						"status": map[string]any{
							"name":           "Done",
							"statusCategory": map[string]any{"key": "done", "name": "Done"},
						},
					},
				}},
			})
		},
	})

	j := newJiraConnector(t, srv)
	items, err := j.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "jira", items[0].Source)
	assert.Equal(t, "ABC-99", items[0].ExternalID)
	assert.Equal(t, "jira_done", items[0].Kind)
	assert.Equal(t, "[ABC-99] Fix the bug", items[0].Title)
	assert.Equal(t, "Bug", items[0].Metadata)
}

func TestJira_Fetch_KindMapping(t *testing.T) {
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	issues := []any{
		map[string]any{"key": "P-1", "fields": map[string]any{
			"summary": "Done", "issuetype": map[string]any{"name": "Story"},
			"status": map[string]any{"statusCategory": map[string]any{"key": "done"}},
		}},
		map[string]any{"key": "P-2", "fields": map[string]any{
			"summary": "WIP", "issuetype": map[string]any{"name": "Task"},
			"status": map[string]any{"statusCategory": map[string]any{"key": "indeterminate"}},
		}},
		map[string]any{"key": "P-3", "fields": map[string]any{
			"summary": "Todo", "issuetype": map[string]any{"name": "Bug"},
			"status": map[string]any{"statusCategory": map[string]any{"key": "new"}},
		}},
	}

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/jira/x/rest/api/3/search/jql": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{"issues": issues})
		},
	})

	j := newJiraConnector(t, srv)
	items, err := j.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "jira_done", items[0].Kind)
	assert.Equal(t, "jira_in_progress", items[1].Kind)
	assert.Equal(t, "jira_todo", items[2].Kind)
}

func TestJira_Fetch_Empty(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/jira/x/rest/api/3/search/jql": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{"issues": []any{}})
		},
	})

	j := newJiraConnector(t, srv)
	items, err := j.Fetch(t.Context(), time.Now())
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestJira_Fetch_HTTPError(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/jira/x/rest/api/3/search/jql": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		},
	})

	j := newJiraConnector(t, srv)
	_, err := j.Fetch(t.Context(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// ── Confluence: cloud ID resolution ──────────────────────────────────────────

func TestConfluence_CloudID_OK(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/_edge/tenant_info": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]string{"cloudId": "cf-cid"})
		},
	})

	c := &ConfluenceConnector{baseURL: srv.URL, email: "u@e.com", token: "tok", client: clientFor(srv)}
	base, err := c.apiBase(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://api.atlassian.com/ex/confluence/cf-cid", base)
}

func TestConfluence_CloudID_Error(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/_edge/tenant_info": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		},
	})

	c := &ConfluenceConnector{baseURL: srv.URL, email: "u@e.com", token: "tok", client: clientFor(srv)}
	_, err := c.apiBase(t.Context())
	require.Error(t, err)
}

// ── Confluence: Fetch ─────────────────────────────────────────────────────────

const cfTestAccountID = "user-account-id"

// newConfluenceConnector sets up a ConfluenceConnector that routes all HTTP calls
// to srv. Cloud ID and user are pre-seeded.
func newConfluenceConnector(t *testing.T, srv *httptest.Server) *ConfluenceConnector {
	t.Helper()
	c := &ConfluenceConnector{
		baseURL: "https://org.atlassian.net",
		email:   "u@e.com",
		token:   "tok",
		client:  clientFor(srv),
	}
	c.cloudIDOnce.Do(func() { c.cloudID = "x" })
	c.userOnce.Do(func() {
		c.userID = cfTestAccountID
		c.spaceKey = "~testuser"
	})
	return c
}

func cfPage(id, title string, versionWhen time.Time, lastUpdatedBy, createdBy string, createdDate time.Time) map[string]any {
	return map[string]any{
		"id":    id,
		"title": title,
		"_links": map[string]any{
			"webui": "/pages/" + id,
		},
		"version": map[string]any{
			"when": versionWhen.UTC().Format(time.RFC3339),
		},
		"history": map[string]any{
			"createdBy":   map[string]any{"accountId": createdBy},
			"createdDate": createdDate.UTC().Format(time.RFC3339),
			"lastUpdated": map[string]any{
				"by":   map[string]any{"accountId": lastUpdatedBy},
				"when": versionWhen.UTC().Format(time.RFC3339),
			},
		},
		"space": map[string]any{"key": "~testuser", "name": "My Space"},
	}
}

func TestConfluence_Fetch_CreatedPage(t *testing.T) {
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	pageTime := date.Add(10 * time.Hour)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/confluence/x/wiki/rest/api/content": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{"results": []any{
				cfPage("p1", "New Page", pageTime, cfTestAccountID, cfTestAccountID, pageTime),
			}})
		},
	})

	c := newConfluenceConnector(t, srv)
	items, err := c.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "confluence", items[0].Source)
	assert.Equal(t, "page:p1", items[0].ExternalID)
	assert.Equal(t, "confluence_created", items[0].Kind)
	assert.Equal(t, "New Page", items[0].Title)
	assert.Equal(t, "My Space", items[0].Metadata)
}

func TestConfluence_Fetch_EditedPage(t *testing.T) {
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	pageTime := date.Add(10 * time.Hour)
	oldCreate := date.AddDate(0, -1, 0)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/confluence/x/wiki/rest/api/content": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{"results": []any{
				cfPage("p2", "Old Page", pageTime, cfTestAccountID, cfTestAccountID, oldCreate),
			}})
		},
	})

	c := newConfluenceConnector(t, srv)
	items, err := c.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "confluence_edited", items[0].Kind)
}

func TestConfluence_Fetch_NotByMe(t *testing.T) {
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	pageTime := date.Add(10 * time.Hour)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/confluence/x/wiki/rest/api/content": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{"results": []any{
				cfPage("p3", "Other's Page", pageTime, "other-id", "other-id", pageTime),
			}})
		},
	})

	c := newConfluenceConnector(t, srv)
	items, err := c.Fetch(t.Context(), date)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestConfluence_Fetch_OutsideDateWindow(t *testing.T) {
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	yesterday := date.Add(-1 * time.Hour)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/confluence/x/wiki/rest/api/content": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{"results": []any{
				cfPage("p4", "Old Page", yesterday, cfTestAccountID, cfTestAccountID, yesterday),
			}})
		},
	})

	c := newConfluenceConnector(t, srv)
	items, err := c.Fetch(t.Context(), date)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestConfluence_Fetch_Empty(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/confluence/x/wiki/rest/api/content": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{"results": []any{}})
		},
	})

	c := newConfluenceConnector(t, srv)
	items, err := c.Fetch(t.Context(), time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestConfluence_Fetch_HTTPError(t *testing.T) {
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/confluence/x/wiki/rest/api/content": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		},
	})

	c := newConfluenceConnector(t, srv)
	_, err := c.Fetch(t.Context(), time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestConfluence_Fetch_PersonalSpaceKeyFallback(t *testing.T) {
	// When the API returns no personalSpace.key, we expect ~accountId as fallback.
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	pageTime := date.Add(5 * time.Hour)

	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/confluence/x/wiki/rest/api/user/current": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, map[string]any{
				"accountId":     cfTestAccountID,
				"personalSpace": map[string]any{},
			})
		},
		"/ex/confluence/x/wiki/rest/api/content": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "~"+cfTestAccountID, r.URL.Query().Get("spaceKey"))
			jsonResponse(w, map[string]any{"results": []any{
				cfPage("pf", "Fallback Page", pageTime, cfTestAccountID, cfTestAccountID, pageTime),
			}})
		},
	})

	c := &ConfluenceConnector{
		baseURL: "https://org.atlassian.net",
		email:   "u@e.com",
		token:   "tok",
		client:  clientFor(srv),
	}
	c.cloudIDOnce.Do(func() { c.cloudID = "x" })
	// Don't pre-seed userOnce so resolveUser actually calls /user/current.

	items, err := c.Fetch(t.Context(), date)
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestConfluence_Fetch_Pagination(t *testing.T) {
	// Two pages of results (pageSize=50). First page is full, second is partial.
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	pageTime := date.Add(10 * time.Hour)

	callCount := 0
	srv := allPathsServer(t, map[string]http.HandlerFunc{
		"/ex/confluence/x/wiki/rest/api/content": func(w http.ResponseWriter, r *http.Request) {
			callCount++
			var results []any
			if r.URL.Query().Get("start") == "0" {
				for i := 0; i < 50; i++ {
					results = append(results, cfPage(
						"p-full-"+time.Now().Format("150405.000000"),
						"Full Page", pageTime,
						cfTestAccountID, cfTestAccountID, pageTime,
					))
				}
			} else {
				// Partial page — triggers end of pagination.
				results = append(results, cfPage("last", "Last Page", pageTime, cfTestAccountID, cfTestAccountID, pageTime))
			}
			jsonResponse(w, map[string]any{"results": results})
		},
	})

	c := newConfluenceConnector(t, srv)
	items, err := c.Fetch(t.Context(), date)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.Len(t, items, 51)
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// seedVelocityData seeds three days of data:
//
//	day1 (oldest) — PR + Jira ticket first appear, undone task created
//	day2 (middle) — PR + Jira carried over (still open/in-progress)
//	day3 (newest) — PR merged, Jira done, task completed
//
// Expected duration for each: 2 calendar days (day3 - day1).
func seedVelocityData(t *testing.T, database *gorm.DB) (day1, day2, day3 db.Day) {
	t.Helper()
	base := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

	day1 = db.Day{Date: base}
	day2 = db.Day{Date: base.AddDate(0, 0, 1)}
	day3 = db.Day{Date: base.AddDate(0, 0, 2)}
	require.NoError(t, database.Create(&day1).Error)
	require.NoError(t, database.Create(&day2).Error)
	require.NoError(t, database.Create(&day3).Error)

	now := time.Now().UTC()

	// GitHub PR: open on day1, open on day2, merged on day3.
	require.NoError(t, database.Create(&db.ActivityItem{DayID: day1.ID, Source: "github", ExternalID: "org/repo#1", Kind: "authored_open", Title: "My PR", FetchedAt: now}).Error)
	require.NoError(t, database.Create(&db.ActivityItem{DayID: day2.ID, Source: "github", ExternalID: "org/repo#1", Kind: "authored_open", Title: "My PR", FetchedAt: now}).Error)
	require.NoError(t, database.Create(&db.ActivityItem{DayID: day3.ID, Source: "github", ExternalID: "org/repo#1", Kind: "authored_merged", Title: "My PR", FetchedAt: now}).Error)

	// Jira: todo on day1, in-progress on day2, done on day3.
	require.NoError(t, database.Create(&db.ActivityItem{DayID: day1.ID, Source: "jira", ExternalID: "PROJ-1", Kind: "jira_todo", Title: "Implement feature", FetchedAt: now}).Error)
	require.NoError(t, database.Create(&db.ActivityItem{DayID: day2.ID, Source: "jira", ExternalID: "PROJ-1", Kind: "jira_in_progress", Title: "Implement feature", FetchedAt: now}).Error)
	require.NoError(t, database.Create(&db.ActivityItem{DayID: day3.ID, Source: "jira", ExternalID: "PROJ-1", Kind: "jira_done", Title: "Implement feature", FetchedAt: now}).Error)

	// Task: created undone on day1, completed on day3.
	require.NoError(t, database.Create(&db.Task{DayID: day1.ID, Title: "write tests", Done: false}).Error)
	require.NoError(t, database.Create(&db.Task{DayID: day2.ID, Title: "write tests", Done: false}).Error)
	require.NoError(t, database.Create(&db.Task{DayID: day3.ID, Title: "write tests", Done: true}).Error)

	return
}

// ── /api/stats/velocity ───────────────────────────────────────────────────────

func TestVelocity_EmptyDB(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/velocity", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp VelocityResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.GitHubAuthored.SampleSize)
	assert.Equal(t, 0, resp.Jira.SampleSize)
	assert.Equal(t, 0, resp.Tasks.SampleSize)
	assert.Empty(t, resp.Slowest)
}

func TestVelocity_ComputesAverageDays(t *testing.T) {
	database, router := newTestRouter(t)
	seedVelocityData(t, database)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/velocity", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp VelocityResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, 1, resp.GitHubAuthored.SampleSize)
	assert.Equal(t, 2.0, resp.GitHubAuthored.AvgDays, "PR open on day1, merged on day3 = 2 days")

	assert.Equal(t, 1, resp.Jira.SampleSize)
	assert.Equal(t, 2.0, resp.Jira.AvgDays, "ticket created day1, done day3 = 2 days")

	assert.Equal(t, 1, resp.Tasks.SampleSize)
	assert.Equal(t, 2.0, resp.Tasks.AvgDays, "task created day1, completed day3 = 2 days")
}

func TestVelocity_SlowestContainsAllSources(t *testing.T) {
	database, router := newTestRouter(t)
	seedVelocityData(t, database)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/velocity", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp VelocityResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	sources := make(map[string]bool)
	for _, s := range resp.Slowest {
		sources[s.Source] = true
	}
	assert.True(t, sources["github"], "slowest must include github item")
	assert.True(t, sources["jira"], "slowest must include jira item")
	assert.True(t, sources["task"], "slowest must include task item")
}

func TestVelocity_UnresolvedItemsExcluded(t *testing.T) {
	database, router := newTestRouter(t)

	// Seed a PR that never reaches a terminal state.
	day := db.Day{Date: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)
	require.NoError(t, database.Create(&db.ActivityItem{
		DayID: day.ID, Source: "github", ExternalID: "org/repo#99",
		Kind: "authored_open", Title: "WIP", FetchedAt: time.Now().UTC(),
	}).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/velocity", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp VelocityResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.GitHubAuthored.SampleSize, "open PR must not be counted")
	assert.Empty(t, resp.Slowest)
}

func TestVelocity_SlowestCappedAt10(t *testing.T) {
	database, router := newTestRouter(t)

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now().UTC()

	// Seed 12 distinct PRs each resolved on day 1 (same-day, 0d each).
	for i := 0; i < 12; i++ {
		day := db.Day{Date: base.AddDate(0, 0, i)}
		require.NoError(t, database.Create(&day).Error)
		extID := "org/repo#" + string(rune('A'+i))
		require.NoError(t, database.Create(&db.ActivityItem{
			DayID: day.ID, Source: "github", ExternalID: extID,
			Kind: "authored_merged", Title: "PR", FetchedAt: now,
		}).Error)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/velocity", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp VelocityResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.LessOrEqual(t, len(resp.Slowest), 10, "slowest must be capped at 10")
}

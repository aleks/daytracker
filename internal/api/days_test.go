package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// ── DayHandler.Get: activity ordering ────────────────────────────────────────

func TestDayGet_ActivitiesOrderedByFetchedAt(t *testing.T) {
	database, router := newTestRouter(t)
	day := db.Day{Date: time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)

	base := time.Now().UTC()
	later := db.ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#2", Kind: "authored_open", Title: "Later PR", FetchedAt: base.Add(1 * time.Minute)}
	earlier := db.ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#1", Kind: "authored_open", Title: "Earlier PR", FetchedAt: base}
	require.NoError(t, database.Create(&later).Error)
	require.NoError(t, database.Create(&earlier).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/2025-03-10", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var detail DayDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Len(t, detail.Activities, 2)
	assert.Equal(t, "Earlier PR", detail.Activities[0].Title, "activities must be ordered by fetched_at asc")
	assert.Equal(t, "Later PR", detail.Activities[1].Title)
}

// ── DayHandler.Get: tasks ordering ───────────────────────────────────────────

func TestDayGet_TasksOrderedByCreatedAt(t *testing.T) {
	database, router := newTestRouter(t)
	day := db.Day{Date: time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)

	// GORM assigns created_at automatically; insert sequentially to get distinct values.
	first := db.Task{DayID: day.ID, Title: "first task"}
	require.NoError(t, database.Create(&first).Error)
	second := db.Task{DayID: day.ID, Title: "second task"}
	require.NoError(t, database.Create(&second).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/2025-03-10", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var detail DayDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Len(t, detail.Tasks, 2)
	assert.Equal(t, "first task", detail.Tasks[0].Title)
	assert.Equal(t, "second task", detail.Tasks[1].Title)
}

// ── DayHandler.List: activity-only days are included ─────────────────────────

func TestDayList_IncludesDayWithActivityOnly(t *testing.T) {
	database, router := newTestRouter(t)
	day := db.Day{Date: time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)
	activity := db.ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#9", Kind: "authored_open", Title: "PR"}
	require.NoError(t, database.Create(&activity).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var days []db.Day
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &days))
	assert.Len(t, days, 1, "a day with only activities must still appear in the list")
}

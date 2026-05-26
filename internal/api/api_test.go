package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&db.Day{}, &db.Task{}, &db.ActivityItem{}, &db.ConnectorState{}))
	return database
}

func newTestRouter(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	database := newTestDB(t)
	r := NewRouter(database, nil, nil)
	return database, r
}

// ── parseDate / parseID ───────────────────────────────────────────────────────

func TestParseDate_Valid(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/2025-01-15", nil)
	router.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusBadRequest, w.Code)
}

func TestParseDate_Invalid(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/not-a-date", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── DayHandler ────────────────────────────────────────────────────────────────

func TestDayList_Empty(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var days []db.Day
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &days))
	assert.Empty(t, days)
}

func TestDayList_ReturnsDays(t *testing.T) {
	database, router := newTestRouter(t)
	day := db.Day{Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var days []db.Day
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &days))
	assert.Len(t, days, 1)
}

func TestDayGet_CreatesIfMissing(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/2025-03-10", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var detail DayDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	assert.Equal(t, time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC), detail.Date.UTC())
	assert.Empty(t, detail.Tasks)
	assert.Empty(t, detail.Activities)
}

func TestDayGet_ReturnsTasksAndActivities(t *testing.T) {
	database, router := newTestRouter(t)
	day := db.Day{Date: time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)
	task := db.Task{DayID: day.ID, Title: "do something"}
	require.NoError(t, database.Create(&task).Error)
	activity := db.ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#1", Kind: "authored_open", Title: "My PR"}
	require.NoError(t, database.Create(&activity).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/2025-03-10", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var detail DayDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	assert.Len(t, detail.Tasks, 1)
	assert.Equal(t, "do something", detail.Tasks[0].Title)
	assert.Len(t, detail.Activities, 1)
	assert.Equal(t, "My PR", detail.Activities[0].Title)
}

func TestDayGet_InvalidDate(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/bad", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── TaskHandler.Create ────────────────────────────────────────────────────────

func TestTaskCreate_OK(t *testing.T) {
	_, router := newTestRouter(t)
	body := `{"title":"write tests"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/days/2025-01-15/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var task db.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &task))
	assert.Equal(t, "write tests", task.Title)
	assert.False(t, task.Done)
}

func TestTaskCreate_MissingTitle(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/days/2025-01-15/tasks", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTaskCreate_BlankTitle(t *testing.T) {
	_, router := newTestRouter(t)
	body := `{"title":"   "}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/days/2025-01-15/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTaskCreate_InvalidDate(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/days/not-a-date/tasks", bytes.NewBufferString(`{"title":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── TaskHandler.Update ────────────────────────────────────────────────────────

func createTask(t *testing.T, database *gorm.DB, title string) db.Task {
	t.Helper()
	day := db.Day{Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)}
	database.Where(db.Day{Date: day.Date}).FirstOrCreate(&day)
	task := db.Task{DayID: day.ID, Title: title}
	require.NoError(t, database.Create(&task).Error)
	return task
}

func TestTaskUpdate_MarkDone(t *testing.T) {
	database, router := newTestRouter(t)
	task := createTask(t, database, "finish report")

	body := `{"done":true}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var updated db.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.True(t, updated.Done)
}

func TestTaskUpdate_RenameTitle(t *testing.T) {
	database, router := newTestRouter(t)
	task := createTask(t, database, "old title")

	body := `{"title":"new title"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var updated db.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "new title", updated.Title)
}

func TestTaskUpdate_BlankTitle(t *testing.T) {
	database, router := newTestRouter(t)
	task := createTask(t, database, "valid")

	body := `{"title":"  "}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTaskUpdate_NotFound(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/9999", bytes.NewBufferString(`{"done":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTaskUpdate_InvalidID(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/abc", bytes.NewBufferString(`{"done":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── TaskHandler.Delete ────────────────────────────────────────────────────────

func TestTaskDelete_OK(t *testing.T) {
	database, router := newTestRouter(t)
	task := createTask(t, database, "to delete")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Confirm gone.
	var count int64
	database.Model(&db.Task{}).Where("id = ?", task.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestTaskDelete_NotFound(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/9999", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTaskDelete_InvalidID(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/0", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── ConnectorHandler ──────────────────────────────────────────────────────────

func TestConnectorList_Empty(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/connectors", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var states []db.ConnectorState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &states))
	assert.Empty(t, states)
}

func TestConnectorList_ReturnsSavedStates(t *testing.T) {
	database, router := newTestRouter(t)
	state := db.ConnectorState{Name: "github"}
	require.NoError(t, database.Create(&state).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/connectors", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var states []db.ConnectorState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &states))
	assert.Len(t, states, 1)
	assert.Equal(t, "github", states[0].Name)
}

func TestConnectorSync_Accepted(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/connectors/github/sync", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestConnectorSync_FullTriggerChannel(t *testing.T) {
	database := newTestDB(t)
	// Create a full channel — Sync must not block.
	trigger := make(chan string, 1)
	trigger <- "existing"
	r := NewRouter(database, nil, trigger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/connectors/jira/sync", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

// ── Health ────────────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ── CORS middleware ───────────────────────────────────────────────────────────

func TestCORS_Preflight(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_Headers(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

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

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// ── TaskHandler.Create: title trimming ───────────────────────────────────────

func TestTaskCreate_TitleIsTrimmed(t *testing.T) {
	_, router := newTestRouter(t)
	body := `{"title":"  padded title  "}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/days/2025-01-15/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var task db.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &task))
	assert.Equal(t, "padded title", task.Title)
}

// ── TaskHandler.Create: day auto-created ────────────────────────────────────

func TestTaskCreate_CreatesDayIfAbsent(t *testing.T) {
	database, router := newTestRouter(t)
	body := `{"title":"auto day task"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/days/2025-06-01/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	expected := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	var day db.Day
	require.NoError(t, database.Where(db.Day{Date: expected}).First(&day).Error)
	assert.Equal(t, expected, day.Date.UTC())
}

// ── TaskHandler.Update: partial update preserves other fields ─────────────────

func TestTaskUpdate_DoneOnlyPreservesTitle(t *testing.T) {
	database, router := newTestRouter(t)
	task := createTask(t, database, "original title")

	body := `{"done":true}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var updated db.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.True(t, updated.Done)
	assert.Equal(t, "original title", updated.Title, "title must not change when only done is updated")
}

func TestTaskUpdate_TitleOnlyPreservesDone(t *testing.T) {
	database, router := newTestRouter(t)
	task := createTask(t, database, "old")
	require.NoError(t, database.Model(&task).Update("done", true).Error)

	body := `{"title":"new"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var updated db.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "new", updated.Title)
	assert.True(t, updated.Done, "done must not be reset when only title is updated")
}

// ── TaskHandler.Update: title trimming ───────────────────────────────────────

func TestTaskUpdate_TitleIsTrimmed(t *testing.T) {
	database, router := newTestRouter(t)
	task := createTask(t, database, "old")

	body := `{"title":"  trimmed  "}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/tasks/%d", task.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var updated db.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "trimmed", updated.Title)
}

// ── TaskHandler.Delete: verify row is gone ────────────────────────────────────

func TestTaskDelete_RowIsRemoved(t *testing.T) {
	database, router := newTestRouter(t)
	task := createTask(t, database, "remove me")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	var count int64
	database.Model(&db.Task{}).Where("id = ?", task.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

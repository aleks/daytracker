package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aleks/daytracker/internal/backup"
	"github.com/aleks/daytracker/internal/connector"
	"github.com/aleks/daytracker/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkLogCreateAndDayOverview(t *testing.T) {
	database := newTestDB(t)
	var backedUp time.Time
	router := NewRouter(database, nil, nil, "http://localhost", func(date time.Time) { backedUp = date })
	post := func(date, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/days/"+date+"/work-logs", bytes.NewBufferString(body)))
		return w
	}
	for _, body := range []string{`{}`, `{"content":"  \n "}`, `invalid`} {
		assert.Equal(t, http.StatusBadRequest, post("2025-01-15", body).Code)
	}
	assert.Equal(t, http.StatusBadRequest, post("invalid", `{"content":"note"}`).Code)

	w := post("2025-01-15", `{"content":"  First line\nSecond line  "}`)
	require.Equal(t, http.StatusCreated, w.Code)
	var entry db.WorkLogEntry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &entry))
	assert.Equal(t, "First line\nSecond line", entry.Content)
	assert.Equal(t, "2025-01-15", backedUp.Format("2006-01-02"))
	require.Equal(t, http.StatusCreated, post("2025-01-15", `{"content":"Another note"}`).Code)
	require.Equal(t, http.StatusCreated, post("2025-01-16", `{"content":"Other day"}`).Code)

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/days/2025-01-15", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var detail DayDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Len(t, detail.WorkLogs, 2)
	assert.Equal(t, []string{"Another note", "First line\nSecond line"}, []string{detail.WorkLogs[0].Content, detail.WorkLogs[1].Content})

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/days", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var days []db.Day
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &days))
	assert.Len(t, days, 2)
}

func TestWorkLogCreateUpdatesHistoricalBackup(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	writer := backup.New(root, database, connector.NewRegistry())
	router := NewRouter(database, nil, nil, "http://localhost", func(date time.Time) {
		assert.NoError(t, writer.WriteDay(context.Background(), date))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/days/2020-01-15/work-logs", bytes.NewBufferString(`{"content":"Historical note"}`)))
	require.Equal(t, http.StatusCreated, w.Code)
	content, err := os.ReadFile(filepath.Join(root, "2020", "01", "15.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "## Work journal\n\nHistorical note")

	var entry db.WorkLogEntry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &entry))
	path := fmt.Sprintf("/api/work-logs/%d", entry.ID)
	for _, body := range []string{`{}`, `{"content":"  "}`, `invalid`} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, path, bytes.NewBufferString(body)))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, path, bytes.NewBufferString(`{"content":" Updated\nentry "}`)))
	require.Equal(t, http.StatusOK, w.Code)
	var updated db.WorkLogEntry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "Updated\nentry", updated.Content)
	assert.Equal(t, entry.CreatedAt, updated.CreatedAt)
	content, err = os.ReadFile(filepath.Join(root, "2020", "01", "15.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Updated\nentry")
	assert.NotContains(t, string(content), "Historical note")

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, path, nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	content, err = os.ReadFile(filepath.Join(root, "2020", "01", "15.md"))
	require.NoError(t, err)
	assert.Equal(t, "# 2020-01-15\n", string(content))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/days", nil))
	var days []db.Day
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &days))
	assert.Empty(t, days)

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(`{"content":"note"}`)))
		assert.Equal(t, http.StatusNotFound, w.Code)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, "/api/work-logs/invalid", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
}

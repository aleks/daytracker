package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/aleks/daytracker/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchWorkJournalIndexAndFilters(t *testing.T) {
	database := newTestDB(t)
	day := db.Day{Date: time.Date(2024, 2, 8, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)
	entry := db.WorkLogEntry{DayID: day.ID, Content: "Earlier discussion about migration plans"}
	require.NoError(t, database.Create(&entry).Error)
	require.NoError(t, database.Create(&db.Task{DayID: day.ID, Title: "migration task"}).Error)
	for i := 0; i < 20; i++ {
		require.NoError(t, database.Create(&db.ActivityItem{DayID: day.ID, Source: "github", ExternalID: fmt.Sprintf("pr-%d", i), Kind: "authored_open", Title: "migration activity"}).Error)
	}
	// Existing rows must become searchable on first setup and on later startups.
	require.NoError(t, db.SetupFTS(database))
	require.NoError(t, db.SetupFTS(database))
	router := NewRouter(database, nil, nil, "http://localhost")
	search := func(query, source string) []SearchResult {
		t.Helper()
		values := url.Values{"q": {query}}
		if source != "" {
			values.Set("source", source)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/search?"+values.Encode(), nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var results []SearchResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &results))
		return results
	}

	results := search("migra", "work_journal")
	require.Len(t, results, 1)
	assert.Equal(t, "2024-02-08", results[0].Date)
	assert.Equal(t, "work_journal", results[0].Type)
	assert.Contains(t, results[0].Title, "migration")
	assert.Empty(t, search("discussion", "tasks"))
	assert.Empty(t, search("discussion", "github"))
	assert.Empty(t, search(`" OR migration`, "work_journal"))
	results = search("migration", "")
	require.Len(t, results, 15)
	assert.Equal(t, "activity", results[0].Type)
	assert.Equal(t, "task", results[1].Type)
	assert.Equal(t, "work_journal", results[2].Type)

	path := fmt.Sprintf("/api/work-logs/%d", entry.ID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, path, bytes.NewBufferString(`{"content":"Updated retrospective notes"}`)))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, search("migration", "work_journal"))
	require.Len(t, search("retrospec", "work_journal"), 1)

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, path, nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, search("retrospec", "work_journal"))

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/days/2024-02-09/work-logs", bytes.NewBufferString(`{"content":"A newly written journal entry"}`)))
	require.Equal(t, http.StatusCreated, w.Code)
	results = search("newly", "work_journal")
	require.Len(t, results, 1)
	assert.Equal(t, "2024-02-09", results[0].Date)
}

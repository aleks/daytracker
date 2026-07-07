package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aleks/daytracker/internal/db"
)

func TestActivityDelete_OK(t *testing.T) {
	database, router := newTestRouter(t)
	day := db.Day{Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)
	item := db.ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#1", Kind: "authored_open", Title: "My PR"}
	require.NoError(t, database.Create(&item).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/activities/%d", item.ID), nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	var count int64
	database.Model(&db.ActivityItem{}).Where("id = ?", item.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestActivityDelete_NotFound(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/activities/9999", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestActivityDelete_InvalidID(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/activities/0", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

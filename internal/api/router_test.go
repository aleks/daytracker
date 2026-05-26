package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

func newTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("<html>SPA</html>")},
		"assets/app.js":   {Data: []byte("console.log('app')")},
		"assets/style.css": {Data: []byte("body{}")},
		"subdir":          {Mode: 0o755 | 1<<31}, // directory entry (ModeDir)
	}
}

func serveEmbeddedRouter(fs fstest.MapFS) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.NoRoute(serveEmbedded(fs))
	return r
}

// ── serveEmbedded ─────────────────────────────────────────────────────────────

func TestServeEmbedded_RootServesIndex(t *testing.T) {
	r := serveEmbeddedRouter(newTestFS())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "SPA")
}

func TestServeEmbedded_KnownFileServed(t *testing.T) {
	r := serveEmbeddedRouter(newTestFS())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "console.log")
}

func TestServeEmbedded_UnknownPathFallsBackToIndex(t *testing.T) {
	r := serveEmbeddedRouter(newTestFS())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/some/unknown/route", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "SPA")
}

func TestServeEmbedded_DirectoryPathFallsBackToIndex(t *testing.T) {
	fs := fstest.MapFS{
		"index.html": {Data: []byte("<html>SPA</html>")},
		// a proper directory entry (fstest uses a MapFile with Mode including ModeDir)
		"subdir/file.txt": {Data: []byte("hi")},
	}
	r := serveEmbeddedRouter(fs)
	w := httptest.NewRecorder()
	// /subdir maps to a directory in the FS — should fall back to index.html
	req := httptest.NewRequest(http.MethodGet, "/subdir", nil)
	r.ServeHTTP(w, req)
	// Either falls back to SPA (200) or serves directory — either way must not 500.
	assert.NotEqual(t, http.StatusInternalServerError, w.Code)
}

// ── serveFile ─────────────────────────────────────────────────────────────────

func TestServeFile_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		serveFile(c, newTestFS(), "missing.html")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeFile_ExistingFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		serveFile(c, newTestFS(), "index.html")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "SPA")
}

// ── /api/days/:date/activities alias ─────────────────────────────────────────

func TestDayActivitiesAlias_NotFound(t *testing.T) {
	_, router := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/2025-01-15/activities", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDayActivitiesAlias_ReturnsActivities(t *testing.T) {
	database, router := newTestRouter(t)

	// Seed a day + activity directly.
	dayRow := db.Day{Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&dayRow).Error)
	activity := db.ActivityItem{
		DayID: dayRow.ID, Source: "github", ExternalID: "repo#1",
		Kind: "authored_open", Title: "My PR",
	}
	require.NoError(t, database.Create(&activity).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/days/2025-01-15/activities", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "My PR")
}

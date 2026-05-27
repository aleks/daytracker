package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// ── ConnectorHandler.Sync: trigger channel behaviour ─────────────────────────

func TestConnectorSync_SendsConnectorName(t *testing.T) {
	database := newTestDB(t)
	trigger := make(chan string, 1)
	r := NewRouter(database, nil, trigger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/connectors/jira/sync", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	select {
	case name := <-trigger:
		assert.Equal(t, "jira", name, "trigger channel must carry the connector name")
	default:
		t.Fatal("trigger channel must have received a value")
	}
}

func TestConnectorSync_NilTrigger_DoesNotPanic(t *testing.T) {
	database := newTestDB(t)
	r := NewRouter(database, nil, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/connectors/github/sync", nil)
	assert.NotPanics(t, func() { r.ServeHTTP(w, req) })
	assert.Equal(t, http.StatusAccepted, w.Code)
}

// ── ConnectorHandler.List: response shape ─────────────────────────────────────

func TestConnectorList_ResponseIncludesAllFields(t *testing.T) {
	database, router := newTestRouter(t)
	state := db.ConnectorState{Name: "github", LastError: "transient"}
	require.NoError(t, database.Create(&state).Error)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/connectors", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var states []db.ConnectorState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &states))
	require.Len(t, states, 1)
	assert.Equal(t, "github", states[0].Name)
	assert.Equal(t, "transient", states[0].LastError)
}

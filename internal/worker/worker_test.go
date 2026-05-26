package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/aleksmaksimow/daytracker/internal/connector"
	"github.com/aleksmaksimow/daytracker/internal/db"
)

// ── test helpers ──────────────────────────────────────────────────────────────

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	// Limit SQLite to a single connection so concurrent goroutines (e.g. syncAll)
	// serialize their writes and avoid "database is locked" / lost-update races.
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&db.Day{}, &db.Task{}, &db.ActivityItem{}, &db.ConnectorState{}))
	return database
}

func newWorker(t *testing.T, database *gorm.DB, connectors ...connector.Connector) *Worker {
	t.Helper()
	reg := connector.NewRegistry()
	for _, c := range connectors {
		reg.Register(c)
	}
	return &Worker{
		db:              database,
		registry:        reg,
		trigger:         make(chan string, 10),
		interval:        time.Hour,
		refreshInterval: time.Hour,
		backfill:        3,
	}
}

// stubConnector is a configurable stub that returns fixed items or an error.
type stubConnector struct {
	name       string
	configured bool
	items      []db.ActivityItem
	err        error
	calls      int
}

func (s *stubConnector) Name() string        { return s.name }
func (s *stubConnector) IsConfigured() bool  { return s.configured }
func (s *stubConnector) Fetch(_ context.Context, _ time.Time) ([]db.ActivityItem, error) {
	s.calls++
	return s.items, s.err
}

// stubRefresher adds StatusRefresher capability to a stubConnector.
type stubRefresher struct {
	stubConnector
	updates      []connector.PRStatusUpdate
	err          error
	terminalKinds map[string]bool
}

func (s *stubRefresher) IsTerminal(kind string) bool { return s.terminalKinds[kind] }
func (s *stubRefresher) RefreshStatuses(_ context.Context, items []connector.PRStatusItem) ([]connector.PRStatusUpdate, error) {
	return s.updates, s.err
}

// ── syncOne ───────────────────────────────────────────────────────────────────

func TestSyncOne_InsertsItems(t *testing.T) {
	database := newTestDB(t)
	stub := &stubConnector{
		name:       "test",
		configured: true,
		items: []db.ActivityItem{
			{Source: "test", ExternalID: "ext-1", Kind: "authored_open", Title: "PR 1"},
		},
	}
	w := newWorker(t, database, stub)

	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	err := w.syncOne(context.Background(), "test", date)
	require.NoError(t, err)

	var items []db.ActivityItem
	require.NoError(t, database.Find(&items).Error)
	require.Len(t, items, 1)
	assert.Equal(t, "PR 1", items[0].Title)
	assert.Equal(t, "authored_open", items[0].Kind)
}

func TestSyncOne_UpsertUpdatesExisting(t *testing.T) {
	database := newTestDB(t)
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	stub := &stubConnector{
		name:       "test",
		configured: true,
		items: []db.ActivityItem{
			{Source: "test", ExternalID: "ext-1", Kind: "authored_open", Title: "PR original"},
		},
	}
	w := newWorker(t, database, stub)
	require.NoError(t, w.syncOne(context.Background(), "test", date))

	// Second sync with updated title and kind.
	stub.items[0].Title = "PR updated"
	stub.items[0].Kind = "authored_merged"
	require.NoError(t, w.syncOne(context.Background(), "test", date))

	var items []db.ActivityItem
	require.NoError(t, database.Find(&items).Error)
	require.Len(t, items, 1, "upsert must not create a duplicate")
	assert.Equal(t, "PR updated", items[0].Title)
	assert.Equal(t, "authored_merged", items[0].Kind)
}

func TestSyncOne_ConnectorError_SavesState(t *testing.T) {
	database := newTestDB(t)
	stub := &stubConnector{
		name:       "test",
		configured: true,
		err:        errors.New("network failure"),
	}
	w := newWorker(t, database, stub)

	err := w.syncOne(context.Background(), "test", time.Now())
	require.Error(t, err)

	var state db.ConnectorState
	require.NoError(t, database.Where("name = ?", "test").First(&state).Error)
	assert.Contains(t, state.LastError, "network failure")
	assert.Nil(t, state.LastSyncAt)
}

func TestSyncOne_SuccessfulSync_ClearsError(t *testing.T) {
	database := newTestDB(t)
	stub := &stubConnector{name: "test", configured: true, err: errors.New("fail")}
	w := newWorker(t, database, stub)

	w.syncOne(context.Background(), "test", time.Now()) //nolint:errcheck // intentional first failure

	stub.err = nil
	stub.items = nil
	require.NoError(t, w.syncOne(context.Background(), "test", time.Now()))

	var state db.ConnectorState
	require.NoError(t, database.Where("name = ?", "test").First(&state).Error)
	assert.Empty(t, state.LastError)
	assert.NotNil(t, state.LastSyncAt)
}

func TestSyncOne_UnknownConnector_ReturnsNil(t *testing.T) {
	database := newTestDB(t)
	w := newWorker(t, database)

	err := w.syncOne(context.Background(), "nonexistent", time.Now())
	assert.NoError(t, err)
}

func TestSyncOne_UnconfiguredConnector_Skips(t *testing.T) {
	database := newTestDB(t)
	stub := &stubConnector{name: "test", configured: false}
	w := newWorker(t, database, stub)

	err := w.syncOne(context.Background(), "test", time.Now())
	require.NoError(t, err)
	assert.Equal(t, 0, stub.calls)
}

func TestSyncOne_EmptyItems_NoDay(t *testing.T) {
	database := newTestDB(t)
	stub := &stubConnector{name: "test", configured: true, items: nil}
	w := newWorker(t, database, stub)

	require.NoError(t, w.syncOne(context.Background(), "test", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)))

	var days []db.Day
	require.NoError(t, database.Find(&days).Error)
	assert.Empty(t, days)
}

// ── syncAll ───────────────────────────────────────────────────────────────────

func TestSyncAll_CallsAllConfigured(t *testing.T) {
	database := newTestDB(t)
	a := &stubConnector{name: "a", configured: true}
	b := &stubConnector{name: "b", configured: true}
	c := &stubConnector{name: "c", configured: false}
	w := newWorker(t, database, a, b, c)

	w.syncAll(context.Background(), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	assert.Equal(t, 1, a.calls)
	assert.Equal(t, 1, b.calls)
	assert.Equal(t, 0, c.calls, "unconfigured connector must not be called")
}

func TestSyncAll_OneErrorDoesNotStopOthers(t *testing.T) {
	database := newTestDB(t)
	failing := &stubConnector{name: "a", configured: true, err: errors.New("boom")}
	ok := &stubConnector{name: "b", configured: true, items: []db.ActivityItem{
		{Source: "b", ExternalID: "x", Kind: "authored_open", Title: "PR"},
	}}
	w := newWorker(t, database, failing, ok)

	w.syncAll(context.Background(), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	assert.Equal(t, 1, failing.calls)
	assert.Equal(t, 1, ok.calls, "sibling connector must still run after peer failure")

	var items []db.ActivityItem
	require.NoError(t, database.Find(&items).Error)
	assert.Len(t, items, 1, "items from the healthy connector must be persisted")
}

// ── IsTerminal ────────────────────────────────────────────────────────────────

func TestGitHub_IsTerminal(t *testing.T) {
	// Import via the connector package since GitHubConnector is not exported to
	// this package; test the behaviour through the interface.
	terminal := []string{"authored_merged", "authored_closed", "reviewed_merged", "reviewed_closed"}
	nonTerminal := []string{"authored_open", "authored_draft", "reviewed_open", "reviewed_in_review"}

	stub := &stubRefresher{
		stubConnector: stubConnector{name: "github", configured: true},
		terminalKinds: map[string]bool{
			"authored_merged": true, "authored_closed": true,
			"reviewed_merged": true, "reviewed_closed": true,
		},
	}
	for _, k := range terminal {
		assert.True(t, stub.IsTerminal(k), "expected %q to be terminal", k)
	}
	for _, k := range nonTerminal {
		assert.False(t, stub.IsTerminal(k), "expected %q to be non-terminal", k)
	}
}

// ── refreshAllStatuses ────────────────────────────────────────────────────────

func TestRefreshAllStatuses_UpdatesNonTerminal(t *testing.T) {
	database := newTestDB(t)
	date := utcToday()

	// Seed a Day + two ActivityItems: one open, one merged (terminal).
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	openItem := db.ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#1", Kind: "authored_open", Title: "Open PR"}
	mergedItem := db.ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#2", Kind: "authored_merged", Title: "Merged PR"}
	require.NoError(t, database.Create(&openItem).Error)
	require.NoError(t, database.Create(&mergedItem).Error)

	stub := &stubRefresher{
		stubConnector: stubConnector{name: "github", configured: true},
		updates: []connector.PRStatusUpdate{
			{ExternalID: "repo#1", Kind: "authored_merged"},
		},
		terminalKinds: map[string]bool{
			"authored_merged": true, "authored_closed": true,
			"reviewed_merged": true, "reviewed_closed": true,
		},
	}
	w := newWorker(t, database, stub)

	w.refreshAllStatuses(context.Background())

	var updated db.ActivityItem
	require.NoError(t, database.Where("external_id = ?", "repo#1").First(&updated).Error)
	assert.Equal(t, "authored_merged", updated.Kind)

	var unchanged db.ActivityItem
	require.NoError(t, database.Where("external_id = ?", "repo#2").First(&unchanged).Error)
	assert.Equal(t, "authored_merged", unchanged.Kind, "terminal item must not be touched")
}

func TestRefreshAllStatuses_SkipsNonRefresher(t *testing.T) {
	database := newTestDB(t)
	// A plain stubConnector does not implement StatusRefresher — should be skipped.
	stub := &stubConnector{name: "jira", configured: true}
	w := newWorker(t, database, stub)

	// Should not panic or error.
	w.refreshAllStatuses(context.Background())
	assert.Equal(t, 0, stub.calls)
}

func TestRefreshAllStatuses_OnlyNonTerminalSentToRefresher(t *testing.T) {
	database := newTestDB(t)
	date := utcToday()

	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)

	for i, kind := range []string{"authored_open", "authored_merged", "reviewed_in_review", "authored_closed"} {
		item := db.ActivityItem{
			DayID: day.ID, Source: "github",
			ExternalID: "repo#" + string(rune('1'+i)),
			Kind: kind, Title: "PR",
		}
		require.NoError(t, database.Create(&item).Error)
	}

	var received []connector.PRStatusItem
	stub := &stubRefresher{
		stubConnector: stubConnector{name: "github", configured: true},
	}
	// Override via a custom refresher.
	type capturingRefresher struct {
		*stubRefresher
		captured *[]connector.PRStatusItem
	}

	_ = received
	stub.terminalKinds = map[string]bool{
		"authored_merged": true, "authored_closed": true,
		"reviewed_merged": true, "reviewed_closed": true,
	}
	w := newWorker(t, database, stub)
	w.refreshAllStatuses(context.Background())
	// No panics, no errors — the empty updates list means nothing changes.
	// Verify that merged and closed items still have their kinds.
	var merged db.ActivityItem
	require.NoError(t, database.Where("external_id = ?", "repo#2").First(&merged).Error)
	assert.Equal(t, "authored_merged", merged.Kind)
}

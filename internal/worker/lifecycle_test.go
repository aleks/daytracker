package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aleksmaksimow/daytracker/internal/connector"
	"github.com/aleksmaksimow/daytracker/internal/db"
)

// ── worker.New env var parsing ────────────────────────────────────────────────

func TestNew_DefaultValues(t *testing.T) {
	t.Setenv("DAYTRACKER_SYNC_INTERVAL", "")
	t.Setenv("DAYTRACKER_STATUS_REFRESH_INTERVAL", "")
	t.Setenv("DAYTRACKER_BACKFILL_DAYS", "")

	database := newTestDB(t)
	w := New(database, connector.NewRegistry())

	assert.Equal(t, 15*time.Minute, w.interval)
	assert.Equal(t, 5*time.Minute, w.refreshInterval)
	assert.Equal(t, 14, w.backfill)
}

func TestNew_EnvOverrides(t *testing.T) {
	t.Setenv("DAYTRACKER_SYNC_INTERVAL", "30m")
	t.Setenv("DAYTRACKER_STATUS_REFRESH_INTERVAL", "2m")
	t.Setenv("DAYTRACKER_BACKFILL_DAYS", "7")

	database := newTestDB(t)
	w := New(database, connector.NewRegistry())

	assert.Equal(t, 30*time.Minute, w.interval)
	assert.Equal(t, 2*time.Minute, w.refreshInterval)
	assert.Equal(t, 7, w.backfill)
}

func TestNew_InvalidEnvIgnored(t *testing.T) {
	t.Setenv("DAYTRACKER_SYNC_INTERVAL", "not-a-duration")
	t.Setenv("DAYTRACKER_STATUS_REFRESH_INTERVAL", "bad")
	t.Setenv("DAYTRACKER_BACKFILL_DAYS", "nope")

	database := newTestDB(t)
	w := New(database, connector.NewRegistry())

	// Falls back to defaults.
	assert.Equal(t, 15*time.Minute, w.interval)
	assert.Equal(t, 5*time.Minute, w.refreshInterval)
	assert.Equal(t, 14, w.backfill)
}

func TestNew_TriggerChan(t *testing.T) {
	database := newTestDB(t)
	w := New(database, connector.NewRegistry())
	ch := w.TriggerChan()
	require.NotNil(t, ch)
	// Must be buffered — sending must not block.
	select {
	case ch <- "test":
	default:
		t.Fatal("trigger channel must be buffered")
	}
}

// ── runBackfill ───────────────────────────────────────────────────────────────

func TestRunBackfill_SyncsCorrectNumberOfDays(t *testing.T) {
	database := newTestDB(t)
	var synced []time.Time
	stub := &callTrackingConnector{
		name:  "test",
		onFetch: func(date time.Time) {
			synced = append(synced, date)
		},
	}
	reg := connector.NewRegistry()
	reg.Register(stub)
	w := &Worker{
		db:       database,
		registry: reg,
		trigger:  make(chan string, 10),
		backfill: 5,
	}

	w.runBackfill(context.Background())

	require.Len(t, synced, 5)
	today := utcToday()
	for i, date := range synced {
		assert.Equal(t, today.AddDate(0, 0, -i), date, "day %d mismatch", i)
	}
}

func TestRunBackfill_StopsOnContextCancel(t *testing.T) {
	database := newTestDB(t)
	calls := 0
	stub := &callTrackingConnector{
		name:  "test",
		onFetch: func(_ time.Time) { calls++ },
	}
	reg := connector.NewRegistry()
	reg.Register(stub)
	w := &Worker{
		db:       database,
		registry: reg,
		trigger:  make(chan string, 10),
		backfill: 10,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	w.runBackfill(ctx)

	assert.Equal(t, 0, calls, "backfill must not call Fetch when context is already cancelled")
}

// ── Run lifecycle ─────────────────────────────────────────────────────────────

func TestRun_ExitsOnContextCancel(t *testing.T) {
	database := newTestDB(t)
	w := &Worker{
		db:              database,
		registry:        connector.NewRegistry(),
		trigger:         make(chan string, 10),
		interval:        time.Hour,
		refreshInterval: time.Hour,
		backfill:        0, // skip backfill
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// ok
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestRun_TriggerCausesSync(t *testing.T) {
	database := newTestDB(t)
	synced := make(chan struct{}, 5)
	stub := &callTrackingConnector{
		name:  "test",
		onFetch: func(_ time.Time) {
			synced <- struct{}{}
		},
	}
	reg := connector.NewRegistry()
	reg.Register(stub)
	w := &Worker{
		db:              database,
		registry:        reg,
		trigger:         make(chan string, 10),
		interval:        time.Hour,
		refreshInterval: time.Hour,
		backfill:        0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)

	// Send a trigger for all connectors.
	w.trigger <- ""

	select {
	case <-synced:
		// Sync was triggered.
	case <-time.After(3 * time.Second):
		t.Fatal("sync not triggered within timeout")
	}
}

// ── callTrackingConnector ─────────────────────────────────────────────────────

type callTrackingConnector struct {
	name    string
	onFetch func(time.Time)
}

func (c *callTrackingConnector) Name() string                 { return c.name }
func (c *callTrackingConnector) IsConfigured() bool           { return true }
func (c *callTrackingConnector) KindLabel(kind string) string { return kind }
func (c *callTrackingConnector) Fetch(_ context.Context, date time.Time) ([]db.ActivityItem, error) {
	if c.onFetch != nil {
		c.onFetch(date)
	}
	return nil, nil
}

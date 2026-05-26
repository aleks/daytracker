package worker

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/aleksmaksimow/daytracker/internal/connector"
	"github.com/aleksmaksimow/daytracker/internal/db"
)

// terminalKinds are PR states that will never change; skip them in refresh.
var terminalKinds = map[string]bool{
	"pr_merged": true,
	"pr_closed": true,
}

type Worker struct {
	db              *gorm.DB
	registry        *connector.Registry
	trigger         chan string
	interval        time.Duration
	refreshInterval time.Duration
	backfill        int
}

func New(database *gorm.DB, registry *connector.Registry) *Worker {
	interval := 15 * time.Minute
	if v := os.Getenv("SYNC_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	refreshInterval := 5 * time.Minute
	if v := os.Getenv("STATUS_REFRESH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			refreshInterval = d
		}
	}

	backfill := 7
	if v := os.Getenv("BACKFILL_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			backfill = n
		}
	}

	return &Worker{
		db:              database,
		registry:        registry,
		trigger:         make(chan string, 10),
		interval:        interval,
		refreshInterval: refreshInterval,
		backfill:        backfill,
	}
}

// TriggerChan returns a send-only channel that causes an immediate sync.
func (w *Worker) TriggerChan() chan<- string {
	return w.trigger
}

func (w *Worker) Run(ctx context.Context) {
	w.runBackfill(ctx)

	syncTicker := time.NewTicker(w.interval)
	refreshTicker := time.NewTicker(w.refreshInterval)
	defer syncTicker.Stop()
	defer refreshTicker.Stop()

	// Run an initial status refresh shortly after startup.
	go w.refreshAllStatuses(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case name := <-w.trigger:
			if name == "" {
				w.syncAll(ctx, utcToday())
			} else {
				w.syncOne(ctx, name, utcToday())
			}
		case <-syncTicker.C:
			w.syncAll(ctx, utcToday())
		case <-refreshTicker.C:
			w.refreshAllStatuses(ctx)
		}
	}
}

func (w *Worker) runBackfill(ctx context.Context) {
	today := utcToday()
	for i := 0; i < w.backfill; i++ {
		date := today.AddDate(0, 0, -i)
		w.syncAll(ctx, date)
	}
}

func (w *Worker) syncAll(ctx context.Context, date time.Time) {
	eg, egCtx := errgroup.WithContext(ctx)
	for _, c := range w.registry.All() {
		c := c
		if !c.IsConfigured() {
			continue
		}
		eg.Go(func() error {
			return w.syncOne(egCtx, c.Name(), date)
		})
	}
	if err := eg.Wait(); err != nil {
		log.Printf("worker: sync errors: %v", err)
	}
}

func (w *Worker) syncOne(ctx context.Context, name string, date time.Time) error {
	c, ok := w.registry.Get(name)
	if !ok {
		return nil
	}
	if !c.IsConfigured() {
		return nil
	}

	items, err := c.Fetch(ctx, date)

	state := db.ConnectorState{Name: name}
	w.db.Where(db.ConnectorState{Name: name}).FirstOrCreate(&state)

	if err != nil {
		state.LastError = err.Error()
		w.db.Save(&state)
		log.Printf("worker: connector %s error: %v", name, err)
		return err
	}

	now := time.Now().UTC()
	state.LastError = ""
	state.LastSyncAt = &now
	w.db.Save(&state)

	if len(items) == 0 {
		return nil
	}

	day := db.Day{Date: date}
	if err := w.db.Where(db.Day{Date: date}).FirstOrCreate(&day).Error; err != nil {
		return err
	}

	for i := range items {
		items[i].DayID = day.ID
		items[i].FetchedAt = now
	}

	return w.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source"}, {Name: "external_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "url", "kind", "metadata", "fetched_at"}),
	}).Create(&items).Error
}

// refreshAllStatuses iterates over every registered connector that implements
// StatusRefresher and updates the kind of non-terminal activity items.
func (w *Worker) refreshAllStatuses(ctx context.Context) {
	for _, c := range w.registry.All() {
		refresher, ok := c.(connector.StatusRefresher)
		if !ok || !c.IsConfigured() {
			continue
		}

		// Collect non-terminal external IDs for this source.
		var items []db.ActivityItem
		if err := w.db.
			Where("source = ?", c.Name()).
			Find(&items).Error; err != nil {
			log.Printf("worker: refresh query %s: %v", c.Name(), err)
			continue
		}

		var ids []string
		for _, item := range items {
			if !terminalKinds[item.Kind] {
				ids = append(ids, item.ExternalID)
			}
		}
		if len(ids) == 0 {
			continue
		}

		updates, err := refresher.RefreshStatuses(ctx, ids)
		if err != nil {
			log.Printf("worker: refresh %s: %v", c.Name(), err)
			continue
		}

		for _, u := range updates {
			w.db.Model(&db.ActivityItem{}).
				Where("source = ? AND external_id = ?", c.Name(), u.ExternalID).
				Update("kind", u.Kind)
		}

		log.Printf("worker: refreshed %d %s PR statuses", len(updates), c.Name())
	}
}

func utcToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

package worker

import (
	"context"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/aleksmaksimow/daytracker/internal/backup"
	"github.com/aleksmaksimow/daytracker/internal/connector"
	"github.com/aleksmaksimow/daytracker/internal/db"
)

// terminalKinds are PR states that will never change; skip them in refresh.
var terminalKinds = map[string]bool{
	"authored_merged": true,
	"authored_closed": true,
	"reviewed_merged": true,
	"reviewed_closed": true,
}

type Worker struct {
	db              *gorm.DB
	registry        *connector.Registry
	backup          *backup.Writer
	trigger         chan string
	interval        time.Duration
	refreshInterval time.Duration
	backfill        int
}

func New(database *gorm.DB, registry *connector.Registry) *Worker {
	interval := 15 * time.Minute
	if v := os.Getenv("DAYTRACKER_SYNC_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	refreshInterval := 5 * time.Minute
	if v := os.Getenv("DAYTRACKER_STATUS_REFRESH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			refreshInterval = d
		}
	}

	backfill := 14
	if v := os.Getenv("DAYTRACKER_BACKFILL_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			backfill = n
		}
	}

	var bw *backup.Writer
	if root := os.Getenv("DAYTRACKER_BACKUP_DIR"); root != "" {
		bw = backup.New(root, database)
		log.Printf("worker: backup dir=%s", root)
	}

	w := &Worker{
		db:              database,
		registry:        registry,
		backup:          bw,
		trigger:         make(chan string, 10),
		interval:        interval,
		refreshInterval: refreshInterval,
		backfill:        backfill,
	}
	log.Printf("worker: sync=%s refresh=%s backfill=%d days", interval, refreshInterval, backfill)
	return w
}

// TriggerChan returns a send-only channel that causes an immediate sync.
func (w *Worker) TriggerChan() chan<- string {
	return w.trigger
}

func (w *Worker) Run(ctx context.Context) {
	w.runBackfill(ctx)
	if ctx.Err() != nil {
		return
	}

	syncTicker := time.NewTicker(w.interval)
	refreshTicker := time.NewTicker(w.refreshInterval)
	// Re-write today's backup file on a short cycle so task edits (done/undone)
	// are reflected without waiting for the next full connector sync.
	backupTicker := time.NewTicker(2 * time.Minute)
	defer syncTicker.Stop()
	defer refreshTicker.Stop()
	defer backupTicker.Stop()

	// Run an initial status refresh shortly after startup.
	var refreshWg sync.WaitGroup
	refreshWg.Add(1)
	go func() {
		defer refreshWg.Done()
		w.refreshAllStatuses(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			refreshWg.Wait()
			return
		case name := <-w.trigger:
			today := utcToday()
			if name == "" {
				w.syncAll(ctx, today)
			} else {
				w.syncOne(ctx, name, today)
			}
			w.writeBackup(ctx, today)
		case <-syncTicker.C:
			today := utcToday()
			w.syncAll(ctx, today)
			w.writeBackup(ctx, today)
		case <-refreshTicker.C:
			w.refreshAllStatuses(ctx)
		case <-backupTicker.C:
			w.writeBackup(ctx, utcToday())
		}
	}
}

func (w *Worker) runBackfill(ctx context.Context) {
	log.Printf("worker: starting backfill for %d days", w.backfill)
	today := utcToday()
	for i := 0; i < w.backfill; i++ {
		if ctx.Err() != nil {
			log.Printf("worker: backfill interrupted at day %d", i)
			return
		}
		date := today.AddDate(0, 0, -i)
		w.syncAll(ctx, date)
		w.writeBackup(ctx, date)
	}
	log.Printf("worker: backfill complete")
}

func (w *Worker) writeBackup(ctx context.Context, date time.Time) {
	if w.backup == nil {
		return
	}
	if err := w.backup.WriteDay(ctx, date); err != nil {
		log.Printf("worker: backup %s: %v", date.Format("2006-01-02"), err)
	}
}

func (w *Worker) syncAll(ctx context.Context, date time.Time) {
	var eg errgroup.Group
	for _, c := range w.registry.All() {
		c := c
		if !c.IsConfigured() {
			continue
		}
		eg.Go(func() error {
			return w.syncOne(ctx, c.Name(), date)
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

	log.Printf("worker: sync %s %s", name, date.Format("2006-01-02"))
	items, err := c.Fetch(ctx, date)

	state := db.ConnectorState{Name: name}
	w.db.Where(db.ConnectorState{Name: name}).FirstOrCreate(&state)

	if err != nil {
		state.LastError = err.Error()
		w.db.Save(&state)
		log.Printf("worker: %s %s error: %v", name, date.Format("2006-01-02"), err)
		return err
	}

	now := time.Now().UTC()
	state.LastError = ""
	state.LastSyncAt = &now
	w.db.Save(&state)

	log.Printf("worker: %s %s fetched %d items", name, date.Format("2006-01-02"), len(items))

	if len(items) == 0 {
		return nil
	}

	day := db.Day{Date: date}
	if err := w.db.Where(db.Day{Date: date}).FirstOrCreate(&day).Error; err != nil {
		return err
	}

	// Copy so we don't mutate the slice returned by Fetch.
	rows := make([]db.ActivityItem, len(items))
	copy(rows, items)
	for i := range rows {
		rows[i].DayID = day.ID
		rows[i].FetchedAt = now
	}

	return w.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source"}, {Name: "external_id"}, {Name: "day_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "url", "kind", "metadata", "fetched_at"}),
	}).Create(&rows).Error
}

// refreshAllStatuses iterates over every registered connector that implements
// StatusRefresher and updates the kind of non-terminal activity items that fall
// within the backfill window (i.e. the same age threshold used for fetching).
func (w *Worker) refreshAllStatuses(ctx context.Context) {
	cutoff := utcToday().AddDate(0, 0, -w.backfill)

	for _, c := range w.registry.All() {
		refresher, ok := c.(connector.StatusRefresher)
		if !ok || !c.IsConfigured() {
			continue
		}

		// Collect non-terminal external IDs within the backfill window.
		var items []db.ActivityItem
		if err := w.db.
			Joins("JOIN days ON days.id = activity_items.day_id").
			Where("activity_items.source = ? AND days.date >= ?", c.Name(), cutoff).
			Find(&items).Error; err != nil {
			log.Printf("worker: refresh query %s: %v", c.Name(), err)
			continue
		}

		// Build a map from external_id to row ID so we can update the specific
		// row rather than all rows for that ticket across days.
		rowByExternalID := make(map[string]uint, len(items))
		var pending []connector.PRStatusItem
		for _, item := range items {
			if !terminalKinds[item.Kind] {
				rowByExternalID[item.ExternalID] = item.ID
				pending = append(pending, connector.PRStatusItem{
					ExternalID:  item.ExternalID,
					CurrentKind: item.Kind,
				})
			}
		}
		if len(pending) == 0 {
			continue
		}

		updates, err := refresher.RefreshStatuses(ctx, pending)
		if err != nil {
			log.Printf("worker: refresh %s: %v", c.Name(), err)
			continue
		}

		for _, u := range updates {
			if id, ok := rowByExternalID[u.ExternalID]; ok {
				w.db.Model(&db.ActivityItem{}).
					Where("id = ?", id).
					Update("kind", u.Kind)
			}
		}

		log.Printf("worker: refreshed %d %s PR statuses", len(updates), c.Name())
	}
}

func utcToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

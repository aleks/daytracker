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

type Worker struct {
	db              *gorm.DB
	registry        *connector.Registry
	backup          *backup.Writer
	trigger         chan string
	interval        time.Duration
	refreshInterval time.Duration
	backfill        int
	// carryDone records dates (YYYY-MM-DD) for which carry-forward has already
	// run this process lifetime, so we don't repeat it on every sync tick.
	carryDone map[string]bool
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
		bw = backup.New(root, database, registry)
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
		carryDone:       make(map[string]bool),
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
	w.carryForward(ctx, utcToday())

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
			w.carryForward(ctx, today)
			w.writeBackup(ctx, today)
		case <-syncTicker.C:
			today := utcToday()
			w.syncAll(ctx, today)
			w.carryForward(ctx, today)
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
			if !refresher.IsTerminal(item.Kind) {
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

// carryForward copies unfinished activity items and undone tasks from yesterday
// into today. It runs at most once per calendar day per process lifetime; the
// upsert logic makes additional runs safe but the guard avoids log noise.
func (w *Worker) carryForward(ctx context.Context, today time.Time) {
	key := today.Format("2006-01-02")
	if w.carryDone[key] {
		return
	}
	w.carryDone[key] = true

	yesterday := today.AddDate(0, 0, -1)

	// ── Activity items ────────────────────────────────────────────────────────

	var yesterdayDay db.Day
	if err := w.db.Where(db.Day{Date: yesterday}).First(&yesterdayDay).Error; err != nil {
		// No yesterday row — nothing to carry forward.
		return
	}

	var candidates []db.ActivityItem
	if err := w.db.Where("day_id = ?", yesterdayDay.ID).Find(&candidates).Error; err != nil {
		log.Printf("worker: carry-forward query activities: %v", err)
		return
	}

	var toCarry []db.ActivityItem
	for _, item := range candidates {
		c, ok := w.registry.Get(item.Source)
		if !ok || !c.ShouldCarryForward(item.Kind) {
			continue
		}
		toCarry = append(toCarry, item)
	}

	var activityCount int
	if len(toCarry) > 0 {
		todayDay := db.Day{Date: today}
		if err := w.db.Where(db.Day{Date: today}).FirstOrCreate(&todayDay).Error; err != nil {
			log.Printf("worker: carry-forward create today: %v", err)
			return
		}

		now := time.Now().UTC()
		rows := make([]db.ActivityItem, len(toCarry))
		copy(rows, toCarry)
		for i := range rows {
			rows[i].ID = 0
			rows[i].DayID = todayDay.ID
			rows[i].FetchedAt = now
		}

		if err := w.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source"}, {Name: "external_id"}, {Name: "day_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"title", "url", "kind", "metadata", "fetched_at"}),
		}).Create(&rows).Error; err != nil {
			log.Printf("worker: carry-forward insert activities: %v", err)
		} else {
			activityCount = len(rows)
		}
	}

	// ── Tasks ─────────────────────────────────────────────────────────────────

	var yesterdayTasks []db.Task
	if err := w.db.Where("day_id = ? AND done = ?", yesterdayDay.ID, false).Find(&yesterdayTasks).Error; err != nil {
		log.Printf("worker: carry-forward query tasks: %v", err)
		return
	}

	var taskCount int
	for _, task := range yesterdayTasks {
		todayDay := db.Day{Date: today}
		if err := w.db.Where(db.Day{Date: today}).FirstOrCreate(&todayDay).Error; err != nil {
			log.Printf("worker: carry-forward create today for task: %v", err)
			continue
		}

		// Only insert if this title doesn't already exist for today.
		var existing int64
		w.db.Model(&db.Task{}).Where("day_id = ? AND title = ?", todayDay.ID, task.Title).Count(&existing)
		if existing > 0 {
			continue
		}

		newTask := db.Task{DayID: todayDay.ID, Title: task.Title, Done: false}
		if err := w.db.Create(&newTask).Error; err != nil {
			log.Printf("worker: carry-forward insert task %q: %v", task.Title, err)
		} else {
			taskCount++
		}
	}

	if activityCount > 0 || taskCount > 0 {
		log.Printf("worker: carry-forward %s: %d activities, %d tasks", key, activityCount, taskCount)
	}
}

func utcToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

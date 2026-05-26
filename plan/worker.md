# Daytracker — Background Worker

## Location

`internal/worker/worker.go` — a single file. No subpackages.

---

## Responsibilities

1. On startup: run a sync for today immediately.
2. On startup: backfill the past N days for any connector that has no records for those days (see Backfill section).
3. On each ticker tick: run a sync for today for all configured connectors.
4. On demand: accept a trigger from the API to sync a specific connector immediately.
5. On shutdown: drain in-flight work and return cleanly.

---

## Worker Struct

```go
package worker

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "golang.org/x/sync/errgroup"
    "gorm.io/gorm"

    "github.com/you/daytracker/internal/connector"
    "github.com/you/daytracker/internal/db"
)

// TriggerRequest asks the worker to run an immediate sync for one connector on one date.
type TriggerRequest struct {
    ConnectorName string
    Date          time.Time // UTC midnight
}

type Worker struct {
    db           *gorm.DB
    registry     *connector.Registry
    interval     time.Duration
    backfillDays int
    triggerCh    chan TriggerRequest
}

func New(gdb *gorm.DB, reg *connector.Registry, interval time.Duration, backfillDays int) *Worker {
    return &Worker{
        db:           gdb,
        registry:     reg,
        interval:     interval,
        backfillDays: backfillDays,
        triggerCh:    make(chan TriggerRequest, 16), // buffered so API doesn't block
    }
}

// TriggerCh returns the channel the API uses to request an immediate sync.
func (w *Worker) TriggerCh() chan<- TriggerRequest {
    return w.triggerCh
}
```

---

## Run Loop

```go
// Run blocks until ctx is cancelled. Call it in a goroutine from main.
func (w *Worker) Run(ctx context.Context) {
    // 1. Immediate sync for today
    today := utcMidnight(time.Now())
    w.syncAll(ctx, today)

    // 2. Backfill past days
    w.backfill(ctx)

    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return

        case <-ticker.C:
            w.syncAll(ctx, utcMidnight(time.Now()))

        case req := <-w.triggerCh:
            w.syncOne(ctx, req.ConnectorName, req.Date)
        }
    }
}
```

---

## syncAll — Parallel Connector Fetch

Runs all configured connectors in parallel using `errgroup`. A single connector failure does not block the others.

```go
func (w *Worker) syncAll(ctx context.Context, date time.Time) {
    g, gctx := errgroup.WithContext(ctx)

    for _, c := range w.registry.All() {
        c := c // capture loop variable
        if !c.IsConfigured() {
            continue
        }
        g.Go(func() error {
            return w.fetchAndUpsert(gctx, c, date)
        })
    }

    // Wait for all; log errors individually inside fetchAndUpsert, not here.
    // We don't propagate the errgroup error because individual connector failures
    // should not prevent other connectors from running.
    _ = g.Wait()
}
```

---

## syncOne — Single Connector On-Demand

Called when the API triggers a manual sync:

```go
func (w *Worker) syncOne(ctx context.Context, name string, date time.Time) {
    c, ok := w.registry.Get(name)
    if !ok {
        slog.Warn("syncOne: unknown connector", "name", name)
        return
    }
    if !c.IsConfigured() {
        slog.Warn("syncOne: connector not configured", "name", name)
        return
    }
    if err := w.fetchAndUpsert(ctx, c, date); err != nil {
        slog.Error("syncOne failed", "connector", name, "err", err)
    }
}
```

---

## fetchAndUpsert

The core unit of work: call `Fetch`, upsert each item, update `ConnectorState`.

```go
func (w *Worker) fetchAndUpsert(ctx context.Context, c connector.Connector, date time.Time) error {
    items, err := c.Fetch(ctx, date)

    // Update connector state regardless of success/failure
    state := db.ConnectorState{Name: c.Name()}
    errMsg := ""
    if err != nil {
        errMsg = err.Error()
        slog.Warn("connector fetch failed", "connector", c.Name(), "date", date.Format("2006-01-02"), "err", err)
    } else {
        now := time.Now().UTC()
        state.LastSyncAt = &now
        slog.Info("connector fetch ok", "connector", c.Name(), "date", date.Format("2006-01-02"), "items", len(items))
    }
    state.LastError = errMsg

    if saveErr := w.db.
        Where(db.ConnectorState{Name: c.Name()}).
        Assign(state).
        FirstOrCreate(&db.ConnectorState{}).Error; saveErr != nil {
        slog.Error("failed to save connector state", "connector", c.Name(), "err", saveErr)
    }

    if err != nil {
        return err
    }

    // Find or create the Day row
    day := db.Day{}
    if dbErr := w.db.
        Where(db.Day{Date: date}).
        Attrs(db.Day{Date: date}).
        FirstOrCreate(&day).Error; dbErr != nil {
        return fmt.Errorf("fetchAndUpsert: create day: %w", dbErr)
    }

    // Upsert each activity item
    for i := range items {
        items[i].DayID = day.ID
        items[i].FetchedAt = time.Now().UTC()
        if upsertErr := connector.UpsertActivity(w.db, items[i]); upsertErr != nil {
            slog.Error("upsert failed", "connector", c.Name(), "external_id", items[i].ExternalID, "err", upsertErr)
            // Continue — don't abort the whole batch for one bad item
        }
    }

    return nil
}
```

---

## Backfill

On startup, for each connector, check whether it has any records in `activity_items` for each of the past `backfillDays` days. If a day is missing records for a connector, queue a `TriggerRequest` into the internal trigger channel so the regular run-loop processes it.

This keeps backfill simple and reuses the same `syncOne` path.

```go
func (w *Worker) backfill(ctx context.Context) {
    today := utcMidnight(time.Now())

    for i := 1; i <= w.backfillDays; i++ {
        date := today.AddDate(0, 0, -i)

        for _, c := range w.registry.All() {
            if !c.IsConfigured() {
                continue
            }
            // Check if this connector has any items for this date
            var count int64
            w.db.Model(&db.ActivityItem{}).
                Joins("JOIN days ON days.id = activity_items.day_id").
                Where("days.date = ? AND activity_items.source = ?", date, c.Name()).
                Count(&count)

            if count == 0 {
                if err := w.fetchAndUpsert(ctx, c, date); err != nil {
                    slog.Warn("backfill fetch failed",
                        "connector", c.Name(),
                        "date", date.Format("2006-01-02"),
                        "err", err)
                }
            }
        }

        // Check for context cancellation between days
        if ctx.Err() != nil {
            return
        }
    }
}
```

Backfill runs synchronously before the main ticker starts. For the default of 7 days × 3 connectors this is fast enough that it doesn't meaningfully delay startup. If it becomes a problem, it can be moved to a separate goroutine.

---

## Configuration

Read in `cmd/server/main.go` before constructing the worker:

```go
interval := 15 * time.Minute
if v := os.Getenv("SYNC_INTERVAL"); v != "" {
    if d, err := time.ParseDuration(v); err == nil {
        interval = d
    }
}

backfillDays := 7
if v := os.Getenv("BACKFILL_DAYS"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n > 0 {
        backfillDays = n
    }
}

w := worker.New(gdb, registry, interval, backfillDays)
```

---

## Wiring in `main.go`

```go
func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    gdb := db.Open()

    registry := connector.NewRegistry(
        connector.NewGitHub(),
        connector.NewJira(),
        connector.NewConfluence(),
    )

    w := worker.New(gdb, registry, interval, backfillDays)

    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()
        w.Run(ctx)
    }()

    r := api.NewRouter(gdb, registry, w.TriggerCh())
    srv := &http.Server{Addr: ":" + port, Handler: r}

    go func() {
        <-ctx.Done()
        shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer shutdownCancel()
        srv.Shutdown(shutdownCtx)
    }()

    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        slog.Error("server error", "err", err)
        os.Exit(1)
    }

    wg.Wait() // wait for worker to drain
}
```

---

## Graceful Shutdown

1. `signal.NotifyContext` catches `SIGINT`/`SIGTERM` and cancels `ctx`.
2. The worker's `Run` loop checks `ctx.Done()` on every iteration and returns.
3. Any in-flight `fetchAndUpsert` calls receive the cancelled context and should return early (connectors respect `ctx`).
4. The HTTP server gets a 10-second shutdown window to drain in-flight requests.
5. `wg.Wait()` in `main` ensures the process doesn't exit until the worker goroutine has returned.

---

## Helper

```go
func utcMidnight(t time.Time) time.Time {
    y, m, d := t.UTC().Date()
    return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
```

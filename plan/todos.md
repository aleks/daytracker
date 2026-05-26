# Daytracker — Todos Feature Spec

This is the first end-to-end feature to build. Everything else (connectors, activity feed) comes after this is working.

---

## Scope

Tasks have:
- `id` (auto-increment)
- `title` (string, required)
- `done` (bool, default false)
- `created_at` (timestamp)
- `day_id` (FK to the day it belongs to)

No priorities. No due dates. No descriptions. No subtasks. The simplest possible todo.

---

## Database

`tasks` table created via GORM `AutoMigrate`:

```go
type Task struct {
    ID        uint      `gorm:"primarykey"`
    DayID     uint      `gorm:"not null;index"`
    Title     string    `gorm:"not null"`
    Done      bool      `gorm:"default:false"`
    CreatedAt time.Time
}
```

`days` table also required (tasks are owned by a day):

```go
type Day struct {
    ID        uint      `gorm:"primarykey"`
    Date      time.Time `gorm:"uniqueIndex;not null"`
    CreatedAt time.Time
}
```

`AutoMigrate` is called in `internal/db/db.go` at startup before the HTTP server starts:
```go
db.AutoMigrate(&Day{}, &Task{})
```

---

## API Routes (Tasks)

All handlers live in `internal/api/tasks.go`.

### `POST /api/days/:date/tasks`

1. Parse `:date` as `time.Time` using `"2006-01-02"` layout. Return `400` if invalid.
2. Find or create a `Day` row for that date using `FirstOrCreate`.
3. Bind request body `{ "title": "..." }`. Return `400` if `title` is empty after trimming.
4. Insert a `Task` row with `DayID` set to the found/created day.
5. Return `201` with the created task as JSON.

### `PATCH /api/tasks/:id`

1. Parse `:id`. Return `400` if not a valid uint.
2. Find the `Task` by ID. Return `404` if not found.
3. Bind request body — only `done` field is accepted for now.
4. Update with `db.Model(&task).Updates(patch)`.
5. Return `200` with updated task.

### `DELETE /api/tasks/:id`

1. Parse `:id`. Return `400` if not valid.
2. Delete with `db.Delete(&Task{}, id)`. Return `404` if `RowsAffected == 0`.
3. Return `204`.

### `GET /api/days/:date` (returns tasks inline)

1. Parse date. Find or create the `Day`.
2. Preload tasks: `db.Preload("Tasks").First(&day, "date = ?", normalizedDate)`.
3. Return the day with its tasks embedded. Activities are an empty array at this stage.

---

## Step-by-Step Build Order

### Step 1 — Scaffold the Go module

```
mkdir -p cmd/server internal/api internal/db
go mod init github.com/you/daytracker
go get github.com/gin-gonic/gin
go get gorm.io/gorm gorm.io/driver/sqlite
```

`cmd/server/main.go` minimal structure:
```go
func main() {
    db := db.Open()          // opens SQLite, runs AutoMigrate
    r := api.NewRouter(db)   // creates Gin router, registers routes
    r.Run(":8080")
}
```

### Step 2 — Implement DB layer

Create `internal/db/db.go`:
- `Open() *gorm.DB` — opens `daytracker.db` (path from `DB_PATH` env var), calls `AutoMigrate`.

Create `internal/db/models.go`:
- `Day` and `Task` structs as above.

Verify: `go run ./cmd/server` creates the DB file and `tasks` table without error.

### Step 3 — Implement task CRUD handlers

Create `internal/api/tasks.go` with a `TaskHandler` struct holding `db *gorm.DB`.

Create `internal/api/days.go` with a `DayHandler` struct.

Create `internal/api/router.go`:
```go
func NewRouter(db *gorm.DB) *gin.Engine {
    r := gin.Default()
    r.Use(corsMiddleware())

    taskH := &TaskHandler{db: db}
    dayH  := &DayHandler{db: db}

    api := r.Group("/api")
    api.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "ok"})
    })
    api.GET("/days", dayH.List)
    api.GET("/days/:date", dayH.Get)
    api.POST("/days/:date/tasks", taskH.Create)
    api.PATCH("/tasks/:id", taskH.Update)
    api.DELETE("/tasks/:id", taskH.Delete)

    return r
}
```

Test with curl:
```sh
# Create a task for today
curl -X POST http://localhost:8080/api/days/2026-05-26/tasks \
  -H 'Content-Type: application/json' \
  -d '{"title": "Test task"}'

# Toggle it done (use the id from the response)
curl -X PATCH http://localhost:8080/api/tasks/1 \
  -H 'Content-Type: application/json' \
  -d '{"done": true}'

# Fetch the day
curl http://localhost:8080/api/days/2026-05-26
```

### Step 4 — Scaffold Vite + Preact + TypeScript

```sh
cd web
npm create vite@latest . -- --template preact-ts
npm install
```

Add the Vite proxy in `vite.config.ts` (see `plan/frontend.md`).

Create `src/types.ts` with `Task`, `DaySummary`, `DayDetail` types.

Create `src/api.ts` with the `api.createTask`, `api.updateTask`, `api.deleteTask`, `api.getDay` functions.

### Step 5 — Build `<TaskList>`

Create `src/components/TaskList.tsx`.

Component state:
```ts
const [newTitle, setNewTitle] = useState('')
const [tasks, setTasks] = useState<Task[]>(initialTasks)
```

Render:
1. A text input bound to `newTitle`. On Enter key or "Add" button click: call `api.createTask(date, newTitle)`, append the returned task to local state, clear input.
2. Sort tasks: undone first, then done.
3. For each task: `<input type="checkbox">` (checked when `done`, on change calls `api.updateTask(id, { done: !done })` and updates local state), `<span>` for title, `<button>` to delete (calls `api.deleteTask(id)` and removes from local state).

Use optimistic updates: update state immediately, log errors to console and revert on failure.

### Step 6 — Wire `<TaskList>` to the API

Create `src/components/DayPage.tsx`:
- On mount, calls `api.getDay(date)` and stores the result in state.
- Passes `tasks` and `date` props to `<TaskList>`.
- `<TaskList>` accepts an `onTasksChanged` callback that triggers a re-fetch of the day.

Create `src/App.tsx`:
- Hard-codes today's date on first load using:
  ```ts
  const today = new Date().toISOString().slice(0, 10)
  ```
- Fetches `api.listDays()` to get historical dates and prepends today.
- Renders one `<DayPage>` per date.

Update `src/main.tsx`:
```tsx
import { render } from 'preact'
import { App } from './App'
import './styles/main.css'

render(<App />, document.getElementById('app')!)
```

### Step 7 — Render tasks grouped under today's date

Run both processes for development:
```sh
# Terminal 1
go run ./cmd/server

# Terminal 2
cd web && npm run dev
```

Open `http://localhost:5173`. Verify:
- Today's date heading is visible.
- Adding a task via the input creates it and shows it in the list.
- Checking the checkbox marks it done (strikethrough).
- Deleting removes it from the list.
- Refreshing the page preserves all tasks (they come from SQLite).

### Step 8 — Embed frontend into binary

Once the feature is working end-to-end, wire up the single-binary production build.

1. Add `assets.go` at the repo root (or in `cmd/server/`):
   ```go
   package main

   import "embed"

   //go:embed all:web/dist
   var webFS embed.FS
   ```

2. In `internal/api/router.go`, accept the `embed.FS` and register the catch-all:
   ```go
   func NewRouter(db *gorm.DB, webFS embed.FS) *gin.Engine {
       // ... existing API routes ...

       sub, _ := fs.Sub(webFS, "web/dist")
       r.NoRoute(func(c *gin.Context) {
           // Try to serve the file as-is; fall back to index.html for SPA routing
           if f, err := sub.Open(strings.TrimPrefix(c.Request.URL.Path, "/")); err == nil {
               f.Close()
               http.FileServer(http.FS(sub)).ServeHTTP(c.Writer, c.Request)
           } else {
               c.FileFromFS("index.html", http.FS(sub))
           }
       })
       return r
   }
   ```

3. Create a `Makefile`:
   ```makefile
   .PHONY: build dev-api dev-web

   build:
       cd web && npm run build
       go build -o daytracker ./cmd/server

   dev-api:
       go run ./cmd/server

   dev-web:
       cd web && npm run dev
   ```

4. Verify production build:
   ```sh
   make build
   ./daytracker         # starts on :8080
   open http://localhost:8080
   ```
   The full app should be reachable from the single binary with no separate `web/dist/` directory needed at runtime.

---

## Acceptance Criteria

- [ ] Adding a task with an empty title is rejected (UI validation + API 400).
- [ ] Tasks persist across browser refreshes.
- [ ] Tasks created for `2026-05-25` appear under the May 25 heading, not today.
- [ ] Toggling done is reflected immediately (optimistic update) and survives a page refresh.
- [ ] Deleting a task removes it immediately and it does not reappear on refresh.
- [ ] The API returns `404` for `PATCH /api/tasks/99999` when that ID does not exist.

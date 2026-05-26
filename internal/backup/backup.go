package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// Writer writes daily markdown snapshots to a directory tree organised as
// <root>/YYYY/MM/DD.md.
type Writer struct {
	root string
	db   *gorm.DB
}

func New(root string, database *gorm.DB) *Writer {
	return &Writer{root: root, db: database}
}

// WriteDay renders the given date to its markdown file, creating parent
// directories as needed. It is idempotent — calling it multiple times for the
// same date overwrites the file with the latest state.
func (w *Writer) WriteDay(ctx context.Context, date time.Time) error {
	date = date.UTC().Truncate(24 * time.Hour)

	var days []db.Day
	if err := w.db.WithContext(ctx).Where(db.Day{Date: date}).Limit(1).Find(&days).Error; err != nil {
		return fmt.Errorf("backup: query day: %w", err)
	}
	if len(days) == 0 {
		return nil
	}
	day := days[0]

	var tasks []db.Task
	if err := w.db.WithContext(ctx).Where("day_id = ?", day.ID).Order("created_at asc").Find(&tasks).Error; err != nil {
		return fmt.Errorf("backup: query tasks: %w", err)
	}

	var activities []db.ActivityItem
	if err := w.db.WithContext(ctx).Where("day_id = ?", day.ID).Order("source asc, fetched_at asc").Find(&activities).Error; err != nil {
		return fmt.Errorf("backup: query activities: %w", err)
	}

	if len(tasks) == 0 && len(activities) == 0 {
		return nil
	}

	content := render(date, tasks, activities)

	dir := filepath.Join(w.root, date.Format("2006"), date.Format("01"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("backup: mkdir %s: %w", dir, err)
	}

	path := filepath.Join(dir, date.Format("02")+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("backup: write %s: %w", path, err)
	}

	return nil
}

func render(date time.Time, tasks []db.Task, activities []db.ActivityItem) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n", date.Format("2006-01-02"))

	if len(tasks) > 0 {
		b.WriteString("\n## Tasks\n\n")
		for _, t := range tasks {
			mark := " "
			if t.Done {
				mark = "x"
			}
			fmt.Fprintf(&b, "- [%s] %s\n", mark, t.Title)
		}
	}

	// Group activities by source, preserving the order sources first appear.
	type group struct {
		source string
		items  []db.ActivityItem
	}
	var order []string
	bySource := make(map[string][]db.ActivityItem)
	for _, a := range activities {
		if _, seen := bySource[a.Source]; !seen {
			order = append(order, a.Source)
		}
		bySource[a.Source] = append(bySource[a.Source], a)
	}

	for _, source := range order {
		items := bySource[source]
		fmt.Fprintf(&b, "\n## %s\n\n", sectionTitle(source))
		for _, a := range items {
			if a.URL != "" {
				fmt.Fprintf(&b, "- [%s](%s) _%s_\n", a.Title, a.URL, kindLabel(a.Kind))
			} else {
				fmt.Fprintf(&b, "- %s _%s_\n", a.Title, kindLabel(a.Kind))
			}
		}
	}

	return b.String()
}

func sectionTitle(source string) string {
	switch source {
	case "github":
		return "GitHub"
	case "jira":
		return "Jira"
	case "confluence":
		return "Confluence"
	default:
		if len(source) == 0 {
			return source
		}
		return strings.ToUpper(source[:1]) + source[1:]
	}
}

func kindLabel(kind string) string {
	switch kind {
	case "authored_open":
		return "open"
	case "authored_merged":
		return "merged"
	case "authored_closed":
		return "closed"
	case "reviewed_open":
		return "reviewed · open"
	case "reviewed_merged":
		return "reviewed · merged"
	case "reviewed_closed":
		return "reviewed · closed"
	case "jira_todo":
		return "to do"
	case "jira_in_progress":
		return "in progress"
	case "jira_done":
		return "done"
	case "confluence_created":
		return "created"
	case "confluence_edited":
		return "edited"
	default:
		return kind
	}
}

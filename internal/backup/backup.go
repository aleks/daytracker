package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/aleksmaksimow/daytracker/internal/connector"
	"github.com/aleksmaksimow/daytracker/internal/db"
)

var urlRE = regexp.MustCompile(`https?://\S+`)

// Writer writes daily markdown snapshots to a directory tree organised as
// <root>/YYYY/MM/DD.md.
type Writer struct {
	root     string
	db       *gorm.DB
	registry *connector.Registry
}

func New(root string, database *gorm.DB, registry *connector.Registry) *Writer {
	return &Writer{root: root, db: database, registry: registry}
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

	content := w.render(date, tasks, activities)

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

func (w *Writer) render(date time.Time, tasks []db.Task, activities []db.ActivityItem) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n", date.Format("2006-01-02"))

	if len(tasks) > 0 {
		b.WriteString("\n## Tasks\n\n")
		for _, t := range tasks {
			mark := " "
			if t.Done {
				mark = "x"
			}
			fmt.Fprintf(&b, "- [%s] %s\n", mark, renderTaskTitle(t.Title))
		}
	}

	// Group activities by source, preserving the order sources first appear.
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
		c, _ := w.registry.Get(source)
		for _, a := range items {
			label := kindLabel(c, a.Kind)
			if a.URL != "" {
				fmt.Fprintf(&b, "- [%s](%s) _%s_\n", a.Title, a.URL, label)
			} else {
				fmt.Fprintf(&b, "- %s _%s_\n", a.Title, label)
			}
		}
	}

	return b.String()
}

// kindLabel resolves a human-readable label using the connector if available,
// falling back to the raw kind string.
func kindLabel(c connector.Connector, kind string) string {
	if c != nil {
		return c.KindLabel(kind)
	}
	return kind
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

// renderTaskTitle replaces bare URLs with numbered markdown links so the
// plain text remains readable without long URLs cluttering the line.
func renderTaskTitle(title string) string {
	urls := urlRE.FindAllString(title, -1)
	if len(urls) == 0 {
		return title
	}
	text := strings.TrimSpace(urlRE.ReplaceAllString(title, ""))
	var sb strings.Builder
	sb.WriteString(text)
	for i, u := range urls {
		label := "Open link"
		if len(urls) > 1 {
			label = fmt.Sprintf("Open link %d", i+1)
		}
		fmt.Fprintf(&sb, " [%s](%s)", label, u)
	}
	return sb.String()
}

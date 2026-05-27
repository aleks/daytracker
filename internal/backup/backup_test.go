package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&db.Day{}, &db.Task{}, &db.ActivityItem{}, &db.ConnectorState{}))
	return database
}

// stubConnector is a minimal connector.Connector implementation for tests.
type stubConnector struct {
	name   string
	labels map[string]string
}

func (s *stubConnector) Name() string        { return s.name }
func (s *stubConnector) IsConfigured() bool  { return true }
func (s *stubConnector) KindLabel(kind string) string {
	if label, ok := s.labels[kind]; ok {
		return label
	}
	return kind
}
func (s *stubConnector) ShouldCarryForward(_ string) bool { return false }
func (s *stubConnector) Fetch(_ context.Context, _ time.Time) ([]db.ActivityItem, error) {
	return nil, nil
}

func newRegistry(connectors ...connector.Connector) *connector.Registry {
	r := connector.NewRegistry()
	for _, c := range connectors {
		r.Register(c)
	}
	return r
}

// ── WriteDay: skip conditions ─────────────────────────────────────────────────

func TestWriteDay_SkipsDateWithNoRow(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry())

	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	err := w.WriteDay(context.Background(), date)
	require.NoError(t, err)

	// No file should have been created anywhere under root.
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	assert.Empty(t, files)
}

func TestWriteDay_SkipsDayWithNoTasksOrActivities(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry())

	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)

	err := w.WriteDay(context.Background(), date)
	require.NoError(t, err)

	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	assert.Empty(t, files)
}

// ── WriteDay: file path ───────────────────────────────────────────────────────

func TestWriteDay_WritesCorrectFilePath(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry())

	date := time.Date(2025, 6, 5, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	task := db.Task{DayID: day.ID, Title: "some task"}
	require.NoError(t, database.Create(&task).Error)

	err := w.WriteDay(context.Background(), date)
	require.NoError(t, err)

	expectedPath := filepath.Join(root, "2025", "06", "05.md")
	_, statErr := os.Stat(expectedPath)
	assert.NoError(t, statErr, "expected file at %s to exist", expectedPath)
}

// ── Rendered content: tasks ───────────────────────────────────────────────────

func TestWriteDay_TaskCheckboxNotDone(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry())

	date := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	task := db.Task{DayID: day.ID, Title: "pending task", Done: false}
	require.NoError(t, database.Create(&task).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))

	content := readFile(t, root, "2025", "07", "01.md")
	assert.Contains(t, content, "- [ ] pending task")
}

func TestWriteDay_TaskCheckboxDone(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry())

	date := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	task := db.Task{DayID: day.ID, Title: "completed task", Done: true}
	require.NoError(t, database.Create(&task).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))

	content := readFile(t, root, "2025", "07", "01.md")
	assert.Contains(t, content, "- [x] completed task")
}

// ── Rendered content: activity sections ──────────────────────────────────────

func TestWriteDay_ActivityGroupedUnderGitHubHeading(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry(&stubConnector{name: "github"}))

	date := time.Date(2025, 7, 2, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	activity := db.ActivityItem{
		DayID:      day.ID,
		Source:     "github",
		ExternalID: "repo#1",
		Kind:       "authored_open",
		Title:      "My PR",
		URL:        "https://github.com/org/repo/pull/1",
	}
	require.NoError(t, database.Create(&activity).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))

	content := readFile(t, root, "2025", "07", "02.md")
	assert.Contains(t, content, "## GitHub")
}

func TestWriteDay_ActivityGroupedUnderJiraHeading(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry(&stubConnector{name: "jira"}))

	date := time.Date(2025, 7, 3, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	activity := db.ActivityItem{
		DayID:      day.ID,
		Source:     "jira",
		ExternalID: "PROJ-1",
		Kind:       "jira_done",
		Title:      "Fix bug",
		URL:        "https://org.atlassian.net/browse/PROJ-1",
	}
	require.NoError(t, database.Create(&activity).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))

	content := readFile(t, root, "2025", "07", "03.md")
	assert.Contains(t, content, "## Jira")
}

func TestWriteDay_ActivityGroupedUnderConfluenceHeading(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry(&stubConnector{name: "confluence"}))

	date := time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	activity := db.ActivityItem{
		DayID:      day.ID,
		Source:     "confluence",
		ExternalID: "page:123",
		Kind:       "confluence_edited",
		Title:      "Design Doc",
		URL:        "https://org.atlassian.net/wiki/spaces/~user/pages/123",
	}
	require.NoError(t, database.Create(&activity).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))

	content := readFile(t, root, "2025", "07", "04.md")
	assert.Contains(t, content, "## Confluence")
}

// ── Rendered content: activity item format ────────────────────────────────────

func TestWriteDay_ActivityItemWithURL(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	stub := &stubConnector{
		name:   "github",
		labels: map[string]string{"authored_open": "open"},
	}
	w := New(root, database, newRegistry(stub))

	date := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	activity := db.ActivityItem{
		DayID:      day.ID,
		Source:     "github",
		ExternalID: "repo#42",
		Kind:       "authored_open",
		Title:      "Add feature",
		URL:        "https://github.com/org/repo/pull/42",
	}
	require.NoError(t, database.Create(&activity).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))

	content := readFile(t, root, "2025", "08", "01.md")
	assert.Contains(t, content, "- [Add feature](https://github.com/org/repo/pull/42) _open_")
}

func TestWriteDay_ActivityItemWithoutURL(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	stub := &stubConnector{
		name:   "jira",
		labels: map[string]string{"jira_done": "done"},
	}
	w := New(root, database, newRegistry(stub))

	date := time.Date(2025, 8, 2, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	activity := db.ActivityItem{
		DayID:      day.ID,
		Source:     "jira",
		ExternalID: "PROJ-99",
		Kind:       "jira_done",
		Title:      "Close ticket",
		URL:        "",
	}
	require.NoError(t, database.Create(&activity).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))

	content := readFile(t, root, "2025", "08", "02.md")
	assert.Contains(t, content, "- Close ticket _done_")
}

// ── KindLabel via stub (jira_done → "done") ───────────────────────────────────

func TestWriteDay_KindLabelUsedForLabel(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	stub := &stubConnector{
		name:   "jira",
		labels: map[string]string{"jira_done": "done"},
	}
	w := New(root, database, newRegistry(stub))

	date := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	activity := db.ActivityItem{
		DayID:      day.ID,
		Source:     "jira",
		ExternalID: "PROJ-10",
		Kind:       "jira_done",
		Title:      "Resolve issue",
		URL:        "https://org.atlassian.net/browse/PROJ-10",
	}
	require.NoError(t, database.Create(&activity).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))

	content := readFile(t, root, "2025", "09", "01.md")
	// The rendered label should be "done" (from KindLabel), not the raw "jira_done".
	assert.Contains(t, content, "_done_")
	assert.NotContains(t, content, "_jira_done_")
}

// ── WriteDay: idempotency ─────────────────────────────────────────────────────

func TestWriteDay_Idempotent(t *testing.T) {
	database := newTestDB(t)
	root := t.TempDir()
	w := New(root, database, newRegistry())

	date := time.Date(2025, 10, 5, 0, 0, 0, 0, time.UTC)
	day := db.Day{Date: date}
	require.NoError(t, database.Create(&day).Error)
	task := db.Task{DayID: day.ID, Title: "idempotent task"}
	require.NoError(t, database.Create(&task).Error)

	require.NoError(t, w.WriteDay(context.Background(), date))
	first := readFile(t, root, "2025", "10", "05.md")

	require.NoError(t, w.WriteDay(context.Background(), date))
	second := readFile(t, root, "2025", "10", "05.md")

	assert.Equal(t, first, second)
}

// ── renderTaskTitle ───────────────────────────────────────────────────────────

func TestRenderTaskTitle_PlainTitle(t *testing.T) {
	result := renderTaskTitle("plain title no urls")
	assert.Equal(t, "plain title no urls", result)
}

func TestRenderTaskTitle_TitleWithOneURL(t *testing.T) {
	result := renderTaskTitle("check this https://example.com/page")
	// URL should be replaced with "[Open link](url)" appended.
	assert.NotContains(t, result, "https://example.com/page ")
	assert.Contains(t, result, "[Open link](https://example.com/page)")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(result), "[Open link](https://example.com/page)"))
}

func TestRenderTaskTitle_TitleWithTwoURLs(t *testing.T) {
	result := renderTaskTitle("see https://example.com/a and https://example.com/b")
	assert.Contains(t, result, "[Open link 1](https://example.com/a)")
	assert.Contains(t, result, "[Open link 2](https://example.com/b)")
}

// ── sectionTitle ─────────────────────────────────────────────────────────────

func TestSectionTitle_KnownSources(t *testing.T) {
	assert.Equal(t, "GitHub", sectionTitle("github"))
	assert.Equal(t, "Jira", sectionTitle("jira"))
	assert.Equal(t, "Confluence", sectionTitle("confluence"))
}

func TestSectionTitle_UnknownSource(t *testing.T) {
	// Unknown sources should be title-cased (first letter uppercased).
	assert.Equal(t, "Custom", sectionTitle("custom"))
}

func TestSectionTitle_EmptySource(t *testing.T) {
	assert.Equal(t, "", sectionTitle(""))
}

// ── helpers ───────────────────────────────────────────────────────────────────

func readFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(parts...)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "expected file %s to exist", path)
	return string(data)
}

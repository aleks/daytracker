package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SearchHandler struct {
	db *gorm.DB
}

// SearchResult is one autocomplete hit returned to the frontend.
type SearchResult struct {
	Date   string `json:"date"`
	Type   string `json:"type"`   // "activity", "task" or "work_journal"
	Source string `json:"source"` // connector name for activities, empty otherwise
	Title  string `json:"title"`
	URL    string `json:"url"`
}

func (h *SearchHandler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	source := strings.TrimSpace(c.Query("source"))

	if q == "" {
		c.JSON(http.StatusOK, []SearchResult{})
		return
	}

	// FTS5 requires the query to end with * for prefix matching.
	ftsQuery := ftsEscape(q) + "*"

	var activities, tasks, journal []SearchResult

	// Activities — skip when filtering to tasks only
	if source != "tasks" && source != "work_journal" {
		type activityRow struct {
			Date   string
			Source string
			Title  string
			URL    string
		}
		activitySQL := `
			SELECT d.date, ai.source, ai.title, ai.url
			FROM activity_items_fts fts
			JOIN activity_items ai ON ai.id = fts.rowid
			JOIN days d ON d.id = ai.day_id
			WHERE activity_items_fts MATCH ?`
		args := []any{ftsQuery}

		if source != "" {
			activitySQL += ` AND ai.source = ?`
			args = append(args, source)
		}
		activitySQL += ` ORDER BY fts.rank LIMIT 15`

		var activityRows []activityRow
		if err := h.db.Raw(activitySQL, args...).Scan(&activityRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, r := range activityRows {
			activities = append(activities, SearchResult{
				Date:   r.Date[:10],
				Type:   "activity",
				Source: r.Source,
				Title:  r.Title,
				URL:    r.URL,
			})
		}
	}

	// Tasks — skip when filtering to a specific connector source
	if source == "" || source == "tasks" {
		type taskRow struct {
			Date  string
			Title string
		}
		var taskRows []taskRow
		taskSQL := `
			SELECT d.date, t.title
			FROM tasks_fts fts
			JOIN tasks t ON t.id = fts.rowid
			JOIN days d ON d.id = t.day_id
			WHERE tasks_fts MATCH ?
			ORDER BY fts.rank LIMIT 15`
		if err := h.db.Raw(taskSQL, ftsQuery).Scan(&taskRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, r := range taskRows {
			tasks = append(tasks, SearchResult{
				Date:  r.Date[:10],
				Type:  "task",
				Title: r.Title,
			})
		}
	}

	if source == "" || source == "work_journal" {
		type journalRow struct {
			Date  string
			Title string
		}
		var journalRows []journalRow
		journalSQL := `
			SELECT d.date, snippet(work_log_entries_fts, 0, '', '', ' … ', 20) AS title
			FROM work_log_entries_fts fts
			JOIN work_log_entries w ON w.id = fts.rowid
			JOIN days d ON d.id = w.day_id
			WHERE work_log_entries_fts MATCH ?
			ORDER BY fts.rank LIMIT 15`
		if err := h.db.Raw(journalSQL, ftsQuery).Scan(&journalRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, r := range journalRows {
			journal = append(journal, SearchResult{Date: r.Date[:10], Type: "work_journal", Title: r.Title})
		}
	}

	// Interleave the ranked groups so one type cannot hide all the others.
	results := make([]SearchResult, 0, 15)
	groups := [][]SearchResult{activities, tasks, journal}
	for i := 0; len(results) < 15; i++ {
		added := false
		for _, group := range groups {
			if i < len(group) && len(results) < 15 {
				results = append(results, group[i])
				added = true
			}
		}
		if !added {
			break
		}
	}

	c.JSON(http.StatusOK, results)
}

func (h *SearchHandler) Sources(c *gin.Context) {
	var sources []string
	if err := h.db.Raw(`SELECT DISTINCT source FROM activity_items ORDER BY source`).Scan(&sources).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sources == nil {
		sources = []string{}
	}
	c.JSON(http.StatusOK, sources)
}

// ftsEscape escapes special FTS5 characters in a query string so user input
// cannot inject FTS operators (AND, OR, NOT, quotes, parentheses).
func ftsEscape(q string) string {
	q = strings.ReplaceAll(q, `"`, `""`)
	return `"` + q + `"`
}

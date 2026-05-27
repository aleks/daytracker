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
	Type   string `json:"type"`   // "activity" or "task"
	Source string `json:"source"` // connector name for activities, empty for tasks
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

	results := make([]SearchResult, 0)

	// Activities — skip when filtering to tasks only
	if source != "tasks" {
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
			results = append(results, SearchResult{
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
			results = append(results, SearchResult{
				Date:  r.Date[:10],
				Type:  "task",
				Title: r.Title,
			})
		}
	}

	// Cap combined results at 15.
	if len(results) > 15 {
		results = results[:15]
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

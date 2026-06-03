package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type VelocityHandler struct {
	db *gorm.DB
}

type VelocityResponse struct {
	GitHubAuthored VelocityMetric  `json:"github_authored"`
	Jira           VelocityMetric  `json:"jira"`
	Tasks          VelocityMetric  `json:"tasks"`
	Slowest        []SlowestItem   `json:"slowest"`
}

type VelocityMetric struct {
	AvgDays    float64 `json:"avg_days"`
	SampleSize int     `json:"sample_size"`
}

type SlowestItem struct {
	Source     string  `json:"source"`
	ExternalID string  `json:"external_id"`
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Days       float64 `json:"days"`
	Kind       string  `json:"kind"`
}

func (h *VelocityHandler) Get(c *gin.Context) {
	from := c.Query("from")
	to := c.Query("to")

	fromCond := "(? = '' OR substr(first_seen, 1, 10) >= ?)"
	toCond := "(? = '' OR substr(resolved_on, 1, 10) <= ?)"
	dateArgs := []any{from, from, to, to}

	var resp VelocityResponse

	// ── Activity velocity (GitHub authored + Jira) ────────────────────────────
	//
	// For each external_id we find the earliest day it appeared and the latest
	// day it appeared with a terminal kind. Items that never reached a terminal
	// state are excluded — they aren't "done" yet.
	//
	// Terminal kinds:
	//   github: authored_merged, authored_closed
	//   jira:   jira_done
	//
	// The from/to filters apply to first_seen and resolved_on respectively so
	// a ticket that started before "from" but finished inside the range is
	// counted.

	type activityVelocityRow struct {
		Source     string
		ExternalID string
		Title      string
		URL        string
		FinalKind  string
		FirstSeen  string
		ResolvedOn string
		Days       float64
	}

	var activityRows []activityVelocityRow
	h.db.Raw(`
		SELECT
			ai.source,
			ai.external_id,
			-- title and URL from the most recent row for this item
			FIRST_VALUE(ai.title) OVER (
				PARTITION BY ai.external_id
				ORDER BY d.date DESC
			) AS title,
			FIRST_VALUE(ai.url) OVER (
				PARTITION BY ai.external_id
				ORDER BY d.date DESC
			) AS url,
			sub.final_kind,
			sub.first_seen,
			sub.resolved_on,
			ROUND(julianday(sub.resolved_on) - julianday(sub.first_seen), 1) AS days
		FROM activity_items ai
		JOIN days d ON d.id = ai.day_id
		JOIN (
			SELECT
				ai2.external_id,
				MIN(substr(d2.date, 1, 10))  AS first_seen,
				MAX(CASE WHEN ai2.kind IN (
					'authored_merged', 'authored_closed', 'jira_done'
				) THEN substr(d2.date, 1, 10) END) AS resolved_on,
				MAX(CASE WHEN ai2.kind IN (
					'authored_merged', 'authored_closed', 'jira_done'
				) THEN ai2.kind END) AS final_kind
			FROM activity_items ai2
			JOIN days d2 ON d2.id = ai2.day_id
			WHERE ai2.source IN ('github', 'jira')
			GROUP BY ai2.external_id
			HAVING resolved_on IS NOT NULL
		) AS sub ON sub.external_id = ai.external_id
		WHERE ai.source IN ('github', 'jira')
		  AND `+fromCond+` AND `+toCond+`
		GROUP BY ai.external_id
		ORDER BY days DESC`,
		dateArgs...,
	).Scan(&activityRows)

	// Deduplicate: the window function + GROUP BY can still yield one row per
	// external_id, but be defensive.
	seen := make(map[string]bool, len(activityRows))
	var deduped []activityVelocityRow
	for _, r := range activityRows {
		if !seen[r.ExternalID] {
			seen[r.ExternalID] = true
			deduped = append(deduped, r)
		}
	}
	activityRows = deduped

	var ghSum, ghCount float64
	var jiraSum, jiraCount float64
	for _, r := range activityRows {
		switch r.Source {
		case "github":
			ghSum += r.Days
			ghCount++
		case "jira":
			jiraSum += r.Days
			jiraCount++
		}
	}
	if ghCount > 0 {
		resp.GitHubAuthored = VelocityMetric{AvgDays: round1(ghSum / ghCount), SampleSize: int(ghCount)}
	}
	if jiraCount > 0 {
		resp.Jira = VelocityMetric{AvgDays: round1(jiraSum / jiraCount), SampleSize: int(jiraCount)}
	}

	// ── Task velocity ─────────────────────────────────────────────────────────
	//
	// Tasks have no external_id; we use title as the dedup key. The start time
	// is the created_at of the earliest row for that title, and the end time is
	// the created_at of the earliest done=1 row.

	type taskVelocityRow struct {
		Title      string
		FirstSeen  string
		ResolvedOn string
		Days       float64
	}
	var taskRows []taskVelocityRow
	h.db.Raw(`
		SELECT
			t.title,
			MIN(substr(d.date, 1, 10)) AS first_seen,
			MIN(CASE WHEN t.done = 1 THEN substr(d.date, 1, 10) END) AS resolved_on,
			ROUND(
				julianday(MIN(CASE WHEN t.done = 1 THEN substr(d.date, 1, 10) END))
				- julianday(MIN(substr(d.date, 1, 10))),
				1
			) AS days
		FROM tasks t
		JOIN days d ON d.id = t.day_id
		GROUP BY t.title
		HAVING resolved_on IS NOT NULL
		  AND `+fromCond+` AND `+toCond,
		dateArgs...,
	).Scan(&taskRows)

	var taskSum, taskCount float64
	for _, r := range taskRows {
		taskSum += r.Days
		taskCount++
	}
	if taskCount > 0 {
		resp.Tasks = VelocityMetric{AvgDays: round1(taskSum / taskCount), SampleSize: int(taskCount)}
	}

	// ── Slowest 10 across all sources ─────────────────────────────────────────

	type slowRow struct {
		Source string
		ID     string
		Title  string
		URL    string
		Kind   string
		Days   float64
	}
	var allSlow []slowRow

	for _, r := range activityRows {
		allSlow = append(allSlow, slowRow{
			Source: r.Source,
			ID:     r.ExternalID,
			Title:  r.Title,
			URL:    r.URL,
			Kind:   r.FinalKind,
			Days:   r.Days,
		})
	}
	for _, r := range taskRows {
		allSlow = append(allSlow, slowRow{
			Source: "task",
			ID:     r.Title,
			Title:  r.Title,
			Kind:   "done",
			Days:   r.Days,
		})
	}

	// Sort descending by days.
	for i := 1; i < len(allSlow); i++ {
		for j := i; j > 0 && allSlow[j].Days > allSlow[j-1].Days; j-- {
			allSlow[j], allSlow[j-1] = allSlow[j-1], allSlow[j]
		}
	}
	if len(allSlow) > 10 {
		allSlow = allSlow[:10]
	}

	resp.Slowest = make([]SlowestItem, 0, len(allSlow))
	for _, r := range allSlow {
		resp.Slowest = append(resp.Slowest, SlowestItem{
			Source:     r.Source,
			ExternalID: r.ID,
			Title:      r.Title,
			URL:        r.URL,
			Days:       r.Days,
			Kind:       r.Kind,
		})
	}

	c.JSON(http.StatusOK, resp)
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

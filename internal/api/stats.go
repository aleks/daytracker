package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type StatsHandler struct {
	db *gorm.DB
}

type StatsResponse struct {
	Period     StatsPeriod      `json:"period"`
	Summary    StatsSummary     `json:"summary"`
	GitHub     StatsGitHub      `json:"github"`
	Jira       StatsJira        `json:"jira"`
	Confluence StatsConfluence  `json:"confluence"`
	Timeline   []StatsDayBucket `json:"timeline"`
	TopDays    []StatsTopDay    `json:"top_days"`
}

type StatsPeriod struct {
	From       string `json:"from"`
	To         string `json:"to"`
	ActiveDays int    `json:"active_days"`
	Unique     bool   `json:"unique"`
}

type StatsSummary struct {
	TasksTotal      int `json:"tasks_total"`
	TasksDone       int `json:"tasks_done"`
	ActivitiesTotal int `json:"activities_total"`
}

type StatsGitHub struct {
	AuthoredTotal            int `json:"authored_total"`
	AuthoredMerged           int `json:"authored_merged"`
	AuthoredOpen             int `json:"authored_open"`
	AuthoredDraft            int `json:"authored_draft"`
	AuthoredApproved         int `json:"authored_approved"`
	AuthoredChangesRequested int `json:"authored_changes_requested"`
	AuthoredInReview         int `json:"authored_in_review"`
	AuthoredClosed           int `json:"authored_closed"`
	ReviewedTotal            int `json:"reviewed_total"`
	ReviewedMerged           int `json:"reviewed_merged"`
	ReviewedOpen             int `json:"reviewed_open"`
	ReviewedDraft            int `json:"reviewed_draft"`
	ReviewedClosed           int `json:"reviewed_closed"`
}

type StatsJira struct {
	Total      int `json:"total"`
	Done       int `json:"done"`
	InProgress int `json:"in_progress"`
	Todo       int `json:"todo"`
}

type StatsConfluence struct {
	Total   int `json:"total"`
	Created int `json:"created"`
	Edited  int `json:"edited"`
}

type StatsDayBucket struct {
	Date       string `json:"date"`
	TasksDone  int    `json:"tasks_done"`
	GitHub     int    `json:"github"`
	Jira       int    `json:"jira"`
	Confluence int    `json:"confluence"`
	Total      int    `json:"total"`
}

type StatsTopDay struct {
	Date  string `json:"date"`
	Total int    `json:"total"`
}

func (h *StatsHandler) Get(c *gin.Context) {
	from := c.Query("from")
	to := c.Query("to")
	unique := c.Query("mode") == "unique"

	// Build the WHERE clause fragments for date filtering.
	fromCond := "(? = '' OR substr(d.date, 1, 10) >= ?)"
	toCond := "(? = '' OR substr(d.date, 1, 10) <= ?)"
	dateArgs := []any{from, from, to, to}

	var resp StatsResponse
	resp.Period.From = from
	resp.Period.To = to
	resp.Period.Unique = unique

	// ── Summary: tasks ────────────────────────────────────────────────────────

	type taskSummaryRow struct {
		Total int
		Done  int
	}
	var taskSummary taskSummaryRow
	if unique {
		// Count distinct task titles; a title is "done" if it was ever completed
		// within the period. Carry-forward never copies done tasks, so MAX(done)
		// per title effectively means "was this task completed in the period."
		h.db.Raw(`
			SELECT COUNT(*) as total, COALESCE(SUM(ever_done), 0) as done
			FROM (
				SELECT t.title,
				       MAX(CASE WHEN t.done = 1 THEN 1 ELSE 0 END) as ever_done
				FROM tasks t
				JOIN days d ON d.id = t.day_id
				WHERE `+fromCond+` AND `+toCond+`
				GROUP BY t.title
			) AS s`,
			dateArgs...,
		).Scan(&taskSummary)
	} else {
		h.db.Raw(`
			SELECT COUNT(*) as total,
			       SUM(CASE WHEN t.done = 1 THEN 1 ELSE 0 END) as done
			FROM tasks t
			JOIN days d ON d.id = t.day_id
			WHERE `+fromCond+` AND `+toCond,
			dateArgs...,
		).Scan(&taskSummary)
	}
	resp.Summary.TasksTotal = taskSummary.Total
	resp.Summary.TasksDone = taskSummary.Done

	// ── Summary: activities total ─────────────────────────────────────────────

	if unique {
		h.db.Raw(`
			SELECT COUNT(DISTINCT ai.external_id) FROM activity_items ai
			JOIN days d ON d.id = ai.day_id
			WHERE `+fromCond+` AND `+toCond,
			dateArgs...,
		).Scan(&resp.Summary.ActivitiesTotal)
	} else {
		h.db.Raw(`
			SELECT COUNT(*) FROM activity_items ai
			JOIN days d ON d.id = ai.day_id
			WHERE `+fromCond+` AND `+toCond,
			dateArgs...,
		).Scan(&resp.Summary.ActivitiesTotal)
	}

	// ── Active days ───────────────────────────────────────────────────────────

	h.db.Raw(`
		SELECT COUNT(DISTINCT substr(d.date, 1, 10))
		FROM days d
		LEFT JOIN tasks t ON t.day_id = d.id AND t.done = 1
		LEFT JOIN activity_items ai ON ai.day_id = d.id
		WHERE (t.id IS NOT NULL OR ai.id IS NOT NULL)
		  AND `+fromCond+` AND `+toCond,
		dateArgs...,
	).Scan(&resp.Period.ActiveDays)

	// ── Activity kind breakdown (shared logic for GitHub, Jira, Confluence) ──
	//
	// In unique mode we pick the most recent occurrence of each external_id
	// within the date window (latest day, latest fetch within that day) so that
	// a carried-forward PR is counted once at its current state.

	type kindCountRow struct {
		Kind string
		Cnt  int
	}

	queryKinds := func(source string) []kindCountRow {
		var rows []kindCountRow
		if unique {
			h.db.Raw(`
				SELECT kind, COUNT(*) as cnt FROM (
					SELECT ai.kind,
					       ROW_NUMBER() OVER (
					           PARTITION BY ai.external_id
					           ORDER BY d.date DESC, ai.fetched_at DESC
					       ) AS rn
					FROM activity_items ai
					JOIN days d ON d.id = ai.day_id
					WHERE ai.source = ?
					  AND `+fromCond+` AND `+toCond+`
				) AS ranked WHERE rn = 1
				GROUP BY kind`,
				append([]any{source}, dateArgs...)...,
			).Scan(&rows)
		} else {
			h.db.Raw(`
				SELECT ai.kind, COUNT(*) as cnt
				FROM activity_items ai
				JOIN days d ON d.id = ai.day_id
				WHERE ai.source = ?
				  AND `+fromCond+` AND `+toCond+`
				GROUP BY ai.kind`,
				append([]any{source}, dateArgs...)...,
			).Scan(&rows)
		}
		return rows
	}

	// ── GitHub ────────────────────────────────────────────────────────────────

	for _, r := range queryKinds("github") {
		switch r.Kind {
		case "authored_merged":
			resp.GitHub.AuthoredMerged = r.Cnt
		case "authored_open":
			resp.GitHub.AuthoredOpen = r.Cnt
		case "authored_draft":
			resp.GitHub.AuthoredDraft = r.Cnt
		case "authored_approved":
			resp.GitHub.AuthoredApproved = r.Cnt
		case "authored_changes_requested":
			resp.GitHub.AuthoredChangesRequested = r.Cnt
		case "authored_in_review":
			resp.GitHub.AuthoredInReview = r.Cnt
		case "authored_closed":
			resp.GitHub.AuthoredClosed = r.Cnt
		case "reviewed_merged":
			resp.GitHub.ReviewedMerged = r.Cnt
		case "reviewed_open":
			resp.GitHub.ReviewedOpen = r.Cnt
		case "reviewed_draft":
			resp.GitHub.ReviewedDraft = r.Cnt
		case "reviewed_closed":
			resp.GitHub.ReviewedClosed = r.Cnt
		}
	}
	resp.GitHub.AuthoredTotal = resp.GitHub.AuthoredMerged + resp.GitHub.AuthoredOpen +
		resp.GitHub.AuthoredDraft + resp.GitHub.AuthoredApproved +
		resp.GitHub.AuthoredChangesRequested + resp.GitHub.AuthoredInReview +
		resp.GitHub.AuthoredClosed
	resp.GitHub.ReviewedTotal = resp.GitHub.ReviewedMerged + resp.GitHub.ReviewedOpen +
		resp.GitHub.ReviewedDraft + resp.GitHub.ReviewedClosed

	// ── Jira ─────────────────────────────────────────────────────────────────

	for _, r := range queryKinds("jira") {
		switch r.Kind {
		case "jira_done":
			resp.Jira.Done = r.Cnt
		case "jira_in_progress":
			resp.Jira.InProgress = r.Cnt
		case "jira_todo":
			resp.Jira.Todo = r.Cnt
		}
	}
	resp.Jira.Total = resp.Jira.Done + resp.Jira.InProgress + resp.Jira.Todo

	// ── Confluence ────────────────────────────────────────────────────────────

	for _, r := range queryKinds("confluence") {
		switch r.Kind {
		case "confluence_created":
			resp.Confluence.Created = r.Cnt
		case "confluence_edited":
			resp.Confluence.Edited = r.Cnt
		}
	}
	resp.Confluence.Total = resp.Confluence.Created + resp.Confluence.Edited

	// ── Timeline: daily buckets ───────────────────────────────────────────────

	type activityDayRow struct {
		Day    string
		Source string
		Cnt    int
	}
	var activityDays []activityDayRow
	if unique {
		h.db.Raw(`
			SELECT substr(d.date, 1, 10) as day, ai.source, COUNT(DISTINCT ai.external_id) as cnt
			FROM activity_items ai
			JOIN days d ON d.id = ai.day_id
			WHERE `+fromCond+` AND `+toCond+`
			GROUP BY substr(d.date, 1, 10), ai.source
			ORDER BY day`,
			dateArgs...,
		).Scan(&activityDays)
	} else {
		h.db.Raw(`
			SELECT substr(d.date, 1, 10) as day, ai.source, COUNT(*) as cnt
			FROM activity_items ai
			JOIN days d ON d.id = ai.day_id
			WHERE `+fromCond+` AND `+toCond+`
			GROUP BY substr(d.date, 1, 10), ai.source
			ORDER BY day`,
			dateArgs...,
		).Scan(&activityDays)
	}

	type taskDayRow struct {
		Day string
		Cnt int
	}
	var taskDays []taskDayRow
	if unique {
		h.db.Raw(`
			SELECT substr(d.date, 1, 10) as day, COUNT(DISTINCT t.title) as cnt
			FROM tasks t
			JOIN days d ON d.id = t.day_id
			WHERE t.done = 1
			  AND `+fromCond+` AND `+toCond+`
			GROUP BY substr(d.date, 1, 10)
			ORDER BY day`,
			dateArgs...,
		).Scan(&taskDays)
	} else {
		h.db.Raw(`
			SELECT substr(d.date, 1, 10) as day, COUNT(*) as cnt
			FROM tasks t
			JOIN days d ON d.id = t.day_id
			WHERE t.done = 1
			  AND `+fromCond+` AND `+toCond+`
			GROUP BY substr(d.date, 1, 10)
			ORDER BY day`,
			dateArgs...,
		).Scan(&taskDays)
	}

	bucketMap := map[string]*StatsDayBucket{}
	ensureBucket := func(day string) *StatsDayBucket {
		if b, ok := bucketMap[day]; ok {
			return b
		}
		b := &StatsDayBucket{Date: day}
		bucketMap[day] = b
		return b
	}
	for _, r := range activityDays {
		b := ensureBucket(r.Day)
		switch r.Source {
		case "github":
			b.GitHub += r.Cnt
		case "jira":
			b.Jira += r.Cnt
		case "confluence":
			b.Confluence += r.Cnt
		}
		b.Total += r.Cnt
	}
	for _, r := range taskDays {
		ensureBucket(r.Day).TasksDone += r.Cnt
	}

	dates := make([]string, 0, len(bucketMap))
	for d := range bucketMap {
		dates = append(dates, d)
	}
	sortStrings(dates)
	timeline := make([]StatsDayBucket, 0, len(dates))
	for _, d := range dates {
		timeline = append(timeline, *bucketMap[d])
	}
	resp.Timeline = timeline

	// ── Top 5 busiest days ────────────────────────────────────────────────────

	type topDayRow struct {
		Day   string
		Total int
	}
	var topDays []topDayRow
	if unique {
		h.db.Raw(`
			SELECT substr(d.date, 1, 10) as day, COUNT(DISTINCT ai.external_id) as total
			FROM activity_items ai
			JOIN days d ON d.id = ai.day_id
			WHERE `+fromCond+` AND `+toCond+`
			GROUP BY substr(d.date, 1, 10)
			ORDER BY total DESC
			LIMIT 5`,
			dateArgs...,
		).Scan(&topDays)
	} else {
		h.db.Raw(`
			SELECT substr(d.date, 1, 10) as day, COUNT(*) as total
			FROM activity_items ai
			JOIN days d ON d.id = ai.day_id
			WHERE `+fromCond+` AND `+toCond+`
			GROUP BY substr(d.date, 1, 10)
			ORDER BY total DESC
			LIMIT 5`,
			dateArgs...,
		).Scan(&topDays)
	}
	resp.TopDays = make([]StatsTopDay, 0, len(topDays))
	for _, r := range topDays {
		resp.TopDays = append(resp.TopDays, StatsTopDay{Date: r.Day, Total: r.Total})
	}

	c.JSON(http.StatusOK, resp)
}

// sortStrings sorts a slice of strings in place (ascending).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

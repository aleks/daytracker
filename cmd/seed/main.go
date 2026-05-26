package main

import (
	"log"
	"math/rand"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

func main() {
	database, err := db.Open()
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	today := utcDay(time.Now())
	rng := rand.New(rand.NewSource(42))

	for daysAgo := 7; daysAgo >= 0; daysAgo-- {
		date := today.AddDate(0, 0, -daysAgo)
		// Skip weekends
		if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			continue
		}
		seedDay(database, rng, date)
	}

	log.Println("seed: done")
}

func seedDay(database *gorm.DB, rng *rand.Rand, date time.Time) {
	day := db.Day{Date: date}
	if err := database.Where(db.Day{Date: date}).FirstOrCreate(&day).Error; err != nil {
		log.Fatalf("day: %v", err)
	}

	seedTasks(database, rng, day, date)
	seedActivities(database, rng, day, date)

	log.Printf("seed: seeded %s", date.Format("2006-01-02"))
}

// ── Tasks ────────────────────────────────────────────────────────────────────

var taskPool = []struct {
	title string
	done  bool
}{
	{"Review PR for auth service refactor", true},
	{"Update deployment runbook", true},
	{"Investigate flaky integration test", true},
	{"Write ADR for caching strategy", false},
	{"Sync with design on onboarding flow", true},
	{"Fix nil pointer in user profile handler", true},
	{"Add pagination to search endpoint", true},
	{"Check Datadog alert for p99 spike", true},
	{"Draft Q3 technical roadmap section", false},
	{"Migrate legacy config to env vars", true},
	{"Pair with Alice on rate limiter", true},
	{"Clean up stale feature flags", false},
	{"Document connector interface", true},
	{"Set up local dev seed script", true},
	{"Bump Go toolchain to 1.25", true},
}

func seedTasks(database *gorm.DB, rng *rand.Rand, day db.Day, date time.Time) {
	count := 2 + rng.Intn(4)
	indices := rng.Perm(len(taskPool))[:count]
	for _, idx := range indices {
		t := taskPool[idx]
		task := db.Task{
			DayID:     day.ID,
			Title:     t.title,
			Done:      t.done,
			CreatedAt: date.Add(time.Duration(8+rng.Intn(4)) * time.Hour),
		}
		database.Create(&task)
	}
}

// ── Activities ───────────────────────────────────────────────────────────────

var githubPRs = []struct {
	id    string
	title string
	url   string
	kind  string
}{
	{"pr-101", "feat(auth): add OAuth2 PKCE flow", "https://github.com/example/api/pull/101", "pr_created"},
	{"pr-102", "fix(db): handle null foreign key on user delete", "https://github.com/example/api/pull/102", "pr_created"},
	{"pr-103", "refactor(search): extract query builder", "https://github.com/example/api/pull/103", "pr_review"},
	{"pr-104", "chore: upgrade gin to v1.12", "https://github.com/example/api/pull/104", "pr_review"},
	{"pr-105", "feat(notifications): add email digest", "https://github.com/example/platform/pull/105", "pr_created"},
	{"pr-106", "fix(cache): invalidate on profile update", "https://github.com/example/platform/pull/106", "pr_review"},
	{"pr-107", "test(integration): add cart checkout suite", "https://github.com/example/platform/pull/107", "pr_review"},
	{"pr-108", "docs: update API reference for v2 endpoints", "https://github.com/example/docs/pull/108", "pr_created"},
}

var jiraTickets = []struct {
	id    string
	title string
	url   string
	kind  string
}{
	{"ENG-441", "Investigate memory leak in session store", "https://jira.example.com/browse/ENG-441", "in_progress"},
	{"ENG-452", "Add rate limiting to /api/search", "https://jira.example.com/browse/ENG-452", "in_review"},
	{"ENG-463", "Migrate user table to UUID primary keys", "https://jira.example.com/browse/ENG-463", "in_progress"},
	{"ENG-471", "Deprecate v1 auth endpoints", "https://jira.example.com/browse/ENG-471", "done"},
	{"ENG-485", "Add structured logging to worker service", "https://jira.example.com/browse/ENG-485", "in_progress"},
	{"ENG-490", "Fix incorrect timezone handling in reports", "https://jira.example.com/browse/ENG-490", "done"},
	{"ENG-501", "Spike: evaluate NATS vs Kafka for event bus", "https://jira.example.com/browse/ENG-501", "in_progress"},
	{"PLAT-22", "Automate DB snapshot to S3", "https://jira.example.com/browse/PLAT-22", "done"},
}

var confluencePages = []struct {
	id    string
	title string
	url   string
	kind  string
}{
	{"page-1001", "ADR-014: Caching Strategy for API Responses", "https://confluence.example.com/pages/1001", "page_created"},
	{"page-1002", "Runbook: Deploying the Auth Service", "https://confluence.example.com/pages/1002", "page_edited"},
	{"page-1003", "Q3 Engineering Roadmap Draft", "https://confluence.example.com/pages/1003", "comment_added"},
	{"page-1004", "On-Call Playbook: Database Incidents", "https://confluence.example.com/pages/1004", "comment_added"},
	{"page-1005", "Architecture Overview: Event-Driven Platform", "https://confluence.example.com/pages/1005", "page_created"},
	{"page-1006", "Team Agreements & Norms", "https://confluence.example.com/pages/1006", "comment_added"},
}

func seedActivities(database *gorm.DB, rng *rand.Rand, day db.Day, date time.Time) {
	var items []db.ActivityItem

	// 1–3 GitHub items
	for _, idx := range rng.Perm(len(githubPRs))[:1+rng.Intn(3)] {
		pr := githubPRs[idx]
		items = append(items, db.ActivityItem{
			DayID:      day.ID,
			Source:     "github",
			ExternalID: pr.id + "-" + date.Format("20060102"),
			Kind:       pr.kind,
			Title:      pr.title,
			URL:        pr.url,
			FetchedAt:  date.Add(time.Duration(9+rng.Intn(6)) * time.Hour),
		})
	}

	// 1–2 Jira items
	for _, idx := range rng.Perm(len(jiraTickets))[:1+rng.Intn(2)] {
		ticket := jiraTickets[idx]
		items = append(items, db.ActivityItem{
			DayID:      day.ID,
			Source:     "jira",
			ExternalID: ticket.id + "-" + date.Format("20060102"),
			Kind:       ticket.kind,
			Title:      ticket.title,
			URL:        ticket.url,
			FetchedAt:  date.Add(time.Duration(9+rng.Intn(6)) * time.Hour),
		})
	}

	// 0–2 Confluence items (not every day)
	if rng.Intn(3) > 0 {
		for _, idx := range rng.Perm(len(confluencePages))[:1+rng.Intn(2)] {
			page := confluencePages[idx]
			items = append(items, db.ActivityItem{
				DayID:      day.ID,
				Source:     "confluence",
				ExternalID: page.id + "-" + date.Format("20060102"),
				Kind:       page.kind,
				Title:      page.title,
				URL:        page.url,
				FetchedAt:  date.Add(time.Duration(9+rng.Intn(6)) * time.Hour),
			})
		}
	}

	if len(items) == 0 {
		return
	}

	database.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source"}, {Name: "external_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "url", "kind", "fetched_at"}),
	}).Create(&items)
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

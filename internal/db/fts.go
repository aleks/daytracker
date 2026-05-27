package db

import (
	"fmt"

	"gorm.io/gorm"
)

// SetupFTS creates the FTS5 virtual tables and sync triggers for activity_items
// and tasks, then backfills any rows that are not yet indexed. Safe to call on
// every startup — all statements are idempotent.
func SetupFTS(database *gorm.DB) error {
	sqls := []string{
		// Virtual tables — content= links them to the real tables so we don't
		// duplicate data; the FTS index stores only the indexed text.
		`CREATE VIRTUAL TABLE IF NOT EXISTS activity_items_fts
		 USING fts5(title, content='activity_items', content_rowid='id')`,

		`CREATE VIRTUAL TABLE IF NOT EXISTS tasks_fts
		 USING fts5(title, content='tasks', content_rowid='id')`,

		// Keep activity_items_fts in sync.
		`CREATE TRIGGER IF NOT EXISTS activity_items_fts_insert
		 AFTER INSERT ON activity_items BEGIN
		   INSERT INTO activity_items_fts(rowid, title) VALUES (new.id, new.title);
		 END`,

		`CREATE TRIGGER IF NOT EXISTS activity_items_fts_update
		 AFTER UPDATE ON activity_items BEGIN
		   INSERT INTO activity_items_fts(activity_items_fts, rowid, title) VALUES ('delete', old.id, old.title);
		   INSERT INTO activity_items_fts(rowid, title) VALUES (new.id, new.title);
		 END`,

		`CREATE TRIGGER IF NOT EXISTS activity_items_fts_delete
		 AFTER DELETE ON activity_items BEGIN
		   INSERT INTO activity_items_fts(activity_items_fts, rowid, title) VALUES ('delete', old.id, old.title);
		 END`,

		// Keep tasks_fts in sync.
		`CREATE TRIGGER IF NOT EXISTS tasks_fts_insert
		 AFTER INSERT ON tasks BEGIN
		   INSERT INTO tasks_fts(rowid, title) VALUES (new.id, new.title);
		 END`,

		`CREATE TRIGGER IF NOT EXISTS tasks_fts_update
		 AFTER UPDATE ON tasks BEGIN
		   INSERT INTO tasks_fts(tasks_fts, rowid, title) VALUES ('delete', old.id, old.title);
		   INSERT INTO tasks_fts(rowid, title) VALUES (new.id, new.title);
		 END`,

		`CREATE TRIGGER IF NOT EXISTS tasks_fts_delete
		 AFTER DELETE ON tasks BEGIN
		   INSERT INTO tasks_fts(tasks_fts, rowid, title) VALUES ('delete', old.id, old.title);
		 END`,

		// Rebuild the FTS index from the content tables on every startup.
		// This is the correct way to sync external content= FTS5 tables and
		// handles the case where data existed before the FTS tables were created.
		`INSERT INTO activity_items_fts(activity_items_fts) VALUES('rebuild')`,
		`INSERT INTO tasks_fts(tasks_fts) VALUES('rebuild')`,
	}

	for _, sql := range sqls {
		if err := database.Exec(sql).Error; err != nil {
			return fmt.Errorf("fts setup: %w", err)
		}
	}
	return nil
}

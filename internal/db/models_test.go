package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMemDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&Day{}, &Task{}, &ActivityItem{}, &ConnectorState{}))
	return database
}

// ── Day: unique date constraint ───────────────────────────────────────────────

func TestDay_UniqueDate(t *testing.T) {
	database := newMemDB(t)
	date := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	require.NoError(t, database.Create(&Day{Date: date}).Error)

	err := database.Create(&Day{Date: date}).Error
	assert.Error(t, err, "inserting a second Day with the same date must fail")
}

// ── ConnectorState: unique name constraint ────────────────────────────────────

func TestConnectorState_UniqueName(t *testing.T) {
	database := newMemDB(t)
	require.NoError(t, database.Create(&ConnectorState{Name: "github"}).Error)

	err := database.Create(&ConnectorState{Name: "github"}).Error
	assert.Error(t, err, "inserting a second ConnectorState with the same name must fail")
}

// ── ActivityItem: composite unique index ─────────────────────────────────────

func TestActivityItem_UniqueCompositeIndex(t *testing.T) {
	database := newMemDB(t)
	day := Day{Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day).Error)

	item := ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#1", Kind: "authored_open", Title: "PR"}
	require.NoError(t, database.Create(&item).Error)

	// Same (source, external_id, day_id) must fail.
	dup := ActivityItem{DayID: day.ID, Source: "github", ExternalID: "repo#1", Kind: "authored_merged", Title: "PR dup"}
	err := database.Create(&dup).Error
	assert.Error(t, err, "duplicate (source, external_id, day_id) must be rejected")
}

func TestActivityItem_SameExternalIDDifferentDays(t *testing.T) {
	database := newMemDB(t)
	day1 := Day{Date: time.Date(2025, 1, 14, 0, 0, 0, 0, time.UTC)}
	day2 := Day{Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, database.Create(&day1).Error)
	require.NoError(t, database.Create(&day2).Error)

	// Same source + external_id but different day_ids must succeed.
	item1 := ActivityItem{DayID: day1.ID, Source: "github", ExternalID: "repo#1", Kind: "authored_open", Title: "PR"}
	item2 := ActivityItem{DayID: day2.ID, Source: "github", ExternalID: "repo#1", Kind: "authored_merged", Title: "PR"}
	require.NoError(t, database.Create(&item1).Error)
	require.NoError(t, database.Create(&item2).Error, "same external_id on different days must be allowed")
}

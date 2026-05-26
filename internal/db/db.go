package db

import (
	"os"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func Open() (*gorm.DB, error) {
	path := os.Getenv("DAYTRACKER_DB_PATH")
	if path == "" {
		path = "./daytracker.db"
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&Day{}, &Task{}, &ActivityItem{}, &ConnectorState{}); err != nil {
		return nil, err
	}

	return db, nil
}

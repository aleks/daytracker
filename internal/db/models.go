package db

import "time"

type Day struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Date      time.Time `gorm:"uniqueIndex;not null" json:"date"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	DayID     uint      `gorm:"not null;index" json:"day_id"`
	Title     string    `gorm:"not null" json:"title"`
	Done      bool      `gorm:"default:false" json:"done"`
	Pinned    bool      `gorm:"default:false" json:"pinned"`
	CreatedAt time.Time `json:"created_at"`
}

type ActivityItem struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	DayID      uint      `gorm:"not null;uniqueIndex:idx_source_ext_day" json:"day_id"`
	Source     string    `gorm:"not null;uniqueIndex:idx_source_ext_day" json:"source"`
	ExternalID string    `gorm:"not null;uniqueIndex:idx_source_ext_day" json:"external_id"`
	Kind       string    `gorm:"not null" json:"kind"`
	Title      string    `gorm:"not null" json:"title"`
	URL        string    `json:"url"`
	Metadata   string    `gorm:"type:text" json:"metadata"`
	FetchedAt  time.Time `json:"fetched_at"`
}

type ConnectorState struct {
	ID         uint       `gorm:"primarykey" json:"id"`
	Name       string     `gorm:"uniqueIndex;not null" json:"name"`
	LastSyncAt *time.Time `json:"last_sync_at"`
	LastError  string     `json:"last_error"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

type DayHandler struct {
	db *gorm.DB
}

type DayDetail struct {
	db.Day
	Tasks      []db.Task         `json:"tasks"`
	Activities []db.ActivityItem `json:"activities"`
}

func (h *DayHandler) List(c *gin.Context) {
	var days []db.Day
	err := h.db.
		Where(`EXISTS (SELECT 1 FROM tasks WHERE tasks.day_id = days.id)
			OR EXISTS (SELECT 1 FROM activity_items WHERE activity_items.day_id = days.id)`).
		Order("date desc").
		Find(&days).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, days)
}

func (h *DayHandler) Get(c *gin.Context) {
	date, ok := parseDate(c)
	if !ok {
		return
	}

	day := db.Day{Date: date}
	if err := h.db.Where(db.Day{Date: date}).FirstOrCreate(&day).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var tasks []db.Task
	if err := h.db.Where("day_id = ?", day.ID).Order("created_at asc").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var activities []db.ActivityItem
	if err := h.db.Where("day_id = ?", day.ID).Order("fetched_at asc").Find(&activities).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, DayDetail{
		Day:        day,
		Tasks:      tasks,
		Activities: activities,
	})
}

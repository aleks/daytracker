package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aleks/daytracker/internal/db"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type WorkLogHandler struct {
	db        *gorm.DB
	backupDay func(time.Time)
}

func (h *WorkLogHandler) findEntry(c *gin.Context) (db.WorkLogEntry, db.Day, bool) {
	id, ok := parseID(c)
	if !ok {
		return db.WorkLogEntry{}, db.Day{}, false
	}
	var entry db.WorkLogEntry
	if err := h.db.First(&entry, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "work journal entry not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return db.WorkLogEntry{}, db.Day{}, false
	}
	var day db.Day
	if err := h.db.First(&day, entry.DayID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return db.WorkLogEntry{}, db.Day{}, false
	}
	return entry, day, true
}

func (h *WorkLogHandler) Update(c *gin.Context) {
	entry, day, ok := h.findEntry(c)
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content is required"})
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content must not be blank"})
		return
	}
	if err := h.db.Model(&entry).Update("content", content).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.backupDay != nil {
		h.backupDay(day.Date)
	}
	c.JSON(http.StatusOK, entry)
}

func (h *WorkLogHandler) Delete(c *gin.Context) {
	entry, day, ok := h.findEntry(c)
	if !ok {
		return
	}
	result := h.db.Delete(&entry)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "work journal entry not found"})
		return
	}
	if h.backupDay != nil {
		h.backupDay(day.Date)
	}
	c.Status(http.StatusNoContent)
}

func (h *WorkLogHandler) Create(c *gin.Context) {
	date, ok := parseDate(c)
	if !ok {
		return
	}

	var body struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content is required"})
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content must not be blank"})
		return
	}

	day := db.Day{Date: date}
	if err := h.db.Where(db.Day{Date: date}).FirstOrCreate(&day).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	entry := db.WorkLogEntry{DayID: day.ID, Content: body.Content}
	if err := h.db.Create(&entry).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.backupDay != nil {
		h.backupDay(date)
	}
	c.JSON(http.StatusCreated, entry)
}

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/aleks/daytracker/internal/db"
)

type ActivityHandler struct {
	db *gorm.DB
}

func (h *ActivityHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	result := h.db.Delete(&db.ActivityItem{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "activity not found"})
		return
	}

	c.Status(http.StatusNoContent)
}

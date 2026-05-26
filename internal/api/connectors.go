package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

type ConnectorHandler struct {
	db      *gorm.DB
	trigger chan<- string
}

func (h *ConnectorHandler) List(c *gin.Context) {
	var states []db.ConnectorState
	if err := h.db.Find(&states).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, states)
}

func (h *ConnectorHandler) Sync(c *gin.Context) {
	name := c.Param("name")
	if h.trigger != nil {
		select {
		case h.trigger <- name:
		default:
		}
	}
	c.JSON(http.StatusAccepted, gin.H{"message": "sync triggered"})
}

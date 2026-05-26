package api

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// NewRouter builds the Gin engine. webFS may be nil in development mode.
func NewRouter(database *gorm.DB, webFS fs.FS, trigger chan<- string) *gin.Engine {
	r := gin.Default()
	r.Use(corsMiddleware())

	taskH := &TaskHandler{db: database}
	dayH := &DayHandler{db: database}
	connH := &ConnectorHandler{db: database, trigger: trigger}

	api := r.Group("/api")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		api.GET("/days", dayH.List)
		api.GET("/days/:date", dayH.Get)
		api.GET("/days/:date/activities", func(c *gin.Context) {
			// Alias: same as GET /days/:date but activities only
			date, ok := parseDate(c)
			if !ok {
				return
			}
			var day db.Day
			if err := database.Where(db.Day{Date: date}).First(&day).Error; err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "day not found"})
				return
			}
			var activities []db.ActivityItem
			database.Where("day_id = ?", day.ID).Find(&activities)
			c.JSON(http.StatusOK, activities)
		})
		api.POST("/days/:date/tasks", taskH.Create)
		api.PATCH("/tasks/:id", taskH.Update)
		api.DELETE("/tasks/:id", taskH.Delete)
		api.GET("/connectors", connH.List)
		api.POST("/connectors/:name/sync", connH.Sync)
	}

	if webFS != nil {
		fileServer := http.FileServer(http.FS(webFS))
		r.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path[1:] // strip leading /
			if _, err := webFS.Open(path); err == nil {
				fileServer.ServeHTTP(c.Writer, c.Request)
			} else {
				// SPA fallback
				c.FileFromFS("index.html", http.FS(webFS))
			}
		})
	}

	return r
}

package api

import (
	"io"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// serveEmbedded returns a Gin handler that serves static files from an embedded
// FS and falls back to index.html for any path that doesn't match a file (SPA routing).
func serveEmbedded(files fs.FS) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/" || path == "" {
			serveFile(c, files, "index.html")
			return
		}
		// Strip leading slash for fs.FS lookup.
		fsPath := path[1:]
		f, err := files.Open(fsPath)
		if err != nil {
			// Not found — serve index.html for SPA routing.
			serveFile(c, files, "index.html")
			return
		}
		stat, err := f.Stat()
		f.Close()
		if err != nil || stat.IsDir() {
			serveFile(c, files, "index.html")
			return
		}
		// Serve the real file via http.ServeFileFS which handles ETags, range, etc.
		http.ServeFileFS(c.Writer, c.Request, files, fsPath)
	}
}

func serveFile(c *gin.Context, files fs.FS, name string) {
	f, err := files.Open(name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	// Determine content type from extension.
	contentType := "text/html; charset=utf-8"
	http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), f.(io.ReadSeeker))
	_ = contentType
}

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

	taskH     := &TaskHandler{db: database}
	dayH      := &DayHandler{db: database}
	connH     := &ConnectorHandler{db: database, trigger: trigger}
	searchH   := &SearchHandler{db: database}
	statsH    := &StatsHandler{db: database}
	velocityH := &VelocityHandler{db: database}

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
		api.GET("/search", searchH.Search)
		api.GET("/sources", searchH.Sources)
		api.GET("/stats", statsH.Get)
		api.GET("/stats/velocity", velocityH.Get)
	}

	if webFS != nil {
		r.NoRoute(serveEmbedded(webFS))
	}

	return r
}

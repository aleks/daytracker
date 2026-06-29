package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"gorm.io/gorm"

	"github.com/aleks/daytracker/internal/db"
)

// NewServer builds and returns a configured MCPServer wired to the given
// database and worker trigger channel.
func NewServer(database *gorm.DB, trigger chan<- string) *server.MCPServer {
	h := &handlers{db: database, trigger: trigger}

	s := server.NewMCPServer(
		"daytracker",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(mcp.NewTool("get_day",
		mcp.WithDescription("Get tasks and activity items for a specific date."),
		mcp.WithString("date",
			mcp.Required(),
			mcp.Description("Date in YYYY-MM-DD format, e.g. 2025-06-29"),
		),
	), h.getDay)

	s.AddTool(mcp.NewTool("list_days",
		mcp.WithDescription("List all days that have at least one task or activity item, newest first."),
	), h.listDays)

	s.AddTool(mcp.NewTool("create_task",
		mcp.WithDescription("Add a new task to a specific date."),
		mcp.WithString("date",
			mcp.Required(),
			mcp.Description("Date in YYYY-MM-DD format"),
		),
		mcp.WithString("title",
			mcp.Required(),
			mcp.Description("Task title"),
		),
	), h.createTask)

	s.AddTool(mcp.NewTool("update_task",
		mcp.WithDescription("Update a task's title, done status, or pinned status."),
		mcp.WithString("date",
			mcp.Required(),
			mcp.Description("Date in YYYY-MM-DD format"),
		),
		mcp.WithNumber("id",
			mcp.Required(),
			mcp.Description("Task ID"),
		),
		mcp.WithString("title",
			mcp.Description("New task title (omit to leave unchanged)"),
		),
		mcp.WithBoolean("done",
			mcp.Description("Mark the task done or not done (omit to leave unchanged)"),
		),
		mcp.WithBoolean("pinned",
			mcp.Description("Pin or unpin the task (omit to leave unchanged)"),
		),
	), h.updateTask)

	s.AddTool(mcp.NewTool("delete_task",
		mcp.WithDescription("Delete a task."),
		mcp.WithString("date",
			mcp.Required(),
			mcp.Description("Date in YYYY-MM-DD format"),
		),
		mcp.WithNumber("id",
			mcp.Required(),
			mcp.Description("Task ID"),
		),
	), h.deleteTask)

	s.AddTool(mcp.NewTool("list_connectors",
		mcp.WithDescription("List all connectors and their last sync time and last error."),
	), h.listConnectors)

	s.AddTool(mcp.NewTool("trigger_sync",
		mcp.WithDescription("Trigger an immediate sync for a connector by name (e.g. github, jira, confluence)."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Connector name: github, jira, or confluence"),
		),
	), h.triggerSync)

	return s
}

type handlers struct {
	db      *gorm.DB
	trigger chan<- string
}

func parseDate(raw string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", raw)
	}
	return t.UTC().Truncate(24 * time.Hour), nil
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func errResult(err error) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(err.Error()), nil
}

func (h *handlers) getDay(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	raw, err := req.RequireString("date")
	if err != nil {
		return errResult(err)
	}
	date, err := parseDate(raw)
	if err != nil {
		return errResult(err)
	}

	day := db.Day{Date: date}
	if err := h.db.Where(db.Day{Date: date}).FirstOrCreate(&day).Error; err != nil {
		return errResult(err)
	}

	var tasks []db.Task
	if err := h.db.Where("day_id = ?", day.ID).Order("created_at asc").Find(&tasks).Error; err != nil {
		return errResult(err)
	}

	var activities []db.ActivityItem
	if err := h.db.Where("day_id = ?", day.ID).Order("fetched_at asc").Find(&activities).Error; err != nil {
		return errResult(err)
	}

	return jsonResult(map[string]any{
		"day":        day,
		"tasks":      tasks,
		"activities": activities,
	})
}

func (h *handlers) listDays(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var days []db.Day
	err := h.db.
		Where(`EXISTS (SELECT 1 FROM tasks WHERE tasks.day_id = days.id)
			OR EXISTS (SELECT 1 FROM activity_items WHERE activity_items.day_id = days.id)`).
		Order("date desc").
		Find(&days).Error
	if err != nil {
		return errResult(err)
	}
	return jsonResult(days)
}

func (h *handlers) createTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	raw, err := req.RequireString("date")
	if err != nil {
		return errResult(err)
	}
	date, err := parseDate(raw)
	if err != nil {
		return errResult(err)
	}
	title, err := req.RequireString("title")
	if err != nil {
		return errResult(err)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errResult(fmt.Errorf("title must not be blank"))
	}

	day := db.Day{Date: date}
	if err := h.db.Where(db.Day{Date: date}).FirstOrCreate(&day).Error; err != nil {
		return errResult(err)
	}

	task := db.Task{DayID: day.ID, Title: title}
	if err := h.db.Create(&task).Error; err != nil {
		return errResult(err)
	}
	return jsonResult(task)
}

func (h *handlers) updateTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	idF, err := req.RequireFloat("id")
	if err != nil {
		return errResult(err)
	}
	id, err := strconv.ParseUint(strconv.Itoa(int(idF)), 10, 64)
	if err != nil || id == 0 {
		return errResult(fmt.Errorf("invalid id %v", idF))
	}

	var task db.Task
	if err := h.db.First(&task, id).Error; err != nil {
		return errResult(fmt.Errorf("task not found"))
	}

	args := req.GetArguments()
	if v := req.GetString("title", ""); v != "" {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return errResult(fmt.Errorf("title must not be blank"))
		}
		task.Title = trimmed
	}
	if v, exists := args["done"]; exists && v != nil {
		if b, ok := v.(bool); ok {
			task.Done = b
		}
	}
	if v, exists := args["pinned"]; exists && v != nil {
		if b, ok := v.(bool); ok {
			task.Pinned = b
		}
	}

	if err := h.db.Save(&task).Error; err != nil {
		return errResult(err)
	}
	return jsonResult(task)
}

func (h *handlers) deleteTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	idF, err := req.RequireFloat("id")
	if err != nil {
		return errResult(err)
	}
	id := uint(idF)
	if id == 0 {
		return errResult(fmt.Errorf("invalid id"))
	}

	result := h.db.Delete(&db.Task{}, id)
	if result.Error != nil {
		return errResult(result.Error)
	}
	if result.RowsAffected == 0 {
		return errResult(fmt.Errorf("task not found"))
	}
	return mcp.NewToolResultText("deleted"), nil
}

func (h *handlers) listConnectors(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var states []db.ConnectorState
	if err := h.db.Find(&states).Error; err != nil {
		return errResult(err)
	}
	return jsonResult(states)
}

func (h *handlers) triggerSync(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return errResult(err)
	}
	if h.trigger != nil {
		select {
		case h.trigger <- name:
		default:
		}
	}
	return mcp.NewToolResultText("sync triggered"), nil
}

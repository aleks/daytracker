package connector

import (
	"context"
	"time"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// Connector fetches activity items for a given calendar date.
type Connector interface {
	Name() string
	IsConfigured() bool
	Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error)
	// KindLabel returns a human-readable label for the given kind string.
	// Falls back to returning kind unchanged for unrecognised values.
	KindLabel(kind string) string
}

// PRStatusItem carries the external ID and current kind of an item to refresh.
type PRStatusItem struct {
	ExternalID  string
	CurrentKind string
}

// PRStatusUpdate pairs an external ID with its refreshed kind string.
type PRStatusUpdate struct {
	ExternalID string
	Kind       string
}

// StatusRefresher is an optional capability a Connector may implement to update
// the live status of previously-fetched items (e.g. PR open → merged).
type StatusRefresher interface {
	// IsTerminal reports whether the given kind string represents a state that
	// can never change, so the worker can skip those items during refresh.
	IsTerminal(kind string) bool
	// RefreshStatuses accepts items that are not yet in a terminal state.
	// CurrentKind is provided so implementations can preserve role prefixes
	// (e.g. "authored_open" → "authored_merged") when updating state.
	RefreshStatuses(ctx context.Context, items []PRStatusItem) ([]PRStatusUpdate, error)
}

// Registry holds all registered connectors.
type Registry struct {
	connectors []Connector
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(c Connector) {
	r.connectors = append(r.connectors, c)
}

func (r *Registry) All() []Connector {
	return r.connectors
}

func (r *Registry) Get(name string) (Connector, bool) {
	for _, c := range r.connectors {
		if c.Name() == name {
			return c, true
		}
	}
	return nil, false
}

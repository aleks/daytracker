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
}

// PRStatusUpdate pairs an external ID with its refreshed kind string.
type PRStatusUpdate struct {
	ExternalID string
	Kind       string
}

// StatusRefresher is an optional capability a Connector may implement to update
// the live status of previously-fetched items (e.g. PR open → merged).
type StatusRefresher interface {
	// RefreshStatuses accepts external IDs of items that are not yet in a
	// terminal state and returns updated kind values for each one.
	RefreshStatuses(ctx context.Context, externalIDs []string) ([]PRStatusUpdate, error)
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

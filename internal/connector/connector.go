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

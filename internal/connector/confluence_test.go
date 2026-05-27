package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── ConfluenceConnector.ShouldCarryForward ────────────────────────────────────

func TestConfluence_ShouldCarryForward(t *testing.T) {
	c := NewConfluence()
	cases := []struct {
		kind     string
		expected bool
	}{
		{"confluence_created", false},
		{"confluence_edited", false},
		{"confluence_commented", false},
		{"unknown_kind", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			assert.Equal(t, tc.expected, c.ShouldCarryForward(tc.kind))
		})
	}
}

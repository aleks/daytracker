package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── JiraConnector.ShouldCarryForward ─────────────────────────────────────────

func TestJira_ShouldCarryForward(t *testing.T) {
	j := NewJira()
	cases := []struct {
		kind     string
		expected bool
	}{
		{"jira_todo", true},
		{"jira_in_progress", true},
		{"jira_done", false},
		{"unknown_kind", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			assert.Equal(t, tc.expected, j.ShouldCarryForward(tc.kind))
		})
	}
}

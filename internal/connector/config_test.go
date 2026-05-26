package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── Jira: Name / IsConfigured / constructor ───────────────────────────────────

func TestJiraName(t *testing.T) {
	assert.Equal(t, "jira", NewJira().Name())
}

func TestJiraIsConfigured_AllSet(t *testing.T) {
	t.Setenv("DAYTRACKER_JIRA_BASE_URL", "https://org.atlassian.net")
	t.Setenv("DAYTRACKER_JIRA_EMAIL", "user@example.com")
	t.Setenv("DAYTRACKER_JIRA_TOKEN", "secret")
	assert.True(t, NewJira().IsConfigured())
}

func TestJiraIsConfigured_MissingURL(t *testing.T) {
	t.Setenv("DAYTRACKER_JIRA_BASE_URL", "")
	t.Setenv("DAYTRACKER_JIRA_EMAIL", "user@example.com")
	t.Setenv("DAYTRACKER_JIRA_TOKEN", "secret")
	assert.False(t, NewJira().IsConfigured())
}

func TestJiraIsConfigured_MissingEmail(t *testing.T) {
	t.Setenv("DAYTRACKER_JIRA_BASE_URL", "https://org.atlassian.net")
	t.Setenv("DAYTRACKER_JIRA_EMAIL", "")
	t.Setenv("DAYTRACKER_JIRA_TOKEN", "secret")
	assert.False(t, NewJira().IsConfigured())
}

func TestJiraIsConfigured_MissingToken(t *testing.T) {
	t.Setenv("DAYTRACKER_JIRA_BASE_URL", "https://org.atlassian.net")
	t.Setenv("DAYTRACKER_JIRA_EMAIL", "user@example.com")
	t.Setenv("DAYTRACKER_JIRA_TOKEN", "")
	assert.False(t, NewJira().IsConfigured())
}

func TestJiraConstructor_TrimsTrailingSlash(t *testing.T) {
	t.Setenv("DAYTRACKER_JIRA_BASE_URL", "https://org.atlassian.net/")
	j := NewJira()
	assert.Equal(t, "https://org.atlassian.net", j.baseURL)
}

// ── Confluence: Name / IsConfigured / constructor ─────────────────────────────

func TestConfluenceName(t *testing.T) {
	assert.Equal(t, "confluence", NewConfluence().Name())
}

func TestConfluenceIsConfigured_AllSet(t *testing.T) {
	t.Setenv("DAYTRACKER_CONFLUENCE_BASE_URL", "https://org.atlassian.net")
	t.Setenv("DAYTRACKER_CONFLUENCE_EMAIL", "user@example.com")
	t.Setenv("DAYTRACKER_CONFLUENCE_TOKEN", "secret")
	assert.True(t, NewConfluence().IsConfigured())
}

func TestConfluenceIsConfigured_MissingURL(t *testing.T) {
	t.Setenv("DAYTRACKER_CONFLUENCE_BASE_URL", "")
	t.Setenv("DAYTRACKER_CONFLUENCE_EMAIL", "user@example.com")
	t.Setenv("DAYTRACKER_CONFLUENCE_TOKEN", "secret")
	assert.False(t, NewConfluence().IsConfigured())
}

func TestConfluenceIsConfigured_MissingEmail(t *testing.T) {
	t.Setenv("DAYTRACKER_CONFLUENCE_BASE_URL", "https://org.atlassian.net")
	t.Setenv("DAYTRACKER_CONFLUENCE_EMAIL", "")
	t.Setenv("DAYTRACKER_CONFLUENCE_TOKEN", "secret")
	assert.False(t, NewConfluence().IsConfigured())
}

func TestConfluenceIsConfigured_MissingToken(t *testing.T) {
	t.Setenv("DAYTRACKER_CONFLUENCE_BASE_URL", "https://org.atlassian.net")
	t.Setenv("DAYTRACKER_CONFLUENCE_EMAIL", "user@example.com")
	t.Setenv("DAYTRACKER_CONFLUENCE_TOKEN", "")
	assert.False(t, NewConfluence().IsConfigured())
}

func TestConfluenceConstructor_TrimsTrailingSlash(t *testing.T) {
	t.Setenv("DAYTRACKER_CONFLUENCE_BASE_URL", "https://org.atlassian.net/")
	c := NewConfluence()
	assert.Equal(t, "https://org.atlassian.net", c.baseURL)
}

// ── GitHub: Name ──────────────────────────────────────────────────────────────

func TestGitHubName(t *testing.T) {
	assert.Equal(t, "github", NewGitHub().Name())
}

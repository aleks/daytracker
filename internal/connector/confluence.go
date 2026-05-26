package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aleksmaksimow/daytracker/internal/db"
)

// ConfluenceConnector fetches pages from the user's personal Confluence space.
// Due to Basic Auth scope limitations, cross-space queries are not supported.
type ConfluenceConnector struct {
	baseURL string
	email   string
	token   string
	client  *http.Client

	cloudIDOnce sync.Once
	cloudID     string
	cloudIDErr  error

	userOnce sync.Once
	userID   string
	spaceKey string
	userErr  error
}

func NewConfluence() *ConfluenceConnector {
	return &ConfluenceConnector{
		baseURL: strings.TrimRight(os.Getenv("DAYTRACKER_CONFLUENCE_BASE_URL"), "/"),
		email:   os.Getenv("DAYTRACKER_CONFLUENCE_EMAIL"),
		token:   os.Getenv("DAYTRACKER_CONFLUENCE_TOKEN"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ConfluenceConnector) Name() string { return "confluence" }

func (c *ConfluenceConnector) IsConfigured() bool {
	return c.baseURL != "" && c.email != "" && c.token != ""
}

func (c *ConfluenceConnector) apiBase(ctx context.Context) (string, error) {
	c.cloudIDOnce.Do(func() {
		c.cloudID, c.cloudIDErr = c.resolveCloudID(ctx)
	})
	if c.cloudIDErr != nil {
		return "", c.cloudIDErr
	}
	return "https://api.atlassian.com/ex/confluence/" + c.cloudID, nil
}

func (c *ConfluenceConnector) resolveCloudID(ctx context.Context) (string, error) {
	body, err := c.get(ctx, c.baseURL+"/_edge/tenant_info", false)
	if err != nil {
		return "", fmt.Errorf("confluence: resolve cloud ID: %w", err)
	}
	var info struct {
		CloudID string `json:"cloudId"`
	}
	if err := json.Unmarshal(body, &info); err != nil || info.CloudID == "" {
		return "", fmt.Errorf("confluence: parse tenant_info: %w", err)
	}
	return info.CloudID, nil
}

// resolveUser fetches the current user's account ID and personal space key.
// The personal space key is always "~{accountId}" on Atlassian cloud.
func (c *ConfluenceConnector) resolveUser(ctx context.Context) (string, string, error) {
	c.userOnce.Do(func() {
		base, err := c.apiBase(ctx)
		if err != nil {
			c.userErr = err
			return
		}
		body, err := c.get(ctx, base+"/wiki/rest/api/user/current", true)
		if err != nil {
			c.userErr = fmt.Errorf("confluence: resolve user: %w", err)
			return
		}
		var user struct {
			AccountID     string `json:"accountId"`
			PersonalSpace struct {
				Key string `json:"key"`
			} `json:"personalSpace"`
		}
		if err := json.Unmarshal(body, &user); err != nil || user.AccountID == "" {
			c.userErr = fmt.Errorf("confluence: parse current user: %w", err)
			return
		}
		c.userID = user.AccountID
		c.spaceKey = user.PersonalSpace.Key
		if c.spaceKey == "" {
			// Fallback: Atlassian always uses ~accountId as the personal space key.
			c.spaceKey = "~" + user.AccountID
		}
	})
	return c.userID, c.spaceKey, c.userErr
}

func (c *ConfluenceConnector) Fetch(ctx context.Context, date time.Time) ([]db.ActivityItem, error) {
	base, err := c.apiBase(ctx)
	if err != nil {
		return nil, err
	}
	myID, spaceKey, err := c.resolveUser(ctx)
	if err != nil {
		return nil, err
	}

	start := date.UTC()
	end := start.AddDate(0, 0, 1)

	const pageSize = 50
	var items []db.ActivityItem

	for offset := 0; ; offset += pageSize {
		params := url.Values{
			"type":     {"page"},
			"spaceKey": {spaceKey},
			"status":   {"any"},
			"expand":   {"version,history.lastUpdated,history,space"},
			"limit":    {fmt.Sprintf("%d", pageSize)},
			"start":    {fmt.Sprintf("%d", offset)},
		}
		body, err := c.get(ctx, base+"/wiki/rest/api/content?"+params.Encode(), true)
		if err != nil {
			return nil, fmt.Errorf("confluence: fetch pages: %w", err)
		}

		var resp cfContentResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("confluence: parse pages: %w", err)
		}

		for _, pg := range resp.Results {
			when, err := time.Parse(time.RFC3339, pg.Version.When)
			if err != nil {
				continue
			}
			when = when.UTC()
			if when.Before(start) || !when.Before(end) {
				continue
			}
			if pg.History.LastUpdated.By.AccountID != myID {
				continue
			}
			kind := "confluence_edited"
			if pg.History.CreatedBy.AccountID == myID {
				created, err := time.Parse(time.RFC3339, pg.History.CreatedDate)
				if err == nil && !created.UTC().Before(start) && created.UTC().Before(end) {
					kind = "confluence_created"
				}
			}
			items = append(items, db.ActivityItem{
				Source:     "confluence",
				ExternalID: "page:" + pg.ID,
				Kind:       kind,
				Title:      pg.Title,
				URL:        c.baseURL + "/wiki" + pg.Links.WebUI,
				Metadata:   pg.Space.Name,
			})
		}

		if len(resp.Results) < pageSize {
			break
		}
	}

	return items, nil
}

func (c *ConfluenceConnector) get(ctx context.Context, endpoint string, auth bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if auth {
		creds := base64.StdEncoding.EncodeToString([]byte(c.email + ":" + c.token))
		req.Header.Set("Authorization", "Basic "+creds)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

type cfContentResponse struct {
	Results []cfContent `json:"results"`
}

type cfContent struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Version cfVersion  `json:"version"`
	History cfHistory  `json:"history"`
	Space   cfSpaceRef `json:"space"`
	Links   cfLinks    `json:"_links"`
}

type cfVersion struct {
	When string `json:"when"`
}

type cfHistory struct {
	CreatedBy   cfUser        `json:"createdBy"`
	CreatedDate string        `json:"createdDate"`
	LastUpdated cfLastUpdated `json:"lastUpdated"`
}

type cfUser struct {
	AccountID string `json:"accountId"`
}

type cfLastUpdated struct {
	By   cfUser `json:"by"`
	When string `json:"when"`
}

type cfSpaceRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type cfLinks struct {
	WebUI string `json:"webui"`
}

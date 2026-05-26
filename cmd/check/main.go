// check is a diagnostic tool that runs each configured connector for today
// and prints what it would store, without touching the database.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aleksmaksimow/daytracker/internal/connector"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	ctx := context.Background()
	date := utcToday()
	fmt.Printf("Checking connectors for %s\n\n", date.Format("2006-01-02"))

	checkGitHub(ctx)
	checkJira(ctx, date)
	checkConfluence(ctx, date)
}

func checkGitHub(ctx context.Context) {
	fmt.Println("=== github ===")
	c := connector.NewGitHub()
	if !c.IsConfigured() {
		fmt.Println("  not configured — run: gh auth login")
		fmt.Println()
		return
	}
	items, err := c.Fetch(ctx, utcToday())
	if err != nil {
		fmt.Printf("  ERROR: %v\n\n", err)
		return
	}
	printItems(items)
}

func checkJira(ctx context.Context, date time.Time) {
	fmt.Println("=== jira ===")

	baseURL := strings.TrimRight(os.Getenv("DAYTRACKER_JIRA_BASE_URL"), "/")
	email := os.Getenv("DAYTRACKER_JIRA_EMAIL")
	token := os.Getenv("DAYTRACKER_JIRA_TOKEN")

	missing := []string{}
	if baseURL == "" {
		missing = append(missing, "DAYTRACKER_JIRA_BASE_URL")
	}
	if email == "" {
		missing = append(missing, "DAYTRACKER_JIRA_EMAIL")
	}
	if token == "" {
		missing = append(missing, "DAYTRACKER_JIRA_TOKEN")
	}
	if len(missing) > 0 {
		fmt.Printf("  not configured — missing: %s\n\n", strings.Join(missing, ", "))
		return
	}

	creds := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	client := &http.Client{Timeout: 15 * time.Second}

	// Step 1: resolve cloud ID via unauthenticated tenant_info.
	resBody, err := jiraGET(ctx, client, "", baseURL+"/_edge/tenant_info")
	if err != nil {
		fmt.Printf("  could not reach %s: %v\n\n", baseURL, err)
		return
	}
	var tenantInfo struct {
		CloudID string `json:"cloudId"`
	}
	_ = json.Unmarshal(resBody, &tenantInfo)
	cloudID := tenantInfo.CloudID
	if cloudID == "" {
		fmt.Printf("  could not resolve cloud ID for %s\n\n", baseURL)
		return
	}
	apiBase := "https://api.atlassian.com/ex/jira/" + cloudID
	fmt.Printf("  cloud ID: %s\n", cloudID)

	// Step 2: verify credentials via /myself.
	myself, err := jiraGET(ctx, client, creds, apiBase+"/rest/api/3/myself")
	if err != nil {
		fmt.Printf("  auth check failed: %v\n\n", err)
		return
	}
	var me struct {
		DisplayName string `json:"displayName"`
		EmailAddr   string `json:"emailAddress"`
	}
	_ = json.Unmarshal(myself, &me)
	fmt.Printf("  authenticated as: %s (%s)\n", me.DisplayName, me.EmailAddr)

	// Step 3: run the JQL search.
	dateStr := date.Format("2006-01-02")
	nextDate := date.AddDate(0, 0, 1).Format("2006-01-02")
	jql := fmt.Sprintf(`assignee = currentUser() AND updated >= "%s" AND updated < "%s"`, dateStr, nextDate)
	fmt.Printf("  JQL: %s\n", jql)

	payload, _ := json.Marshal(map[string]any{
		"jql":        jql,
		"fields":     []string{"summary", "status", "issuetype"},
		"maxResults": 100,
	})

	respBody, statusCode, err := jiraPOST(ctx, client, creds, apiBase+"/rest/api/3/search/jql", payload)
	if err != nil {
		fmt.Printf("  request failed: %v\n\n", err)
		return
	}
	if statusCode != http.StatusOK {
		fmt.Printf("  unexpected status %d: %s\n\n", statusCode, string(respBody))
		return
	}

	var result struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		fmt.Printf("  parse error: %v\n  raw: %s\n\n", err, string(respBody))
		return
	}

	if len(result.Issues) == 0 {
		fmt.Println("  no issues matched the JQL for this date")
		fmt.Println()
		return
	}

	fmt.Printf("  %d issue(s) found:\n", len(result.Issues))
	for _, issue := range result.Issues {
		fmt.Printf("    [%s] %s — %s\n", issue.Key, issue.Fields.Summary, issue.Fields.Status.Name)
	}
	fmt.Println()
}

func checkConfluence(ctx context.Context, date time.Time) {
	fmt.Println("=== confluence ===")

	baseURL := strings.TrimRight(os.Getenv("DAYTRACKER_CONFLUENCE_BASE_URL"), "/")
	email := os.Getenv("DAYTRACKER_CONFLUENCE_EMAIL")
	token := os.Getenv("DAYTRACKER_CONFLUENCE_TOKEN")

	missing := []string{}
	if baseURL == "" {
		missing = append(missing, "DAYTRACKER_CONFLUENCE_BASE_URL")
	}
	if email == "" {
		missing = append(missing, "DAYTRACKER_CONFLUENCE_EMAIL")
	}
	if token == "" {
		missing = append(missing, "DAYTRACKER_CONFLUENCE_TOKEN")
	}
	if len(missing) > 0 {
		fmt.Printf("  not configured — missing: %s\n\n", strings.Join(missing, ", "))
		return
	}

	creds := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	client := &http.Client{Timeout: 15 * time.Second}

	// Resolve cloud ID.
	resBody, err := jiraGET(ctx, client, "", baseURL+"/_edge/tenant_info")
	if err != nil {
		fmt.Printf("  could not reach %s: %v\n\n", baseURL, err)
		return
	}
	var tenantInfo struct {
		CloudID string `json:"cloudId"`
	}
	_ = json.Unmarshal(resBody, &tenantInfo)
	if tenantInfo.CloudID == "" {
		fmt.Printf("  could not resolve cloud ID for %s\n\n", baseURL)
		return
	}
	apiBase := "https://api.atlassian.com/ex/confluence/" + tenantInfo.CloudID
	fmt.Printf("  cloud ID: %s\n", tenantInfo.CloudID)

	// Verify credentials and resolve personal space key.
	meBody, err := jiraGET(ctx, client, creds, apiBase+"/wiki/rest/api/user/current?expand=personalSpace")
	if err != nil {
		fmt.Printf("  auth check failed: %v\n\n", err)
		return
	}
	var me struct {
		AccountID     string `json:"accountId"`
		DisplayName   string `json:"displayName"`
		PersonalSpace struct {
			Key string `json:"key"`
		} `json:"personalSpace"`
	}
	_ = json.Unmarshal(meBody, &me)
	spaceKey := me.PersonalSpace.Key
	if spaceKey == "" {
		spaceKey = "~" + me.AccountID
	}
	fmt.Printf("  authenticated as: %s (space: %s)\n", me.DisplayName, spaceKey)

	start := date.UTC()
	end := start.AddDate(0, 0, 1)

	type cfPage struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Version struct {
			When string `json:"when"`
		} `json:"version"`
		History struct {
			CreatedBy struct {
				AccountID string `json:"accountId"`
			} `json:"createdBy"`
			CreatedDate string `json:"createdDate"`
			LastUpdated struct {
				By struct {
					AccountID string `json:"accountId"`
				} `json:"by"`
			} `json:"lastUpdated"`
		} `json:"history"`
	}

	const pageSize = 50
	count := 0
	for offset := 0; ; offset += pageSize {
		pageParams := url.Values{
			"type":     {"page"},
			"spaceKey": {spaceKey},
			"status":   {"any"},
			"expand":   {"version,history.lastUpdated,history,space"},
			"limit":    {fmt.Sprintf("%d", pageSize)},
			"start":    {fmt.Sprintf("%d", offset)},
		}
		pageBody, err := jiraGET(ctx, client, creds, apiBase+"/wiki/rest/api/content?"+pageParams.Encode())
		if err != nil {
			fmt.Printf("  pages query failed: %v\n\n", err)
			fmt.Println()
			return
		}
		var r struct {
			Results []cfPage `json:"results"`
		}
		_ = json.Unmarshal(pageBody, &r)

		for _, pg := range r.Results {
			when, err := time.Parse(time.RFC3339, pg.Version.When)
			if err != nil {
				continue
			}
			when = when.UTC()
			if when.Before(start) || !when.Before(end) {
				continue
			}
			if pg.History.LastUpdated.By.AccountID != me.AccountID {
				continue
			}
			kind := "edited"
			if pg.History.CreatedBy.AccountID == me.AccountID {
				created, err := time.Parse(time.RFC3339, pg.History.CreatedDate)
				if err == nil && !created.UTC().Before(start) && created.UTC().Before(end) {
					kind = "created"
				}
			}
			fmt.Printf("    [%s] %s (%s)\n", pg.ID, pg.Title, kind)
			count++
		}
		if len(r.Results) < pageSize {
			break
		}
	}
	fmt.Printf("  pages: %d matching\n", count)
	fmt.Println()
}

func jiraGET(ctx context.Context, client *http.Client, creds, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if creds != "" {
		req.Header.Set("Authorization", "Basic "+creds)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func jiraPOST(ctx context.Context, client *http.Client, creds, url string, payload []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func printItems(items interface{ }) {
	// use json for a compact but readable dump
	b, _ := json.MarshalIndent(items, "  ", "  ")
	fmt.Printf("  %s\n\n", string(b))
}

func utcToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// fetchInstallationRepoFromAPI hits GET /installation/repositories with
// the per-installation token to find the first repository scoped to
// this install. We use it after the install callback to populate
// app.GitHubAppRepo automatically.
//
// The function is intentionally narrow: one HTTP round-trip, returns
// the first repo's "owner/name", silently returns "" on multi-repo or
// no-repo installs (the user can edit the field manually). Failures
// are not fatal — the linkage is already stored.
func fetchInstallationRepoFromAPI(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/installation/repositories?per_page=2", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)

	cli := &http.Client{Timeout: 10 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github: list installation repositories: status %d", resp.StatusCode)
	}
	var out struct {
		TotalCount   int `json:"total_count"`
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	// Only auto-populate when the install scopes to exactly one repo.
	// More than one is ambiguous; zero shouldn't happen in practice.
	if out.TotalCount != 1 || len(out.Repositories) != 1 {
		return "", nil
	}
	return out.Repositories[0].FullName, nil
}

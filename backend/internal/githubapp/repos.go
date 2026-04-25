package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Installation describes one /app/installations entry — i.e. somewhere
// the platform's GitHub App is installed (a user account or org). The
// account.login is what shows in GitHub's UI as the "owner".
type Installation struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"-"`
	AccountLogin string `json:"-"`
	Account      struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"` // "User" / "Organization"
	} `json:"account"`
}

// Repo is the subset of a GitHub Repository response Teal needs for
// the per-app picker — just enough to identify it (full_name) and
// disambiguate to the user (private flag, default branch).
type Repo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

// ListInstallations enumerates every installation of the platform's
// App. Two pages would be unusual for a single-tenant Teal install, so
// we don't currently follow Link headers — the per-page=100 default is
// almost always the whole answer.
func ListInstallations(ctx context.Context, doer HTTPDoer, cfg Config, now time.Time) ([]Installation, error) {
	if !cfg.Configured() {
		return nil, fmt.Errorf("githubapp: not configured")
	}
	jwt, err := MintAppJWT(cfg.AppID, cfg.PrivateKeyPEM, now)
	if err != nil {
		return nil, err
	}
	req, err := http2NewRequest(ctx, "GET",
		"https://api.github.com/app/installations?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+jwt)

	if doer == nil {
		doer = http.DefaultClient
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("githubapp: list installations: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("githubapp: list installations returned %d: %s", resp.StatusCode, string(body))
	}
	var out []Installation
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("githubapp: list installations decode: %w", err)
	}
	for i := range out {
		out[i].AccountID = out[i].Account.ID
		out[i].AccountLogin = out[i].Account.Login
	}
	return out, nil
}

// ListInstallationRepos returns every repo the given installation can
// see. Authenticates with an installation access token (NOT the App
// JWT) — GitHub's /installation/repositories endpoint requires it.
//
// Caller usually fetches the token via TokenCache so unrelated calls
// share the 1h cache window.
func ListInstallationRepos(ctx context.Context, doer HTTPDoer, token InstallationToken) ([]Repo, error) {
	if token.Token == "" {
		return nil, fmt.Errorf("githubapp: empty installation token")
	}
	req, err := http2NewRequest(ctx, "GET",
		"https://api.github.com/installation/repositories?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token.Token)

	if doer == nil {
		doer = http.DefaultClient
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("githubapp: list repos: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("githubapp: list repos returned %d: %s", resp.StatusCode, string(body))
	}
	var wrapper struct {
		Repositories []Repo `json:"repositories"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("githubapp: list repos decode: %w", err)
	}
	return wrapper.Repositories, nil
}

// FormatInstallationID is a tiny helper so callers don't have to
// import strconv just to render an int64 in a URL/log message. Keeps
// installation-ID handling in one place.
func FormatInstallationID(id int64) string {
	return strconv.FormatInt(id, 10)
}

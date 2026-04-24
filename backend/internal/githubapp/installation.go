package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Config bundles the GitHub App credentials + identity. Loaded from
// platform_settings at the call site (see ConfigFromStore in store.go).
type Config struct {
	AppID         int64
	AppSlug       string // used to build install URLs (https://github.com/apps/<slug>/installations/new)
	PrivateKeyPEM []byte
	WebhookSecret []byte
}

// Configured reports whether enough is present to mint tokens. Used by
// the engine before attempting App auth and by the API to surface
// "GitHub App not set up" errors with a useful message.
func (c Config) Configured() bool {
	return c.AppID > 0 && len(c.PrivateKeyPEM) > 0
}

// InstallationToken is one short-lived (1h) token that authenticates
// as the App's installation on a specific account/repo. Use it as a
// PAT-equivalent for git over https.
type InstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

// HTTPDoer is the subset of *http.Client TokenSource uses; tests stub
// it to skip the real network call.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// MintInstallationToken requests a fresh installation access token
// from GitHub. Caller is responsible for caching — see TokenCache.
func MintInstallationToken(ctx context.Context, http HTTPDoer, cfg Config, installationID int64, now time.Time) (InstallationToken, error) {
	if installationID <= 0 {
		return InstallationToken{}, errors.New("githubapp: installation ID required")
	}
	if !cfg.Configured() {
		return InstallationToken{}, errors.New("githubapp: not configured (missing app ID or private key)")
	}
	jwt, err := MintAppJWT(cfg.AppID, cfg.PrivateKeyPEM, now)
	if err != nil {
		return InstallationToken{}, err
	}

	url := "https://api.github.com/app/installations/" + strconv.FormatInt(installationID, 10) + "/access_tokens"
	req, err := http2NewRequest(ctx, "POST", url, nil)
	if err != nil {
		return InstallationToken{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := http.Do(req)
	if err != nil {
		return InstallationToken{}, fmt.Errorf("githubapp: request token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return InstallationToken{}, fmt.Errorf("githubapp: token request returned %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return InstallationToken{}, fmt.Errorf("githubapp: decode token response: %w", err)
	}
	if out.Token == "" {
		return InstallationToken{}, errors.New("githubapp: empty token in response")
	}
	return InstallationToken{Token: out.Token, ExpiresAt: out.ExpiresAt}, nil
}

// http2NewRequest is a thin shim so we can build a request without an
// import cycle if we later move HTTPDoer to a different package. Same
// behaviour as http.NewRequestWithContext; named non-conflictingly so
// readers see at the call site that body=nil is intentional.
func http2NewRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, url, body)
}

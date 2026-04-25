package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Manifest is the JSON document GitHub consumes when creating an App
// from a manifest. Field names match GitHub's docs verbatim — don't
// rename without checking https://docs.github.com/en/apps/sharing-
// github-apps/registering-a-github-app-from-a-manifest .
//
// Only fields Teal sets are modelled. Permissions / events live in
// nested maps because GitHub accepts arbitrary keys we don't want to
// enumerate as Go fields.
type Manifest struct {
	Name           string            `json:"name"`
	URL            string            `json:"url"`
	HookAttributes ManifestHook      `json:"hook_attributes"`
	RedirectURL    string            `json:"redirect_url"`
	Public         bool              `json:"public"`
	DefaultEvents  []string          `json:"default_events"`
	DefaultPerms   map[string]string `json:"default_permissions"`
}

// ManifestHook is the webhook delivery target the manifest declares
// for the App. Active=true is required for push events to actually
// reach Teal.
type ManifestHook struct {
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

// BuildManifest returns the canonical Teal manifest for a given
// public base URL. The base URL must already be https:// in prod;
// callers in dev can pass http:// (GitHub doesn't enforce, but
// webhook deliveries to http:// will fail in practice).
//
// Two callbacks are wired off the base URL:
//   - hook_attributes.url: /api/v1/webhooks/github-app  (push events)
//   - redirect_url:        /api/v1/settings/github-app/manifest-callback
//                          (the post-create code exchange — distinct from
//                          the per-app install setup-callback)
//
// Permissions: contents:read (clone the repo), metadata:read (list
// installations + repos for the per-app picker). Subscribed events:
// push (auto-deploy on commits to the configured branch).
func BuildManifest(publicBaseURL, name string) Manifest {
	if name == "" {
		name = "Teal"
	}
	base := strings.TrimRight(publicBaseURL, "/")
	return Manifest{
		Name: name,
		URL:  base,
		HookAttributes: ManifestHook{
			URL:    base + "/api/v1/webhooks/github-app",
			Active: true,
		},
		RedirectURL:   base + "/api/v1/settings/github-app/manifest-callback",
		Public:        false,
		DefaultEvents: []string{"push"},
		DefaultPerms: map[string]string{
			"contents": "read",
			"metadata": "read",
		},
	}
}

// ManifestCreateURL returns the github.com URL the user's browser
// must POST the manifest to. orgSlug == "" creates a user-owned App;
// non-empty creates an org-owned App (the operator must be an org
// admin or member with the right permission).
func ManifestCreateURL(orgSlug string) string {
	if orgSlug == "" {
		return "https://github.com/settings/apps/new"
	}
	return "https://github.com/organizations/" + orgSlug + "/settings/apps/new"
}

// ManifestConversion is GitHub's response to the post-create code
// exchange. It carries everything the platform needs to operate the
// App: numeric ID, slug (for install URLs), private key (for JWT
// minting), webhook secret (for delivery HMAC verification), and the
// app's html_url for the success-page link.
type ManifestConversion struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	NodeID        string `json:"node_id"`
	OwnerLogin    string `json:"-"` // populated from Owner.Login
	Name          string `json:"name"`
	HTMLURL       string `json:"html_url"`
	PEM           string `json:"pem"`
	WebhookSecret string `json:"webhook_secret"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// ExchangeManifestCode trades the temporary code GitHub redirected
// back with for the App's persistent credentials. Per spec the
// exchange POST has no body; the code is in the URL path.
//
// The endpoint is anonymous (no JWT required) — that's by design,
// the code is single-use and short-lived (roughly 10 minutes).
func ExchangeManifestCode(ctx context.Context, doer HTTPDoer, code string) (ManifestConversion, error) {
	if code == "" {
		return ManifestConversion{}, errors.New("githubapp: manifest code is empty")
	}
	url := "https://api.github.com/app-manifests/" + code + "/conversions"
	req, err := http2NewRequest(ctx, "POST", url, nil)
	if err != nil {
		return ManifestConversion{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if doer == nil {
		doer = http.DefaultClient
	}
	resp, err := doer.Do(req)
	if err != nil {
		return ManifestConversion{}, fmt.Errorf("githubapp: manifest exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ManifestConversion{}, fmt.Errorf("githubapp: manifest exchange returned %d: %s", resp.StatusCode, string(body))
	}
	var out ManifestConversion
	if err := json.Unmarshal(body, &out); err != nil {
		return ManifestConversion{}, fmt.Errorf("githubapp: manifest exchange decode: %w", err)
	}
	out.OwnerLogin = out.Owner.Login
	return out, nil
}

package githubapp

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// TokenCache memoises installation tokens by installation ID. Tokens
// last 1h; we refresh at the 50-minute mark to leave headroom for slow
// clones. Process-local — restart wipes the cache (a few extra mints
// on bounce, no security concern).
type TokenCache struct {
	httpClient HTTPDoer
	now        func() time.Time

	mu     sync.Mutex
	tokens map[int64]InstallationToken
}

// freshnessGap is the safety margin before expiry at which we mint a
// new token instead of returning the cached one. 10 minutes is enough
// for any deploy step that runs longer than expected.
const freshnessGap = 10 * time.Minute

// NewTokenCache constructs a cache backed by the given HTTP client.
// Pass nil to use http.DefaultClient.
func NewTokenCache(client HTTPDoer) *TokenCache {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &TokenCache{
		httpClient: client,
		now:        func() time.Time { return time.Now().UTC() },
		tokens:     map[int64]InstallationToken{},
	}
}

// Get returns a usable installation token, minting a new one if the
// cached value is missing or near expiry.
func (c *TokenCache) Get(ctx context.Context, cfg Config, installationID int64) (InstallationToken, error) {
	c.mu.Lock()
	cached, ok := c.tokens[installationID]
	c.mu.Unlock()
	now := c.now()
	if ok && cached.ExpiresAt.After(now.Add(freshnessGap)) {
		return cached, nil
	}
	tok, err := MintInstallationToken(ctx, c.httpClient, cfg, installationID, now)
	if err != nil {
		return InstallationToken{}, err
	}
	c.mu.Lock()
	c.tokens[installationID] = tok
	c.mu.Unlock()
	return tok, nil
}

// Invalidate drops a single installation's cached token. Used when an
// admin uninstalls the App or rotates credentials.
func (c *TokenCache) Invalidate(installationID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, installationID)
}

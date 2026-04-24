package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDoer is a stub HTTP client. Each call increments calls; the
// response body is the configured token + expiry.
type fakeDoer struct {
	calls atomic.Int64
	token string
	exp   time.Time
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	body, _ := json.Marshal(map[string]any{
		"token":      f.token,
		"expires_at": f.exp.Format(time.RFC3339),
	})
	return &http.Response{
		StatusCode: 201,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     http.Header{},
	}, nil
}

func cfg(t *testing.T) Config {
	return Config{AppID: 1, PrivateKeyPEM: newPKCS1PEM(t)}
}

func TestCacheReusesTokenWithinFreshnessWindow(t *testing.T) {
	doer := &fakeDoer{token: "ghs_abc", exp: time.Now().UTC().Add(50 * time.Minute)}
	c := NewTokenCache(doer)
	c.now = func() time.Time { return time.Now().UTC() }

	for i := 0; i < 5; i++ {
		tok, err := c.Get(context.Background(), cfg(t), 99)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if tok.Token != "ghs_abc" {
			t.Errorf("got token %q", tok.Token)
		}
	}
	if doer.calls.Load() != 1 {
		t.Errorf("HTTP calls = %d, want 1 (cache should kick in)", doer.calls.Load())
	}
}

func TestCacheRefreshesNearExpiry(t *testing.T) {
	// Token expires very soon — within the freshnessGap.
	doer := &fakeDoer{token: "ghs_xyz", exp: time.Now().UTC().Add(2 * time.Minute)}
	c := NewTokenCache(doer)
	c.now = func() time.Time { return time.Now().UTC() }

	for i := 0; i < 3; i++ {
		_, err := c.Get(context.Background(), cfg(t), 99)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if doer.calls.Load() != 3 {
		t.Errorf("HTTP calls = %d, want 3 (cache should not return near-expiry tokens)", doer.calls.Load())
	}
}

func TestCacheInvalidateForcesRefresh(t *testing.T) {
	doer := &fakeDoer{token: "ghs_a", exp: time.Now().UTC().Add(50 * time.Minute)}
	c := NewTokenCache(doer)
	c.now = func() time.Time { return time.Now().UTC() }

	_, _ = c.Get(context.Background(), cfg(t), 1)
	c.Invalidate(1)
	_, _ = c.Get(context.Background(), cfg(t), 1)

	if doer.calls.Load() != 2 {
		t.Errorf("HTTP calls = %d, want 2", doer.calls.Load())
	}
}

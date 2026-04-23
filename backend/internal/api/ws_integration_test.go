package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/crypto"
	"github.com/sariakos/teal/backend/internal/realtime"
	"github.com/sariakos/teal/backend/internal/store"
)

// TestWebSocketRoundTrip verifies that an authenticated WebSocket
// client can subscribe to a topic and receive published events. Covers
// the upgrade path, the subscribe op, the json envelope, and the
// hub→socket→client glue end-to-end.
func TestWebSocketRoundTrip(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	authn := &auth.Authenticator{
		Sessions: auth.NewSessionManager(st.Sessions, false),
		APIKeys:  auth.NewAPIKeyManager(st.APIKeys),
		Users:    st.Users,
	}
	codec, _ := crypto.NewCodec([]byte("ws-test-secret-padding-to-32!!!!!!"))
	hub := realtime.NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	deps := Deps{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:         st,
		Docker:        fakeDockerClient{},
		Authenticator: authn,
		RateLimiter:   auth.NewLoginRateLimiter(50, time.Minute),
		Codec:         codec,
		Hub:           hub,
	}
	ts := httptest.NewServer(newRouter(deps))
	defer ts.Close()

	// Bootstrap admin and capture cookies.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{
		"email":    "ws@example.com",
		"password": "correct horse battery staple",
	})
	res, err := client.Post(ts.URL+"/api/v1/register-bootstrap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("bootstrap status: %d", res.StatusCode)
	}
	res.Body.Close()

	// Build the WS URL with the auth cookie attached via a custom
	// http.Header.
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/api/v1/ws"
	u, _ := url.Parse(ts.URL)
	cookies := jar.Cookies(u)

	hdr := http.Header{}
	for _, c := range cookies {
		hdr.Add("Cookie", c.String())
	}

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer dialCancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPClient: client,
		HTTPHeader: hdr,
	})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.CloseNow()

	// Subscribe to a topic.
	subBody, _ := json.Marshal(map[string]string{"op": "subscribe", "topic": "test.topic"})
	wctx, cancelW := context.WithTimeout(context.Background(), 1*time.Second)
	if err := conn.Write(wctx, websocket.MessageText, subBody); err != nil {
		t.Fatalf("ws write: %v", err)
	}
	cancelW()

	// Give the server a beat to process the subscribe op, then publish.
	// (The server reads in a separate goroutine; ordering depends on the
	// scheduler.)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.HasSubscribers("test.topic") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !hub.HasSubscribers("test.topic") {
		t.Fatal("server never registered subscription")
	}

	hub.Publish("test.topic", map[string]string{"hello": "world"})

	// Expect to receive the publish.
	rctx, cancelR := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelR()
	_, msg, err := conn.Read(rctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var got struct {
		Topic string         `json:"topic"`
		Data  map[string]any `json:"data"`
	}
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Topic != "test.topic" {
		t.Errorf("topic = %q", got.Topic)
	}
	if got.Data["hello"] != "world" {
		t.Errorf("data = %+v", got.Data)
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// TestWebSocketRequiresAuth verifies that an unauthenticated upgrade
// is rejected with 401 — without auth we'd be exposing the realtime
// surface to any HTTP client.
func TestWebSocketRequiresAuth(t *testing.T) {
	st, _ := store.Open(context.Background(), filepath.Join(t.TempDir(), "ws.db"))
	defer st.Close()
	authn := &auth.Authenticator{
		Sessions: auth.NewSessionManager(st.Sessions, false),
		APIKeys:  auth.NewAPIKeyManager(st.APIKeys),
		Users:    st.Users,
	}
	codec, _ := crypto.NewCodec([]byte("ws-test-secret-padding-to-32!!!!!!"))
	hub := realtime.NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	deps := Deps{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:         st,
		Docker:        fakeDockerClient{},
		Authenticator: authn,
		RateLimiter:   auth.NewLoginRateLimiter(50, time.Minute),
		Codec:         codec,
		Hub:           hub,
	}
	ts := httptest.NewServer(newRouter(deps))
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/api/v1/ws"
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(dialCtx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}

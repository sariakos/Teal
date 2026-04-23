package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/sariakos/teal/backend/internal/auth"
	"github.com/sariakos/teal/backend/internal/realtime"
)

// wsHandler bridges the realtime hub to coder/websocket. One connection
// per browser tab; multiplexes subscriptions over the same socket. Auth
// is the same as any other authenticated endpoint — the cookie session
// is on the upgrade request.
type wsHandler struct {
	logger *slog.Logger
	hub    *realtime.Hub
	// allowedOrigins is the set of dev CORS origins. Empty means "same
	// origin only" (the common-and-correct case in prod).
	allowedOrigins []string
}

// wsClientMsg is the schema clients send: subscribe/unsubscribe to a
// topic. Anything else is ignored (forward compatibility).
type wsClientMsg struct {
	Op    string `json:"op"`
	Topic string `json:"topic"`
}

// wsServerMsg is the schema the server sends. Topic + arbitrary JSON
// payload. A "_meta" topic is reserved for server-initiated messages
// (drop notices, errors).
type wsServerMsg struct {
	Topic string `json:"topic"`
	Data  any    `json:"data,omitempty"`
}

// pingInterval keeps the connection alive through proxies that drop
// idle TCP. coder/websocket's Ping returns when the peer pongs.
const pingInterval = 25 * time.Second

func (h *wsHandler) handle(w http.ResponseWriter, r *http.Request) {
	subj := auth.FromContext(r.Context())
	if subj.IsZero() {
		writeError(w, http.StatusUnauthorized, "auth required")
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.originPatterns(r),
	})
	if err != nil {
		h.logger.Warn("ws: accept failed", "err", err)
		return
	}
	defer c.CloseNow()

	// One subscription per connection. Topics are added/removed via the
	// op messages — start empty.
	sub := h.hub.Subscribe(nil)
	defer h.hub.Unsubscribe(sub)

	// Reader goroutine handles client→server (subscribe/unsubscribe).
	// Writer (this goroutine) drains the subscription and pings.
	go h.readLoop(r.Context(), c, sub)

	// Writer loop: forwards hub events to the socket and sends periodic
	// pings. Exits when the subscription channel is closed (peer hung up
	// or hub Unsubscribe).
	pingT := time.NewTicker(pingInterval)
	defer pingT.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-sub.C:
			if !ok {
				return
			}
			body, err := json.Marshal(wsServerMsg{Topic: evt.Topic, Data: evt.Data})
			if err != nil {
				h.logger.Warn("ws: marshal failed", "topic", evt.Topic, "err", err)
				continue
			}
			wctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			err = c.Write(wctx, websocket.MessageText, body)
			cancel()
			if err != nil {
				return
			}
			// If the hub dropped events, surface that on a meta topic so
			// the UI can show "you missed N updates".
			if d := sub.Drops(); d > 0 {
				meta, _ := json.Marshal(wsServerMsg{
					Topic: "_meta",
					Data:  map[string]any{"drops": d, "after": evt.Topic},
				})
				wctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				_ = c.Write(wctx, websocket.MessageText, meta)
				cancel()
			}
		case <-pingT.C:
			pctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// readLoop services the client → server direction (subscribe / unsubscribe).
// Returns when the connection is closed by either end.
func (h *wsHandler) readLoop(ctx context.Context, c *websocket.Conn, sub *realtime.Subscription) {
	for {
		_, body, err := c.Read(ctx)
		if err != nil {
			// Normal close codes are not errors worth logging.
			if !isCloseExpected(err) {
				h.logger.Debug("ws: read closed", "err", err)
			}
			return
		}
		var msg wsClientMsg
		if err := json.Unmarshal(body, &msg); err != nil {
			h.logger.Debug("ws: bad client message", "err", err)
			continue
		}
		if msg.Topic == "" {
			continue
		}
		switch msg.Op {
		case "subscribe":
			h.hub.AddTopic(sub, msg.Topic)
		case "unsubscribe":
			h.hub.RemoveTopic(sub, msg.Topic)
		}
	}
}

// originPatterns returns the patterns coder/websocket should accept. In
// prod (no DevCORSOrigins) we trust the same-origin default. In dev we
// allow the SvelteKit dev server.
func (h *wsHandler) originPatterns(r *http.Request) []string {
	if len(h.allowedOrigins) == 0 {
		return nil // coder/websocket defaults to Host-only same-origin
	}
	out := make([]string, 0, len(h.allowedOrigins))
	for _, o := range h.allowedOrigins {
		// Accept patterns are host:port, no scheme. Strip the scheme.
		o = strings.TrimPrefix(o, "https://")
		o = strings.TrimPrefix(o, "http://")
		out = append(out, o)
	}
	return out
}

// isCloseExpected reports whether the error is a normal/going-away close
// code (i.e. not worth logging at warn).
func isCloseExpected(err error) bool {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		switch ce.Code {
		case websocket.StatusNormalClosure, websocket.StatusGoingAway:
			return true
		}
	}
	return false
}

// Package logging configures the structured slog logger used across Teal and
// provides helpers for propagating a per-request ID through context.
//
// What it does:
//   - Builds an *slog.Logger configured for either human-readable text (dev)
//     or JSON (prod) output at a chosen level.
//   - Injects/retrieves a request ID from context.Context so handlers and
//     downstream calls can share the same correlation token without touching
//     net/http types directly.
//
// What it does NOT do:
//   - Emit access logs. That's the API middleware's job (it uses the logger
//     this package returns).
//   - Ship logs anywhere. Output is stdout; aggregation is the operator's
//     responsibility (docker logs, journald, etc.).
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// New constructs a logger for the given format ("text"|"json") and level
// ("debug"|"info"|"warn"|"error"). Invalid values fall back to text/info
// rather than panicking — config validation already rejects bad values, so
// reaching the fallback here is genuinely unexpected and not worth crashing
// over.
func New(format, level string) *slog.Logger {
	return NewWithWriter(os.Stdout, format, level)
}

// NewWithWriter is like New but writes to an arbitrary io.Writer. Used by
// tests to capture log output.
func NewWithWriter(w io.Writer, format, level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	var h slog.Handler
	if strings.ToLower(format) == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// requestIDKey is the context key under which the request ID is stored. It is
// an unexported type to prevent collisions with other packages that might use
// strings as keys.
type requestIDKey struct{}

// WithRequestID returns a new context carrying the given request ID. The API
// middleware calls this once per request; handlers and downstream packages
// retrieve the value via RequestIDFromContext.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext returns the request ID associated with ctx, or the
// empty string if none was set. Callers should treat empty as "no correlation
// available" rather than an error.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// LoggerWithRequestID returns base annotated with the request_id field from
// ctx, if one is present. Use this in handlers to ensure all log lines for a
// request carry the same correlation token.
func LoggerWithRequestID(ctx context.Context, base *slog.Logger) *slog.Logger {
	if id := RequestIDFromContext(ctx); id != "" {
		return base.With("request_id", id)
	}
	return base
}

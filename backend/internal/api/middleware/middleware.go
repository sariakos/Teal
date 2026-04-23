// Package middleware holds the standard HTTP middleware stack used by the
// Teal API: panic recovery, request-id propagation, and access logging.
//
// Each middleware is a single small function. They all share the
// `func(http.Handler) http.Handler` shape so they can be composed by chi
// (or any other router) in any order.
package middleware

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/sariakos/teal/backend/internal/logging"
)

// requestIDHeader is the inbound and outbound header used to carry the
// correlation ID. We accept an existing one (so reverse proxies can supply
// it) and always emit one back so clients can quote it in bug reports.
const requestIDHeader = "X-Request-ID"

// Recover converts any panic in a downstream handler into a 500 response and
// logs the stack trace. Without this, a panic would tear down the request
// and leave the client with a dangling connection.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logging.LoggerWithRequestID(r.Context(), logger).Error(
						"panic recovered",
						"panic", rec,
						"stack", string(debug.Stack()),
						"path", r.URL.Path,
						"method", r.Method,
					)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID assigns each request a hex correlation ID (or reuses one from
// the inbound X-Request-ID header) and stores it on the context. The same ID
// is echoed back in the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := logging.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AccessLog logs one structured line per completed request, including the
// status code, byte count, and duration. The status code is captured by a
// thin response writer wrapper so we don't need handlers to cooperate.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rw, r)

			logging.LoggerWithRequestID(r.Context(), logger).Info(
				"http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"bytes", rw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
			)
		})
	}
}

// JSONResponse forces the Content-Type header on every response. Individual
// handlers can override it before writing the body if they need something
// else (file downloads, etc.).
func JSONResponse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	var b [8]byte
	// crypto/rand.Read is documented to never fail on supported platforms.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.wrote = true
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Hijack delegates to the underlying ResponseWriter when it supports
// hijacking (the WS upgrade path needs this). Returning an explicit
// "not supported" error if it doesn't would mask real bugs — let the
// caller see the panic from the type assertion.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return s.ResponseWriter.(http.Hijacker).Hijack()
}

// Flush + Push pass through too so SSE / HTTP/2-specific code keeps
// working through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

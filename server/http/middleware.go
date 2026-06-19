package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// statusRecorder wraps an http.ResponseWriter to remember the status code that was
// written, which the bare interface does not expose. RequestLogger uses it to log
// the response status. It embeds the underlying writer so all other methods (e.g.
// Flush, Push) pass straight through.
type statusRecorder struct {
	http.ResponseWriter
	status int  // the captured status code (StatusOK if the handler only Write's)
	wrote  bool // whether the status has been latched yet
}

// WriteHeader latches the first status code written and forwards it. Later calls
// (which net/http already warns about) do not overwrite the recorded value.
func (sr *statusRecorder) WriteHeader(code int) {
	if !sr.wrote {
		sr.status = code
		sr.wrote = true
	}
	sr.ResponseWriter.WriteHeader(code)
}

// Write forwards the body and, on the first call, latches an implicit 200 status
// to mirror net/http's behaviour when a handler writes without calling WriteHeader.
func (sr *statusRecorder) Write(b []byte) (int, error) {
	if !sr.wrote {
		sr.status = http.StatusOK
		sr.wrote = true
	}
	return sr.ResponseWriter.Write(b)
}

// Recover converts a panic in a downstream handler into a 500 JSON error and
// logs it at Error severity. A nil logger is treated as a no-op.
func Recover(l logger.Logger) Middleware {
	if l == nil {
		l = logger.Nop{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					l.Log(logger.SeverityError, map[string]any{
						"event": "panic", "recover": rec,
						"method": r.Method, "path": r.URL.Path,
					})
					Error(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger logs one structured line per request (method, path, status,
// duration) at Info severity. A nil logger is treated as a no-op.
func RequestLogger(l logger.Logger) Middleware {
	if l == nil {
		l = logger.Nop{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(sr, r)
			l.Log(logger.SeverityInfo, map[string]any{
				"event": "request", "method": r.Method, "path": r.URL.Path,
				"status": sr.status, "dur_ms": time.Since(start).Milliseconds(),
			})
		})
	}
}

// ridKey is the context key for the request ID.
type ridKey struct{}

// RequestID ensures every request carries an X-Request-ID: it honours an inbound
// header or generates one, echoes it on the response, and stashes it in context
// (retrieve with RequestIDFrom).
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = genID()
			}
			w.Header().Set("X-Request-ID", id)
			ctx := context.WithValue(r.Context(), ridKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFrom returns the request ID stored by RequestID, or "" if absent.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(ridKey{}).(string); ok {
		return id
	}
	return ""
}

// Auth gates requests through verify. A nil error passes; a *StatusError maps to
// its status/message; any other error becomes 401. Compose with When for
// conditional auth (e.g. only on a /admin group).
func Auth(verify func(*http.Request) error) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := verify(r); err != nil {
				var se *StatusError
				if errors.As(err, &se) {
					Error(w, se.Status, se.Message)
					return
				}
				Error(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// genID returns a short random hex token for request IDs.
func genID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should not fail; fall back to a fixed marker rather than panic.
		return "req-unknown"
	}
	return hex.EncodeToString(b[:])
}

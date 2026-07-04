package httpserver

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// statusRecorder wraps an http.ResponseWriter to remember the status code that was
// written, which the bare interface does not expose. RequestLogger uses it to log
// the response status.
//
// Embedding the ResponseWriter interface only promotes the interface's own three
// methods — it does NOT make the wrapper satisfy the writer's optional upgrade
// interfaces (http.Hijacker, http.Flusher, http.Pusher, io.ReaderFrom), which
// callers discover by type assertion. Always wrap through wrapRecorder, which
// selects a variant exposing exactly the interfaces the underlying writer
// supports — otherwise WebSocket upgrades (Hijacker) and SSE (Flusher) silently
// break behind the middleware.
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

// wrapRecorder wraps w for status recording and returns both the recorder (for
// reading the captured status afterwards) and the http.ResponseWriter to hand
// downstream. The returned writer exposes exactly the optional interfaces
// (http.Flusher, http.Hijacker, http.Pusher, io.ReaderFrom) that w itself
// supports, so feature detection by type assertion keeps working through the
// wrapper. The variant set covers the real net/http writer shapes: HTTP/1.1
// (Flusher+Hijacker+ReaderFrom), HTTP/2 (Flusher+Pusher), and test recorders
// (Flusher or nothing).
func wrapRecorder(w http.ResponseWriter) (*statusRecorder, http.ResponseWriter) {
	sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	_, fl := w.(http.Flusher)
	_, hj := w.(http.Hijacker)
	_, ps := w.(http.Pusher)
	_, rf := w.(io.ReaderFrom)
	switch {
	case fl && hj && rf: // HTTP/1.1 server writer
		return sr, &flushHijackReadFromRecorder{sr}
	case fl && hj:
		return sr, &flushHijackRecorder{sr}
	case fl && ps: // HTTP/2 server writer
		return sr, &flushPushRecorder{sr}
	case fl:
		return sr, &flushRecorder{sr}
	case hj:
		return sr, &hijackRecorder{sr}
	default:
		return sr, sr
	}
}

// The forwarding methods below assert on the recorder's underlying writer; the
// assertions cannot fail because wrapRecorder only selects a variant after
// probing the writer for that interface.

func (sr *statusRecorder) flush() { sr.ResponseWriter.(http.Flusher).Flush() }

func (sr *statusRecorder) hijack() (net.Conn, *bufio.ReadWriter, error) {
	return sr.ResponseWriter.(http.Hijacker).Hijack()
}

func (sr *statusRecorder) push(target string, opts *http.PushOptions) error {
	return sr.ResponseWriter.(http.Pusher).Push(target, opts)
}

// readFrom latches the implicit 200 (net/http writes headers on first body
// bytes) and delegates to the writer's sendfile fast path.
func (sr *statusRecorder) readFrom(r io.Reader) (int64, error) {
	if !sr.wrote {
		sr.status = http.StatusOK
		sr.wrote = true
	}
	return sr.ResponseWriter.(io.ReaderFrom).ReadFrom(r)
}

type flushRecorder struct{ *statusRecorder }

func (v *flushRecorder) Flush() { v.flush() }

type hijackRecorder struct{ *statusRecorder }

func (v *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return v.hijack() }

type flushHijackRecorder struct{ *statusRecorder }

func (v *flushHijackRecorder) Flush()                                       { v.flush() }
func (v *flushHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return v.hijack() }

type flushHijackReadFromRecorder struct{ *statusRecorder }

func (v *flushHijackReadFromRecorder) Flush() { v.flush() }
func (v *flushHijackReadFromRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return v.hijack()
}
func (v *flushHijackReadFromRecorder) ReadFrom(r io.Reader) (int64, error) { return v.readFrom(r) }

type flushPushRecorder struct{ *statusRecorder }

func (v *flushPushRecorder) Flush()                                   { v.flush() }
func (v *flushPushRecorder) Push(t string, o *http.PushOptions) error { return v.push(t, o) }

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
			sr, ww := wrapRecorder(w)
			start := time.Now()
			next.ServeHTTP(ww, r)
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

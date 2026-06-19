package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/logger"
)

// capLogger records the severities it was asked to log (concurrency-safe).
type capLogger struct {
	mu  sync.Mutex
	got []logger.Severity
}

func (c *capLogger) Log(s logger.Severity, _ any) {
	c.mu.Lock()
	c.got = append(c.got, s)
	c.mu.Unlock()
}

func (c *capLogger) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func TestRecover(t *testing.T) {
	t.Parallel()
	log := &capLogger{}
	h := Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code=%d, want 500", rec.Code)
	}
	if log.count() == 0 {
		t.Error("panic should have been logged")
	}
}

func TestAuth(t *testing.T) {
	t.Parallel()
	ok := func(*http.Request) error { return nil }
	deny := func(*http.Request) error { return Status(http.StatusForbidden, "nope") }
	boom := func(*http.Request) error { return context.Canceled } // arbitrary non-StatusError

	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = JSON(w, 200, "ok") })

	cases := []struct {
		name   string
		verify func(*http.Request) error
		want   int
	}{
		{"pass", ok, http.StatusOK},
		{"statuserror", deny, http.StatusForbidden},
		{"other-error-401", boom, http.StatusUnauthorized},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		Auth(c.verify)(final).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		if rec.Code != c.want {
			t.Errorf("%s: code=%d want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestRequestID(t *testing.T) {
	t.Parallel()

	// generates one when absent, echoes on response, and puts it in context.
	var seen string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
		w.WriteHeader(204)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if seen == "" {
		t.Error("request id not in context")
	}
	if rec.Header().Get("X-Request-ID") != seen {
		t.Errorf("response header %q != context %q", rec.Header().Get("X-Request-ID"), seen)
	}

	// honours an inbound id.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "abc123")
	h.ServeHTTP(rec, req)
	if seen != "abc123" {
		t.Errorf("inbound id not honoured: %q", seen)
	}
}

func TestRequestLogger(t *testing.T) {
	t.Parallel()
	log := &capLogger{}
	h := RequestLogger(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = JSON(w, http.StatusTeapot, "x")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if log.count() != 1 {
		t.Errorf("expected 1 log line, got %d", log.count())
	}
}

package httpserver

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

// --- statusRecorder optional-interface forwarding ---------------------------

// fake writers for the interface-parity matrix.
type baseRW struct{ header http.Header }

func newBaseRW() *baseRW                      { return &baseRW{header: http.Header{}} }
func (f *baseRW) Header() http.Header         { return f.header }
func (f *baseRW) Write(b []byte) (int, error) { return len(b), nil }
func (f *baseRW) WriteHeader(int)             {}

type flusherRW struct {
	*baseRW
	flushed bool
}

func (f *flusherRW) Flush() { f.flushed = true }

type hijackerRW struct{ *baseRW }

func (f *hijackerRW) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("fake hijack called")
}

type pusherRW struct {
	*baseRW
	flushed bool
	pushed  string
}

func (f *pusherRW) Flush()                                   { f.flushed = true }
func (f *pusherRW) Push(t string, _ *http.PushOptions) error { f.pushed = t; return nil }

type readFromRW struct {
	*baseRW
	flushed  bool
	readFrom bool
}

func (f *readFromRW) Flush() { f.flushed = true }
func (f *readFromRW) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("fake hijack called")
}
func (f *readFromRW) ReadFrom(r io.Reader) (int64, error) {
	f.readFrom = true
	return io.Copy(io.Discard, r)
}

func TestWrapRecorder_InterfaceParity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		w              http.ResponseWriter
		fl, hj, ps, rf bool
	}{
		{"bare", newBaseRW(), false, false, false, false},
		{"flusher", &flusherRW{baseRW: newBaseRW()}, true, false, false, false},
		{"hijacker", &hijackerRW{baseRW: newBaseRW()}, false, true, false, false},
		{"http2 flush+push", &pusherRW{baseRW: newBaseRW()}, true, false, true, false},
		{"http1 flush+hijack+readfrom", &readFromRW{baseRW: newBaseRW()}, true, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ww := wrapRecorder(tc.w)
			if _, ok := ww.(http.Flusher); ok != tc.fl {
				t.Errorf("Flusher: got %v, want %v", ok, tc.fl)
			}
			if _, ok := ww.(http.Hijacker); ok != tc.hj {
				t.Errorf("Hijacker: got %v, want %v", ok, tc.hj)
			}
			if _, ok := ww.(http.Pusher); ok != tc.ps {
				t.Errorf("Pusher: got %v, want %v", ok, tc.ps)
			}
			if _, ok := ww.(io.ReaderFrom); ok != tc.rf {
				t.Errorf("ReaderFrom: got %v, want %v", ok, tc.rf)
			}
		})
	}
}

func TestWrapRecorder_Delegation(t *testing.T) {
	t.Parallel()

	// Flush delegates.
	fw := &flusherRW{baseRW: newBaseRW()}
	_, ww := wrapRecorder(fw)
	ww.(http.Flusher).Flush()
	if !fw.flushed {
		t.Fatal("Flush did not reach the underlying writer")
	}

	// Push delegates.
	pw := &pusherRW{baseRW: newBaseRW()}
	_, ww = wrapRecorder(pw)
	if err := ww.(http.Pusher).Push("/style.css", nil); err != nil || pw.pushed != "/style.css" {
		t.Fatalf("Push did not reach the underlying writer (err %v, pushed %q)", err, pw.pushed)
	}

	// Hijack delegates (fake returns a marker error).
	hw := &hijackerRW{baseRW: newBaseRW()}
	_, ww = wrapRecorder(hw)
	if _, _, err := ww.(http.Hijacker).Hijack(); err == nil || err.Error() != "fake hijack called" {
		t.Fatalf("Hijack did not reach the underlying writer: %v", err)
	}

	// ReadFrom delegates and latches the implicit 200.
	rw := &readFromRW{baseRW: newBaseRW()}
	sr, ww2 := wrapRecorder(rw)
	if _, err := ww2.(io.ReaderFrom).ReadFrom(strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if !rw.readFrom {
		t.Fatal("ReadFrom did not reach the underlying writer")
	}
	if sr.status != http.StatusOK || !sr.wrote {
		t.Fatalf("ReadFrom must latch implicit 200, got status %d wrote %v", sr.status, sr.wrote)
	}
}

func TestRequestLogger_HijackWebsocketStyle(t *testing.T) {
	t.Parallel()
	// A WebSocket-style upgrade behind RequestLogger: the handler must be able
	// to assert http.Hijacker through the wrapped writer (this failed before
	// wrapRecorder existed) and speak raw bytes on the connection.
	handler := RequestLogger(&capLogger{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijacker through middleware", http.StatusInternalServerError)
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: raw\r\nConnection: Upgrade\r\n\r\nhello")
		rw.Flush()
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	got := string(buf[:n])
	if !strings.Contains(got, "101 Switching Protocols") {
		t.Fatalf("hijacked upgrade failed through RequestLogger, got: %q", got)
	}
}

func TestRequestLogger_FlusherSSEStyle(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder() // implements http.Flusher
	handler := RequestLogger(&capLogger{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Error("no flusher through middleware")
			return
		}
		_, _ = w.Write([]byte("event: ping\n\n"))
		fl.Flush()
	}))
	req := httptest.NewRequest("GET", "/events", nil)
	handler.ServeHTTP(rec, req)
	if !rec.Flushed {
		t.Fatal("Flush did not propagate to the underlying recorder")
	}
}

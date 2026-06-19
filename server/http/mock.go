package httpserver

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

// RecordedRequest is a snapshot of a request the MockServer served.
type RecordedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// MockServer is a loopback test double: a real net/http server (via httptest) in
// front of the same routing core, plus stub registration and request recording.
// Route mutation and recording are mutex-guarded so concurrent client requests
// are race-free. Always Close it (t.Cleanup).
type MockServer struct {
	s  *Server
	ts *httptest.Server

	routesMu sync.RWMutex // guards router mutation vs. dispatch reads
	recMu    sync.Mutex   // guards the recorded slice
	recorded []RecordedRequest

	maxRecordBody int64
}

// NewMock builds a MockServer. Options configure the underlying Server (logger,
// middleware, custom 404/405) exactly like New.
func NewMock(opts ...Option) *MockServer {
	m := &MockServer{s: New(opts...), maxRecordBody: DefaultMaxBodyBytes}
	m.ts = httptest.NewServer(m.recordingHandler())
	return m
}

func (m *MockServer) recordingHandler() http.Handler {
	base := m.s.handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(io.LimitReader(r.Body, m.maxRecordBody))
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		m.recMu.Lock()
		m.recorded = append(m.recorded, RecordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Header: r.Header.Clone(),
			Body:   body,
		})
		m.recMu.Unlock()

		m.routesMu.RLock()
		defer m.routesMu.RUnlock()
		base.ServeHTTP(w, r)
	})
}

// URL is the base URL clients should hit (e.g. http://127.0.0.1:PORT).
func (m *MockServer) URL() string { return m.ts.URL }

// Client returns an *http.Client wired to the mock.
func (m *MockServer) Client() *http.Client { return m.ts.Client() }

// Close shuts the loopback server down.
func (m *MockServer) Close() { m.ts.Close() }

// register is the mutex-guarded mock equivalent of Group.register (panics on a
// bad/duplicate pattern, matching the real server's startup policy).
func (m *MockServer) register(method, pattern string, h http.Handler) *MockServer {
	m.routesMu.Lock()
	defer m.routesMu.Unlock()
	if err := m.s.router.Handle(method, pattern, h); err != nil {
		panic("httpserver: mock route registration failed for " + method + " " + pattern + ": " + err.Error())
	}
	return m
}

// Handle registers h for a "METHOD /path" pattern. Chainable.
func (m *MockServer) Handle(pattern string, h http.Handler) *MockServer {
	method, path := splitMethodPattern(pattern)
	return m.register(method, path, h)
}

func (m *MockServer) Get(pattern string, h http.HandlerFunc) *MockServer {
	return m.register(http.MethodGet, pattern, h)
}
func (m *MockServer) Post(pattern string, h http.HandlerFunc) *MockServer {
	return m.register(http.MethodPost, pattern, h)
}
func (m *MockServer) Put(pattern string, h http.HandlerFunc) *MockServer {
	return m.register(http.MethodPut, pattern, h)
}
func (m *MockServer) Delete(pattern string, h http.HandlerFunc) *MockServer {
	return m.register(http.MethodDelete, pattern, h)
}

// Stub registers a canned JSON response (status + body) for a method+pattern.
// Chainable: mock.Stub("GET", "/u/{id}", 200, user).Stub(...).
func (m *MockServer) Stub(method, pattern string, status int, body any) *MockServer {
	return m.register(method, pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = JSON(w, status, body)
	}))
}

// Recorded returns a snapshot copy of the requests served so far.
func (m *MockServer) Recorded() []RecordedRequest {
	m.recMu.Lock()
	defer m.recMu.Unlock()
	out := make([]RecordedRequest, len(m.recorded))
	copy(out, m.recorded)
	return out
}

// Reset clears recorded requests (keeps registered routes).
func (m *MockServer) Reset() {
	m.recMu.Lock()
	m.recorded = nil
	m.recMu.Unlock()
}

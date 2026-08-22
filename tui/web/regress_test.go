package web

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/tui"
)

// Regressions for lector r1. Each one reproduces a probe that failed.

// #7: Stop closed the events channel while Submit could pass the done check and
// then send into it — a data race, then a send on a closed channel.
func TestRegress_StopDoesNotRaceSubmit(t *testing.T) {
	t.Parallel()
	for range 20 {
		b := started(t, hello(), EventQueue(1))
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 200 {
					// Any error is fine; a panic or a race is not.
					_ = b.Submit(tui.KeyEvent{Code: 'x'})
				}
			}()
		}
		wg.Add(1)
		go func() { defer wg.Done(); _ = b.Stop() }()
		wg.Wait()
	}
}

// #6: any positive size was accepted and allocated directly, so a huge or
// overflowing product panicked in makeslice or exhausted memory.
func TestRegress_GridSizeIsBounded(t *testing.T) {
	t.Parallel()

	hostile := []struct{ cols, rows int }{
		{math.MaxInt, 2},
		{2, math.MaxInt},
		{math.MaxInt, math.MaxInt},
		// The product overflows int and comes out small and positive, which is
		// exactly how a naive bounds check gets passed by the value meant to
		// trip it.
		{1 << 31, 1 << 33},
		{MaxCols + 1, 1},
		{1, MaxRows + 1},
		{MaxCols, MaxRows}, // legal individually, over the cell cap together
		{0, 5},
		{-1, -1},
	}
	for _, c := range hostile {
		if err := validGrid(c.cols, c.rows); err == nil {
			t.Errorf("validGrid(%d, %d) accepted a grid this server will not allocate", c.cols, c.rows)
		}
	}
	// A realistic large terminal is still fine.
	for _, c := range []struct{ cols, rows int }{{80, 24}, {400, 100}, {1000, 200}} {
		if err := validGrid(c.cols, c.rows); err != nil {
			t.Errorf("validGrid(%d, %d) refused a realistic size: %v", c.cols, c.rows, err)
		}
	}

	// Through the public surface, which is where the panic came from.
	b := started(t, hello())
	if err := b.Resize(math.MaxInt, 2); !errors.Is(err, ErrGridTooLarge) {
		t.Errorf("Resize(MaxInt, 2) = %v, want ErrGridTooLarge", err)
	}
	if got, _ := b.Size(); got != (tui.Size{W: 20, H: 5}) {
		t.Errorf("a refused resize changed the size to %+v", got)
	}
	if !(Hello{Cols: math.MaxInt, Rows: 2, Metrics: Metrics{CellW: 8, CellH: 16}}).valid() == false {
		t.Error("an unbounded hello must be invalid")
	}
}

// #5a: a resize mutated the grid and then ignored a failed Submit, so the App
// kept laying out for a size the backend no longer had.
func TestRegress_ResizeDoesNotDesyncWhenTheAppIsBusy(t *testing.T) {
	t.Parallel()
	b := started(t, hello(), EventQueue(1))
	// Fill the queue so the resize event cannot be delivered.
	if err := b.Submit(tui.KeyEvent{Code: 'a'}); err != nil {
		t.Fatal(err)
	}
	before, _ := b.Size()

	if err := b.Resize(40, 10); !errors.Is(err, ErrEventOverflow) {
		t.Fatalf("Resize = %v, want ErrEventOverflow", err)
	}
	after, _ := b.Size()
	if after != before {
		t.Errorf("the grid changed to %+v while the App still believes %+v — the two "+
			"now disagree with no mechanism to notice", after, before)
	}
}

// #5b: Limits.QueueDepth was documented as 1024 while the Backend defaulted to
// 256 and nothing read the field, so the documented limit and the real one were
// different numbers.
func TestRegress_QueueDepthIsTheRealCapacity(t *testing.T) {
	t.Parallel()
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	const depth = 7
	if _, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: testPolicy(t),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, WithLimits(Limits{QueueDepth: depth})); err != nil {
		t.Fatal(err)
	}
	s, err := m.Create(context.Background(), &auth.Identity{Subject: "alice"}, hello())
	if err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for range depth * 4 {
		if err := s.Backend().Submit(tui.KeyEvent{Code: 'x'}); err != nil {
			break
		}
		accepted++
	}
	if accepted != depth {
		t.Errorf("the queue accepted %d events, want the configured %d", accepted, depth)
	}
}

// #5c: on overflow the read pump retried once and then advanced past the event,
// losing input while still claiming an ordered un-coalesced stream.
func TestRegress_DeliverRetriesUntilAccepted(t *testing.T) {
	t.Parallel()
	l, m := loopFor(t, testPolicy(t), nil, BackendOptions(EventQueue(1)))
	l.limits = DefaultLimits().normalize()
	limiter := newBucket(1e9, 1e9, nil)

	s, err := m.Create(context.Background(), &auth.Identity{Subject: "alice"}, hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), &auth.Identity{Subject: "alice"}, hello()); err != nil {
		t.Fatal(err)
	}
	backend := s.Backend()

	// One slot, two events: the second can only land once the App drains.
	if err := backend.Submit(tui.KeyEvent{Code: 'x'}); err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		<-backend.Events() // make room
		close(drained)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.deliver(ctx, &fakeConn{}, s, limiter, tui.KeyEvent{Code: 'y'}); err != nil {
		t.Fatalf("deliver = %v, want the event to be waited for rather than dropped", err)
	}
	<-drained
	select {
	case ev := <-backend.Events():
		if k, ok := ev.(tui.KeyEvent); !ok || k.Code != 'y' {
			t.Errorf("event = %#v, want the retried 'y'", ev)
		}
	default:
		t.Error("the event was dropped rather than retried — the stream is not un-coalesced")
	}
}

// #4a: the App's context came from the WebSocket, so a disconnect cancelled it
// immediately and the detach window of §2.8 was unreachable.
func TestRegress_SessionOutlivesTheConnectionContext(t *testing.T) {
	t.Parallel()
	m, _ := manager(t, IdleTimeout(time.Hour))

	connCtx, disconnect := context.WithCancel(context.Background())
	s, err := m.Create(connCtx, &auth.Identity{Subject: "alice"}, hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), &auth.Identity{Subject: "alice"}, hello()); err != nil {
		t.Fatal(err)
	}

	// The browser goes away.
	disconnect()
	m.Detach(s.ID(), s.Lease())

	select {
	case <-s.Done():
		t.Fatal("the session died with its connection — the detach window is unreachable, " +
			"so a dropped socket destroys the user's work")
	case <-time.After(150 * time.Millisecond):
	}
	if m.Len() != 1 {
		t.Errorf("%d sessions, want the detached one still alive", m.Len())
	}
	// And a reconnect works.
	if _, err := m.Attach(s.ID(), &auth.Identity{Subject: "alice"}, hello()); err != nil {
		t.Errorf("reconnect refused: %v", err)
	}
}

// #4b: a second attach to a LIVE session was silently accepted, so two browsers
// shared one grid, cursor and event stream.
func TestRegress_ConcurrentTakeoverIsRefused(t *testing.T) {
	t.Parallel()
	m, _ := manager(t)
	id := &auth.Identity{Subject: "alice"}
	s, err := m.Create(context.Background(), id, hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), id, hello()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), id, hello()); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("second live attach = %v, want ErrSessionBusy", err)
	}
	// After the first connection goes, a reconnect is allowed.
	m.Detach(s.ID(), s.Lease())
	if _, err := m.Attach(s.ID(), id, hello()); err != nil {
		t.Errorf("reconnect after detach refused: %v", err)
	}
}

// #4c: Detach was not lease-scoped, so a slow teardown could mark a session
// unattached while a newer connection was in fact live — and the sweep would then
// evict a session somebody is looking at.
func TestRegress_StaleDetachDoesNotUnattachTheLiveConnection(t *testing.T) {
	t.Parallel()
	m, _ := manager(t)
	id := &auth.Identity{Subject: "alice"}
	s, err := m.Create(context.Background(), id, hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), id, hello()); err != nil {
		t.Fatal(err)
	}
	stale := s.Lease()
	m.Detach(s.ID(), stale)
	if _, err := m.Attach(s.ID(), id, hello()); err != nil {
		t.Fatal(err)
	}
	live := s.Lease()
	if live == stale {
		t.Fatal("the lease did not advance on reattach")
	}

	// The old connection's deferred cleanup finally runs.
	m.Detach(s.ID(), stale)

	s.mu.Lock()
	attached := s.attached
	s.mu.Unlock()
	if !attached {
		t.Error("a stale detach unattached the live connection, so the sweep would " +
			"evict a session somebody is looking at")
	}
}

// #4d: nothing ever called Evict, so a detached session on an idle server lived
// forever holding its App, grid and goroutines.
func TestRegress_EvictionIsScheduled(t *testing.T) {
	t.Parallel()
	m, _ := manager(t, IdleTimeout(time.Second))
	stop := m.Start()
	defer stop()

	id := &auth.Identity{Subject: "alice"}
	s, err := m.Create(context.Background(), id, hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), id, hello()); err != nil {
		t.Fatal(err)
	}
	m.Detach(s.ID(), s.Lease())

	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("no scheduler evicted an idle session — nothing but a Create ever swept")
	}
	// Idempotent.
	stop()
	stop()
}

// #2: authRequest omitted r.TLS, so a verified client-certificate chain reached
// auth/mtls as nil and it always returned ErrNoVerifiedChain.
func TestRegress_TLSStateReachesTheFactors(t *testing.T) {
	t.Parallel()
	leaf := &x509.Certificate{
		Subject:     pkixName("alice"),
		NotAfter:    time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	r := httptest.NewRequest(http.MethodGet, "/attach", nil)
	r.RemoteAddr = "203.0.113.7:1"
	r.TLS = &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{leaf}},
	}

	req := authRequest(r, clientMessage{T: msgHello})
	if req.TLS == nil {
		t.Fatal("TLS state was dropped, so auth/mtls can never succeed")
	}
	if len(req.TLS.VerifiedChains) != 1 || len(req.TLS.VerifiedChains[0]) != 1 {
		t.Fatalf("verified chain not projected: %+v", req.TLS)
	}
	if got := req.TLS.VerifiedChains[0][0].CommonName; got != "alice" {
		t.Errorf("CommonName = %q, want alice", got)
	}
	// A plaintext request carries no TLS state, rather than an empty one that
	// looks like a failed verification.
	plain := httptest.NewRequest(http.MethodGet, "/attach", nil)
	if authRequest(plain, clientMessage{T: msgHello}).TLS != nil {
		t.Error("a plaintext request produced TLS state")
	}
}

// #9: the audit record concatenated an authenticated Subject raw, and a
// newline-bearing principal forged a second log line. "Authenticated" only means
// a factor vouched for it — an allowed_signers principal or a certificate CN is
// not constrained to be newline-free.
func TestRegress_AuditFieldsAreSanitized(t *testing.T) {
	t.Parallel()
	got := sessionAudit{
		Kind:    "attached",
		Subject: "alice\nweb session attached subject=root",
		ID:      "abcdefgh\nforged",
		Reason:  "x\ny",
	}.String()
	if idx := indexByte(got, '\n'); idx >= 0 {
		t.Fatalf("a newline survived at %d: %q", idx, got)
	}
	if !contains(got, "?web session") {
		t.Errorf("the newline was not replaced: %q", got)
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// #1: Origin and Host were judged after the upgrade. The Guard wrapper decides
// while a plain 403 is still possible, so the WS handler is never reached.
func TestRegress_HandshakeGuardRunsBeforeTheUpgrade(t *testing.T) {
	t.Parallel()
	h := handlerFor(t)
	reached := false
	guarded := h.Guard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	for name, setup := range map[string]func(*http.Request){
		"cross origin":  func(r *http.Request) { r.Host = "tui.example.test"; r.Header.Set("Origin", "https://attacker.test") },
		"absent origin": func(r *http.Request) { r.Host = "tui.example.test" },
		"wrong host":    func(r *http.Request) { r.Host = "attacker.test"; r.Header.Set("Origin", "https://tui.example.test") },
	} {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		setup(r)
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, r)
		if reached {
			t.Fatalf("%s: the upgrade handler was reached", name)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: code = %d, want 403", name, rec.Code)
		}
		// The hardening headers apply to a refusal too: an attacker choosing the
		// path must not choose the headers.
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no CSP on the refusal", name)
		}
	}

	// A conforming handshake passes through.
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Host = "tui.example.test"
	r.Header.Set("Origin", "https://tui.example.test")
	guarded.ServeHTTP(httptest.NewRecorder(), r)
	if !reached {
		t.Error("a conforming handshake was refused")
	}
}

// #1b: mutating the caller's slice after NewHandler changed the live allowlist.
func TestRegress_AllowedOriginsIsCloned(t *testing.T) {
	t.Parallel()
	origins := []string{"https://tui.example.test"}
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: testPolicy(t),
		AllowedOrigins: origins, ExpectedHost: "tui.example.test",
	}, m)
	if err != nil {
		t.Fatal(err)
	}
	origins[0] = "https://attacker.test"

	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Host = "tui.example.test"
	r.Header.Set("Origin", "https://attacker.test")
	if err := h.cfg.checkHandshake(r); err == nil {
		t.Error("mutating the caller's slice changed the live allowlist")
	}
	if h.AllowedOrigins()[0] != "https://tui.example.test" {
		t.Errorf("allowlist = %v", h.AllowedOrigins())
	}
	// The accessor must also hand back a copy.
	got := h.AllowedOrigins()
	got[0] = "https://attacker.test"
	if h.AllowedOrigins()[0] == "https://attacker.test" {
		t.Error("AllowedOrigins returns the live slice")
	}
}

// #8: the reserved table was hard-coded in the client as well, despite the
// client's comment saying it was injected.
func TestRegress_ReservedTableIsInjectedNotDuplicated(t *testing.T) {
	t.Parallel()
	h := handlerFor(t)
	rec := httptest.NewRecorder()
	h.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	for _, r := range ReservedRules() {
		if !contains(body, `"key":"`+r.Key+`"`) {
			t.Errorf("reserved rule %+v was not shipped to the client", r)
		}
	}
	// And the client must not carry its own copy of the chords.
	for _, hardcoded := range []string{`e.key === 'F5'`, `k === 't' ||`, `k === 'q'`} {
		if contains(clientJS, hardcoded) {
			t.Errorf("the client still hard-codes %q instead of walking the injected table", hardcoded)
		}
	}
	// Copied, so a caller cannot alter what the server enforces.
	rules := ReservedRules()
	rules[0].Key = "a"
	if ReservedRules()[0].Key == "a" {
		t.Error("ReservedRules returns the live table")
	}
}

// A hostile WSPath must not be able to break out of the injected JSON.
func TestRegress_HostileWSPathIsContained(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		`/ws"</script><script>alert(1)</script>`,
		"/ws alert(1)",
		`/ws');alert(1);//`,
	} {
		h := handlerFor(t, WSPath(p))
		rec := httptest.NewRecorder()
		h.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		body := rec.Body.String()
		if contains(body, "<script>alert(1)</script>") {
			t.Errorf("path %q broke out of the injected config", p)
		}
		if contains(body, "</script><script>") {
			t.Errorf("path %q closed the script element", p)
		}
	}
}

func pkixName(cn string) pkix.Name { return pkix.Name{CommonName: cn} }

package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server"
	httpserver "github.com/yongjohnlee80/golib/server/http"
	"github.com/yongjohnlee80/golib/server/ws"
)

// startWS builds an httpserver with the full recommended middleware stack, an
// echo WS endpoint at /ws, and returns the base address + a stop func.
func startWS(t *testing.T, opts ...ws.Option) (srv *httpserver.Server, stop func() error) {
	t.Helper()
	lg := logger.Nop{}
	srv = httpserver.New(
		httpserver.Addr("127.0.0.1:0"),
		httpserver.Middlewares(httpserver.Recover(lg), httpserver.RequestLogger(lg), httpserver.RequestID()),
	)
	srv.Handle("GET /ws", ws.Handler(srv.Sessions(), echoFn, opts...))
	srv.Get("/readyz", srv.Readyz())

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx) }()
	deadline := time.After(2 * time.Second)
	for srv.Addr() == "" {
		select {
		case <-deadline:
			t.Fatal("server never bound")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	return srv, func() error {
		cancel()
		return <-errc
	}
}

// echoFn echoes every message until the session ends.
func echoFn(ctx context.Context, s *ws.Session) {
	for {
		mt, p, err := s.Read(ctx)
		if err != nil {
			return
		}
		if err := s.Write(ctx, mt, p); err != nil {
			return
		}
	}
}

// criterion 1: echo round-trip (text, binary, JSON) through the full
// middleware stack — end-to-end through the wrapRecorder path.
func TestWS_EchoThroughMiddleware(t *testing.T) {
	t.Parallel()
	srv, stop := startWS(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	// text
	if err := c.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	mt, p, err := c.Read(ctx)
	if err != nil || mt != websocket.MessageText || string(p) != "hello" {
		t.Fatalf("text echo = %v %q %v", mt, p, err)
	}
	// binary
	if err := c.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	mt, p, err = c.Read(ctx)
	if err != nil || mt != websocket.MessageBinary || len(p) != 3 {
		t.Fatalf("binary echo = %v %v %v", mt, p, err)
	}
	// JSON round-trip via the raw frames (server echoes bytes)
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"n":42}`)); err != nil {
		t.Fatal(err)
	}
	_, p, err = c.Read(ctx)
	if err != nil || !strings.Contains(string(p), "42") {
		t.Fatalf("json echo = %q %v", p, err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// criterion 2: same-origin default-deny; allow-pattern opt-in; refusal is an
// ordinary HTTP error.
func TestWS_OriginPolicy(t *testing.T) {
	t.Parallel()
	srv, stop := startWS(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Cross-origin: refused by default.
	h := http.Header{}
	h.Set("Origin", "http://evil.example.com")
	_, res, err := websocket.Dial(ctx, "ws://"+srv.Addr()+"/ws", &websocket.DialOptions{HTTPHeader: h})
	if err == nil {
		t.Fatal("cross-origin upgrade must be refused by default")
	}
	if res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("refusal status = %v, want 403", res)
	}

	// Same pattern explicitly allowed.
	srv2, stop2 := startWS(t, ws.InsecureAllowOrigins("evil.example.com"))
	defer stop2()
	c, _, err := websocket.Dial(ctx, "ws://"+srv2.Addr()+"/ws", &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatalf("allowed origin refused: %v", err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// criterion 3: the shutdown boundary — live sessions get StatusGoingAway,
// fresh dials during drain get HTTP 503, registry empties, shutdown returns.
func TestWS_ShutdownBoundary(t *testing.T) {
	t.Parallel()
	srv, stop := startWS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	// Wait until the session registered.
	deadline := time.After(2 * time.Second)
	for srv.Sessions().Len() == 0 {
		select {
		case <-deadline:
			t.Fatal("session never registered")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	stopped := make(chan error, 1)
	go func() { stopped <- stop() }()

	// The live client observes the GoingAway close.
	_, _, rerr := c.Read(ctx)
	if rerr == nil {
		t.Fatal("read during shutdown must fail with a close error")
	}
	if code := websocket.CloseStatus(rerr); code != websocket.StatusGoingAway {
		t.Fatalf("close code = %v (err %v), want StatusGoingAway", code, rerr)
	}

	if err := <-stopped; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if n := srv.Sessions().Len(); n != 0 {
		t.Errorf("registry has %d sessions after shutdown", n)
	}

	// A fresh dial is now refused with a plain HTTP error (server stopped —
	// the pre-handshake gate path is asserted separately below).
	if _, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+"/ws", nil); err == nil {
		t.Error("dial after shutdown must fail")
	}
}

// criterion 3 (gate half): a request reaching the handler during drain gets
// a plain HTTP 503 from the pre-handshake Reserve gate — the upgrade is never
// accepted (no 101), so accept-then-instant-close cannot occur.
func TestWS_UpgradeDuringDrainGets503(t *testing.T) {
	t.Parallel()
	var reg server.Registry
	h := ws.Handler(&reg, func(ctx context.Context, s *ws.Session) {})

	// Hold drain open so the gate is observably active.
	release := make(chan struct{})
	unreg := reg.Register(holdSession{release})
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- reg.Drain(ctx)
	}()
	// Reserve() failing is the gate; wait until drain engaged (probe
	// reservations are cancelled so they don't hold the drain open).
	deadline := time.After(2 * time.Second)
	for {
		probe, ok := reg.Reserve()
		if !ok {
			break
		}
		probe.Cancel()
		select {
		case <-deadline:
			t.Fatal("registry never started draining")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "AAAAAAAAAAAAAAAAAAAAAA==")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 from the pre-handshake gate", rec.Code)
	}
	if rec.Code == http.StatusSwitchingProtocols {
		t.Fatal("upgrade must never be accepted during drain")
	}

	close(release)
	unreg()
	if err := <-done; err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

type holdSession struct{ release chan struct{} }

func (h holdSession) Close() error { return nil }
func (h holdSession) Drain(ctx context.Context) error {
	select {
	case <-h.release:
	case <-ctx.Done():
	}
	return nil
}

// criterion 4: oversize message → read fails, close 1009, goroutine exits.
func TestWS_ReadLimit(t *testing.T) {
	t.Parallel()
	srv, stop := startWS(t, ws.ReadLimit(16))
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	big := make([]byte, 64)
	if err := c.Write(ctx, websocket.MessageBinary, big); err != nil {
		t.Fatal(err)
	}
	_, _, rerr := c.Read(ctx)
	if rerr == nil {
		t.Fatal("expected close after oversize message")
	}
	if code := websocket.CloseStatus(rerr); code != websocket.StatusMessageTooBig {
		t.Fatalf("close code = %v, want StatusMessageTooBig", code)
	}
	// Session unregisters.
	deadline := time.After(2 * time.Second)
	for srv.Sessions().Len() != 0 {
		select {
		case <-deadline:
			t.Fatal("session never unregistered after read-limit close")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// criterion 5: a peer that never reads (so never pongs) is detected by
// keepalive and the session ends without leaking.
func TestWS_KeepaliveDetectsDeadPeer(t *testing.T) {
	t.Parallel()
	srv, stop := startWS(t, ws.Keepalive(30*time.Millisecond, 30*time.Millisecond))
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+srv.Addr()+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	// Never read: pings are never answered.

	deadline := time.After(3 * time.Second)
	for srv.Sessions().Len() != 0 {
		select {
		case <-deadline:
			t.Fatal("keepalive never ended the dead-peer session")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// criterion 7: dependency isolation — only server/ws imports the websocket
// library; the core packages stay dependency-free.
func TestWS_DependencyIsolation(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not available")
	}
	out, err := exec.Command("go", "list", "-deps",
		"github.com/yongjohnlee80/golib/server",
		"github.com/yongjohnlee80/golib/server/http").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "coder/websocket") {
		t.Error("server core packages must not depend on the websocket library")
	}
}

// errorsAs is a tiny local wrapper to avoid importing errors just for one call.
func errorsAs(err error, target *websocket.CloseError) bool {
	ce, ok := err.(websocket.CloseError)
	if ok {
		*target = ce
	}
	return ok
}

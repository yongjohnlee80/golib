package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server/ws"
	"github.com/yongjohnlee80/golib/tui"
)

// fakeConn is a scripted transport. Reads come from a queue; writes are
// recorded.
type fakeConn struct {
	mu      sync.Mutex
	inbound []clientMessage
	sent    []serverMessage
	closed  bool
	code    ws.StatusCode
	reason  string
	readErr error
	block   chan struct{} // when set, reads block here after the script runs
}

func (f *fakeConn) push(m clientMessage) {
	f.mu.Lock()
	f.inbound = append(f.inbound, m)
	f.mu.Unlock()
}

func (f *fakeConn) ReadJSON(ctx context.Context, v any) error {
	for {
		f.mu.Lock()
		if len(f.inbound) > 0 {
			m := f.inbound[0]
			f.inbound = f.inbound[1:]
			f.mu.Unlock()
			b, _ := json.Marshal(m)
			return json.Unmarshal(b, v)
		}
		err, block := f.readErr, f.block
		f.mu.Unlock()
		if err != nil {
			return err
		}
		if block != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-block:
				continue
			}
		}
		return errors.New("fakeConn: script exhausted")
	}
}

func (f *fakeConn) WriteJSON(_ context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var m serverMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	f.mu.Lock()
	f.sent = append(f.sent, m)
	f.mu.Unlock()
	return nil
}

func (f *fakeConn) Close(code ws.StatusCode, reason string) error {
	f.mu.Lock()
	if !f.closed {
		f.closed, f.code, f.reason = true, code, reason
	}
	f.mu.Unlock()
	return nil
}

func (f *fakeConn) closeInfo() (bool, ws.StatusCode, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed, f.code, f.reason
}

func (f *fakeConn) messages() []serverMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]serverMessage(nil), f.sent...)
}

// denyFactor always refuses.
type denyFactor struct{}

func (denyFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (denyFactor) Verify(context.Context, *auth.Request) (auth.Contribution, error) {
	return auth.Contribution{}, auth.Reason("test: denied")
}

func loopFor(t *testing.T, policy auth.Policy, factory AppFactory, opts ...ManagerOption) (*sessionLoop, *Manager) {
	t.Helper()
	if factory == nil {
		factory = func(*Backend) Runner { return newFakeApp() }
	}
	m, err := NewManager(factory, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})
	cfg := Config{
		Addr:           "127.0.0.1:0",
		Policy:         policy,
		AllowedOrigins: []string{"https://tui.example.test"},
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	return &sessionLoop{
		cfg: cfg, mgr: m, log: nopLogger{}, limits: DefaultLimits().normalize(),
		decoder: &decoder{},
	}, m
}

type nopLogger struct{}

func (nopLogger) Log(logger.Severity, any) {}

func handshakeReq(origin string) requestInfo {
	r := httptest.NewRequest(http.MethodGet, "/attach", nil)
	r.RemoteAddr = "203.0.113.7:44321"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return requestInfo{http: r}
}

func helloMsg() clientMessage {
	return clientMessage{T: msgHello, Cols: 20, Rows: 5, CellW: 8, CellH: 16}
}

// THE ordering contract of §2.8: no App exists until Policy.Authenticate
// succeeds, on every branch.
func TestSessionLoop_NoAppBeforeAuthentication(t *testing.T) {
	t.Parallel()

	built := 0
	var mu sync.Mutex
	factory := func(*Backend) Runner {
		mu.Lock()
		built++
		mu.Unlock()
		return newFakeApp()
	}
	deny, err := auth.NewPolicy(auth.Leaf(denyFactor{}))
	if err != nil {
		t.Fatal(err)
	}
	l, m := loopFor(t, deny, factory)

	c := &fakeConn{}
	c.push(helloMsg())
	if err := l.serve(context.Background(), c, handshakeReq("https://tui.example.test")); err == nil {
		t.Fatal("a denied policy must fail the attach")
	}

	mu.Lock()
	n := built
	mu.Unlock()
	if n != 0 {
		t.Errorf("the AppFactory ran %d times for an unauthenticated client", n)
	}
	if m.Len() != 0 {
		t.Errorf("%d sessions exist after a denied attach", m.Len())
	}
	closed, code, _ := c.closeInfo()
	if !closed || code != closePolicy {
		t.Errorf("close = %v %v, want a policy-violation close", closed, code)
	}
}

// A cross-origin handshake is refused BEFORE the auth machinery runs, so a probe
// cannot even reach it.
func TestSessionLoop_OriginCheckedBeforeAuth(t *testing.T) {
	t.Parallel()

	authRan := false
	var mu sync.Mutex
	p, err := auth.NewPolicy(auth.Leaf(spyFactor{onVerify: func() {
		mu.Lock()
		authRan = true
		mu.Unlock()
	}}))
	if err != nil {
		t.Fatal(err)
	}
	l, m := loopFor(t, p, nil)

	for name, origin := range map[string]string{
		"cross origin":   "https://attacker.test",
		"absent":         "",
		"null":           "null",
		"scheme differs": "http://tui.example.test",
	} {
		c := &fakeConn{}
		c.push(helloMsg())
		err := l.serve(context.Background(), c, handshakeReq(origin))
		if !errors.Is(err, ErrOriginDenied) {
			t.Errorf("%s: err = %v, want ErrOriginDenied", name, err)
		}
		closed, _, reason := c.closeInfo()
		if !closed {
			t.Errorf("%s: connection not closed", name)
		}
		// The refusal must not say WHICH control tripped.
		if reason != "forbidden" {
			t.Errorf("%s: close reason %q explains too much", name, reason)
		}
	}
	mu.Lock()
	ran := authRan
	mu.Unlock()
	if ran {
		t.Error("the auth machinery ran for a cross-origin handshake")
	}
	if m.Len() != 0 {
		t.Errorf("%d sessions created", m.Len())
	}
}

type spyFactor struct{ onVerify func() }

func (spyFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (s spyFactor) Verify(context.Context, *auth.Request) (auth.Contribution, error) {
	if s.onVerify != nil {
		s.onVerify()
	}
	return auth.Contribution{Method: "spy", Subject: "alice"}, nil
}

// The first message must be a hello. Anything else is a protocol error, not an
// opportunity to guess what the client meant.
func TestSessionLoop_FirstMessageMustBeHello(t *testing.T) {
	t.Parallel()
	l, _ := loopFor(t, testPolicy(t), nil)

	for name, m := range map[string]clientMessage{
		"a key":            {T: msgKey, Key: "a"},
		"an ack":           {T: msgAck, Rev: 1},
		"unmeasured hello": {T: msgHello},
		"no size":          {T: msgHello, CellW: 8, CellH: 16},
		"no metrics":       {T: msgHello, Cols: 20, Rows: 5},
	} {
		c := &fakeConn{}
		c.push(m)
		if err := l.serve(context.Background(), c, handshakeReq("https://tui.example.test")); err == nil {
			t.Errorf("%s was accepted as a hello", name)
		}
		if closed, _, _ := c.closeInfo(); !closed {
			t.Errorf("%s: connection not closed", name)
		}
	}
}

// A successful attach reaches the App and reports the session id.
func TestSessionLoop_SuccessfulAttach(t *testing.T) {
	t.Parallel()
	l, m := loopFor(t, testPolicy(t), nil)

	c := &fakeConn{block: make(chan struct{})}
	c.push(helloMsg())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- l.serve(ctx, c, handshakeReq("https://tui.example.test")) }()

	// Wait for the ready message.
	deadline := time.Now().Add(2 * time.Second)
	var ready *serverMessage
	for time.Now().Before(deadline) && ready == nil {
		for _, msg := range c.messages() {
			if msg.T == msgReady {
				got := msg
				ready = &got
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ready == nil {
		t.Fatal("no ready message")
	}
	if ready.Session == "" {
		t.Error("ready carried no session id, so a reconnect cannot resume")
	}
	if m.Len() != 1 {
		t.Errorf("%d sessions, want 1", m.Len())
	}
	cancel()
	<-done
}

// An authenticated user must not be handed a NEW session when their attach to
// somebody else's is refused: that would turn an authorization failure into a
// success from the client's point of view.
func TestSessionLoop_MismatchedSessionIsNotSilentlyReplaced(t *testing.T) {
	t.Parallel()
	l, m := loopFor(t, testPolicy(t), nil)

	// alice's session, created out of band.
	alices, err := m.Create(context.Background(), &auth.Identity{Subject: "alice"}, hello())
	if err != nil {
		t.Fatal(err)
	}

	// The policy authenticates everyone as "alice"... so use a second manager
	// entry owned by someone else.
	bobs, err := m.Create(context.Background(), &auth.Identity{Subject: "bob"}, hello())
	if err != nil {
		t.Fatal(err)
	}
	_ = alices

	msg := helloMsg()
	msg.Session = bobs.ID() // alice claims bob's session
	c := &fakeConn{}
	c.push(msg)
	err = l.serve(context.Background(), c, handshakeReq("https://tui.example.test"))
	if !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("err = %v, want ErrSubjectMismatch", err)
	}
	if n := m.Len(); n != 2 {
		t.Errorf("%d sessions, want the original 2 — a refused attach created one", n)
	}
}

// An unknown session id means the session expired while the client was away, so
// a fresh one is the right answer.
func TestSessionLoop_ExpiredSessionGetsAFreshOne(t *testing.T) {
	t.Parallel()
	l, m := loopFor(t, testPolicy(t), nil)

	msg := helloMsg()
	msg.Session = "long-gone"
	c := &fakeConn{block: make(chan struct{})}
	c.push(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = l.serve(ctx, c, handshakeReq("https://tui.example.test")) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.Len() == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("an expired session id did not yield a fresh session")
}

// --- limits -----------------------------------------------------------------

func TestBucket_RateAndBurst(t *testing.T) {
	t.Parallel()
	now := time.Now()
	b := newBucket(100, 10, func() time.Time { return now })

	// The burst is available immediately.
	for i := range 10 {
		if wait := b.take(); wait != 0 {
			t.Fatalf("token %d waited %v inside the burst", i, wait)
		}
	}
	// The eleventh must wait, and the wait must be finite and proportional.
	wait := b.take()
	if wait <= 0 {
		t.Fatal("the bucket did not throttle past its burst")
	}
	if wait > time.Second {
		t.Errorf("wait = %v, implausible for 100/s", wait)
	}
	// Time passing refills it.
	now = now.Add(100 * time.Millisecond) // 10 tokens at 100/s
	for i := range 10 {
		if w := b.take(); w != 0 {
			t.Fatalf("token %d waited %v after a refill", i, w)
		}
	}
	// Refill is CAPPED at the burst: an idle client cannot bank unlimited
	// credit and then spend it all at once.
	now = now.Add(time.Hour)
	granted := 0
	for range 1000 {
		if b.take() != 0 {
			break
		}
		granted++
	}
	if granted > 11 {
		t.Errorf("%d tokens granted after an hour idle, burst is 10", granted)
	}
}

// A client that fills the queue and lets it drain is bursty, not abusive.
// Closing on the first full queue would punish a momentarily busy App loop.
func TestOverload_RequiresSustainedPressure(t *testing.T) {
	t.Parallel()
	now := time.Now()
	o := newOverload(2*time.Second, func() time.Time { return now })

	if o.full() {
		t.Error("the first full observation must not trip the close")
	}
	now = now.Add(time.Second)
	if o.full() {
		t.Error("one second of pressure must not trip a two-second grace")
	}
	// Draining resets it.
	o.clear()
	now = now.Add(time.Hour)
	if o.full() {
		t.Error("pressure after a drain must start a new window")
	}
	now = now.Add(3 * time.Second)
	if !o.full() {
		t.Error("sustained pressure past the grace must trip the close")
	}
}

func TestLimits_Normalize(t *testing.T) {
	t.Parallel()
	d := DefaultLimits()
	if got := (Limits{}).normalize(); got != d {
		t.Errorf("a zero Limits must yield the defaults: got %+v want %+v", got, d)
	}
	// Setting one field must not clear the rest.
	got := Limits{QueueDepth: 4}.normalize()
	if got.QueueDepth != 4 {
		t.Errorf("QueueDepth = %d, want 4", got.QueueDepth)
	}
	if got.MaxMessage != d.MaxMessage || got.EventsPerSecond != d.EventsPerSecond {
		t.Errorf("setting one field cleared others: %+v", got)
	}
	// §2.9's table.
	if d.MaxMessage != 64<<10 || d.EventsPerSecond != 500 || d.Burst != 2000 ||
		d.QueueDepth != 1024 || d.OverloadGrace != 2*time.Second {
		t.Errorf("defaults drifted from ADR-0009 §2.9: %+v", d)
	}
}

// A sustained flood closes the connection rather than growing the queue.
func TestSessionLoop_SustainedFloodCloses(t *testing.T) {
	t.Parallel()
	now := time.Now()
	// A tiny queue and no grace, so the flood is unambiguous.
	l, _ := loopFor(t, testPolicy(t), nil, BackendOptions(EventQueue(2)))
	l.limits = Limits{EventsPerSecond: 1e9, Burst: 1e9, QueueDepth: 2, OverloadGrace: 0}.normalize()
	l.limits.OverloadGrace = 0
	l.now = func() time.Time { return now }
	l.sleep = func(context.Context, time.Duration) { now = now.Add(time.Second) }

	c := &fakeConn{}
	c.push(helloMsg())
	// An App that never reads its events: the queue fills and stays full.
	for i := range 200 {
		c.push(clientMessage{T: msgText, Text: fmt.Sprint(i % 10)})
	}
	err := l.serve(context.Background(), c, handshakeReq("https://tui.example.test"))
	if !errors.Is(err, ErrEventOverflow) {
		t.Fatalf("err = %v, want ErrEventOverflow", err)
	}
	closed, code, reason := c.closeInfo()
	if !closed || code != closePolicy {
		t.Errorf("close = %v %v %q, want a policy-violation close", closed, code, reason)
	}
}

// An unknown message type degrades rather than disconnecting: a newer client
// talking to an older server should still work.
func TestSessionLoop_UnknownMessageIsDropped(t *testing.T) {
	t.Parallel()
	l, _ := loopFor(t, testPolicy(t), nil)
	if _, err := l.translate(clientMessage{T: "from-the-future"}); err == nil {
		t.Error("an unknown type should report a note")
	}
	// A second hello is refused rather than re-authenticating mid-stream.
	if _, err := l.translate(clientMessage{T: msgHello}); err == nil {
		t.Error("a second hello must be refused")
	}
	// A dropped key is a decision, not an error.
	evs, err := l.translate(clientMessage{T: msgKey, Key: "CapsLock"})
	if err != nil || len(evs) != 0 {
		t.Errorf("evs = %v err = %v: a dropped key is a decision", evs, err)
	}
}

func TestSessionLoop_TranslateShapes(t *testing.T) {
	t.Parallel()
	l, _ := loopFor(t, testPolicy(t), nil)

	evs, err := l.translate(clientMessage{T: msgText, Text: "hi"})
	if err != nil || len(evs) != 2 {
		t.Fatalf("text: %v %v", evs, err)
	}
	evs, err = l.translate(clientMessage{T: msgPaste, Text: "a\r\nb"})
	if err != nil || len(evs) != 1 {
		t.Fatalf("paste: %v %v", evs, err)
	}
	if p := evs[0].(tui.PasteEvent); p.Text != "a\nb" {
		t.Errorf("paste text = %q", p.Text)
	}
	evs, err = l.translate(clientMessage{T: msgFocus, Gained: true})
	if err != nil || len(evs) != 1 {
		t.Fatalf("focus: %v %v", evs, err)
	}
	if f := evs[0].(tui.FocusEvent); !f.Gained || !f.Terminal {
		t.Errorf("focus = %#v", f)
	}
	evs, err = l.translate(clientMessage{T: msgMouse, Kind: "down", Button: 0, X: 2, Y: 3})
	if err != nil || len(evs) != 1 {
		t.Fatalf("mouse: %v %v", evs, err)
	}
}

// Frame encoding must resolve colors and clear the reverse bit, so a client
// cannot apply the swap twice and undo it.
func TestEncodeFrame(t *testing.T) {
	t.Parallel()
	f := Frame{
		Rev: 7, Full: true, W: 2, H: 1,
		Updates: []tui.CellUpdate{
			{X: 0, Y: 0, Cell: tui.Cell{Content: "a", Width: 1, Attrs: tui.CellAttrs{
				FG:   tui.CellColor{Kind: tui.CellColorRGB, R: 0xff},
				BG:   tui.CellColor{Kind: tui.CellColorANSI, Index: 4},
				Mask: tui.AttrBold | tui.AttrReverse,
			}}},
			{X: 1, Y: 0, Cell: tui.Cell{Content: "漢", Width: 2}},
		},
		Cursor: cursorState{Visible: true, X: 1, Y: 0, Shape: tui.CursorShapeBar},
	}
	m := encodeFrame(f)
	if m.T != msgFrame || m.Rev != 7 || !m.Full || m.W != 2 || m.H != 1 {
		t.Fatalf("envelope = %+v", m)
	}
	if len(m.Updates) != 2 {
		t.Fatalf("%d updates", len(m.Updates))
	}
	// Reverse applied during encoding: fg and bg are swapped...
	if m.Updates[0].F != "var(--a4)" || m.Updates[0].B != "#ff0000" {
		t.Errorf("colors not swapped: F=%q B=%q", m.Updates[0].F, m.Updates[0].B)
	}
	// ...and the reverse bit is CLEARED, so the client cannot swap again.
	if m.Updates[0].A&uint16(tui.AttrReverse) != 0 {
		t.Error("the reverse bit reached the client, which would let it undo the swap")
	}
	if m.Updates[0].A&uint16(tui.AttrBold) == 0 {
		t.Error("bold was lost")
	}
	if m.Updates[1].W != 2 {
		t.Errorf("wide cell width = %d", m.Updates[1].W)
	}
	if m.Cursor == nil || !m.Cursor.Visible || m.Cursor.X != 1 {
		t.Errorf("cursor = %+v", m.Cursor)
	}
}

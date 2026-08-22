package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/tui"
)

// Regressions for lector r3.

// #1: Resize held resizeMu across delivery and mutation, but Flush did not — so
// an App that dequeued an expansion and painted at a coordinate valid in the NEW
// size had its cells applied to the old, smaller grid and silently dropped. The
// render was simply lost and the screen stayed blank there.
func TestRegress3_FlushAfterResizeIsNotDiscarded(t *testing.T) {
	t.Parallel()
	b := started(t, hello(), EventQueue(8)) // 20x5

	// Force the interleaving: while the resize is half-applied, an App paints at
	// a coordinate only the NEW size can hold.
	var flushErr atomic.Value
	var ran atomic.Bool
	done := make(chan struct{})
	b.resizeGap = func() {
		ran.Store(true)
		go func() {
			defer close(done)
			// (30, 2) is out of range at 20x5 and in range at 40x12.
			if err := b.Flush([]tui.CellUpdate{put(30, 2, "X")}); err != nil {
				flushErr.Store(err)
			}
		}()
		// The Flush must NOT complete inside the gap: it has to wait for the grid
		// to actually be the size the event announced.
		//
		// Deliberately NOT waiting for `done` here — that would deadlock, since
		// Flush wants the very lock this gap is holding. The first version of this
		// test did exactly that and hung, which was itself evidence the
		// serialization works.
		select {
		case <-done:
			flushErr.Store(errors.New("Flush completed while the grid was still the old size"))
		case <-time.After(50 * time.Millisecond):
		}
	}

	if err := b.Resize(40, 12); err != nil {
		t.Fatal(err)
	}
	if !ran.Load() {
		t.Fatal("the gap hook never ran")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the Flush never completed after the resize released the lock")
	}
	if v := flushErr.Load(); v != nil {
		t.Fatalf("%v", v)
	}
	// The painted cell must be present, which is the whole point: the App's
	// render survived the resize.
	if got := b.framer.serverGrid().at(30, 2).Content; got != "X" {
		t.Errorf("cell (30,2) = %q, want \"X\" — the App's render was discarded by the "+
			"resize", got)
	}
}

// #2: the pre-auth slot was held for the entire authenticated pump, so
// MaxPending became a cap on LIVE sessions: with MaxPending=1 one healthy
// session refused every newcomer despite spare MaxSessions and nothing pending.
func TestRegress3_PendingSlotIsReleasedAfterAuth(t *testing.T) {
	t.Parallel()
	l, m := loopFor(t, testPolicy(t), nil, MaxSessions(8))
	l.pending = newGate(1)

	c := &fakeConn{block: make(chan struct{})}
	c.push(helloMsg())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = l.serve(ctx, c, handshakeReq("https://tui.example.test")) }()

	// Wait for the session to be live.
	deadline := time.Now().Add(3 * time.Second)
	for m.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if m.Len() == 0 {
		t.Fatal("the first session never came up")
	}
	// Give the release a moment to land after ready.
	for range 50 {
		if l.pending.pending() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := l.pending.pending(); got != 0 {
		t.Fatalf("pending = %d while one healthy session is live — MaxPending is acting "+
			"as a cap on live sessions", got)
	}

	// A second client is admitted, which is the observable consequence.
	second := &fakeConn{block: make(chan struct{})}
	second.push(helloMsg())
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	errCh := make(chan error, 1)
	go func() { errCh <- l.serve(ctx2, second, handshakeReq("https://tui.example.test")) }()

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range second.messages() {
			if msg.T == msgReady {
				return
			}
		}
		select {
		case err := <-errCh:
			t.Fatalf("the second client was refused: %v", err)
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the second client never became ready")
}

// #3: `return shutdownErr` evaluated the variable before the deferred function
// assigned it, so a failed shutdown returned nil — the error I had just been told
// not to discard was discarded anyway.
func TestRegress3_ServeReturnsShutdownFailure(t *testing.T) {
	t.Parallel()
	// An App that ignores its context, so Shutdown cannot drain it.
	m, err := NewManager(func(*Backend) Runner { return stubbornApp{} },
		ManagerLogger(nopLogger{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), &auth.Identity{Subject: "alice"}, hello()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Shutdown must report the session that would not exit.
	if err := m.Shutdown(ctx); err == nil {
		t.Fatal("Shutdown reported success for an App that never exits")
	}
}

type stubbornApp struct{}

func (stubbornApp) Run(context.Context) error {
	// Deliberately ignores cancellation, which is what a buggy App does.
	time.Sleep(30 * time.Second)
	return nil
}

// #4: rev 11 declared password a ticket minter, but the attach protocol still
// carried subject/pw and the credential mapping still projected them — so a
// custom client could authenticate a password directly over the WebSocket. The
// split existed in the prose and not in the code.
func TestRegress3_AttachProtocolCarriesNoPassword(t *testing.T) {
	t.Parallel()

	// The wire type has no password fields at all: a client cannot express one.
	blob, err := json.Marshal(clientMessage{T: msgHello, Cols: 20, Rows: 5, CellW: 8, CellH: 16})
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"password", `"pw"`, `"subject"`} {
		if strings.Contains(string(blob), banned) {
			t.Errorf("the attach message carries %s", banned)
		}
	}

	// And a hostile client sending those keys anyway has them ignored: the
	// decoded message cannot hold them, so the credential map never sees them.
	var m clientMessage
	if err := json.Unmarshal([]byte(`{"t":"hello","subject":"alice","pw":"secret",
		"password":"secret","cols":20,"rows":5,"cw":8,"ch":16}`), &m); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/attach", nil)
	r.RemoteAddr = "203.0.113.7:1"
	req := authRequest(r, m)
	for _, banned := range []string{"subject", "password"} {
		if _, ok := req.Credentials[banned]; ok {
			t.Errorf("credential %q reached the attach request from a hostile client", banned)
		}
	}
}

// #5: LimitReader + one Decode was not a bound. Decode stops at the end of the
// first JSON value, so a correct-password object followed by junk decoded fine,
// never hit the limit, and minted a ticket.
func TestRegress3_LoginBodyBoundIsNotBypassable(t *testing.T) {
	t.Parallel()
	h, _ := loginHandler(t, "correct-horse", "alice")

	cases := map[string]string{
		"valid prefix then junk": `{"subject":"alice","password":"correct-horse"}` +
			strings.Repeat("x", maxLoginBody*2),
		"valid prefix then a second object": `{"subject":"alice","password":"correct-horse"}` +
			`{"subject":"alice","password":"correct-horse"}`,
		"valid prefix then whitespace and junk": `{"subject":"alice","password":"correct-horse"}` +
			"\n\n" + strings.Repeat("{", 100),
		"oversized single object": `{"subject":"alice","password":"` +
			strings.Repeat("x", maxLoginBody*2) + `"}`,
	}
	for name, body := range cases {
		rec := httptest.NewRecorder()
		h.ServeLogin(rec, loginPost(body))
		if rec.Code == http.StatusOK {
			t.Errorf("%s: minted a ticket for a body that exceeds the bound", name)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: code = %d, want 401", name, rec.Code)
		}
	}
	// The control: exactly one object, within the bound, still works.
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"correct-horse"}`))
	if rec.Code != http.StatusOK {
		t.Errorf("a well-formed login was refused: %d %s", rec.Code, rec.Body.String())
	}
	// Trailing whitespace alone is fine — a client may pretty-print.
	rec = httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"correct-horse"}`+"\n"))
	if rec.Code != http.StatusOK {
		t.Errorf("trailing newline was refused: %d", rec.Code)
	}
}

// #6: an unconditional document-click handler refocused the capture element, so
// clicking the username or password input immediately lost focus and the form
// could not be used with a mouse at all.
func TestRegress3_LoginFormKeepsFocus(t *testing.T) {
	t.Parallel()
	i := strings.Index(clientJS, "document.addEventListener('click'")
	if i < 0 {
		t.Fatal("the document click handler is gone; this test needs updating")
	}
	handler := clientJS[i:]
	if j := strings.Index(handler, "\n  });"); j > 0 {
		handler = handler[:j]
	}
	if !strings.Contains(handler, "box.hidden") {
		t.Errorf("the refocus handler does not check whether the login form is visible: %q", handler)
	}
	if !strings.Contains(handler, "box.contains(e.target)") {
		t.Errorf("the refocus handler does not exempt clicks inside the form: %q", handler)
	}
	// The mouse handler must not steal focus either while the form is up.
	if !strings.Contains(clientJS, "// the form owns focus while it is up") {
		t.Error("the grid mouse handler still refocuses unconditionally")
	}
}

// #7: the helper promised ticket/mTLS/SSH arms that ServeLogin could never
// present a credential for, so they were unreachable by construction.
func TestRegress3_PasswordHelperTakesNoUnreachableArms(t *testing.T) {
	t.Parallel()
	tracker, err := auth.NewMemTracker(8, auth.Backoff{
		Threshold: 5, Base: time.Minute, Max: time.Minute, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := PasswordPolicyExample(
		claimingFactor{subject: "alice", password: "pw"}, tracker,
		contextualFactor{allow: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	// A ticket presented to the LOGIN policy proves nothing: the login route
	// never projects one, so an arm that accepted it would be dead weight.
	_, err = p.Authenticate(context.Background(), &auth.Request{
		Credentials: map[string]auth.Secret{"ticket": auth.NewSecret("anything")},
	})
	if err == nil {
		t.Error("the login policy accepted a ticket, which the login route never sends")
	}
	// The password path still works.
	if _, err := p.Authenticate(context.Background(), &auth.Request{
		Credentials: map[string]auth.Secret{
			"subject":  auth.NewSecret("alice"),
			"password": auth.NewSecret("pw"),
		},
	}); err != nil {
		t.Errorf("the password path is broken: %v", err)
	}
}

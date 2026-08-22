package web

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/mtls"
	"github.com/yongjohnlee80/golib/tui"
)

// Regressions for lector r2.

// #1: the limiter lived on the shared sessionLoop, so concurrent connections
// overwrote each other's bucket — a data race, and clients throttling each other
// even without one.
func TestRegress2_LimiterIsConnectionLocal(t *testing.T) {
	t.Parallel()
	l, m := loopFor(t, testPolicy(t), nil, MaxSessions(8))

	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := &fakeConn{block: make(chan struct{})}
			c.push(helloMsg())
			for j := range 20 {
				c.push(clientMessage{T: msgText, Text: fmt.Sprint(j % 10)})
			}
			ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
			defer cancel()
			_ = l.serve(ctx, c, handshakeReq("https://tui.example.test"))
		}(i)
	}
	wg.Wait()
	_ = m
}

// #2: Create held m.mu, then called drop on the cancelled path, which locks m.mu
// again — a self-deadlock a cancelled connection reaches routinely.
func TestRegress2_CancelledCreateDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	m, _ := manager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := m.Create(ctx, &auth.Identity{Subject: "alice"}, hello())
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Create deadlocked on a cancelled context")
	}
	if m.Len() != 0 {
		t.Errorf("%d sessions after a cancelled create", m.Len())
	}
}

// #3a: a resize is the ONLY report of that size — the next arrives only when the
// user drags the window again — so it must be retried, not logged and skipped.
func TestRegress2_ResizeIsRetriedNotDropped(t *testing.T) {
	t.Parallel()
	l, m := loopFor(t, testPolicy(t), nil, BackendOptions(EventQueue(1)))
	s, err := m.Create(context.Background(), &auth.Identity{Subject: "alice"}, hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), &auth.Identity{Subject: "alice"}, hello()); err != nil {
		t.Fatal(err)
	}
	backend := s.Backend()

	// Fill the single slot so the resize cannot land yet.
	if err := backend.Submit(tui.KeyEvent{Code: 'x'}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		<-backend.Events()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	limiter := newBucket(1e9, 1e9, nil)
	if err := l.deliverResize(ctx, &fakeConn{}, s, limiter, 40, 12); err != nil {
		t.Fatalf("deliverResize = %v, want the resize waited for", err)
	}

	// Drain whatever the App would see and look for the resize.
	found := false
	for range 4 {
		select {
		case ev := <-backend.Events():
			if r, ok := ev.(tui.ResizeEvent); ok && r.W == 40 && r.H == 12 {
				found = true
			}
		default:
		}
	}
	if !found {
		t.Fatal("the resize was lost — the client stays the wrong size until the user " +
			"drags the window again")
	}
	// Start was never called here (no serve loop), so Size reports ErrNotStarted;
	// the framer is the thing under test.
	if w, h := backend.framer.size(); w != 40 || h != 12 {
		t.Errorf("grid = %dx%d, want 40x12", w, h)
	}
}

// #3b: submit-then-mutate left a window in which an App could dequeue the
// ResizeEvent and read the OLD size — the exact disagreement the submit-first
// ordering was supposed to prevent.
//
// The interleaving is FORCED with the resizeGap seam rather than raced for. A
// version of this test that simply ran an observer against the real window
// passed with the lock removed, because the window is a few instructions wide:
// it was testing luck.
func TestRegress2_ResizeIsAnOrderedTransition(t *testing.T) {
	t.Parallel()
	b := started(t, hello(), EventQueue(8))

	var (
		early   atomic.Bool
		gapRuns atomic.Int64
	)
	b.resizeGap = func() {
		gapRuns.Add(1)
		// A concurrent Size() must NOT be able to complete while the grid is
		// half-updated. Size takes resizeMu, which Resize is holding, so it has
		// to wait for the transition to finish.
		started := make(chan struct{})
		done := make(chan struct{})
		go func() {
			close(started)
			_, _ = b.Size()
			close(done)
		}()
		<-started
		select {
		case <-done:
			early.Store(true)
		case <-time.After(50 * time.Millisecond):
			// Correct: it is still blocked.
		}
	}

	if err := b.Resize(40, 12); err != nil {
		t.Fatal(err)
	}
	if gapRuns.Load() != 1 {
		t.Fatalf("the gap hook ran %d times, want 1", gapRuns.Load())
	}
	if early.Load() {
		t.Error("Size completed while the resize was half-applied — an App can observe " +
			"the event and then a stale size")
	}

	// And after the transition, event and size agree.
	select {
	case ev := <-b.Events():
		r, ok := ev.(tui.ResizeEvent)
		if !ok || r.W != 40 || r.H != 12 {
			t.Fatalf("event = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no resize event")
	}
	got, err := b.Size()
	if err != nil {
		t.Fatal(err)
	}
	if got != (tui.Size{W: 40, H: 12}) {
		t.Errorf("Size = %+v, want 40x12", got)
	}
}

// #4: the constraint check only rejected nil, so an IDENTITY factor passed and
// added a second way in rather than narrowing the first.
func TestRegress2_PasswordConstraintMustBeContextual(t *testing.T) {
	t.Parallel()
	tracker, err := auth.NewMemTracker(8, auth.Backoff{
		Threshold: 2, Base: time.Minute, Max: time.Minute, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	// An identity factor as the "constraint" would satisfy the Any by itself.
	_, err = PasswordPolicyExample(claimingFactor{subject: "alice", password: "pw"}, tracker, alwaysFactor{subject: "sneaky"})
	if err == nil {
		t.Fatal("an identity factor was accepted as a contextual constraint — it would " +
			"satisfy the policy on its own")
	}
	if !strings.Contains(err.Error(), "contextual") {
		t.Errorf("err = %v, should name the requirement", err)
	}
	// A genuine contextual factor is accepted.
	if _, err := PasswordPolicyExample(claimingFactor{subject: "alice", password: "pw"}, tracker, contextualFactor{allow: true}); err != nil {
		t.Errorf("a contextual constraint was refused: %v", err)
	}
}

// #6: MaxSessions bounds only what exists after a hello, so an unauthenticated
// waiting room was unbounded.
func TestRegress2_PreAuthConnectionsAreCapped(t *testing.T) {
	t.Parallel()
	l, _ := loopFor(t, testPolicy(t), nil)
	l.pending = newGate(2)

	// Connections that never send a hello occupy a slot until the deadline.
	l.limits.HelloTimeout = time.Hour // so they really do hold it
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &fakeConn{block: make(chan struct{})}
			_ = l.serve(ctx, c, handshakeReq("https://tui.example.test"))
		}()
	}
	// Wait for both slots to be taken.
	deadline := time.Now().Add(2 * time.Second)
	for l.pending.pending() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if l.pending.pending() != 2 {
		t.Fatalf("pending = %d, want 2", l.pending.pending())
	}

	// A third is REFUSED, not queued: queueing would move the unbounded waiting
	// room down a level rather than remove it.
	third := &fakeConn{}
	third.push(helloMsg())
	err := l.serve(context.Background(), third, handshakeReq("https://tui.example.test"))
	if !errors.Is(err, ErrPendingLimit) {
		t.Errorf("err = %v, want ErrPendingLimit", err)
	}
	closed, code, _ := third.closeInfo()
	if !closed || code != closeTryAgain {
		t.Errorf("close = %v %v, want 1013 Try Again Later", closed, code)
	}
	cancel()
	wg.Wait()
	// The slots are returned.
	if got := l.pending.pending(); got != 0 {
		t.Errorf("pending = %d after the connections ended, want 0", got)
	}
}

// A client that connects and says nothing must not be held forever.
func TestRegress2_HelloDeadline(t *testing.T) {
	t.Parallel()
	l, _ := loopFor(t, testPolicy(t), nil)
	l.limits.HelloTimeout = 80 * time.Millisecond

	c := &fakeConn{block: make(chan struct{})}
	start := time.Now()
	err := l.serve(context.Background(), c, handshakeReq("https://tui.example.test"))
	if err == nil {
		t.Fatal("a silent client was accepted")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %v — the hello deadline did not fire", elapsed)
	}
	if l.pending.pending() != 0 {
		t.Errorf("the pre-auth slot was not returned: %d", l.pending.pending())
	}
}

// #8: the client cleared its credential only on the success branch, so a failed
// measurement left the permanent listener holding the ticket with the socket
// open.
func TestRegress2_ClientClearsCredentialOnEveryBranch(t *testing.T) {
	t.Parallel()
	// The measure-failure branch must clear AND close.
	i := strings.Index(clientJS, "if (!measure()) {")
	if i < 0 {
		t.Fatal("the measure-failure branch is gone; this test needs updating")
	}
	branch := clientJS[i:]
	// A fixed window: slicing at the first "}" lands inside the object literal
	// that clears the credential.
	if len(branch) > 700 {
		branch = branch[:700]
	}
	if j := strings.Index(branch, "const s = gridSize()"); j > 0 {
		branch = branch[:j]
	}
	if !strings.Contains(branch, "cred = { ticket: '', session: '' }") {
		t.Errorf("the failure branch does not clear the credential: %q", branch)
	}
	if !strings.Contains(branch, "ws.close(") {
		t.Errorf("the failure branch leaves the socket open awaiting a hello: %q", branch)
	}
}

// Should-fix: the mTLS regression tested projection only. This runs a REAL
// auth/mtls factor through create and reattach.
func TestRegress2_MTLSPolicyCreateAndReattach(t *testing.T) {
	t.Parallel()
	leaf := &x509.Certificate{
		Subject:     pkix.Name{CommonName: "alice"},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	factor := mtls.New(func(c auth.Certificate) (string, error) { return c.CommonName, nil })
	policy, err := auth.NewPolicy(auth.Leaf(factor))
	if err != nil {
		t.Fatal(err)
	}
	l, m := loopFor(t, policy, nil)

	withTLS := func() requestInfo {
		r := httptest.NewRequest(http.MethodGet, "/attach", nil)
		r.RemoteAddr = "203.0.113.7:1"
		r.Host = "tui.example.test"
		r.Header.Set("Origin", "https://tui.example.test")
		r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{leaf}}}
		return requestInfo{http: r}
	}

	// Create: an mTLS client attaches with NO ticket.
	c := &fakeConn{block: make(chan struct{})}
	c.push(helloMsg())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = l.serve(ctx, c, withTLS()) }()

	var sessionID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && sessionID == "" {
		for _, msg := range c.messages() {
			if msg.T == msgReady {
				sessionID = msg.Session
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if sessionID == "" {
		t.Fatal("an mTLS client with a verified chain could not create a session")
	}
	cancel()

	// Reattach: still no ticket, and the same session.
	deadline = time.Now().Add(2 * time.Second)
	for m.Len() > 0 {
		s, ok := m.Get(sessionID)
		if !ok {
			break
		}
		s.mu.Lock()
		attached := s.attached
		s.mu.Unlock()
		if !attached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first connection never detached")
		}
		time.Sleep(5 * time.Millisecond)
	}
	msg := helloMsg()
	msg.Session = sessionID
	c2 := &fakeConn{block: make(chan struct{})}
	c2.push(msg)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	go func() { _ = l.serve(ctx2, c2, withTLS()) }()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m2 := range c2.messages() {
			if m2.T == msgReady {
				if m2.Session != sessionID {
					t.Errorf("reattach produced session %q, want %q", m2.Session, sessionID)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("an mTLS client could not reattach without a ticket")
}

// Should-fix: the attempt correlation had no negative control. A refused attach
// must carry a reference, and a successful one must not leak one into the close
// reason.
func TestRegress2_AttemptCorrelationNegativeControl(t *testing.T) {
	t.Parallel()
	deny, err := auth.NewPolicy(auth.Leaf(denyFactor{}))
	if err != nil {
		t.Fatal(err)
	}
	l, _ := loopFor(t, deny, nil)
	c := &fakeConn{}
	c.push(helloMsg())
	_ = l.serve(context.Background(), c, handshakeReq("https://tui.example.test"))

	_, _, reason := c.closeInfo()
	if !strings.HasPrefix(reason, "unauthorized") {
		t.Fatalf("close reason = %q", reason)
	}
	// The control: a reference must actually be present, or the "diagnosable
	// refusal" claim is empty.
	if !strings.Contains(reason, "ref=") {
		t.Errorf("no attempt reference in %q — a user has nothing to quote", reason)
	}
	ref := reason[strings.Index(reason, "ref=")+4:]
	if ref == "" {
		t.Error("the reference is empty")
	}
	// And it must differ per attempt.
	c2 := &fakeConn{}
	c2.push(helloMsg())
	_ = l.serve(context.Background(), c2, handshakeReq("https://tui.example.test"))
	_, _, reason2 := c2.closeInfo()
	if reason2 == reason {
		t.Error("two refusals produced the same reference — it is not per-attempt")
	}
}

func TestGate(t *testing.T) {
	t.Parallel()
	g := newGate(2)
	if !g.enter() || !g.enter() {
		t.Fatal("the first two slots must be available")
	}
	if g.enter() {
		t.Error("a third slot was handed out")
	}
	g.leave()
	if !g.enter() {
		t.Error("a returned slot was not reusable")
	}
	// Over-leaving must not create slots.
	for range 10 {
		g.leave()
	}
	if g.pending() != 0 {
		t.Errorf("pending = %d, want 0", g.pending())
	}
	if !g.enter() || !g.enter() {
		t.Error("the gate lost its capacity")
	}
	if g.enter() {
		t.Error("over-leaving inflated the capacity")
	}
}

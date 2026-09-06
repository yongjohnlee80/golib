package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
)

// --- the derived key ---------------------------------------------------------

func TestHandoffID(t *testing.T) {
	t.Parallel()

	const ticket = "a-high-entropy-single-use-ticket"
	id := HandoffID(ticket)
	if id == "" {
		t.Fatal("no id for a real ticket")
	}
	// Deterministic: the login route and the attach path compute it independently
	// and must agree, which is the entire mechanism.
	if HandoffID(ticket) != id {
		t.Error("HandoffID is not deterministic, so login and attach cannot agree")
	}
	if HandoffID(ticket+"x") == id {
		t.Error("distinct tickets collide")
	}
	if HandoffID("") != "" {
		t.Error("an absent ticket must yield an absent handoff, not a hash of nothing")
	}
	// It must reveal nothing about the ticket.
	if strings.Contains(id, ticket) {
		t.Error("the handoff contains the ticket")
	}
	// DOMAIN SEPARATION from the token store's own sha256(ticket) index. Two
	// hashes of one secret for two purposes must differ, or leaking one becomes a
	// lookup key for the other.
	// token indexes by sha256(ticket); the handoff hashes a DOMAIN-PREFIXED
	// value, so the two must differ for the same ticket.
	plain := sha256.Sum256([]byte(ticket))
	if id == base64.RawURLEncoding.EncodeToString(plain[:]) {
		t.Error("the handoff equals a plain sha256 of the ticket — no domain separation, " +
			"so leaking one becomes a lookup key for the other")
	}
	// URL-safe and fixed width, so it can be a map key and appear in a log.
	for _, c := range id {
		ok := c == '-' || c == '_' || (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !ok {
			t.Fatalf("handoff %q contains %q", id, c)
		}
	}
}

// --- the stash ---------------------------------------------------------------

// Take must empty the slot, which is what makes the move to a park single-claim
// even if a hook runs twice.
func TestStash_SingleClaim(t *testing.T) {
	t.Parallel()
	s := &Stash{}
	s.Set("session-A")
	if got := s.Take(); got != "session-A" {
		t.Fatalf("Take = %v", got)
	}
	if got := s.Take(); got != nil {
		t.Errorf("second Take = %v, want nil — the slot must empty on claim", got)
	}
	// Nil-safe: a factor used by a caller with no per-login state needs no special
	// case.
	var nilStash *Stash
	nilStash.Set("x")
	if nilStash.Take() != nil {
		t.Error("a nil Stash must be inert, not panic")
	}
	if StashFromContext(context.Background()) != nil {
		t.Error("a context with no login request must yield no stash")
	}
	if StashFromContext(nil) != nil {
		t.Error("a nil context must yield no stash")
	}
}

// Concurrent logins by ONE subject must get distinct slots. This is the case a
// subject-keyed park cannot serve, and the reason the slot is request-scoped.
func TestStash_ConcurrentSameSubjectAreIndependent(t *testing.T) {
	t.Parallel()
	const n = 32
	got := make([]any, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := &Stash{}
			ctx := withStash(context.Background(), st)
			// What a factor does: allocate, stash.
			StashFromContext(ctx).Set(i)
			// What OnLogin does: move it out.
			got[i] = StashFromContext(ctx).Take()
		}(i)
	}
	wg.Wait()
	for i := range n {
		if got[i] != i {
			t.Fatalf("login %d received %v — concurrent same-subject logins are not isolated", i, got[i])
		}
	}
}

// --- the four paths ---------------------------------------------------------

// parkingHandler builds a Handler whose login parks into a fake upstream park, so
// every handoff path can be observed.
type park struct {
	mu       sync.Mutex
	held     map[string]string // handoff -> upstream session
	claimed  []string
	released map[string]HandoffReason
}

func newPark() *park {
	return &park{held: map[string]string{}, released: map[string]HandoffReason{}}
}

func (p *park) hold(handoff, sess string) {
	p.mu.Lock()
	p.held[handoff] = sess
	p.mu.Unlock()
}

func (p *park) claim(handoff string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.held[handoff]
	if ok {
		delete(p.held, handoff)
		p.claimed = append(p.claimed, handoff)
	}
	return s, ok
}

func (p *park) release(handoff string, r HandoffReason) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.held, handoff)
	p.released[handoff] = r
}

func (p *park) outstanding() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.held)
}

func (p *park) reason(handoff string) (HandoffReason, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.released[handoff]
	return r, ok
}

// stashingFactor models a consumer's daemon-backed login: it allocates upstream
// state during Verify and stashes it.
type stashingFactor struct {
	subject, password string
	alloc             func() string
}

func (stashingFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (f stashingFactor) Claim(r *auth.Request) string {
	if r == nil {
		return ""
	}
	return r.Credentials["subject"].Reveal()
}
func (f stashingFactor) Verify(ctx context.Context, r *auth.Request) (auth.Contribution, error) {
	if r.Credentials["subject"].Reveal() != f.subject ||
		r.Credentials["password"].Reveal() != f.password {
		return auth.Contribution{}, auth.Reason("test: refused")
	}
	// The allocation happens HERE because this is the only place holding the
	// credential — and it goes in the request's slot, not a shared key.
	StashFromContext(ctx).Set(f.alloc())
	return auth.Contribution{Method: "test", Subject: f.subject, IssuedAt: time.Now()}, nil
}

func handoffHandler(t *testing.T, p *park, opts ...ManagerOption) (*Handler, *Manager, *token.MemStore) {
	t.Helper()
	store := token.NewMemStore(64)
	tracker, err := auth.NewMemTracker(64, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	loginPolicy, err := PasswordPolicyExample(
		stashingFactor{subject: "alice", password: "pw", alloc: func() string {
			n++
			return "upstream-" + string(rune('A'+n-1))
		}},
		tracker, contextualFactor{allow: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}

	base := []ManagerOption{
		OnHandoffUnused(func(h string, r HandoffReason) { p.release(h, r) }),
	}
	m, err := NewManager(func(_ *Backend, info *SessionInfo) Runner {
		if info == nil || info.Identity == nil {
			t.Error("the factory received no identity")
			return newFakeApp()
		}
		// The claim: this is what a consumer does with the handoff.
		if info.Handoff != "" {
			if _, ok := p.claim(info.Handoff); !ok {
				t.Errorf("factory could not claim handoff %q", info.Handoff)
			}
		}
		return newFakeApp()
	}, append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m,
		HandlerLogger(nopLogger{}),
		OnLogin(func(handoff string, id *auth.Identity, st *Stash) error {
			v := st.Take()
			if v == nil {
				return errors.New("nothing was stashed during verification")
			}
			// Published through CommitPark, which is the contract for a consumer
			// parking by hand: the write happens while the admission reservation's
			// own lock is held, so the entry cannot land on a slot that has already
			// been reclaimed. A hook that writes to its park directly gets its slot
			// back immediately and its entry goes unaccounted.
			if !st.CommitPark(func() { p.hold(handoff, v.(string)) }) {
				return errors.New("the admission reservation lapsed; not parking")
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return h, m, store
}

func login(t *testing.T, h *Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	var out loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Ticket == "" {
		t.Fatal("no ticket")
	}
	return out.Ticket
}

// Path 1 + 2: a login parks, and a Create CLAIMS.
func TestHandoff_CreateClaims(t *testing.T) {
	t.Parallel()
	p := newPark()
	h, m, _ := handoffHandler(t, p)

	ticket := login(t, h)
	if p.outstanding() != 1 {
		t.Fatalf("%d parked after login, want 1", p.outstanding())
	}

	c := &fakeConn{block: make(chan struct{})}
	msg := helloMsg()
	msg.Ticket = ticket
	c.push(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = h.loop.serve(ctx, c, handshakeReq("https://tui.example.test")) }()

	waitFor(t, func() bool { return m.Len() == 1 }, "the session to come up")
	if p.outstanding() != 0 {
		t.Errorf("%d parked after a Create, want 0 — the factory did not claim it", p.outstanding())
	}
	if len(p.claimed) != 1 || p.claimed[0] != HandoffID(ticket) {
		t.Errorf("claimed = %v, want the login's handoff", p.claimed)
	}
}

// Path 3: a REATTACH runs no factory, so nothing claims that login's handoff.
// This is what leaked before handoffs were released, and is the reason the
// release hook exists.
func TestHandoff_ReattachReleases(t *testing.T) {
	t.Parallel()
	p := newPark()
	h, m, _ := handoffHandler(t, p)

	// A live session to reattach to.
	first := login(t, h)
	c1 := &fakeConn{block: make(chan struct{})}
	m1 := helloMsg()
	m1.Ticket = first
	c1.push(m1)
	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() { _ = h.loop.serve(ctx1, c1, handshakeReq("https://tui.example.test")) }()
	// Wait for the READY MESSAGE, not for the manager's count: the session is
	// registered before the ready frame is written, so m.Len()==1 does not yet
	// mean the id has reached the client.
	sessionID := waitForSession(t, c1)
	// The first connection goes away, leaving the session detached and resumable.
	cancel1()
	waitFor(t, func() bool {
		s, ok := m.Get(sessionID)
		if !ok {
			return false
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.attached
	}, "the first connection to detach")

	// A fresh login, then a REATTACH. The new login parked; the reattach runs no
	// factory; the parked entry must be released, not left behind.
	second := login(t, h)
	if p.outstanding() != 1 {
		t.Fatalf("%d parked after the reconnect login, want 1", p.outstanding())
	}
	c2 := &fakeConn{block: make(chan struct{})}
	m2 := helloMsg()
	m2.Ticket = second
	m2.Session = sessionID
	c2.push(m2)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	go func() { _ = h.loop.serve(ctx2, c2, handshakeReq("https://tui.example.test")) }()

	waitFor(t, func() bool { return p.outstanding() == 0 }, "the reattach to release the handoff")
	r, ok := p.reason(HandoffID(second))
	if !ok {
		t.Fatal("the reattached login's handoff was never released — it is leaked")
	}
	if r != ReattachedExisting {
		t.Errorf("reason = %v, want ReattachedExisting", r)
	}
	// And the session was resumed, not replaced.
	if m.Len() != 1 {
		t.Errorf("%d sessions, want the original resumed", m.Len())
	}
}

// Path 4: authentication succeeded but the attach failed, so the handoff is dead.
func TestHandoff_FailedAttachReleases(t *testing.T) {
	t.Parallel()
	p := newPark()
	// A cap of 1, already full, so the next Create fails with ErrSessionLimit.
	h, m, _ := handoffHandler(t, p, MaxSessions(1))
	if _, err := m.Create(context.Background(), &auth.Identity{Subject: "someone"}, hello()); err != nil {
		t.Fatal(err)
	}

	ticket := login(t, h)
	if p.outstanding() != 1 {
		t.Fatalf("%d parked, want 1", p.outstanding())
	}
	c := &fakeConn{}
	msg := helloMsg()
	msg.Ticket = ticket
	c.push(msg)
	if err := h.loop.serve(context.Background(), c, handshakeReq("https://tui.example.test")); err == nil {
		t.Fatal("the attach should have failed on the session limit")
	}
	r, ok := p.reason(HandoffID(ticket))
	if !ok {
		t.Fatal("a failed attach leaked its handoff")
	}
	if r != AttachFailed {
		t.Errorf("reason = %v, want AttachFailed", r)
	}
}

// An OnLogin that cannot record the login must FAIL it: returning a ticket for
// state that does not exist hands out a credential for nothing.
func TestHandoff_OnLoginErrorFailsTheLogin(t *testing.T) {
	t.Parallel()
	p := newPark()
	h, _, store := handoffHandler(t, p)
	h.onLogin = func(string, *auth.Identity, *Stash) error {
		return errors.New("park is broken")
	}

	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code == http.StatusOK {
		t.Fatal("a login whose state could not be recorded returned success")
	}
	if strings.Contains(rec.Body.String(), "ticket") {
		t.Error("a ticket was returned for state that was never parked")
	}
	_ = store
}

// A reconnect at a FULL session cap must still be able to log in. This
// is the deadlock that counting parked handoffs against MaxSessions produces.
func TestHandoff_ReconnectAtFullSessionCap(t *testing.T) {
	t.Parallel()
	p := newPark()
	h, m, _ := handoffHandler(t, p, MaxSessions(1))

	ticket := login(t, h)
	c := &fakeConn{block: make(chan struct{})}
	msg := helloMsg()
	msg.Ticket = ticket
	c.push(msg)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = h.loop.serve(ctx, c, handshakeReq("https://tui.example.test")) }()
	waitFor(t, func() bool { return m.Len() == 1 }, "the session")
	cancel()

	// The cap is full. A reconnect must log in FIRST, which is only possible
	// because parked logins have their own budget.
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("a reconnect could not log in at a full session cap: %d %s — the two "+
			"budgets of §2.12.4 have collapsed into one", rec.Code, rec.Body.String())
	}
}

// The parked-login budget is bounded, and a refused login does not consume a slot.
func TestHandoff_PendingLoginBudget(t *testing.T) {
	t.Parallel()
	p := newPark()
	h, _, _ := handoffHandler(t, p)
	h.pending = newGate(3)

	// A REFUSED login must not consume a slot. Checked with the budget NOT full,
	// because the budget is judged BEFORE the credential — see below.
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"wrong"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong password returned %d, want 401", rec.Code)
	}
	if got := h.pending.pending(); got != 0 {
		t.Fatalf("a failed login consumed %d slots, want 0", got)
	}

	for i := range 3 {
		if _, err := tryLogin(h); err != nil {
			t.Fatalf("login %d: %v", i, err)
		}
	}
	if got := h.pending.pending(); got != 3 {
		t.Fatalf("pending = %d after 3 parked logins, want 3", got)
	}
	if _, err := tryLogin(h); err == nil {
		t.Fatal("a fourth login was accepted past the pending budget")
	}

	// With the budget full, EVERY login is refused with 503 — including one with a
	// wrong password. That ordering is deliberate: no credential is verified for a
	// request that will be refused anyway, which also means a full park cannot be
	// used as a credential oracle.
	rec = httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"wrong"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503: the budget is judged before the credential", rec.Code)
	}
}

func tryLogin(h *Handler) (string, error) {
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		return "", errors.New(rec.Body.String())
	}
	var out loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		return "", err
	}
	return out.Ticket, nil
}

// --- peer binding -----------------------------------------------------------

func TestBindPeer(t *testing.T) {
	t.Parallel()
	id := &auth.Identity{Subject: "alice"}

	t.Run("on: a different peer is refused and the session terminated", func(t *testing.T) {
		m, _ := manager(t, BindPeer(true))
		s, err := m.CreateFor(context.Background(), id, hello(),
			SessionInfo{Identity: id, Peer: "203.0.113.7"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.AttachFrom(s.ID(), id, hello(), "198.51.100.9"); !errors.Is(err, ErrPeerChanged) {
			t.Fatalf("err = %v, want ErrPeerChanged", err)
		}
		// Terminated, not merely refused: if the address changed because a
		// credential was stolen, the session is what the thief wants.
		select {
		case <-s.Done():
		case <-time.After(3 * time.Second):
			t.Error("the session survived a peer mismatch")
		}
	})

	t.Run("on: the same peer reattaches", func(t *testing.T) {
		m, _ := manager(t, BindPeer(true))
		s, err := m.CreateFor(context.Background(), id, hello(),
			SessionInfo{Identity: id, Peer: "203.0.113.7"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.AttachFrom(s.ID(), id, hello(), "203.0.113.7"); err != nil {
			t.Errorf("the original peer was refused: %v", err)
		}
	})

	t.Run("off by default: an address change is ignored", func(t *testing.T) {
		m, _ := manager(t)
		s, err := m.CreateFor(context.Background(), id, hello(),
			SessionInfo{Identity: id, Peer: "203.0.113.7"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.AttachFrom(s.ID(), id, hello(), "198.51.100.9"); err != nil {
			t.Errorf("binding is off, so the address must not matter: %v", err)
		}
	})

	t.Run("the loopback no-op is real", func(t *testing.T) {
		// Under the documented SSH local-forward every connection arrives
		// from 127.0.0.1, so binding binds to a constant. Asserted so nobody
		// deploys behind a forward believing this protects them.
		m, _ := manager(t, BindPeer(true))
		s, err := m.CreateFor(context.Background(), id, hello(),
			SessionInfo{Identity: id, Peer: "127.0.0.1"})
		if err != nil {
			t.Fatal(err)
		}
		// A different browser, same forward: indistinguishable.
		if _, err := m.AttachFrom(s.ID(), id, hello(), "127.0.0.1"); err != nil {
			t.Errorf("two clients behind one forward are indistinguishable, so this "+
				"must succeed — that is the no-op: %v", err)
		}
	})
}

// waitForSession blocks until the connection has been sent a ready frame and
// returns the session id it carries.
func waitForSession(t *testing.T, c *fakeConn) string {
	t.Helper()
	var id string
	waitFor(t, func() bool {
		for _, msg := range c.messages() {
			if msg.T == msgReady {
				id = msg.Session
				return true
			}
		}
		return false
	}, "a ready frame carrying a session id")
	if id == "" {
		t.Fatal("the ready frame carried no session id")
	}
	return id
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

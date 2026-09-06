package web

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

// fakeApp blocks until its context ends, which is what a real App does.
type fakeApp struct {
	started chan struct{}
	ret     error
	once    sync.Once
}

func (f *fakeApp) Run(ctx context.Context) error {
	f.once.Do(func() { close(f.started) })
	<-ctx.Done()
	return f.ret
}

func newFakeApp() *fakeApp { return &fakeApp{started: make(chan struct{})} }

func identity(subject string) *auth.Identity {
	return &auth.Identity{Subject: subject}
}

func manager(t *testing.T, opts ...ManagerOption) (*Manager, *[]*fakeApp) {
	t.Helper()
	var mu sync.Mutex
	var apps []*fakeApp
	m, err := NewManager(func(*Backend, *SessionInfo) Runner {
		app := newFakeApp()
		mu.Lock()
		apps = append(apps, app)
		mu.Unlock()
		return app
	}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	return m, &apps
}

// THE invariant, made structural: no App exists before authentication.
//
// The signature requires an *auth.Identity, so an unauthenticated call cannot be
// written — which is a stronger guarantee than an ordering rule a later refactor
// can quietly invert.
func TestManager_RefusesToCreateWithoutAnIdentity(t *testing.T) {
	t.Parallel()
	built := 0
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { built++; return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]*auth.Identity{
		"nil identity":  nil,
		"empty subject": {},
		"blank subject": {Subject: ""},
	} {
		if _, err := m.Create(context.Background(), id, hello()); !errors.Is(err, ErrNoIdentity) {
			t.Errorf("%s: err = %v, want ErrNoIdentity", name, err)
		}
	}
	if built != 0 {
		t.Fatalf("the AppFactory ran %d times without an authenticated identity", built)
	}
	if m.Len() != 0 {
		t.Errorf("%d sessions exist", m.Len())
	}
}

func TestNewManager_RequiresAFactory(t *testing.T) {
	t.Parallel()
	if _, err := NewManager(nil); err == nil {
		t.Error("a manager with no AppFactory would accept authenticated attaches and serve " +
			"nothing, which is worse than failing at startup")
	}
}

// An authenticated user must not be able to attach to somebody else's session by
// guessing its id. Authentication establishes that you are somebody; it does not
// establish that you are the right somebody.
func TestManager_AttachChecksThePrincipal(t *testing.T) {
	t.Parallel()
	m, _ := manager(t)
	s, err := m.Create(context.Background(), identity("alice"), hello())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Attach(s.ID(), identity("bob"), hello()); !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("err = %v, want ErrSubjectMismatch: bob attached to alice's session", err)
	}
	// The owner can.
	if _, err := m.Attach(s.ID(), identity("alice"), hello()); err != nil {
		t.Errorf("the owner was refused: %v", err)
	}
	// And an unauthenticated attach is impossible to express.
	if _, err := m.Attach(s.ID(), nil, hello()); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("err = %v, want ErrNoIdentity", err)
	}
}

// The session cap is enforced, and it refuses rather than evicting
// somebody else's live session — otherwise the cap becomes a DoS tool against
// existing users.
func TestManager_SessionCapRefusesRatherThanEvicts(t *testing.T) {
	t.Parallel()
	m, apps := manager(t, MaxSessions(2))

	first, err := m.Create(context.Background(), identity("alice"), hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), identity("bob"), hello()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), identity("carol"), hello()); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("err = %v, want ErrSessionLimit", err)
	}
	if m.Len() != 2 {
		t.Errorf("%d sessions, want 2", m.Len())
	}
	// The existing sessions are untouched.
	select {
	case <-first.Done():
		t.Error("an existing session was torn down to make room for a new one")
	default:
	}
	if n := len(*apps); n != 2 {
		t.Errorf("%d Apps built, want 2: the refused session must not have built one", n)
	}
}

// A detached session survives the idle window so a flaky network does not
// destroy work, then is evicted with full teardown.
func TestManager_IdleEvictionAfterDetach(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m, _ := manager(t, IdleTimeout(time.Minute))
	m.now = func() time.Time { return now }

	s, err := m.Create(context.Background(), identity("alice"), hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), identity("alice"), hello()); err != nil {
		t.Fatal(err)
	}

	// An ATTACHED session is never idle-evicted: the user is looking at it, and
	// "idle" means nobody is connected, not that nobody has typed.
	now = now.Add(time.Hour)
	m.Evict()
	if m.Len() != 1 {
		t.Fatal("an attached session was evicted for idleness")
	}

	m.Detach(s.ID(), s.Lease())
	// Inside the window, it survives.
	m.Evict()
	if m.Len() != 1 {
		t.Fatal("a session was evicted before its idle window elapsed")
	}
	// Past the window, it goes.
	now = now.Add(2 * time.Minute)
	m.Evict()
	if m.Len() != 0 {
		t.Fatalf("%d sessions survived eviction", m.Len())
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("eviction did not tear the session down")
	}
	if _, err := s.backend.Size(); !errors.Is(err, ErrNotStarted) && s.backend.Err() == nil {
		// Stop must have run: Events is closed.
		if _, open := <-s.backend.Events(); open {
			t.Error("the backend was not stopped by eviction")
		}
	}
}

// After disconnect and after eviction, Stop has run and goroutines
// have exited.
func TestManager_NoGoroutineLeak(t *testing.T) {
	// Not parallel: it counts goroutines.
	settle := func() {
		for range 50 {
			runtime.Gosched()
			time.Sleep(2 * time.Millisecond)
		}
	}
	settle()
	before := runtime.NumGoroutine()

	now := time.Now()
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() },
		IdleTimeout(time.Minute), MaxSessions(32))
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return now }

	var sessions []*Session
	for i := range 20 {
		s, err := m.Create(context.Background(), identity(fmt.Sprintf("user%d", i)), hello())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Attach(s.ID(), identity(fmt.Sprintf("user%d", i)), hello()); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, s)
	}
	// Half disconnect and are evicted; half are shut down.
	for i, s := range sessions {
		if i%2 == 0 {
			m.Detach(s.ID(), s.Lease())
		}
	}
	now = now.Add(time.Hour)
	m.Evict()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		select {
		case <-s.Done():
		case <-time.After(2 * time.Second):
			t.Fatalf("session %s did not exit", idPrefix(s.ID()))
		}
		if _, open := <-s.backend.Events(); open {
			t.Errorf("session %s: backend not stopped", idPrefix(s.ID()))
		}
	}

	settle()
	after := runtime.NumGoroutine()
	if after > before+2 {
		t.Errorf("goroutines: %d before, %d after 20 sessions — leak", before, after)
	}
	if m.Len() != 0 {
		t.Errorf("%d sessions survived shutdown", m.Len())
	}
}

// Teardown must run on every exit path, including an App that returns an error
// or panics: the alternative is a leaked grid and a live goroutine holding an
// authenticated identity.
func TestManager_TeardownRunsWhenTheAppFails(t *testing.T) {
	t.Parallel()
	boom := errors.New("app exploded")
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return &failingApp{err: boom} })
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.Create(context.Background(), identity("alice"), hello())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a failing App did not tear the session down")
	}
	if !errors.Is(s.Err(), boom) {
		t.Errorf("Err = %v, want the App's error", s.Err())
	}
	if _, open := <-s.backend.Events(); open {
		t.Error("Stop did not run after the App failed")
	}
	if m.Len() != 0 {
		t.Errorf("%d sessions remain after the App exited", m.Len())
	}
}

type failingApp struct{ err error }

func (f *failingApp) Run(context.Context) error { return f.err }

// A stale session must not deny a live user: eviction runs before the cap is
// judged.
func TestManager_StaleSessionDoesNotDenyANewOne(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m, _ := manager(t, MaxSessions(1), IdleTimeout(time.Minute))
	m.now = func() time.Time { return now }

	s, err := m.Create(context.Background(), identity("alice"), hello())
	if err != nil {
		t.Fatal(err)
	}
	m.Detach(s.ID(), s.Lease())
	now = now.Add(time.Hour)

	// The cap is 1 and a session exists, but it is dead. A refusal here would
	// mean one abandoned browser tab locks everyone out until a sweep runs.
	if _, err := m.Create(context.Background(), identity("bob"), hello()); err != nil {
		t.Fatalf("a stale session denied a live user: %v", err)
	}
}

func TestManager_UnknownSessionAndShutdownBehavior(t *testing.T) {
	t.Parallel()
	m, _ := manager(t)
	if _, err := m.Attach("nope", identity("alice"), hello()); !errors.Is(err, ErrUnknownSession) {
		t.Errorf("err = %v, want ErrUnknownSession", err)
	}
	// Detach and Close on an unknown id are no-ops, not panics: a client can
	// disconnect after its session was already evicted.
	m.Detach("nope", 1)
	m.Close("nope")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	// After shutdown, no new session may start.
	if _, err := m.Create(context.Background(), identity("alice"), hello()); !errors.Is(err, ErrStopped) {
		t.Errorf("err = %v, want ErrStopped after shutdown", err)
	}
}

// Session ids must be high-entropy and unique: a guessable handle plus any
// credential is a shortcut to somebody else's screen.
func TestSessionIDs(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, 1000)
	var length int
	for range 1000 {
		id, err := randomID()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
		if length == 0 {
			length = len(id)
		} else if len(id) != length {
			t.Errorf("id length varies: %d then %d", length, len(id))
		}
		for _, c := range id {
			// URL-safe and log-safe without escaping.
			ok := c == '-' || c == '_' ||
				(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
			if !ok {
				t.Fatalf("id %q contains %q, which is not URL-safe", id, c)
			}
		}
	}
	if length < 40 {
		t.Errorf("id length %d is too short for %d bytes of entropy", length, sessionIDBytes)
	}
}

// The audit record must carry the principal and a correlatable session handle
// and never a credential — and only a PREFIX of the id, so a log is not a
// source of reusable handles.
func TestSessionAudit_Rendering(t *testing.T) {
	t.Parallel()
	full := "0123456789abcdefghij"
	got := sessionAudit{Kind: "attached", Subject: "alice", ID: full, Reason: "idle"}.String()
	want := "web session attached subject=alice session=01234567… reason=idle"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if len(got) > 0 && contains(got, full) {
		t.Error("the full session id reached the log")
	}
	if got := (sessionAudit{Kind: "created"}).String(); got != "web session created" {
		t.Errorf("got %q", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Concurrent create/attach/detach/evict must be race-free: a browser closing a
// laptop lid mid-attach is ordinary.
func TestManager_ConcurrentLifecycle(t *testing.T) {
	t.Parallel()
	m, _ := manager(t, MaxSessions(64), IdleTimeout(time.Millisecond))
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			subject := fmt.Sprintf("user%d", i)
			s, err := m.Create(context.Background(), identity(subject), hello())
			if err != nil {
				return // the cap is legitimately reachable here
			}
			_, _ = m.Attach(s.ID(), identity(subject), hello())
			m.Detach(s.ID(), s.Lease())
			m.Evict()
			m.Close(s.ID())
		}(i)
	}
	wg.Wait()
}

// A session whose App exits immediately must still be attachable by the caller
// that created it.
//
// CreateFor registers the session, starts the App on its own goroutine, and
// returns — and that goroutine's teardown DROPS the session from the registry.
// A caller doing create-then-attach is therefore racing its own App, and an App
// that exits promptly (crashed on startup, refused its config, panicked) wins:
// the session is gone before the attach, which reports ErrUnknownSession for a
// session the caller has just been handed. On the wire that is a 1008 policy
// close with no frames sent, indistinguishable from a refusal.
//
// Made deterministic by WAITING for the App to finish rather than by hoping to
// lose the race: <-Done is the guarantee that teardown has already run. The
// production symptom was a 1.2% flake in TestSSO_EndToEnd_AppPanicIsContained.
func TestManager_CreateAttached_SurvivesAnAppThatExitsImmediately(t *testing.T) {
	t.Parallel()
	m, err := NewManager(func(*Backend, *SessionInfo) Runner {
		return runnerFunc(func(context.Context) error { return nil }) // exits at once
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})

	id := identity("alice")
	s, err := m.CreateAttachedFor(context.Background(), id, hello(),
		SessionInfo{Identity: id})
	if err != nil {
		t.Fatalf("create+attach for an App that exits immediately: %v", err)
	}
	// The App is free to have finished already — that is the point. What must
	// hold is that this connection was attached before it could.
	<-s.Done()
	if s.Lease() == 0 {
		t.Error("the session was never attached, so the creator holds no lease")
	}
}

// The two-step it replaces, kept as the witness for WHY the method exists. If
// this ever stops failing, the window has closed by some other route and
// CreateAttachedFor's justification needs re-reading rather than assuming.
func TestManager_CreateThenAttach_LosesToAnAppThatExitsImmediately(t *testing.T) {
	t.Parallel()
	m, err := NewManager(func(*Backend, *SessionInfo) Runner {
		return runnerFunc(func(context.Context) error { return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})

	id := identity("alice")
	s, err := m.CreateFor(context.Background(), id, hello(), SessionInfo{Identity: id})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Done() // the App has finished and teardown has dropped the session
	if _, err := m.AttachFrom(s.ID(), id, hello(), ""); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("err = %v, want ErrUnknownSession — if the two-step no longer "+
			"loses this race, re-read why CreateAttachedFor exists", err)
	}
}

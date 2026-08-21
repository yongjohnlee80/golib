package web

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/logger"
)

// Session-layer errors.
var (
	// ErrSessionLimit means the concurrent-session cap is reached. It is a
	// refusal rather than an eviction of somebody else's live session: dropping
	// an active user to admit a new one would make the cap a denial-of-service
	// tool against existing users.
	ErrSessionLimit = errors.New("web: concurrent session limit reached")

	// ErrNoIdentity means a session was requested without an authenticated
	// identity. It is a PROGRAMMING error, and the signature exists to make it
	// one: see [Manager.Create].
	ErrNoIdentity = errors.New("web: refusing to create a session without an authenticated identity")

	// ErrUnknownSession means the id does not name a live session.
	ErrUnknownSession = errors.New("web: unknown session")

	// ErrSubjectMismatch means an attach presented a different principal than
	// the session was created for.
	ErrSubjectMismatch = errors.New("web: session belongs to a different principal")
)

// Runner is the application a session drives. [tui.App] satisfies it.
//
// An interface rather than a concrete *tui.App so the session lifecycle can be
// tested without a terminal, a component tree or a real frame loop — the parts
// under test here are teardown and eviction, and a real App would only add ways
// for those tests to be flaky.
type Runner interface {
	Run(ctx context.Context) error
}

// AppFactory builds the application for a new session.
//
// It receives the session's [Backend] and is called ONLY after authentication
// has succeeded — see [Manager.Create], which cannot be reached without an
// identity.
type AppFactory func(*Backend) Runner

// Manager owns session lifecycle: create, attach, detach, evict, shut down
// (ADR-0009 §2.8).
//
// Sessions are the real work of this package. Rendering a grid is
// straightforward; making sure that a browser closing its laptop lid does not
// leak an App, a grid, a goroutine and an authenticated identity is not.
type Manager struct {
	factory  AppFactory
	log      logger.Logger
	maxSess  int
	idle     time.Duration
	now      func() time.Time
	newID    func() (string, error)
	backends []Option

	mu       sync.Mutex
	sessions map[string]*Session
	closed   bool
}

// ManagerOption configures a [Manager].
type ManagerOption func(*Manager)

// MaxSessions caps concurrent sessions (default 8).
//
// A cap exists because each session is an App, a grid and goroutines, all
// allocated on behalf of a remote party. Without one, the cost of a session is
// paid by the server and chosen by the client.
func MaxSessions(n int) ManagerOption {
	return func(m *Manager) {
		if n > 0 {
			m.maxSess = n
		}
	}
}

// IdleTimeout sets how long a DETACHED session survives before eviction
// (default 5 minutes).
//
// The window exists so a flaky network does not destroy the user's work: the App
// may outlive a brief disconnect and the reconnecting client resyncs. It is not
// a session lifetime — an attached session is never idle-evicted, because the
// user is looking at it.
func IdleTimeout(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.idle = d
		}
	}
}

// ManagerLogger sets the log sink.
func ManagerLogger(l logger.Logger) ManagerOption {
	return func(m *Manager) {
		if l != nil {
			m.log = l
		}
	}
}

// BackendOptions are applied to each session's [Backend].
func BackendOptions(opts ...Option) ManagerOption {
	return func(m *Manager) { m.backends = opts }
}

// Defaults for a [Manager].
const (
	DefaultMaxSessions = 8
	DefaultIdleTimeout = 5 * time.Minute
)

// NewManager builds a session manager.
//
// A nil factory is refused: a manager that cannot build an App would accept
// authenticated attaches and then serve nothing, which is worse than failing at
// startup.
func NewManager(factory AppFactory, opts ...ManagerOption) (*Manager, error) {
	if factory == nil {
		return nil, errors.New("web.NewManager: an AppFactory is required")
	}
	m := &Manager{
		factory:  factory,
		log:      logger.Nop{},
		maxSess:  DefaultMaxSessions,
		idle:     DefaultIdleTimeout,
		now:      time.Now,
		newID:    randomID,
		sessions: make(map[string]*Session),
	}
	for _, o := range opts {
		if o != nil {
			o(m)
		}
	}
	return m, nil
}

// Session is one browser session: one App, one Backend, one principal.
type Session struct {
	id      string
	subject string
	backend *Backend

	mu       sync.Mutex
	attached bool
	lastSeen time.Time
	created  time.Time

	cancel context.CancelFunc
	done   chan struct{}
	runErr error
}

// ID is the session identifier. It is a high-entropy random value, not a
// counter: a guessable id plus a stolen credential is a shortcut to somebody
// else's screen.
func (s *Session) ID() string { return s.id }

// Subject is the authenticated principal this session belongs to.
func (s *Session) Subject() string { return s.subject }

// Backend is the session's [Backend].
func (s *Session) Backend() *Backend { return s.backend }

// Done closes when the session's App has exited and teardown has run.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err reports why the App exited.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runErr
}

// Create starts a session for an authenticated identity.
//
// # The invariant, made structural
//
// The signature REQUIRES an *auth.Identity. No App is created and no input is
// accepted until Policy.Authenticate has succeeded, on every branch — and the
// way to guarantee that is to make an unauthenticated call impossible to write,
// rather than to document an ordering that a later refactor can quietly invert.
// A nil identity is refused.
func (m *Manager) Create(ctx context.Context, id *auth.Identity, h Hello) (*Session, error) {
	if id == nil || id.Subject == "" {
		return nil, ErrNoIdentity
	}
	if !h.valid() {
		return nil, errors.New("web: client hello has no usable size or font metrics")
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrStopped
	}
	// Evict what is already dead before judging the cap, so a stale session does
	// not deny a live user.
	m.evictLocked(m.now())
	if len(m.sessions) >= m.maxSess {
		m.mu.Unlock()
		logger.Notice(m.log, sessionAudit{Kind: "refused", Subject: id.Subject, Reason: "session limit"})
		return nil, ErrSessionLimit
	}
	sid, err := m.newID()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}

	backend := New(m.backends...)
	runCtx, cancel := context.WithCancel(ctx)
	s := &Session{
		id:       sid,
		subject:  id.Subject,
		backend:  backend,
		created:  m.now(),
		lastSeen: m.now(),
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	m.sessions[sid] = s
	m.mu.Unlock()

	app := m.factory(backend)
	if app == nil {
		m.drop(sid)
		cancel()
		close(s.done)
		return nil, errors.New("web: AppFactory returned no application")
	}

	go func() {
		defer close(s.done)
		// Teardown runs on EVERY exit path — clean return, error, panic in the
		// App — because the alternative is a leaked grid and a live goroutine
		// holding an authenticated identity.
		defer func() {
			_ = backend.Stop()
			cancel()
			m.drop(sid)
			logger.Info(m.log, sessionAudit{Kind: "closed", Subject: s.subject, ID: sid})
		}()
		err := app.Run(runCtx)
		s.mu.Lock()
		s.runErr = err
		s.mu.Unlock()
	}()

	logger.Info(m.log, sessionAudit{Kind: "created", Subject: id.Subject, ID: sid})
	return s, nil
}

// Attach binds a client to an existing session.
//
// # Why the identity is required here too
//
// Every attach re-runs the completed policy from scratch (§2.8), so this also
// takes an identity — and it checks that the principal MATCHES the session's.
// Without that check, any authenticated user could attach to any session id and
// look at somebody else's screen; authentication would establish that you are
// somebody, not that you are the right somebody.
func (m *Manager) Attach(sessionID string, id *auth.Identity, h Hello) (*Session, error) {
	if id == nil || id.Subject == "" {
		return nil, ErrNoIdentity
	}
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil, ErrUnknownSession
	}
	if s.subject != id.Subject {
		logger.Notice(m.log, sessionAudit{Kind: "denied", Subject: id.Subject, ID: sessionID,
			Reason: "principal does not own this session"})
		// Deliberately the same error a stranger's id produces, so probing
		// cannot distinguish "exists but not yours" from "does not exist".
		return nil, ErrSubjectMismatch
	}
	if err := s.backend.Attach(h); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.attached = true
	s.lastSeen = m.now()
	s.mu.Unlock()
	logger.Info(m.log, sessionAudit{Kind: "attached", Subject: id.Subject, ID: sessionID})
	return s, nil
}

// Detach records that a client disconnected. The session survives for
// [IdleTimeout] so a reconnect can resync.
func (m *Manager) Detach(sessionID string) {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return
	}
	s.mu.Lock()
	s.attached = false
	s.lastSeen = m.now()
	s.mu.Unlock()
	s.backend.Detach()
	logger.Info(m.log, sessionAudit{Kind: "detached", ID: sessionID, Subject: s.subject})
}

// Close ends a session immediately.
func (m *Manager) Close(sessionID string) {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return
	}
	s.cancel()
	_ = s.backend.Stop()
}

// Evict ends every session idle longer than the timeout. Safe to call
// repeatedly; a manager with a ticker calls it, and so do tests.
func (m *Manager) Evict() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evictLocked(m.now())
}

// evictLocked ends timed-out sessions. Caller holds the lock.
//
// An ATTACHED session is never evicted for idleness: the user is looking at it,
// and "idle" here means "nobody is connected", not "nobody has typed".
func (m *Manager) evictLocked(now time.Time) {
	for id, s := range m.sessions {
		s.mu.Lock()
		attached, last := s.attached, s.lastSeen
		s.mu.Unlock()
		if attached || now.Sub(last) < m.idle {
			continue
		}
		logger.Info(m.log, sessionAudit{Kind: "evicted", ID: id, Subject: s.subject,
			Reason: "idle"})
		s.cancel()
		_ = s.backend.Stop()
		// The run goroutine's deferred teardown removes it from the map; this
		// only stops it being counted against the cap in the meantime.
		delete(m.sessions, id)
	}
}

// Shutdown ends every session and waits for their Apps to exit.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	live := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		live = append(live, s)
	}
	m.mu.Unlock()

	for _, s := range live {
		s.cancel()
		_ = s.backend.Stop()
	}
	for _, s := range live {
		select {
		case <-s.done:
		case <-ctx.Done():
			return fmt.Errorf("web: %d session(s) did not exit: %w", len(live), ctx.Err())
		}
	}
	return nil
}

// Len reports the number of live sessions.
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// Get returns a live session by id.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *Manager) drop(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// sessionAudit is the session lifecycle audit record. Every attach, refusal and
// eviction emits one. It carries the principal and the session id and NEVER a
// credential, a ticket or any cell content.
type sessionAudit struct {
	Kind    string
	Subject string
	ID      string
	Reason  string
}

func (s sessionAudit) String() string {
	out := "web session " + s.Kind
	if s.Subject != "" {
		out += " subject=" + s.Subject
	}
	if s.ID != "" {
		// The session id is high-entropy but it is still a handle, so only a
		// prefix is logged: enough to correlate, not enough to reuse.
		out += " session=" + idPrefix(s.ID)
	}
	if s.Reason != "" {
		out += " reason=" + s.Reason
	}
	return out
}

func idPrefix(id string) string {
	const n = 8
	if len(id) <= n {
		return id
	}
	return id[:n] + "…"
}

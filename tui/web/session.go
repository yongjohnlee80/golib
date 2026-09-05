package web

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

	// ErrPeerChanged means an attach came from a different address than the one
	// that created the session, with [BindPeer] on. The session is terminated.
	ErrPeerChanged = errors.New("web: peer address changed; session terminated")

	// ErrPendingLogins means the parked-login budget is full (ADR-0009 §2.12.4).
	ErrPendingLogins = errors.New("web: too many logins awaiting an attach")
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
// It receives the session's [Backend]. On the network path it runs only after
// [auth.Policy] has succeeded; [Manager.Create] requires an identity VALUE,
// which constrains that path but not another in-process caller. See
// [Manager.Create] for where the boundary is.
// It receives a [SessionInfo] carrying the authenticated identity, the login
// handoff (if any) and the creating peer. The identity is what makes a per-user
// App possible at all; an earlier signature took only the Backend and so could
// not know who the session was for (ADR-0009 §2.12).
type AppFactory func(*Backend, *SessionInfo) Runner

// Manager owns session lifecycle: create, attach, detach, evict, shut down
// (ADR-0009 §2.8).
//
// Sessions are the real work of this package. Rendering a grid is
// straightforward; making sure that a browser closing its laptop lid does not
// leak an App, a grid, a goroutine and an authenticated identity is not.
//
// # Trust boundary
//
// The exported lifecycle methods are TRUSTED: they take an authenticated
// identity but cannot verify that it was actually authenticated, because
// auth.Identity is a plain exported struct. They are the seam a transport uses,
// and the boundary they enforce is "outside the process", not "elsewhere in the
// process". See [Manager.Create].
type Manager struct {
	// base is the root of every session's lifetime. Sessions outlive the
	// connection that created them; see Create.
	base     context.Context
	factory  AppFactory
	log      logger.Logger
	maxSess  int
	idle     time.Duration
	now      func() time.Time
	newID    func() (string, error)
	backends []Option

	bindPeer bool
	unused   func(string, HandoffReason)
	// settle returns the admission slot a parked handoff owns, once that handoff
	// can no longer be waiting for an attach. Installed by NewHandler, which owns
	// the gate; nil for a Manager used on its own.
	settle func(string)

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

// BindPeer binds a session to the peer address that created it: an attach from a
// different address is refused and the session terminated (ADR-0009 §2.13).
//
// OFF by default, for two reasons stated in the ADR and worth repeating where a
// caller will read them. Under the documented SSH local-forward every connection
// arrives from 127.0.0.1, so this binds to a constant and protects nothing. And
// it trades away the detach window: a laptop moving from wifi to cellular changes
// address and gets logged out. Turn it on for a TLS-on-a-real-address deployment,
// knowing which of its own features it weakens.
func BindPeer(on bool) ManagerOption {
	return func(m *Manager) { m.bindPeer = on }
}

// OnHandoffUnused registers the release hook for handoffs no [AppFactory] will
// claim (ADR-0009 §2.12.2).
//
// Called at most once per handoff, for a reattach or a failed attach. Without it
// a consumer that parks per-login state leaks an entry on every reconnect.
func OnHandoffUnused(fn func(handoff string, reason HandoffReason)) ManagerOption {
	return func(m *Manager) { m.unused = fn }
}

// BackendOptions are applied to each session's [Backend].
func BackendOptions(opts ...Option) ManagerOption {
	return func(m *Manager) { m.backends = slices.Clone(opts) }
}

// setQueueDepth forces the event queue capacity, so [Limits.QueueDepth] is the
// one number that decides it. Called by [NewHandler]; a BackendOptions-supplied
// EventQueue is applied first and this overrides it, because the documented limit
// must win over an incidental one.
func (m *Manager) setQueueDepth(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends = append(slices.Clone(m.backends), EventQueue(n))
}

// Defaults for a [Manager].
const (
	DefaultMaxSessions = 8
	DefaultIdleTimeout = 5 * time.Minute
)

// ErrSessionBusy means the session already has a live connection.
var ErrSessionBusy = errors.New("web: session already has a live connection")

// Start begins the periodic idle sweep and returns a stop function.
//
// Without a scheduler, eviction only ran when a Create happened to trigger it,
// so a detached session on an idle server lived forever and held its App, its
// grid and its goroutines (lector r1). The sweep interval is a quarter of the
// idle timeout, so a session is evicted within 25% of the configured window
// rather than at some arbitrary later moment.
func (m *Manager) Start() (stop func()) {
	interval := m.idle / 4
	if interval < time.Second {
		interval = time.Second
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				m.Evict()
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

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
		base:     context.Background(),
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

	peer string // the address that created it; checked when BindPeer is on

	mu       sync.Mutex
	attached bool
	lease    uint64 // increments per attach; identifies the live connection
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
// # What requiring an identity does and does not guarantee
//
// On the AUTHENTICATED NETWORK PATH, `serve` obtains its identity only from
// Policy.Authenticate, so the ordering there cannot be inverted without the
// compiler objecting, and a nil or subject-less identity is refused here.
//
// That is the whole of the guarantee. It is NOT structural against an in-process
// caller: auth.Identity is an exported struct, so any code in the binary can
// write `&auth.Identity{Subject: "root"}` and call this — the tests in this
// package do exactly that. Earlier comments called an unauthenticated call
// "impossible to write"; that was an overstatement (lector r1, and r2 because I
// left it standing in several other doc comments after narrowing one).
func (m *Manager) Create(ctx context.Context, id *auth.Identity, h Hello) (*Session, error) {
	return m.CreateFor(ctx, id, h, SessionInfo{Identity: id})
}

// CreateFor starts a session and passes info to the [AppFactory].
//
// Create delegates here with a bare identity, so a caller with no login handoff
// keeps the simpler call.
func (m *Manager) CreateFor(ctx context.Context, id *auth.Identity, h Hello, info SessionInfo) (*Session, error) {
	return m.create(ctx, id, h, info, false)
}

// CreateAttachedFor starts a session and attaches the calling connection to it
// as one step.
//
// [Manager.CreateFor] followed by [Manager.AttachFrom] looks equivalent and is
// not. CreateFor starts the App on its own goroutine, and that goroutine's
// teardown DROPS the session from the registry — so a caller doing the two-step
// is racing its own App. An App that exits promptly (crashed on startup,
// refused its config, panicked) wins that race, the session is gone before the
// attach, and AttachFrom reports [ErrUnknownSession] for a session the caller
// has just been handed. On the wire the client then gets a 1008 policy close
// with no frames sent, indistinguishable from a refusal, rather than a session
// that opens and immediately ends.
//
// This attaches BEFORE the App goroutine starts, so the window does not exist.
// The ordering difference is confined to this method: CreateFor's behaviour is
// unchanged for callers that want an unattached session.
func (m *Manager) CreateAttachedFor(ctx context.Context, id *auth.Identity, h Hello, info SessionInfo) (*Session, error) {
	return m.create(ctx, id, h, info, true)
}

// create is CreateFor, optionally attaching the caller before the App runs.
func (m *Manager) create(ctx context.Context, id *auth.Identity, h Hello, info SessionInfo, attach bool) (*Session, error) {
	if id == nil || id.Subject == "" {
		return nil, ErrNoIdentity
	}
	// The handoff is settled however this returns: on success the factory has had
	// its chance to claim, and on failure the attach path releases it. Either way
	// nothing is still waiting for it, so its admission slot goes back.
	defer m.settleHandoff(info.Handoff)
	if !h.valid() {
		return nil, errors.New("web: client hello has no usable size or font metrics")
	}
	// Checked BEFORE the lock. An earlier version checked it while holding m.mu
	// and then called m.drop, which takes m.mu again — a self-deadlock on a path
	// a cancelled connection reaches routinely (lector r2). No session exists
	// yet here, so there is nothing to drop either.
	if err := ctx.Err(); err != nil {
		return nil, err
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
	// The App's context comes from the MANAGER's lifetime, not from the
	// WebSocket that happened to create the session.
	//
	// Deriving it from the connection meant a disconnect cancelled the App
	// immediately, so the detach window of §2.8 — the whole reason a flaky
	// network does not destroy work — was unreachable: Session.Done fired the
	// moment the socket closed (lector r1). ctx is honored for the CREATE call
	// only; the session outlives it by design.
	runCtx, cancel := context.WithCancel(m.base)
	s := &Session{
		id:       sid,
		subject:  id.Subject,
		peer:     info.Peer,
		backend:  backend,
		created:  m.now(),
		lastSeen: m.now(),
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	m.sessions[sid] = s
	m.mu.Unlock()

	info.Identity = id
	app := m.factory(backend, &info)
	if app == nil {
		m.drop(sid)
		cancel()
		close(s.done)
		return nil, errors.New("web: AppFactory returned no application")
	}

	// Attached BEFORE the App goroutine starts, so its teardown cannot drop the
	// session out from under a caller that has not had its turn yet.
	if attach {
		if _, err := m.attachSession(s, id, h, info.Peer); err != nil {
			// The App goroutine has NOT started, so the teardown defer that
			// normally stops the backend will never run — this path owns the
			// cleanup outright. Same shape as the AppFactory-returned-nil case
			// above, plus the Stop that [Manager.Close] does for a session whose
			// App did start.
			m.drop(sid)
			cancel()
			_ = s.backend.Stop()
			close(s.done)
			return nil, err
		}
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
		// An App panic ends THIS SESSION, not the process.
		//
		// One process serves N users, so an unrecovered panic here took every other
		// user's session down with it — and it was reached by application code, the
		// least trustworthy code in the building. It is contained the same way
		// server.Scaffold contains a connection handler's panic, and for the same
		// reason. The panic is recorded as the session's run error, so a consumer
		// sees a failed session rather than a silently closed one.
		//
		// It is contained, not swallowed: the record names it a panic and carries
		// the value.
		defer func() {
			if rec := recover(); rec != nil {
				err := fmt.Errorf("web: the application panicked: %v", rec)
				s.mu.Lock()
				s.runErr = err
				s.mu.Unlock()
				logger.Error(m.log, err, sessionAudit{Kind: "panic",
					Subject: s.subject, ID: sid})
			}
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
	return m.AttachFrom(sessionID, id, h, "")
}

// AttachFrom binds a client to an existing session, checking the peer address
// when [BindPeer] is on.
func (m *Manager) AttachFrom(sessionID string, id *auth.Identity, h Hello, peer string) (*Session, error) {
	if id == nil || id.Subject == "" {
		return nil, ErrNoIdentity
	}
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil, ErrUnknownSession
	}
	return m.attachSession(s, id, h, peer)
}

// attachSession is AttachFrom without the registry lookup.
//
// Split out so the CREATE path can attach a session it already holds a pointer
// to, without going back through a registry the App's teardown may already have
// removed it from — see [Manager.CreateAttachedFor]. Every check below is
// therefore run identically on both paths; for a session created moments ago the
// ownership and peer checks pass trivially, and that is deliberate. One code
// path is worth more than the two lines it saves to skip them.
func (m *Manager) attachSession(s *Session, id *auth.Identity, h Hello, peer string) (*Session, error) {
	sessionID := s.id
	if s.subject != id.Subject {
		logger.Notice(m.log, sessionAudit{Kind: "denied", Subject: id.Subject, ID: sessionID,
			Reason: "principal does not own this session"})
		// Deliberately the same error a stranger's id produces, so probing
		// cannot distinguish "exists but not yours" from "does not exist".
		return nil, ErrSubjectMismatch
	}

	// PEER BINDING. Checked before the lease, so a mismatched address cannot even
	// take the connection slot. Terminating rather than merely refusing is
	// deliberate: if the address changed because a credential was stolen, the
	// session is what the thief is reaching for (§2.13).
	if m.bindPeer && s.peer != "" && peer != "" && s.peer != peer {
		logger.Notice(m.log, sessionAudit{Kind: "denied", Subject: id.Subject, ID: sessionID,
			Reason: "peer address changed"})
		s.cancel()
		_ = s.backend.Stop()
		return nil, ErrPeerChanged
	}

	// ONE connection at a time. A second attach while one is live was silently
	// accepted, so two browsers shared a grid, a cursor and an event stream and
	// the last writer won (lector r1). Concurrent takeover is an authorization
	// decision, and the answer here is no: reconnect after the first connection
	// is gone.
	s.mu.Lock()
	if s.attached {
		s.mu.Unlock()
		logger.Notice(m.log, sessionAudit{Kind: "denied", Subject: id.Subject, ID: sessionID,
			Reason: "session already has a live connection"})
		return nil, ErrSessionBusy
	}
	s.attached = true
	s.lease++
	lease := s.lease
	s.lastSeen = m.now()
	s.mu.Unlock()

	if err := s.backend.Attach(h); err != nil {
		s.mu.Lock()
		if s.lease == lease {
			s.attached = false
		}
		s.mu.Unlock()
		return nil, err
	}
	logger.Info(m.log, sessionAudit{Kind: "attached", Subject: id.Subject, ID: sessionID})
	return s, nil
}

// Lease reports the current connection generation. A caller holding a stale
// lease must not mutate the session: its connection has been replaced.
func (s *Session) Lease() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lease
}

// Detach records that the connection holding lease went away.
//
// Lease-scoped so a slow teardown cannot detach a connection that has already
// replaced it: without that, a reconnect racing the previous connection's
// deferred cleanup would be marked unattached while it was in fact live, and the
// idle sweep would then evict a session somebody is looking at.
func (m *Manager) Detach(sessionID string, lease uint64) {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return
	}
	s.mu.Lock()
	if s.lease != lease {
		s.mu.Unlock()
		return // a newer connection owns this session
	}
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

// releaseHandoff tells the consumer a parked handoff will never be claimed.
//
// At most once per handoff: the caller's park is single-claim, but calling twice
// would still be a bug worth not writing, so every call site is a terminal path.
func (m *Manager) releaseHandoff(handoff string, reason HandoffReason) {
	if handoff == "" {
		return
	}
	m.settleHandoff(handoff)
	if m.unused == nil {
		return
	}
	m.unused(handoff, reason)
}

// settleHandoff returns the admission slot the handoff owned. Idempotent, since a
// handoff can be settled by a claim, a release, or the gate's own expiry and those
// paths do not coordinate.
func (m *Manager) settleHandoff(handoff string) {
	if handoff == "" || m.settle == nil {
		return
	}
	m.settle(handoff)
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
	// Every rendered field is sanitized and bounded. Subject is the AUTHENTICATED
	// principal, which makes it tempting to treat as trusted — but "authenticated"
	// only means a factor vouched for it, and a subject can legitimately come
	// from an allowed_signers principal or a certificate CN, neither of which is
	// constrained to be free of newlines. One forged the second line of a log in
	// lector's probe.
	out := "web session " + s.Kind
	if s.Subject != "" {
		out += " subject=" + sanitizeHeader(s.Subject)
	}
	if s.ID != "" {
		// The session id is high-entropy but it is still a handle, so only a
		// prefix is logged: enough to correlate, not enough to reuse.
		out += " session=" + idPrefix(s.ID)
	}
	if s.Reason != "" {
		out += " reason=" + sanitizeHeader(s.Reason)
	}
	return out
}

func idPrefix(id string) string {
	const n = 8
	// Sanitized as well: an id normally comes from randomID and is safe, but this
	// also renders ids that arrived from a client message.
	id = sanitizeHeader(id)
	if len(id) <= n {
		return id
	}
	return id[:n] + "…"
}

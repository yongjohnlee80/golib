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

// SSO wires the whole login-handoff workflow of ADR-0009 §2.12, so a consumer
// supplies only the things that are genuinely theirs: how to allocate upstream
// state, and how to release it.
//
// # Every auth mechanism, one route
//
// golib/auth has two kinds of mechanism, and they allocate at different moments:
//
//   - [auth/password] authenticates at a LOGIN, so it can allocate while it still
//     holds the credential. That state is parked and later CLAIMED.
//   - [auth/token] (an SSH-minted or otherwise out-of-band ticket), [auth/mtls]
//     and [auth/sshkey] authenticate at the ATTACH itself. No login ran and
//     nothing was parked, so their state is PROVISIONED.
//   - [auth/ipallow] is contextual: it narrows any of the above and admits none of
//     them alone, so it never allocates anything.
//
// [SSO.Session] resolves that difference, and [SSO.Factory] hides it: one build
// function serves every mechanism.
//
// # Why this exists rather than only documentation
//
// The raw seam has four paths — park on login, claim on create, release on
// reattach, release on failed attach — plus an expiry sweep. Every one of them
// must be wired or upstream state leaks, and the commonest of them (reattach) is
// the one a consumer is least likely to think of, because nothing in the happy
// path exercises it. A protocol with four obligations described in prose is a
// protocol someone implements three-quarters of.
//
// So the obligations are structural here:
//
//   - [SSO.Options] returns BOTH hooks together, so the release side is not
//     something to remember separately. Go being Go, a caller can still discard
//     one of the two returned options or override the hooks afterwards; what the
//     API removes is having to KNOW that the second hook exists.
//   - Release is REQUIRED. [NewSSO] fails without it, so "allocated and never
//     cleaned up" is not a state this type can be constructed in.
//   - The expiry sweep is internal and calls Release, so a client that abandons a
//     login cannot leave state behind by the consumer forgetting a timer.
//   - The park's capacity also sets [MaxPendingLogins], so the two bounds cannot
//     drift into disagreeing about how many logins may be in flight.
//   - [SSO.Factory] puts the release on the App's own stack as a deferred call, so
//     it runs on every exit path including a panic.
//
// # The shape of a consumer
//
//	sso, err := web.NewSSO(web.SSOConfig[*myUpstream]{
//	    Max: 8, TTL: 30 * time.Second,
//	    Provision: func(ctx context.Context, id *auth.Identity) (*myUpstream, error) {
//	        return dial(ctx, id.Subject) // for ticket / mTLS / SSHSIG attaches
//	    },
//	    Release: func(u *myUpstream, r web.HandoffReason) { u.Logout(); u.Close() },
//	})
//
//	// In the login factor's Verify — the only place holding the credential:
//	sso.Stash(ctx, upstream)
//
//	// The app: one build function for every mechanism, release handled.
//	mgr, err := web.NewManager(
//	    sso.Factory(func(b *web.Backend, id *auth.Identity, up *myUpstream) web.Runner {
//	        return myApp(b, id, up)
//	    }),
//	    mOpt,
//	)
//
//	// Wiring, both hooks at once:
//	hOpt, mOpt := sso.Options()
//
// [SSO.Claim] remains for a consumer writing their own Runner; [SSO.Factory] is
// the form to reach for.
type SSO[T any] struct {
	ttl       time.Duration
	max       int
	release   func(T, HandoffReason)
	provision func(context.Context, *auth.Identity) (T, error)
	now       func() time.Time
	log       logger.Logger

	// settle returns the admission slot a parked handoff owns. Installed by
	// [SSO.Options], because only the park knows when its entry is actually gone:
	// the Manager returns from CreateFor as soon as the factory hands back a
	// Runner, and the claim happens later on the session's own goroutine. Settling
	// on the Manager's schedule left a window in which the gate had a free slot
	// while the park was still full, so a new login could authenticate, allocate,
	// and then be told 503 (lector r2 on PR #14).
	settle func(string)

	mu     sync.Mutex
	cond   *sync.Cond
	parked map[string]parkedEntry[T]
	closed bool
	// provisioning counts Provision calls in flight. Close waits for them, so
	// "nothing is outstanding after Close returns" is true rather than likely.
	provisioning int
	// panics counts contained Release panics, for a test and a metric.
	panics int
}

type parkedEntry[T any] struct {
	value   T
	expires time.Time
	// timer fires the expiry. One per entry rather than a package ticker: an empty
	// park then costs nothing and a lone abandoned login is still released on time.
	// The previous version only swept when ANOTHER login arrived, so on an idle
	// server one abandoned login stayed logged in upstream indefinitely — the
	// doc claimed an internal sweep that did not exist (lector r1 on PR #14).
	timer *time.Timer
}

// SSOConfig configures [NewSSO].
type SSOConfig[T any] struct {
	// Provision allocates upstream state for a session whose attach carried NO
	// login — the direct mechanisms of ADR-0001: an SSH-minted ticket, an mTLS
	// verified chain, or an SSHSIG challenge. Those authenticate at attach, so no
	// login route ran and nothing was parked.
	//
	// It also covers the stock auth/password factor, which verifies a hash and
	// knows nothing about this package: a login through it stashes nothing, so its
	// session is provisioned like any other. Without this, only a CUSTOM login
	// factor that called Stash would get upstream state, and every other mechanism
	// would need a second allocation path with its own cleanup — the "two ways to
	// do it, one of them forgotten" shape this type exists to prevent.
	//
	// A nil Provision REFUSES a direct attach rather than handing an App a zero
	// value: a nil upstream session reaching application code fails later and
	// further from the cause than a refused connection does. A consumer who wants
	// guest sessions supplies a Provision returning a guest value, so there is one
	// mechanism rather than a flag.
	Provision func(ctx context.Context, id *auth.Identity) (T, error)

	// Release cleans up parked state. REQUIRED.
	//
	// Called exactly once per parked value, on whichever path applies: a reattach,
	// a failed attach, or expiry. It is the consumer's teardown — typically a
	// logout followed by a close, in that order, because closing a transport does
	// not usually revoke a credential.
	//
	// It must not block for long: it runs on the attach path for the first two
	// reasons, and on the sweeper for the third.
	Release func(T, HandoffReason)

	// Max is the capacity of the park, and also becomes [MaxPendingLogins] so the
	// two cannot disagree. Zero uses [DefaultMaxPendingLogins].
	Max int

	// TTL bounds how long a parked value waits for an attach. Zero uses
	// [DefaultHandoffTTL].
	//
	// It exists because this package cannot know that a client walked away — it
	// never saw a connection — so an abandoned login is only detectable by time.
	//
	// Enforced by a timer per parked entry, so a single abandoned login on an
	// otherwise idle server is still released on time, and [SSO.Claim] refuses an
	// entry past its deadline.
	TTL time.Duration

	// Clock overrides the time source, for tests.
	Clock func() time.Time

	// Logger receives the events this type cannot return to anyone: a Release that
	// panicked, whose value is already gone. Defaults to a no-op sink.
	Logger logger.Logger
}

// SessionEnded is the [HandoffReason] for state released because the session it
// belonged to ended — the ordinary teardown, not a failure.
const SessionEnded HandoffReason = 3

// LoginFailed is the [HandoffReason] for state a login allocated and then could
// not park: a later factor refused, the ticket failed to mint, the park was full.
//
// Distinct from [Expired] because the two mean different things to whoever reads
// the log. Expired says a user walked away after authenticating successfully;
// LoginFailed says the login never completed at all, and the state exists only
// because verification is the one place that holds the credential.
const LoginFailed HandoffReason = 4

// ErrNoUpstream means a session needs upstream state and there is no way to get
// it: the attach parked none and no Provision is configured.
var ErrNoUpstream = errors.New("web: no parked login and no Provision, so this " +
	"session cannot be given upstream state")

// DefaultHandoffTTL is how long a parked login waits for its attach.
//
// Short on purpose: a real client attaches within a round trip of receiving its
// ticket, and every second beyond that is a window in which allocated upstream
// state sits unused.
const DefaultHandoffTTL = 30 * time.Second

// Expired is the [HandoffReason] for a login that was never attached.
const Expired HandoffReason = 2

// NewSSO builds the handoff workflow.
//
// A nil Release is refused rather than defaulted to a no-op: a park whose entries
// are dropped without cleanup is precisely the leak this type exists to prevent,
// and defaulting would make the dangerous case the quiet one.
func NewSSO[T any](cfg SSOConfig[T]) (*SSO[T], error) {
	if cfg.Release == nil {
		return nil, errors.New("web.NewSSO: Release is required — parked state that is " +
			"dropped without cleanup is the leak this type exists to prevent")
	}
	if cfg.Max < 0 {
		return nil, fmt.Errorf("web.NewSSO: negative Max %d", cfg.Max)
	}
	if cfg.TTL < 0 {
		return nil, fmt.Errorf("web.NewSSO: negative TTL %s", cfg.TTL)
	}
	s := &SSO[T]{
		ttl:       cfg.TTL,
		max:       cfg.Max,
		release:   cfg.Release,
		provision: cfg.Provision,
		now:       cfg.Clock,
		log:       cfg.Logger,
		parked:    make(map[string]parkedEntry[T]),
	}
	if s.log == nil {
		s.log = logger.Nop{}
	}
	s.cond = sync.NewCond(&s.mu)
	if s.ttl == 0 {
		s.ttl = DefaultHandoffTTL
	}
	if s.max == 0 {
		s.max = DefaultMaxPendingLogins
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s, nil
}

// Stash records state produced during credential verification.
//
// Call it from the login factor's Verify, which is the only place holding the
// credential. The value moves into the park when the ticket is minted; a consumer
// never touches the handoff itself.
//
// Returns an error outside a login request, so a factor used in the wrong place
// fails loudly instead of silently discarding the state it just allocated.
func (s *SSO[T]) Stash(ctx context.Context, v T) error {
	slot := StashFromContext(ctx)
	if slot == nil {
		return errors.New("web: Stash called outside a login request — the value would " +
			"be discarded and its upstream state leaked")
	}
	// The cleanup is registered WITH the value, so every path between here and the
	// park releases it: a later factor refusing, the ticket failing to mint, the
	// park being full. A bare Set would leave those paths leaking.
	return slot.setOwned(v, func() { s.releaseValue(v, LoginFailed) })
}

// settleHandoff returns the admission slot this handoff owned, if any. Called
// from every path that removes a parked entry — and only from those paths, so the
// slot cannot come back before the entry is gone.
func (s *SSO[T]) settleHandoff(handoff string) {
	if handoff == "" || s.settle == nil {
		return
	}
	s.settle(handoff)
}

// releaseValue runs the consumer's cleanup, contained.
//
// Contained because this is called from teardown paths — a failed login, an
// expiry, a session ending — and a panicking Release must not take the process
// with it or replace the failure that is already being handled. It is recorded
// rather than swallowed: the value is gone either way, and pretending the cleanup
// succeeded would be worse than a loud line.
func (s *SSO[T]) releaseValue(v T, r HandoffReason) {
	defer func() {
		if rec := recover(); rec != nil {
			s.mu.Lock()
			s.panics++
			s.mu.Unlock()
			// EMITTED, not merely counted. The doc says a panicking Release is
			// recorded rather than swallowed, and a private counter nobody can read
			// is swallowing it with extra steps (lector r2 on PR #14). The upstream
			// state is gone either way, so the line is the only trace there is.
			logger.Error(s.log, fmt.Errorf("web: SSO Release panicked: %v", rec),
				map[string]any{"component": "web.SSO", "event": "release panic",
					"reason": r.String()})
		}
	}()
	s.release(v, r)
}

// Claim takes the state parked by the login that created this session.
//
// Call it from the [AppFactory]. ok is false whenever no login parked anything for
// this handoff, which is a normal case rather than an error: an mTLS or
// SSH-challenge attach presents no ticket at all, and a ticket minted out of band
// derives a perfectly good handoff that was never parked against. The park is the
// authority, not the handoff string.
//
// An entry past its TTL is refused and released rather than handed over.
func (s *SSO[T]) Claim(info *SessionInfo) (T, bool) {
	var zero T
	if info == nil || info.Handoff == "" {
		return zero, false
	}
	s.mu.Lock()
	e, ok := s.parked[info.Handoff]
	if ok {
		// Single-claim: removed as it is handed over, so a repeated call cannot
		// give two sessions the same upstream state.
		delete(s.parked, info.Handoff)
		if e.timer != nil {
			e.timer.Stop()
		}
	}
	s.mu.Unlock()
	if !ok {
		return zero, false
	}
	// The entry is gone, so the admission slot it held can go back — and not one
	// instruction earlier.
	s.settleHandoff(info.Handoff)
	// An entry past its TTL is REFUSED and released, not handed over. Claim used
	// to ignore expires entirely, so a TTL shorter than the attach ticket's had no
	// effect and an App could be handed state the park had already given up on
	// (lector r1 on PR #14). Released outside the lock.
	// Deadline reached counts as expired, so an entry cannot sit exactly on its
	// deadline and be handed over.
	if !e.expires.After(s.now()) {
		s.releaseValue(e.value, Expired)
		return zero, false
	}
	return e.value, true
}

// Options returns the handler and manager wiring, together.
//
// Together on purpose. Returning them separately would let a consumer wire the
// login side and forget the release side, which is the leak of §2.12 with extra
// steps.
func (s *SSO[T]) Options() (HandlerOption, ManagerOption) {
	hOpt := func(h *Handler) {
		OnLogin(func(handoff string, _ *auth.Identity, slot *Stash) error {
			v, ok := slot.Take().(T)
			if !ok {
				// NOTHING WAS STASHED. This is the stock auth/password factor: it
				// verifies a hash and knows nothing about this package, so it cannot
				// call Stash. Refusing the login here meant the shipped password
				// mechanism returned 503 through the helper that claimed to support
				// every mechanism (lector r1 on PR #14).
				//
				// With a Provision, the login is fine: it mints its ticket, parks
				// nothing, and the attach provisions like any other direct
				// mechanism. Without one there is no way to give the session
				// upstream state at all, so the login fails HERE rather than
				// handing out a ticket that is guaranteed to fail at attach.
				if s.provision != nil {
					return nil
				}
				return errors.New("web: the login stashed no upstream state and no " +
					"Provision is configured, so the ticket it would mint could not " +
					"produce a usable session")
			}
			if err := s.hold(handoff, v); err != nil {
				// The value is ours now — Take transferred ownership — and the login
				// is about to fail, so it must not be dropped on the floor.
				s.releaseValue(v, LoginFailed)
				return err
			}
			return nil
		})(h)
		// The park's capacity IS the pending-login budget, so the two cannot
		// drift into disagreeing.
		MaxPendingLogins(s.max)(h)
		// And the park, not the Manager, returns the slots: see SSO.settle. The
		// backstop deadline covers the case where nothing settles at all — a
		// session that failed before its Runner started — so it must outlast BOTH
		// the ticket and the park's own TTL, or the gate would free a slot while
		// the park still held its entry.
		h.parkSettles = true
		if s.ttl > h.pendingHold {
			h.pendingHold = s.ttl
		}
		s.settle = func(handoff string) {
			if h.pending != nil {
				h.pending.release(handoff)
			}
		}
	}
	mOpt := OnHandoffUnused(func(handoff string, r HandoffReason) {
		s.Release(handoff, r)
	})
	return hOpt, mOpt
}

// Session returns this session's upstream state and the release that MUST run
// when the session ends.
//
// It unifies the two origins so a consumer has one path regardless of how the
// user authenticated:
//
//   - a login parked state during verification, so this CLAIMS it;
//   - nothing was parked for this attach, so this PROVISIONS it. That covers an
//     mTLS or SSHSIG attach, a ticket minted out of band, AND a login through the
//     stock auth/password factor, which verifies a hash and has no way to call
//     [SSO.Stash].
//
// Prefer [SSO.Factory], which calls this and guarantees the release runs. Use
// Session directly only when writing your own Runner, and defer the release.
func (s *SSO[T]) Session(ctx context.Context, info *SessionInfo) (T, func(), error) {
	var zero T
	noop := func() {}
	if info == nil || info.Identity == nil {
		return zero, noop, errors.New("web: no identity")
	}
	if v, ok := s.Claim(info); ok {
		return v, s.releaseOnce(v), nil
	}
	if s.provision == nil {
		return zero, noop, ErrNoUpstream
	}
	// After Close, provisioning is refused. Close is the last step of shutdown, so
	// anything allocated after it has nothing left to release it — and a Manager
	// can still be draining a session whose Run has only just started (lector r1
	// on PR #14). Shutdown order is handler stop, Manager shutdown, then this.
	//
	// Checking the flag and then calling Provision was not enough: a Provision
	// blocked on I/O could start, Close could return, and the Provision could then
	// resume and hand back a live session nothing was left to close (lector r2
	// reproduced it). So the call is BRACKETED — registered before, retired after —
	// and Close waits for the bracket to empty. The consumer's I/O still happens
	// with no lock held.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return zero, noop, ErrStopped
	}
	s.provisioning++
	s.mu.Unlock()

	v, err := s.provision(ctx, info.Identity)

	s.mu.Lock()
	s.provisioning--
	raced := s.closed
	// Broadcast unconditionally: Close may be waiting on this one.
	s.cond.Broadcast()
	s.mu.Unlock()

	if err != nil {
		return zero, noop, err
	}
	if raced {
		// Close won. What was just allocated is released here rather than handed
		// over, because after Close nothing else will ever release it.
		s.releaseValue(v, SessionEnded)
		return zero, noop, ErrStopped
	}
	return v, s.releaseOnce(v), nil
}

// releaseOnce returns a release that runs the consumer's cleanup exactly once
// however many times it is called, so a Runner that both defers it and calls it
// on an early return cannot double-release.
func (s *SSO[T]) releaseOnce(v T) func() {
	var once sync.Once
	return func() { once.Do(func() { s.releaseValue(v, SessionEnded) }) }
}

// Factory wraps a build function into an [AppFactory] that acquires and releases
// upstream state around it.
//
// This is the form to use. The build function receives a ready upstream session
// and never sees claim, provision or release — so the release cannot be
// forgotten, which is the failure this type exists to prevent.
//
// # Exit paths, including panics
//
// The release is a deferred call in the wrapping Runner, so it runs when the App
// returns, when it returns an error, and while a panic unwinds. The panic case is
// only useful because [Manager] contains an App panic as that session's failure
// rather than letting it end the process; without that containment the release
// would run and the process would die anyway. A panic inside the consumer's
// Release is contained too, so it cannot replace the failure being handled — but
// Release should not panic, and one that does is recorded, not retried.
//
//	mgr, err := web.NewManager(
//	    sso.Factory(func(b *web.Backend, id *auth.Identity, up *myUpstream) web.Runner {
//	        return myApp(b, id, up)
//	    }),
//	    managerOpt,
//	)
//
// A session that cannot be given upstream state gets no App: Run returns an
// error, which the [Manager] treats as the session failing. That is deliberate —
// an App holding a nil upstream session fails later and further from the cause.
func (s *SSO[T]) Factory(build func(*Backend, *auth.Identity, T) Runner) AppFactory {
	return func(b *Backend, info *SessionInfo) Runner {
		return &ssoRunner[T]{sso: s, backend: b, info: info, build: build}
	}
}

// ssoRunner acquires inside Run rather than in the factory.
//
// The factory has no context, and acquisition may make a network call that must
// be cancellable by the session's own lifetime instead of hanging a session
// creation. It also puts the release on the same stack as the work it protects.
type ssoRunner[T any] struct {
	sso     *SSO[T]
	backend *Backend
	info    *SessionInfo
	build   func(*Backend, *auth.Identity, T) Runner
}

func (r *ssoRunner[T]) Run(ctx context.Context) error {
	up, release, err := r.sso.Session(ctx, r.info)
	if err != nil {
		return err
	}
	defer release()

	app := r.build(r.backend, r.info.Identity, up)
	if app == nil {
		return errors.New("web: the build function returned no application")
	}
	return app.Run(ctx)
}

// hold parks a value, sweeping expired entries first so an abandoned login cannot
// occupy capacity indefinitely.
func (s *SSO[T]) hold(handoff string, v T) error {
	if handoff == "" {
		return errors.New("web: empty handoff")
	}
	now := s.now()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStopped
	}
	stale := s.expiredLocked(now)
	if len(s.parked) >= s.max {
		s.mu.Unlock()
		s.releaseAll(stale, Expired)
		return ErrPendingLogins
	}
	if _, dup := s.parked[handoff]; dup {
		s.mu.Unlock()
		s.releaseAll(stale, Expired)
		return errors.New("web: this handoff is already parked")
	}
	// The timer is what makes the expiry real without a consumer's ticker. It is
	// armed while the entry is in the map and stopped by whatever removes it, so a
	// claimed entry never fires and an abandoned one always does.
	e := parkedEntry[T]{value: v, expires: now.Add(s.ttl)}
	e.timer = time.AfterFunc(s.ttl, func() { s.expire(handoff) })
	s.parked[handoff] = e
	s.mu.Unlock()
	// Released outside the lock: a consumer's cleanup may make network calls, and
	// holding the park's lock across one would block every other login.
	s.releaseAll(stale, Expired)
	return nil
}

// expire is the timer's callback: it removes the entry if it is still parked and
// still expired, then releases it outside the lock.
func (s *SSO[T]) expire(handoff string) {
	s.mu.Lock()
	e, ok := s.parked[handoff]
	if ok && e.expires.After(s.now()) {
		// The clock was moved back, or a test clock has not reached the deadline.
		// Re-arm rather than release early: releasing destroys a live session. The
		// remaining duration is positive here, so this cannot become a tight loop.
		ok = false
		if e.timer != nil {
			e.timer.Reset(e.expires.Sub(s.now()))
		}
	}
	if ok {
		delete(s.parked, handoff)
	}
	s.mu.Unlock()
	if ok {
		s.settleHandoff(handoff)
		s.releaseValue(e.value, Expired)
	}
}

// Release cleans up one parked entry. Idempotent: an entry already claimed or
// released is a no-op, so the hook firing twice cannot double-release.
func (s *SSO[T]) Release(handoff string, r HandoffReason) {
	s.mu.Lock()
	e, ok := s.parked[handoff]
	if ok {
		delete(s.parked, handoff)
		if e.timer != nil {
			e.timer.Stop()
		}
	}
	s.mu.Unlock()
	if ok {
		s.settleHandoff(handoff)
		s.releaseValue(e.value, r)
	}
}

// Sweep releases expired entries.
//
// Not something a consumer needs to call: each parked entry carries its own timer
// and [SSO.Claim] refuses an expired entry. It remains exported for a consumer
// driving the park from a test clock, where no real timer will fire.
func (s *SSO[T]) Sweep() {
	now := s.now()
	s.mu.Lock()
	stale := s.expiredLocked(now)
	s.mu.Unlock()
	s.releaseAll(stale, Expired)
}

// Close releases every parked entry and refuses further provisioning.
//
// It is the LAST step of shutdown: stop the handler, shut the [Manager] down and
// let its sessions finish, then Close. In the other order a session still being
// started could provision state after the park stopped accepting responsibility
// for it.
//
// It BLOCKS until every [SSO.Session] call already inside Provision has returned.
// Those calls release what they produced rather than handing it over, so once
// Close returns nothing this type allocated is still outstanding — which is the
// only version of that sentence worth writing down.
func (s *SSO[T]) Close() {
	s.mu.Lock()
	s.closed = true
	// Everything already in flight observes closed and releases its own value; this
	// waits for that to have happened, so "nothing is outstanding once Close
	// returns" is a fact rather than a hope.
	for s.provisioning > 0 {
		s.cond.Wait()
	}
	all := make([]evicted[T], 0, len(s.parked))
	for h, e := range s.parked {
		all = append(all, evicted[T]{handoff: h, value: e.value})
		if e.timer != nil {
			e.timer.Stop()
		}
		delete(s.parked, h)
	}
	s.mu.Unlock()
	s.releaseAll(all, Expired)
}

// ReleasePanics reports how many consumer Release calls have panicked and been
// contained. A non-zero value means upstream state was abandoned mid-cleanup, so
// it belongs on a dashboard rather than only in a log.
func (s *SSO[T]) ReleasePanics() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.panics
}

// Len reports how many entries are parked, for tests and a metric.
func (s *SSO[T]) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.parked)
}

// expiredLocked removes and returns expired values. Caller holds the lock.
// evicted is a removed entry plus the handoff whose slot it held, so the release
// can happen outside the lock and still settle the right slot.
type evicted[T any] struct {
	handoff string
	value   T
}

func (s *SSO[T]) expiredLocked(now time.Time) []evicted[T] {
	var stale []evicted[T]
	for h, e := range s.parked {
		if now.After(e.expires) {
			stale = append(stale, evicted[T]{handoff: h, value: e.value})
			if e.timer != nil {
				e.timer.Stop()
			}
			delete(s.parked, h)
		}
	}
	return stale
}

func (s *SSO[T]) releaseAll(vals []evicted[T], r HandoffReason) {
	for _, v := range vals {
		s.settleHandoff(v.handoff)
		s.releaseValue(v.value, r)
	}
}

package web

import (
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/errs"
)

// Admission is the pending-login budget that the login route, a park ([SSO]) and the session
// [Manager] share: the login route leases a slot from it before it verifies a password, a park
// publishes its entry through it, and whoever settles a handoff returns the slot to it.
//
// Build it first and hand it to each part, so none of them writes another's fields and the order
// they are built in does not matter:
//
//	adm := web.NewAdmission(web.AdmissionCap(8))
//	sso, _ := web.NewSSO(web.SSOConfig[T]{..., Admission: adm})
//	mgr, _ := web.NewManager(sso.Factory(build), web.ManagerAdmission(adm))
//	h, _ := web.NewHandler(cfg, mgr) // the Manager's Admission
//
// A Manager given none makes its own, which serves a deployment without a park. An SSO attaches
// itself when built with one; from then on its park settles the handoffs and the Manager does not,
// decided each time a handoff settles, not when something was built.
type Admission struct {
	gate *gate
	hold time.Duration
	now  func() time.Time

	mu   sync.Mutex
	park attachedPark // the attached park, read at each use
}

// attachedPark is what an attached park ([SSO]) gives its Admission: the login hook that parks a login's
// state, the release of a handoff no session will claim, and how long an entry stays parked.
type attachedPark interface {
	parkLogin(handoff string, id *auth.Identity, stash *Stash) error
	releaseUnused(handoff string, reason HandoffReason)
	entryTTL() time.Duration
}

// AdmissionOption configures an [Admission].
type AdmissionOption func(*Admission)

// AdmissionCap caps logins parked awaiting an attach (default [DefaultMaxPendingLogins]).
//
// A separate budget from [MaxSessions] on purpose: they bound different resources, and a
// reconnect at a full session cap must still be able to log in. An attached [SSO]'s park holds
// as many entries as this cap.
func AdmissionCap(n int) AdmissionOption {
	return func(a *Admission) {
		if n > 0 {
			a.gate.max = n
		}
	}
}

// AdmissionHold sets the backstop lifetime of a keyed slot: how long a handoff that nothing
// settles keeps its slot. It is never shorter than the attach ticket's life, nor than an attached
// park's entry lifetime, whatever is set here.
func AdmissionHold(d time.Duration) AdmissionOption {
	return func(a *Admission) {
		if d > 0 {
			a.hold = d
		}
	}
}

// DefaultMaxPendingLogins is the parked-login budget when none is given.
const DefaultMaxPendingLogins = 8

// NewAdmission builds a pending-login budget.
func NewAdmission(opts ...AdmissionOption) *Admission {
	a := &Admission{gate: newGate(DefaultMaxPendingLogins), now: time.Now}
	for _, o := range opts {
		if o != nil {
			o(a)
		}
	}
	a.gate.now = a.now
	return a
}

// Cap is the number of logins that may be parked awaiting an attach.
func (a *Admission) Cap() int { return a.gate.max }

// attach makes p the park that settles this Admission's handoffs. One park per Admission: a second
// would leave it unclear whose entries the slots account for.
func (a *Admission) attach(p attachedPark) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.park != nil {
		return errs.Wrap(errs.ErrPrecondition, "web: this Admission already has a park (an SSO) attached")
	}
	a.park = p
	return nil
}

// attached is the park that settles handoffs now, or nil.
func (a *Admission) attached() attachedPark {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.park
}

// deadline is when a keyed slot taken now expires: the backstop hold, but never sooner than the
// attach ticket's life or an attached park's entry lifetime, so the gate cannot free a slot while
// the ticket is still good or the park still holds its entry.
func (a *Admission) deadline() time.Time { return a.now().Add(a.holdNow()) }

// holdNow is the backstop hold in force now: the set hold, the ticket's life, and an attached
// park's entry lifetime, whichever is longest.
func (a *Admission) holdNow() time.Duration {
	hold := max(a.hold, loginTicketTTL)
	if p := a.attached(); p != nil {
		hold = max(hold, p.entryTTL())
	}
	return hold
}

// commit turns a login's reservation into a slot that accounts for an entry and publishes the
// entry while the gate's lock is held, so membership and publication are one step. false: the
// reservation lapsed and nothing was published.
func (a *Admission) commit(handoff string, publish func()) bool {
	return a.gate.commit(handoff, a.deadline(), publish)
}

// settleFromPark returns the slot a parked handoff owned: called by the park once its entry is
// gone (claimed, released or expired).
func (a *Admission) settleFromPark(handoff string) { a.gate.release(handoff) }

// settleFromManager returns the slot a handoff owned once the Manager is done with it, unless a
// park is attached: a park settles its own, later, when its entry is actually gone. The Manager
// returns as soon as the factory hands back a Runner, before the claim, so settling then would
// free a slot while the park is still full.
func (a *Admission) settleFromManager(handoff string) {
	if a.attached() != nil {
		return
	}
	a.gate.release(handoff)
}

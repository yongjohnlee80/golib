package postgres

// Destroyer is the OPTIONAL capability a pinned connection offers when its physical
// destruction can be demanded outright, with no appeal to whether the wire looks
// reusable.
//
// A CONSUMER THAT CANNOT PROVE A BACKEND IS FIT MUST BE ABLE TO SAY SO IN ONE CALL, and
// [PinnedConn.Discard] is not that call. Discard decides reuse from wire mechanics —
// nothing queued, nothing poisoned, no transaction the driver opened — because wire
// mechanics are the only thing a driver can observe. A backend whose reset the server
// just refused passes every one of those tests: the frames all went out and came back in
// order, and the state the consumer failed to clear is invisible from here. Discard
// therefore returns it to the pool, which is precisely the outcome the consumer was
// trying to prevent.
//
// The workaround that exists without this interface is worse than the problem. A
// consumer can put the handle into a state the reuse predicate rejects — queue a frame
// and never flush it, say — and let the predicate reach the conclusion for it. That
// couples the consumer to an UNEXPORTED predicate: the reuse rule may be widened for a
// good reason on this side, and the consumer's failed reset silently becomes a reuse of
// contaminated state, with no compile error and no test anywhere to catch it. Destroy
// makes the demand explicit, so the two can only disagree out loud.
//
// It is a SEPARATE interface rather than a method on [PinnedConn] because PinnedConn is
// published: every explicit implementation of it outside this package — consumers' fakes
// included — would stop compiling the day a method was added, which is not an additive
// change. The same reasoning put [ParameterStatusReporter] and [SimpleQuerier] behind
// their own interfaces. Reach it the same way:
//
//	if d, ok := pc.(postgres.Destroyer); ok { d.Destroy() } else { /* older golib */ }
//
// A consumer that misses the assertion is talking to a build without the capability and
// must fall back to Discard AND say so, because that fallback is the weaker guarantee
// this interface exists to replace.
type Destroyer interface {
	// Destroy relinquishes the acquisition and destroys the physical connection,
	// UNCONDITIONALLY: it poisons the handle under the lock so no new wire operation
	// starts, interrupts and then barriers behind any in-flight read or write so the
	// close cannot race it, closes the socket, and gives the lease back to a pool that
	// will find a closed member and discard it.
	//
	// A quiescent, healthy, perfectly reusable wire is destroyed exactly as a broken one
	// is. That is the point: the caller, not the driver, is the one who knows the
	// SESSION is unfit, and nothing about the wire can overrule it.
	//
	// It is idempotent and terminal. Repeated calls are no-ops, a Destroy after a
	// Discard is a no-op, and every other face refuses with [ErrPoisoned] afterwards.
	//
	// A Destroy after a SUCCESSFUL [PinnedConn.Release] is also a no-op, and that is
	// not a hole in the guarantee: Release already handed the member back, so the pool
	// may have given it to another goroutine, and closing the socket would then destroy
	// a stranger's connection. A consumer that may need to destroy a backend must
	// therefore decide BEFORE it releases — which is the order a release gate runs in
	// anyway, since the decision is what the gate is for.
	Destroy()
}

// Compile-time proof that the pinned handle carries the capability, and that reaching it
// needs no change to PinnedConn.
var _ Destroyer = (*pinnedConn)(nil)

// Destroy destroys the pinned member outright. See [Destroyer.Destroy].
func (p *pinnedConn) Destroy() { p.relinquish(true) }

// Destroy destroys pc's backend when pc carries the capability, reporting whether it
// did. It is the typed entry point a consumer calls instead of asserting the interface
// at every site, mirroring [PinSessionConn]: a false answer means the handle predates
// the capability, and the caller owes its own weaker teardown — never a silent nothing.
func Destroy(pc PinnedConn) bool {
	d, ok := pc.(Destroyer)
	if !ok {
		return false
	}
	d.Destroy()
	return true
}

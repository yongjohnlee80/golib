package decl

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/parse"
)

// Update is one delivery from a [Provider].
//
// Its Values are applied as ONE propagation, which is the reason a provider
// delivers a map rather than a name and a value. A provider that owns a palette
// hands over the whole palette, and no binding ever sees half of it.
type Update struct {
	// Version orders this provider's deliveries. It must STRICTLY INCREASE.
	//
	// The engine drops an update whose version is not newer than the last one it
	// accepted, which is what makes a late delivery harmless: callbacks reach
	// the engine through a scheduler, so two updates in flight can arrive in
	// either order, and without this the older one would win by arriving last.
	Version uint64
	// Values are the names that now hold these values.
	Values map[string]parse.SpecValue
}

// Provider is a host object that OWNS some sources and says when they change.
//
// It is the push-free half of the reactive layer: [Tree.SetSource] is the host
// telling the engine, and a provider is the engine asking to be told.
//
// # The gap this interface exists to close
//
// Reading current values and then subscribing has a WINDOW between the two, and
// a change landing in that window is lost forever — the engine holds a value
// that is already stale and will never be corrected, because the change it
// missed has already been delivered to nobody.
//
// So Subscribe does both: it must deliver the current values to fn BEFORE it
// returns, synchronously, as the first update. There is then no moment at which
// a change can fall between the two operations, because there are not two.
type Provider interface {
	// Subscribe delivers the current values synchronously, then every later
	// change, and returns the names it provides and how to stop.
	//
	// The names must match what the synchronous first update carried: they are
	// what the engine declares as sources, and a name delivered later that was
	// never declared cannot be bound by any schema.
	//
	// cancel must be safe to call once and must stop further deliveries. The
	// engine calls it on every exit path.
	Subscribe(fn func(Update)) (names []string, cancel func() error, err error)
}

// Sentinels for the provider layer.
var (
	// ErrNoScheduler reports a provider subscribed with nowhere to run.
	ErrNoScheduler = errors.New("decl: this tree has no scheduler, so a provider has nowhere to deliver")

	// ErrProviderContract reports a provider that broke its half of the
	// agreement — no synchronous first update, or a name it never declared.
	ErrProviderContract = errors.New("decl: the provider broke its contract")
)

// WithScheduler installs how the engine reaches the goroutine that owns the
// tree.
//
// A [Tree] is not safe for concurrent use and does not try to be: it is owned
// by one goroutine, the same one the toolkit's loop runs on. A provider's
// callback arrives from wherever the host's data lives, which is not that
// goroutine, so the engine hands the work to sched and sched puts it on the
// right one — tui.App.Update is exactly this function.
//
// It is REQUIRED for providers rather than optional, and a subscription without
// it is refused. The alternative is a documented hazard, and a hazard nobody is
// stopped from reaching is not a decision: a provider firing on its own
// goroutine would mutate the tree underneath the loop, and the failure would be
// a rare corrupted frame rather than an error anyone could act on.
func WithScheduler(sched func(func())) Option {
	return func(t *Tree) { t.sched = sched }
}

// provider is one live subscription.
type provider struct {
	names  []string
	cancel func() error
	// version is the last version ACCEPTED. A delivery at or below it is stale
	// and dropped.
	version uint64
	// seen reports whether any version has been accepted, so that a provider
	// legitimately starting at zero is not mistaken for a stale delivery.
	seen bool
	// gone stops deliveries that are already in flight when the subscription
	// ends. cancel stops NEW ones; it cannot recall a callback that has already
	// been handed to the scheduler and is waiting its turn.
	gone bool
}

// Subscribe registers a provider and declares the sources it owns, before Mount.
//
// The names it provides become declared sources, so a schema can be checked
// against them exactly as if the host had declared each one — a provider is
// where a value comes from, not a different kind of value.
func (t *Tree) Subscribe(p Provider) error {
	if t.ph != phaseIdle || t.root != NoNode {
		return SchemaError{Op: "subscribe", Err: fmt.Errorf(
			"%w: providers are subscribed before Mount, so the source set a schema "+
				"is checked against is fixed when planning begins", ErrPhase)}
	}
	if p == nil {
		return SchemaError{Op: "subscribe",
			Err: fmt.Errorf("%w: the provider is nil", ErrProviderContract)}
	}
	if t.sched == nil {
		return SchemaError{Op: "subscribe", Err: fmt.Errorf(
			"%w: pass WithScheduler so deliveries reach the goroutine that owns "+
				"this tree", ErrNoScheduler)}
	}

	pr := &provider{}

	// first captures the SYNCHRONOUS delivery. Until Subscribe returns there is
	// no subscription to route through the scheduler, and routing the initial
	// snapshot would put it behind changes that have not happened yet.
	var first *Update
	var delivered bool
	fn := func(u Update) {
		if !delivered {
			delivered = true
			cp := u
			first = &cp
			return
		}
		t.deliver(pr, u)
	}

	names, cancel, err := p.Subscribe(fn)
	if err != nil {
		return SchemaError{Op: "subscribe", Err: fmt.Errorf("%w: %w", ErrProviderContract, err)}
	}

	// From here every failure must UNSUBSCRIBE, and its error must survive
	// alongside the one that caused it. A provider left subscribed to a tree
	// that refused it goes on delivering into an engine that has no sources for
	// it — and a cancel error swallowed here is a resource nobody knows leaked.
	fail := func(cause error) error {
		if cancel == nil {
			return cause
		}
		pr.gone = true
		if cerr := cancel(); cerr != nil {
			return errors.Join(cause, SchemaError{Op: "unsubscribe",
				Err: fmt.Errorf("%w: %w", ErrProviderContract, cerr)})
		}
		return cause
	}

	if cancel == nil {
		return fail(SchemaError{Op: "subscribe", Err: fmt.Errorf(
			"%w: no way to unsubscribe was returned", ErrProviderContract)})
	}
	if !delivered || first == nil {
		return fail(SchemaError{Op: "subscribe", Err: fmt.Errorf(
			"%w: Subscribe returned without delivering the current values; a change "+
				"landing between the two would be lost", ErrProviderContract)})
	}
	if len(names) == 0 {
		return fail(SchemaError{Op: "subscribe", Err: fmt.Errorf(
			"%w: the provider declared no names", ErrProviderContract)})
	}

	// The declared names become sources, and the first update is their initial
	// value. A name the snapshot does not carry is still declared: a provider
	// may legitimately start with nothing to say about one of its names, and
	// refusing that would force it to invent a value.
	owned := make(map[string]bool, len(names))
	for _, n := range names {
		owned[n] = true
	}
	for n := range first.Values {
		if !owned[n] {
			return fail(SchemaError{Op: "subscribe", Detail: n, Err: fmt.Errorf(
				"%w: delivered %q, which it did not declare; no schema could bind it",
				ErrProviderContract, n)})
		}
	}
	// The names go in ALL OR NONE. Injecting them one at a time and returning
	// on the first refusal left the earlier ones behind: the caller was told the
	// subscription failed, and a schema could still bind sources belonging to a
	// provider that is not attached to anything — sources nothing will ever
	// update, because the delivery path was torn down.
	added := make([]string, 0, len(names))
	undo := func() {
		for _, n := range added {
			delete(t.injected, n)
			delete(t.sources, n)
		}
	}
	for _, n := range names {
		v, ok := first.Values[n]
		if !ok {
			v = parse.SpecValue{Kind: parse.SpecValueString}
		}
		if err := t.inject("subscribe", n, SourceValue(v)); err != nil {
			undo()
			return fail(err)
		}
		added = append(added, n)
	}

	pr.names, pr.cancel = names, cancel
	pr.version, pr.seen = first.Version, true
	t.providers = append(t.providers, pr)
	return nil
}

// deliver routes one later update onto the tree's own goroutine.
//
// Everything that decides whether the update is CURRENT happens on that
// goroutine, not here. Dropping a stale version at the call site would compare
// against a version another delivery is in the middle of advancing, which is the
// race this indirection exists to remove rather than one it may keep.
func (t *Tree) deliver(pr *provider, u Update) {
	t.sched(func() {
		if pr.gone {
			// A delivery already queued when the subscription ended. cancel
			// stops new ones; it cannot recall this one.
			return
		}
		if pr.seen && u.Version <= pr.version {
			// STALE. Two updates in flight can reach the scheduler in either
			// order, and without this the older one wins by arriving last.
			return
		}
		owned := make(map[string]bool, len(pr.names))
		for _, n := range pr.names {
			owned[n] = true
		}
		for n := range u.Values {
			if !owned[n] {
				t.reportProviderError(SchemaError{Op: "provider", Detail: n, Err: fmt.Errorf(
					"%w: delivered %q, which it did not declare", ErrProviderContract, n)})
				return
			}
		}

		// The version advances only after a SUCCESSFUL propagation, for the same
		// reason the quiet cache does: a failed update that had already advanced
		// the version would make the provider's retry look stale and be dropped,
		// and the failure would become permanent in silence.
		if _, err := t.SetSources(u.Values); err != nil {
			t.reportProviderError(err)
			return
		}
		pr.version, pr.seen = u.Version, true
	})
}

func (t *Tree) reportProviderError(err error) {
	if t.onProviderError != nil {
		t.onProviderError(err)
	}
}

// WithProviderErrorSink installs where an error from a provider delivery goes.
//
// A delivery arrives with no caller to return to, so without a sink its error
// has nowhere to be. Naming the sink is the same decision the adapter's handler
// sink makes, for the same reason.
func WithProviderErrorSink(sink func(error)) Option {
	return func(t *Tree) { t.onProviderError = sink }
}

// unsubscribeAll ends every subscription, joining what they report.
//
// Errors are JOINED rather than returned first-wins: each provider is a
// separate resource, and a host that hears about one leak and not the other
// three has been told something worse than nothing.
func (t *Tree) unsubscribeAll() error {
	var errs []error
	for _, pr := range t.providers {
		pr.gone = true
		if pr.cancel == nil {
			continue
		}
		if err := pr.cancel(); err != nil {
			errs = append(errs, SchemaError{Op: "unsubscribe",
				Err: fmt.Errorf("%w: %w", ErrProviderContract, err)})
		}
		// Cleared so a second Destroy does not cancel twice. A provider is
		// entitled to treat that as a programming error, and it would be one.
		pr.cancel = nil
	}
	t.providers = nil
	return errors.Join(errs...)
}

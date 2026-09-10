package tui

import (
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/errs"
)

// Bus is the App's broadcast event bus: exactly one instance exists per App (App.Bus()).
//
// # Architectural Invariants
//
// 1. Zero-Reflection Dispatch: Subscriptions compile down to direct type-asserted closures.
//    Event publication performs NO reflect.Call and incurs zero per-publish heap allocations.
// 2. Enqueue-Only Publish: Publishing an event (Bus.Publish) enqueues a delivery task onto
//    the App loop's program queue (Lane B) and returns immediately. Handlers are NEVER
//    executed synchronously on the publisher's goroutine. If handlers executed synchronously,
//    background worker tasks could directly mutate component state, destroying the
//    single-goroutine concurrency guarantee.
// 3. Copy-on-Write Subscription Lists: Delivery snapshots iterate over subscription slices
//    without holding mutex locks, allowing handlers to safely subscribe or unsubscribe mid-delivery.
type Bus struct {
	app *App

	mu   sync.Mutex
	subs map[reflect.Type][]*subscription // copy-on-write slices
}

// subscription is one registered handler. fn is a compiler-generated
// closure doing a plain type assertion — dispatch performs NO reflect.Call
// and NO per-publish allocation. cancelled is the tombstone: a handler
// cancelled during delivery of the same batch is skipped.
type subscription struct {
	fn        func(any)
	cancelled atomic.Bool
}

// newBus builds the App's bus.
func newBus(app *App) *Bus {
	return &Bus{app: app, subs: make(map[reflect.Type][]*subscription)}
}

// Subscribe registers fn for every published value of dynamic type T. The
// reflect.Type key is resolved ONCE here; fn always runs on the loop
// goroutine. cancel is idempotent and safe from any goroutine. Matching is
// exact dynamic type — no interface-assignability fan-out in v1, so
// subscribing to an interface type never matches. Bare Subscribe is for
// App-lifetime listeners; components use SubscribeScoped.
func Subscribe[T any](b *Bus, fn func(T)) (cancel func()) {
	if fn == nil {
		panic(errs.Fatal{Op: "tui: Subscribe", Rule: "nil handler"})
	}
	t := reflect.TypeOf((*T)(nil)).Elem()
	s := &subscription{fn: func(v any) { fn(v.(T)) }}

	b.mu.Lock()
	// Copy-on-write: delivery snapshots iterate without holding the lock.
	next := make([]*subscription, len(b.subs[t]), len(b.subs[t])+1)
	copy(next, b.subs[t])
	b.subs[t] = append(next, s)
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.cancelled.Store(true) // tombstone: skipped mid-batch
			b.remove(t, s)
		})
	}
}

// SubscribeScoped binds a bus subscription to the component's mounted lifetime.
// When the component unmounts, the subscription is automatically cancelled via
// Context.OnUnmount, preventing memory leaks and stale event delivery. This is
// the standard subscription method recommended for all components and widgets.
func SubscribeScoped[T any](c *Context, fn func(T)) (cancel func()) {
	cancel = Subscribe(c.app.bus, fn)
	c.OnUnmount(cancel)
	return cancel
}

// remove rebuilds the type's handler slice without s (copy-on-write under
// the mutex).
func (b *Bus) remove(t reflect.Type, s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.subs[t]
	next := make([]*subscription, 0, len(old))
	for _, e := range old {
		if e != s {
			next = append(next, e)
		}
	}
	if len(next) == 0 {
		delete(b.subs, t)
		return
	}
	b.subs[t] = next
}

// Publish enqueues v for delivery. ENQUEUE-ONLY: it never invokes handlers
// on the caller's goroutine — delivery happens on the loop goroutine when
// the program lane drains. Safe from any goroutine; never blocks. Handlers
// registered during a delivery see only subsequent publishes; handlers are
// invoked in subscription order.
func (b *Bus) Publish(v any) {
	if v == nil {
		panic(errs.Fatal{Op: "tui: Bus.Publish", Rule: "nil value"})
	}
	b.app.queue.push(programItem{fn: func() { b.deliver(v) }})
}

// deliver runs on the loop goroutine: snapshot the handler slice for v's
// dynamic type (exact match) and invoke in subscription order, skipping
// tombstoned handlers.
func (b *Bus) deliver(v any) {
	t := reflect.TypeOf(v)
	b.mu.Lock()
	snapshot := b.subs[t] // copy-on-write slice: safe to iterate unlocked
	b.mu.Unlock()
	for _, s := range snapshot {
		if s.cancelled.Load() {
			continue
		}
		s.fn(v)
	}
}

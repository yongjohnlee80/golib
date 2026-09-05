package server

import (
	"context"
	"fmt"
	"sync"
)

// Session is anything with a connection-scoped lifetime that graceful
// shutdown must account for: a raw conn, a WebSocket session, an SFTP
// channel. Close force-terminates it (ADR-0006 §2.2).
type Session interface{ Close() error }

// Drainer is optionally implemented by Sessions that can end politely
// (e.g. a WebSocket session sending a close frame and awaiting the peer).
// Drain must return when ctx is done.
type Drainer interface {
	Drain(ctx context.Context) error
}

// Registry tracks live Sessions for drain-aware shutdown. Safe for
// concurrent use. The zero value is ready.
type Registry struct {
	mu       sync.Mutex
	live     map[*regEntry]struct{}
	asked    map[*regEntry]struct{} // already sent a polite end during drain
	reserved int
	draining bool
	closed   bool          // Drain returned: terminal, no new work ever
	changed  chan struct{} // closed + replaced on every state change
}

type regEntry struct{ s Session }

func (r *Registry) init() {
	if r.live == nil {
		r.live = make(map[*regEntry]struct{})
		r.asked = make(map[*regEntry]struct{})
	}
	if r.changed == nil {
		r.changed = make(chan struct{})
	}
}

// broadcast wakes every waiter. Callers hold r.mu.
func (r *Registry) broadcast() {
	close(r.changed)
	r.changed = make(chan struct{})
}

// Register adds an ESTABLISHED session. During or after Drain the session is
// closed immediately (no new work during shutdown) and the returned
// unregister is a no-op.
//
// If establishment does real work before the session exists — a protocol
// handshake, a caller-supplied factory — claim the slot with [Registry.Reserve]
// FIRST and finish with [Reservation.Complete]. A connection being established
// but not yet registered is invisible to [Registry.Drain], which then has
// nothing to wait for and reports a clean shutdown while the work is still in
// flight. Register alone is correct only when the session already exists.
func (r *Registry) Register(s Session) (unregister func()) {
	r.mu.Lock()
	r.init()
	if r.draining || r.closed {
		r.mu.Unlock()
		_ = s.Close()
		return func() {}
	}
	e := &regEntry{s: s}
	r.live[e] = struct{}{}
	r.broadcast()
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.live, e)
		delete(r.asked, e)
		r.broadcast()
		r.mu.Unlock()
	}
}

// Len reports the number of live sessions.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.live)
}

// Reservation is a claimed-but-not-yet-established session slot (ADR-0006
// §2.2): take it with Reserve before establishment work (e.g. a WebSocket
// handshake) so shutdown can refuse new work while a protocol-level refusal
// is still possible.
type Reservation struct {
	r    *Registry
	once sync.Once
}

// Reserve atomically claims a slot for a session about to be established. It
// returns ok=false once Drain has begun. Drain waits for open reservations
// exactly like live sessions, so establishment that won the race completes
// and is then drained politely.
func (r *Registry) Reserve() (res *Reservation, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	if r.draining || r.closed {
		return nil, false
	}
	r.reserved++
	return &Reservation{r: r}, true
}

// Complete binds the established session to the reservation and returns its
// unregister func. If Drain began (or finished) while the reservation was
// open, the session is still registered (or closed) accordingly — the drain
// loop ends it politely; establishment is never accept-then-abandoned.
func (res *Reservation) Complete(s Session) (unregister func()) {
	r := res.r
	res.once.Do(func() {
		r.mu.Lock()
		r.reserved--
		if r.closed {
			r.broadcast()
			r.mu.Unlock()
			_ = s.Close()
			unregister = func() {}
			return
		}
		e := &regEntry{s: s}
		r.live[e] = struct{}{}
		r.broadcast()
		r.mu.Unlock()
		unregister = func() {
			r.mu.Lock()
			delete(r.live, e)
			delete(r.asked, e)
			r.broadcast()
			r.mu.Unlock()
		}
	})
	if unregister == nil {
		unregister = func() {}
	}
	return unregister
}

// Cancel releases the reservation (establishment failed).
func (res *Reservation) Cancel() {
	res.once.Do(func() {
		res.r.mu.Lock()
		res.r.reserved--
		res.r.broadcast()
		res.r.mu.Unlock()
	})
}

// Drain asks every live session to end — Drainer.Drain where implemented,
// Close otherwise — then waits for unregisters and open reservations,
// bounded by ctx. Sessions still live at the deadline are force-Closed and
// their count reported in the returned error. After Drain begins, Reserve
// fails and Register closes the session immediately.
func (r *Registry) Drain(ctx context.Context) error {
	r.mu.Lock()
	r.init()
	r.draining = true
	for {
		// Politely end every session not yet asked (covers sessions completed
		// from reservations after drain started).
		for e := range r.live {
			if _, ok := r.asked[e]; ok {
				continue
			}
			r.asked[e] = struct{}{}
			go func(s Session) {
				if d, ok := s.(Drainer); ok {
					_ = d.Drain(ctx)
					return
				}
				_ = s.Close()
			}(e.s)
		}
		if len(r.live) == 0 && r.reserved == 0 {
			r.closed = true
			r.mu.Unlock()
			return nil
		}
		ch := r.changed
		r.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			r.mu.Lock()
			r.closed = true
			n := len(r.live) + r.reserved
			for e := range r.live {
				_ = e.s.Close()
			}
			r.live = map[*regEntry]struct{}{}
			r.asked = map[*regEntry]struct{}{}
			r.mu.Unlock()
			return fmt.Errorf("server: drain deadline: %d session(s) force-closed", n)
		}
		r.mu.Lock()
	}
}

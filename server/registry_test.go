package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSession records how it was ended.
type fakeSession struct {
	closed  atomic.Bool
	drained atomic.Bool
	block   chan struct{} // when set, Drain blocks until closed
}

func (f *fakeSession) Close() error {
	f.closed.Store(true)
	return nil
}

func (f *fakeSession) Drain(ctx context.Context) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.drained.Store(true)
	return nil
}

// plainSession implements only Session (no Drainer).
type plainSession struct{ closed atomic.Bool }

func (p *plainSession) Close() error {
	p.closed.Store(true)
	return nil
}

func TestRegistry_DrainPrefersDrainerOverClose(t *testing.T) {
	t.Parallel()
	var r Registry
	fs := &fakeSession{}
	ps := &plainSession{}
	unregF := r.Register(fs)
	unregP := r.Register(ps)

	done := make(chan error, 1)
	go func() { done <- r.Drain(context.Background()) }()

	// Sessions unregister once they observe their polite end.
	deadline := time.After(2 * time.Second)
	for !fs.drained.Load() || !ps.closed.Load() {
		select {
		case <-deadline:
			t.Fatalf("polite ends not delivered: drained=%v closed=%v", fs.drained.Load(), ps.closed.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	unregF()
	unregP()
	if err := <-done; err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if fs.closed.Load() {
		t.Error("Drainer session must be drained, not force-closed")
	}
}

func TestRegistry_DrainDeadlineForceCloses(t *testing.T) {
	t.Parallel()
	var r Registry
	stubborn := &fakeSession{block: make(chan struct{})} // never unregisters
	_ = r.Register(stubborn)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := r.Drain(ctx)
	if err == nil || !strings.Contains(err.Error(), "1 session(s) force-closed") {
		t.Fatalf("err = %v, want force-close report", err)
	}
	if !stubborn.closed.Load() {
		t.Error("session past the deadline must be force-closed")
	}
}

func TestRegistry_RegisterDuringDrainClosesImmediately(t *testing.T) {
	t.Parallel()
	var r Registry
	blocker := &fakeSession{block: make(chan struct{})}
	unreg := r.Register(blocker)

	done := make(chan error, 1)
	go func() { done <- r.Drain(context.Background()) }()
	waitDraining(t, &r)

	late := &plainSession{}
	_ = r.Register(late)
	if !late.closed.Load() {
		t.Error("Register during drain must close the session immediately")
	}

	close(blocker.block)
	unreg()
	if err := <-done; err != nil {
		t.Fatalf("Drain: %v", err)
	}

	// After drain (terminal): same behavior.
	after := &plainSession{}
	_ = r.Register(after)
	if !after.closed.Load() {
		t.Error("Register after drain must close the session immediately")
	}
}

func TestRegistry_ReserveRefusedDuringDrain(t *testing.T) {
	t.Parallel()
	var r Registry
	blocker := &fakeSession{block: make(chan struct{})}
	unreg := r.Register(blocker)

	done := make(chan error, 1)
	go func() { done <- r.Drain(context.Background()) }()
	waitDraining(t, &r)

	if _, ok := r.Reserve(); ok {
		t.Fatal("Reserve must fail once drain has begun")
	}
	close(blocker.block)
	unreg()
	<-done
	if _, ok := r.Reserve(); ok {
		t.Fatal("Reserve must fail after drain finished")
	}
}

func TestRegistry_DrainWaitsForReservation(t *testing.T) {
	t.Parallel()
	var r Registry
	res, ok := r.Reserve()
	if !ok {
		t.Fatal("Reserve before drain must succeed")
	}

	done := make(chan error, 1)
	go func() { done <- r.Drain(context.Background()) }()
	waitDraining(t, &r)

	select {
	case err := <-done:
		t.Fatalf("Drain returned %v while a reservation was open", err)
	case <-time.After(50 * time.Millisecond):
	}

	// Complete the reservation mid-drain: the session becomes live and is
	// politely drained — never accept-then-abandoned.
	s := &fakeSession{}
	unreg := res.Complete(s)
	deadline := time.After(2 * time.Second)
	for !s.drained.Load() {
		select {
		case <-deadline:
			t.Fatal("completed-during-drain session did not get a polite end")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	unreg()
	if err := <-done; err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

func TestRegistry_ReservationCancelReleasesDrain(t *testing.T) {
	t.Parallel()
	var r Registry
	res, _ := r.Reserve()
	done := make(chan error, 1)
	go func() { done <- r.Drain(context.Background()) }()
	waitDraining(t, &r)
	res.Cancel()
	if err := <-done; err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

func TestRegistry_ConcurrentChurn(t *testing.T) {
	t.Parallel()
	var r Registry
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if res, ok := r.Reserve(); ok {
				unreg := res.Complete(&plainSession{})
				time.Sleep(time.Millisecond)
				unreg()
			}
		})
		wg.Go(func() {
			unreg := r.Register(&plainSession{})
			time.Sleep(time.Millisecond)
			unreg()
		})
	}
	time.Sleep(5 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := r.Drain(ctx)
	wg.Wait()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "force-closed") {
		t.Fatalf("Drain: %v", err)
	}
}

// waitDraining spins until the registry reports draining.
func waitDraining(t *testing.T, r *Registry) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		r.mu.Lock()
		d := r.draining
		r.mu.Unlock()
		if d {
			return
		}
		select {
		case <-deadline:
			t.Fatal("registry never started draining")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

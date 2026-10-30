package password

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

// TestFactor_HashingIsBoundedByMaxConcurrent: a Verify hashes only in one of MaxConcurrent slots
// (GOMAXPROCS by default). With every slot held it hashes nothing and waits, until its ctx ends,
// the known subject's path and the unknown one's alike; with a slot free it verifies.
func TestFactor_HashingIsBoundedByMaxConcurrent(t *testing.T) {
	t.Parallel()
	store := NewMemStore()
	if err := store.Add("alice", "pw", cheap()); err != nil {
		t.Fatal(err)
	}
	if f := factor(t, store); cap(f.slots) != runtime.GOMAXPROCS(0) {
		t.Errorf("default slots %d, want GOMAXPROCS %d", cap(f.slots), runtime.GOMAXPROCS(0))
	}
	f := factor(t, store, MaxConcurrent(1))
	if cap(f.slots) != 1 {
		t.Fatalf("MaxConcurrent(1): %d slots", cap(f.slots))
	}
	f.slots <- struct{}{} // every slot held, as by a verification hashing
	for _, r := range []struct{ subject, pw string }{{"alice", "pw"}, {"nobody", "pw"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := f.Verify(ctx, request(r.subject, r.pw))
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s, every slot held: %v, want it to wait out its ctx", r.subject, err)
		}
	}
	<-f.slots
	if c, err := f.Verify(context.Background(), request("alice", "pw")); err != nil || c.Subject != "alice" {
		t.Errorf("a slot free: %+v %v, want alice verified", c, err)
	}
	if len(f.slots) != 0 {
		t.Errorf("%d slots still held after the Verify returned", len(f.slots))
	}
}

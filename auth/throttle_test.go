package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- fakes ------------------------------------------------------------------

var errWrong = errors.New("wrong credential")

// countingFactor records how many times it was actually invoked, which is how
// the "work happens on every path" property is asserted structurally rather than
// by a stopwatch.
type countingFactor struct {
	calls   atomic.Int64
	succeed func(*Request) bool
}

func (c *countingFactor) Kind() FactorKind { return FactorIdentity }

func (c *countingFactor) Verify(_ context.Context, r *Request) (Contribution, error) {
	c.calls.Add(1)
	if c.succeed != nil && c.succeed(r) {
		return Contribution{Method: "fake", Subject: claimedSubject(r), IssuedAt: time.Now()}, nil
	}
	return Contribution{}, errWrong
}

// recordingTracker logs the exact sequence of operations, so "the same
// operations in the same order on every path" is a testable claim.
type recordingTracker struct {
	mu    sync.Mutex
	ops   []string
	inner Tracker
}

func (t *recordingTracker) log(op string) {
	t.mu.Lock()
	t.ops = append(t.ops, op)
	t.mu.Unlock()
}

func (t *recordingTracker) sequence() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.ops))
	copy(out, t.ops)
	return out
}

func (t *recordingTracker) reset() {
	t.mu.Lock()
	t.ops = nil
	t.mu.Unlock()
}

func (t *recordingTracker) Locked(k string, now time.Time) (bool, time.Duration) {
	t.log("locked " + k)
	return t.inner.Locked(k, now)
}

func (t *recordingTracker) Fail(k string, now time.Time) time.Duration {
	t.log("fail " + k)
	return t.inner.Fail(k, now)
}

func (t *recordingTracker) Reset(k string) {
	t.log("reset " + k)
	t.inner.Reset(k)
}

func attempt(subject string, addr string) *Request {
	r := &Request{Credentials: map[string]Secret{"subject": NewSecret(subject)}}
	if addr != "" {
		r.Peer = netip.MustParseAddrPort(addr)
	}
	return r
}

// --- the property that matters ---------------------------------------------

// Every path must perform the SAME tracker operations in the SAME order, and
// must run the inner factor exactly once. Otherwise the fast path identifies
// itself: only an existing account can be locked out, so detecting lockout by
// timing enumerates users.
func TestThrottle_AllPathsDoEqualWork(t *testing.T) {
	t.Parallel()

	known := "alice"
	inner := &countingFactor{succeed: func(r *Request) bool {
		return claimedSubject(r) == known && r.Credentials["password"].Reveal() == "right"
	}}
	rec := &recordingTracker{inner: NewMemTracker(8, Backoff{Threshold: 1, Base: time.Minute, Max: time.Minute, Forget: time.Hour})}
	th, err := NewThrottle(inner, rec)
	if err != nil {
		t.Fatal(err)
	}

	// Lock alice out first, so the "locked" path is reachable below.
	for range 3 {
		_, _ = th.Verify(context.Background(), attempt(known, "10.0.0.9:1"))
	}
	if locked, _ := rec.inner.Locked("s:"+known, time.Now()); !locked {
		t.Fatal("fixture failed: alice is not locked out")
	}

	paths := []struct {
		name string
		r    *Request
	}{
		{"unknown subject", attempt("nobody", "10.0.0.1:1")},
		{"known subject, wrong credential", attempt("bob", "10.0.0.2:1")},
		{"locked", attempt(known, "10.0.0.3:1")},
		{"no claim at all", &Request{}},
	}
	var want []string
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			rec.reset()
			before := inner.calls.Load()
			_, err := th.Verify(context.Background(), p.r)
			if err == nil {
				t.Fatal("this path must fail")
			}
			if got := inner.calls.Load() - before; got != 1 {
				t.Errorf("inner factor ran %d times, want exactly 1: a path that "+
					"skips the work identifies itself by timing", got)
			}
			got := shape(rec.sequence())
			if want == nil {
				want = got
			} else if !equal(want, got) {
				t.Errorf("operation shape = %v, want %v (identical to the other paths)", got, want)
			}
		})
	}
}

// A full tracker must not change the shape of the work either.
func TestThrottle_TrackerFullPathIsIdentical(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{}
	// max 2 records, and the throttle writes two per attempt, so the table is
	// full from the very first attempt onward.
	mem := NewMemTracker(2, Backoff{Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour})
	rec := &recordingTracker{inner: mem}
	th, err := NewThrottle(inner, rec)
	if err != nil {
		t.Fatal(err)
	}

	var shapes [][]string
	for i := range 20 {
		rec.reset()
		before := inner.calls.Load()
		_, _ = th.Verify(context.Background(), attempt(fmt.Sprintf("user%d", i), fmt.Sprintf("10.0.%d.%d:1", i/256, i%256)))
		if got := inner.calls.Load() - before; got != 1 {
			t.Fatalf("attempt %d: inner ran %d times, want 1", i, got)
		}
		shapes = append(shapes, shape(rec.sequence()))
	}
	for i, s := range shapes {
		if !equal(shapes[0], s) {
			t.Errorf("attempt %d shape = %v, want %v: a full tracker takes a different path", i, s, shapes[0])
		}
	}
	if n := mem.Len(); n > 2 {
		t.Errorf("tracker holds %d records, cap is 2 — the bound is not enforced", n)
	}
}

// shape strips the key from each op, leaving the operation sequence. The KEYS
// legitimately differ between paths; the sequence must not.
func shape(ops []string) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		for i := range len(op) {
			if op[i] == ' ' {
				// keep the namespace prefix (s:/a:) so ordering is checked too
				out = append(out, op[:i+1]+op[i+1:i+3])
				break
			}
		}
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A correct credential arriving DURING backoff must still be refused and must
// still count. If it reset the counter, the lockout would be bypassable by
// retrying — which is the whole thing it exists to prevent.
func TestThrottle_LockedRefusesEvenACorrectCredential(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{succeed: func(*Request) bool { return true }}
	mem := NewMemTracker(16, Backoff{Threshold: 1, Base: time.Hour, Max: time.Hour, Forget: 2 * time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	// Two failures against the same subject via a factor that always succeeds is
	// impossible, so lock the keys directly.
	now := time.Now()
	mem.Fail("s:alice", now)
	mem.Fail("s:alice", now)

	_, err = th.Verify(context.Background(), attempt("alice", "10.0.0.1:1"))
	if !errors.Is(err, ErrThrottled) {
		t.Fatalf("err = %v, want ErrThrottled", err)
	}
	if locked, _ := mem.Locked("s:alice", time.Now()); !locked {
		t.Error("a locked attempt must count as a failure, or backoff is bypassable by retrying")
	}
}

// Success clears the counters, so a legitimate user who mistypes twice is not
// penalised later.
func TestThrottle_SuccessResets(t *testing.T) {
	t.Parallel()
	right := func(r *Request) bool { return r.Credentials["password"].Reveal() == "right" }
	inner := &countingFactor{succeed: right}
	mem := NewMemTracker(16, Backoff{Threshold: 3, Base: time.Minute, Max: time.Minute, Forget: time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	bad := attempt("alice", "10.0.0.1:1")
	for range 2 {
		if _, err := th.Verify(context.Background(), bad); !errors.Is(err, errWrong) {
			t.Fatalf("err = %v, want the inner error passed through", err)
		}
	}
	good := attempt("alice", "10.0.0.1:1")
	good.Credentials["password"] = NewSecret("right")
	got, err := th.Verify(context.Background(), good)
	if err != nil {
		t.Fatalf("a correct credential below the threshold must succeed: %v", err)
	}
	if got.Subject != "alice" {
		t.Errorf("contribution = %+v", got)
	}
	if mem.Len() != 0 {
		t.Errorf("%d records survive a success — the counters were not reset", mem.Len())
	}
}

// The per-ADDRESS counter has to bite independently, or one attacker walking a
// user list never accumulates anything.
func TestThrottle_AddressCounterIsIndependent(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{}
	mem := NewMemTracker(1024, Backoff{Threshold: 3, Base: time.Minute, Max: time.Minute, Forget: time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	// Four distinct subjects, one source. No subject reaches the threshold.
	for i := range 4 {
		_, _ = th.Verify(context.Background(), attempt(fmt.Sprintf("user%d", i), "10.0.0.7:1"))
	}
	if locked, _ := mem.Locked("s:user0", time.Now()); locked {
		t.Error("no single subject should be locked after one failure each")
	}
	if locked, _ := mem.Locked("a:10.0.0.7", time.Now()); !locked {
		t.Error("the source address must be locked: a user-list walk accumulates nowhere else")
	}
}

// The port must not be part of the key: a fresh port per connection would give
// an attacker a fresh counter per attempt.
func TestThrottle_PortIsNotPartOfTheAddressKey(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{}
	mem := NewMemTracker(1024, Backoff{Threshold: 2, Base: time.Minute, Max: time.Minute, Forget: time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		_, _ = th.Verify(context.Background(), attempt("alice", fmt.Sprintf("10.0.0.8:%d", 1000+i)))
	}
	if locked, _ := mem.Locked("a:10.0.0.8", time.Now()); !locked {
		t.Fatal("three attempts from three ports did not accumulate: the port is in the key")
	}
	if mem.Len() != 2 {
		t.Errorf("%d records for one subject and one address, want 2", mem.Len())
	}
}

func TestNewThrottle_Validation(t *testing.T) {
	t.Parallel()
	if _, err := NewThrottle(nil, NewMemTracker(0, Backoff{})); err == nil {
		t.Error("a nil factor must fail at construction")
	}
	if _, err := NewThrottle(&countingFactor{}, nil); err == nil {
		t.Error("a nil Tracker must fail at construction, not silently disable counting")
	}
	if _, err := NewThrottle(&countingFactor{}, NewMemTracker(0, Backoff{}), SubjectKey(nil)); err == nil {
		t.Error("SubjectKey(nil) must fail: it would disable per-subject counting")
	}
	if _, err := NewThrottle(&countingFactor{}, NewMemTracker(0, Backoff{}), AddressKey(nil)); err == nil {
		t.Error("AddressKey(nil) must fail")
	}
}

// Throttling changes when a factor admits, never what it proves, so the wrapper
// must be transparent to policy construction.
func TestThrottle_PreservesKindAndPolicyValidity(t *testing.T) {
	t.Parallel()
	th, err := NewThrottle(&countingFactor{}, NewMemTracker(0, Backoff{}))
	if err != nil {
		t.Fatal(err)
	}
	if th.Kind() != FactorIdentity {
		t.Error("wrapping an identity factor must not make it contextual")
	}
	if _, err := NewPolicy(Leaf(th)); err != nil {
		t.Errorf("a throttled identity factor alone must form a valid policy: %v", err)
	}
}

// --- MemTracker -------------------------------------------------------------

func TestMemTracker_BackoffGrowsAndForgets(t *testing.T) {
	t.Parallel()
	b := Backoff{Threshold: 2, Base: time.Second, Max: 8 * time.Second, Forget: time.Minute}
	m := NewMemTracker(16, b)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	// The first Threshold failures are free.
	for i := 1; i <= 2; i++ {
		if d := m.Fail("k", now); d != 0 {
			t.Fatalf("failure %d: backoff = %v, want 0 (below the threshold)", i, d)
		}
		if locked, _ := m.Locked("k", now); locked {
			t.Fatalf("failure %d locked the key below the threshold", i)
		}
	}
	for i, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second} {
		if d := m.Fail("k", now); d != want {
			t.Errorf("failure %d past threshold: backoff = %v, want %v", i+1, d, want)
		}
	}
	locked, retry := m.Locked("k", now)
	if !locked || retry <= 0 {
		t.Fatalf("locked = %v retry = %v", locked, retry)
	}
	// Past the backoff but inside Forget: unlocked, and the count is retained.
	if locked, _ := m.Locked("k", now.Add(9*time.Second)); locked {
		t.Error("still locked after the backoff elapsed")
	}
	// Past Forget: the run is forgotten, so the next failure starts over.
	if locked, _ := m.Locked("k", now.Add(2*time.Minute)); locked {
		t.Error("a forgotten record must not report locked")
	}
	if d := m.Fail("k", now.Add(2*time.Minute)); d != 0 {
		t.Errorf("after Forget the count must restart: backoff = %v, want 0", d)
	}
}

func TestMemTracker_Reset(t *testing.T) {
	t.Parallel()
	m := NewMemTracker(16, Backoff{Threshold: 0, Base: time.Minute, Max: time.Minute, Forget: time.Hour})
	now := time.Now()
	m.Fail("k", now)
	if locked, _ := m.Locked("k", now); !locked {
		t.Fatal("threshold 0 must lock on the first failure")
	}
	m.Reset("k")
	if locked, _ := m.Locked("k", now); locked {
		t.Error("Reset did not clear the record")
	}
	if m.Len() != 0 {
		t.Error("Reset left the record in place")
	}
}

// The counters are keyed by attacker-supplied values, so an unbounded map would
// be a memory-exhaustion vector reachable WITHOUT authenticating — a defense
// that introduces a vulnerability.
func TestMemTracker_BoundedUnderFlood(t *testing.T) {
	t.Parallel()
	const cap = 64
	m := NewMemTracker(cap, Backoff{Threshold: 1, Base: time.Second, Max: time.Second, Forget: time.Hour})
	now := time.Now()
	for i := range 100_000 {
		m.Fail(fmt.Sprintf("s:flood-%d", i), now)
		if n := m.Len(); n > cap {
			t.Fatalf("after %d distinct keys the tracker holds %d records, cap is %d", i+1, n, cap)
		}
	}
	if m.Len() == 0 {
		t.Error("the tracker evicted everything — no counting survives a flood at all")
	}
}

// Expired records are swept before anything live is evicted, so a flood of stale
// keys does not cost an active victim their counter.
func TestMemTracker_SweepsExpiredBeforeEvicting(t *testing.T) {
	t.Parallel()
	const cap = 4
	m := NewMemTracker(cap, Backoff{Threshold: 1, Base: time.Second, Max: time.Second, Forget: time.Minute})
	old := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for i := range cap {
		m.Fail(fmt.Sprintf("stale-%d", i), old)
	}
	// An hour later every record is past Forget, so a new key sweeps rather than
	// evicting.
	later := old.Add(time.Hour)
	m.Fail("victim", later)
	if _, ok := m.records["victim"]; !ok {
		t.Fatal("the new record was not stored")
	}
	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1: the stale records were not swept", m.Len())
	}
}

func TestMemTracker_ConcurrentUse(t *testing.T) {
	t.Parallel()
	m := NewMemTracker(128, DefaultBackoff())
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			now := time.Now()
			for i := range 200 {
				key := fmt.Sprintf("k%d", (g*i)%50)
				m.Fail(key, now)
				m.Locked(key, now)
				if i%7 == 0 {
					m.Reset(key)
				}
			}
		}(g)
	}
	wg.Wait()
	if m.Len() > 128 {
		t.Errorf("Len = %d after concurrent use, cap is 128", m.Len())
	}
}

func TestBackoff_NoOverflow(t *testing.T) {
	t.Parallel()
	b := Backoff{Threshold: 0, Base: time.Second, Max: time.Hour, Forget: time.Hour}.normalize()
	// A very long run of failures must saturate at Max, never wrap negative —
	// a negative duration would read as "not locked" and silently disable the
	// lockout for the most persistent attacker.
	for _, n := range []int{1, 10, 62, 63, 64, 1000, 1 << 20} {
		if d := b.delay(n); d < 0 || d > b.Max {
			t.Errorf("delay(%d) = %v, want within (0, %v]", n, d, b.Max)
		}
	}
	if d := b.delay(0); d != 0 {
		t.Errorf("delay(0) = %v, want 0", d)
	}
}

func TestBackoff_Normalize(t *testing.T) {
	t.Parallel()
	got := Backoff{Threshold: -5, Base: 0, Max: -1, Forget: 0}.normalize()
	if got.Threshold != 0 || got.Base <= 0 || got.Max < got.Base || got.Forget <= 0 {
		t.Errorf("normalize produced an unusable Backoff: %+v", got)
	}
	if d := DefaultBackoff(); d.Threshold != 5 || d.Base != time.Second || d.Max != 5*time.Minute || d.Forget != 15*time.Minute {
		t.Errorf("DefaultBackoff() = %+v", d)
	}
}

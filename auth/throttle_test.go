package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// --- fakes ------------------------------------------------------------------

var errWrong = errors.New("wrong credential")

// countingFactor records how many times it was actually invoked, which is how
// "work happens on every path" is asserted structurally rather than by a
// stopwatch. It implements Claimant, reading the "subject" credential.
type countingFactor struct {
	calls   atomic.Int64
	succeed func(*Request) bool
}

func (c *countingFactor) Kind() FactorKind { return FactorIdentity }

func (c *countingFactor) Claim(r *Request) string {
	if r == nil {
		return ""
	}
	return r.Credentials["subject"].Reveal()
}

func (c *countingFactor) Verify(_ context.Context, r *Request) (Contribution, error) {
	c.calls.Add(1)
	if c.succeed != nil && c.succeed(r) {
		return Contribution{Method: "fake", Subject: c.Claim(r), IssuedAt: time.Now()}, nil
	}
	return Contribution{}, errWrong
}

// opaqueFactor cannot name a principal before verifying — the shape of a bearer
// token or an mTLS chain. It deliberately does NOT implement Claimant.
type opaqueFactor struct {
	calls    atomic.Int64
	consumed atomic.Int64
}

func (o *opaqueFactor) Kind() FactorKind { return FactorIdentity }
func (o *opaqueFactor) Verify(context.Context, *Request) (Contribution, error) {
	o.calls.Add(1)
	// Models a single-use credential: consumed atomically on presentation.
	o.consumed.Add(1)
	return Contribution{}, errWrong
}

// recordingTracker logs the exact sequence of operations, so "the same
// operations in the same order on every path" is a testable claim.
type recordingTracker struct {
	mu    sync.Mutex
	ops   []string
	inner Tracker
	fail  error
}

func (t *recordingTracker) log(op string) {
	t.mu.Lock()
	t.ops = append(t.ops, op)
	t.mu.Unlock()
}

func (t *recordingTracker) sequence() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.ops...)
}

func (t *recordingTracker) reset() {
	t.mu.Lock()
	t.ops = nil
	t.mu.Unlock()
}

func (t *recordingTracker) Locked(ctx context.Context, k string, now time.Time) (bool, time.Duration, error) {
	t.log("locked " + k[:2])
	if t.fail != nil {
		return false, 0, t.fail
	}
	return t.inner.Locked(ctx, k, now)
}

func (t *recordingTracker) Fail(ctx context.Context, k string, now time.Time) (time.Duration, error) {
	t.log("fail " + k[:2])
	if t.fail != nil {
		return 0, t.fail
	}
	return t.inner.Fail(ctx, k, now)
}

func (t *recordingTracker) Reset(ctx context.Context, k string) error {
	t.log("reset " + k[:2])
	if t.fail != nil {
		return t.fail
	}
	return t.inner.Reset(ctx, k)
}

func attempt(subject string, addr string) *Request {
	r := &Request{Credentials: map[string]Secret{"subject": NewSecret(subject)}}
	if addr != "" {
		r.Peer = netip.MustParseAddrPort(addr)
	}
	return r
}

func tracker(t *testing.T, max int, b Backoff) *MemTracker {
	t.Helper()
	m, err := NewMemTracker(max, b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func fastBackoff() Backoff {
	return Backoff{Threshold: 1, Base: time.Minute, Max: time.Minute, Forget: time.Hour}
}

// --- construction contract --------------------------------------------------

// Per-subject lockout is impossible for a factor that cannot name a principal
// before verifying. Defaulting would put every such client on ONE shared
// counter, so any one attacker could lock out everybody while their own address
// counter stayed clean. That must be a construction error, not a surprise.
func TestNewThrottle_RequiresADeterminedSubjectSource(t *testing.T) {
	t.Parallel()

	if _, err := NewThrottle(&opaqueFactor{}, tracker(t, 0, Backoff{})); err == nil {
		t.Fatal("a factor without Claimant must not silently get a shared global counter")
	} else if !containsAll(err.Error(), "auth.Claimant", "auth.SubjectClaim", "auth.AddressOnly") {
		t.Errorf("the error must name all three ways out: %v", err)
	}

	// All three ways out work.
	if _, err := NewThrottle(&opaqueFactor{}, tracker(t, 0, Backoff{}), AddressOnly()); err != nil {
		t.Errorf("AddressOnly: %v", err)
	}
	if _, err := NewThrottle(&opaqueFactor{}, tracker(t, 0, Backoff{}),
		SubjectClaim(func(*Request) string { return "x" })); err != nil {
		t.Errorf("SubjectClaim: %v", err)
	}
	if _, err := NewThrottle(&countingFactor{}, tracker(t, 0, Backoff{})); err != nil {
		t.Errorf("a Claimant factor needs no option: %v", err)
	}

	// Contradictory options are a mistake, not a precedence puzzle.
	if _, err := NewThrottle(&countingFactor{}, tracker(t, 0, Backoff{}),
		AddressOnly(), SubjectClaim(func(*Request) string { return "x" })); err == nil {
		t.Error("AddressOnly + SubjectClaim must be refused")
	}
	if _, err := NewThrottle(nil, tracker(t, 0, Backoff{})); err == nil {
		t.Error("a nil factor must fail at construction")
	}
	if _, err := NewThrottle(&countingFactor{}, nil); err == nil {
		t.Error("a nil Tracker must fail at construction, not silently disable counting")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// An attempt that names no principal must NOT share a counter with every other
// such attempt: that is the global-lockout failure in a different disguise.
func TestThrottle_UnnamedAttemptsAreScopedToTheirAddress(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{}
	mem := tracker(t, 1024, fastBackoff())
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	// Two failures from address .1 with no subject claim: enough to lock.
	for range 2 {
		_, _ = th.Verify(context.Background(), attempt("", "10.0.0.1:1"))
	}
	if _, err := th.Verify(context.Background(), attempt("", "10.0.0.1:1")); !errors.Is(err, ErrThrottled) {
		t.Fatalf("err = %v: the unattributed counter is not accumulating", err)
	}
	// A DIFFERENT address, also unnamed, must be unaffected.
	if _, err := th.Verify(context.Background(), attempt("", "10.0.0.2:1")); !errors.Is(err, errWrong) {
		t.Errorf("err = %v: unnamed attempts share one global counter across addresses", err)
	}
	// And a named subject from a clean address is unaffected too.
	if _, err := th.Verify(context.Background(), attempt("alice", "10.0.0.3:1")); !errors.Is(err, errWrong) {
		t.Errorf("err = %v: the unattributed counter leaked into named subjects", err)
	}
}

// --- the timing property ----------------------------------------------------

// Within subject mode every path must perform the SAME tracker operations in the
// SAME order and run the inner factor exactly once. Otherwise the fast path
// identifies itself: only an existing account can be locked out, so detecting
// lockout by timing enumerates users.
func TestThrottle_AllPathsDoEqualWork(t *testing.T) {
	t.Parallel()

	known := "alice"
	inner := &countingFactor{succeed: func(r *Request) bool {
		return r.Credentials["subject"].Reveal() == known && r.Credentials["password"].Reveal() == "right"
	}}
	rec := &recordingTracker{inner: tracker(t, 8, fastBackoff())}
	th, err := NewThrottle(inner, rec)
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		_, _ = th.Verify(context.Background(), attempt(known, "10.0.0.9:1"))
	}
	if _, err := th.Verify(context.Background(), attempt(known, "10.0.0.9:1")); !errors.Is(err, ErrThrottled) {
		t.Fatalf("fixture failed: alice is not locked out (%v)", err)
	}

	paths := []struct {
		name string
		r    *Request
	}{
		{"unknown subject", attempt("nobody", "10.0.0.1:1")},
		{"known subject, wrong credential", attempt("bob", "10.0.0.2:1")},
		{"locked", attempt(known, "10.0.0.3:1")},
	}
	var want []string
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			rec.reset()
			before := inner.calls.Load()
			if _, err := th.Verify(context.Background(), p.r); err == nil {
				t.Fatal("this path must fail")
			}
			if got := inner.calls.Load() - before; got != 1 {
				t.Errorf("inner factor ran %d times, want exactly 1: a path that "+
					"skips the work identifies itself by timing", got)
			}
			got := rec.sequence()
			if want == nil {
				want = got
			} else if !equalSeq(want, got) {
				t.Errorf("operation sequence = %v, want %v (identical to the other paths)", got, want)
			}
		})
	}
}

func equalSeq(a, b []string) bool {
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

// --- (a) the ALGORITHMIC property, observed without a clock ------------------

// Eviction must not scan the table. It examines a bounded SAMPLE
// (evictionSample records) and evicts the oldest of those — so its cost does
// not grow with the cap.
//
// THIS ASSERTS THAT WITHOUT TIMING ANYTHING, which is the point. A full scan
// and a bounded sample differ in an observable way that no machine load can
// perturb: a full scan always finds the globally-oldest record and evicts it,
// while a sample of 8 out of `cap` finds it only by luck. Seed one record as
// distinctly oldest, insert a new key, and ask whether that record survived.
//
// The arithmetic is what makes this deterministic in practice rather than
// flaky. With cap = 4096 and evictionSample = 8, a sampled eviction touches the
// distinctly-oldest record with probability 8/4096 — about 0.2% — so over 20
// rounds it is expected to survive ~19.96 times. A full scan evicts it 20 times
// out of 20. The threshold below sits between those two, nowhere near either.
func TestThrottle_EvictionSamplesRatherThanScanning(t *testing.T) {
	t.Parallel()

	const (
		cap    = 4096
		rounds = 20
		// A full scan would score 0. A bounded sample is expected to score
		// ~19.96. Anything at or above this is incompatible with a scan.
		minSurvivals = 17
	)

	survived := 0
	for r := 0; r < rounds; r++ {
		m := tracker(t, cap, Backoff{Threshold: 1_000_000, Base: time.Second, Max: time.Second, Forget: time.Hour})
		now := time.Now()
		ctx := context.Background()
		for i := 0; i < cap; i++ {
			if _, err := m.Fail(ctx, fmt.Sprintf("k%d", i), now); err != nil {
				t.Fatal(err)
			}
		}
		if got := m.Len(); got != cap {
			t.Fatalf("round %d: filled to %d, want %d", r, got, cap)
		}

		// One record is distinctly the oldest BY lastSeen, which is what
		// eviction orders on. `last` stays recent on purpose: an old `last`
		// would make it EXPIRED, and expired records are dropped by a
		// different branch, which would prove nothing about ordering.
		const victim = "k0"
		m.mu.Lock()
		m.records[victim].lastSeen = now.Add(-24 * time.Hour)
		m.mu.Unlock()

		if _, err := m.Fail(ctx, fmt.Sprintf("new-%d", r), now); err != nil {
			t.Fatal(err)
		}

		m.mu.Lock()
		_, stillThere := m.records[victim]
		m.mu.Unlock()
		if stillThere {
			survived++
		}
	}

	t.Logf("globally-oldest record survived %d/%d evictions (a full scan scores 0, "+
		"a %d-of-%d sample is expected to score ~%.2f)",
		survived, rounds, evictionSample, cap,
		float64(rounds)*(1-float64(evictionSample)/float64(cap)))

	if survived < minSurvivals {
		t.Errorf("the globally-oldest record survived only %d/%d evictions, want >= %d: "+
			"eviction is finding the true oldest, which means it is SCANNING the table "+
			"rather than sampling it — cost then grows with the cap, which is both a DoS "+
			"amplifier and a timing signal for \"new key at capacity\"",
			survived, rounds, minSurvivals)
	}
}

// A full tracker must not change the shape OR the cost of the work. Proving
// that needs both a structural and a wall-clock check, so this does both, with a
// negative control proving the measurement can actually see an O(cap) regression.
func TestThrottle_TrackerFullPathIsEquivalent(t *testing.T) {
	const cap = 4096

	inner := &countingFactor{}
	full := tracker(t, cap, Backoff{Threshold: 1_000_000, Base: time.Second, Max: time.Second, Forget: time.Hour})
	rec := &recordingTracker{inner: full}
	th, err := NewThrottle(inner, rec)
	if err != nil {
		t.Fatal(err)
	}

	// Fill the table, then keep going: every further attempt is a miss at
	// capacity, which is the path an attacker triggers at will.
	now := time.Now()
	for i := 0; full.Len() < cap; i++ {
		if _, err := full.Fail(context.Background(), fmt.Sprintf("s:pad-%d", i), now); err != nil {
			t.Fatal(err)
		}
		if i > cap*4 {
			t.Fatalf("could not fill the tracker: Len = %d", full.Len())
		}
	}

	var shapes [][]string
	for i := range 20 {
		rec.reset()
		before := inner.calls.Load()
		_, _ = th.Verify(context.Background(), attempt(fmt.Sprintf("user%d", i), fmt.Sprintf("10.%d.%d.1:1", i/256, i%256)))
		if got := inner.calls.Load() - before; got != 1 {
			t.Fatalf("attempt %d: inner ran %d times, want 1", i, got)
		}
		shapes = append(shapes, rec.sequence())
	}
	for i, s := range shapes {
		if !equalSeq(shapes[0], s) {
			t.Errorf("attempt %d sequence = %v, want %v: a full tracker takes a different path", i, s, shapes[0])
		}
	}
	if n := full.Len(); n > cap {
		t.Fatalf("tracker holds %d records, cap is %d — the bound is not enforced", n, cap)
	}

	// --- wall clock -----------------------------------------------------
	//
	// The ALGORITHMIC half of this property is asserted without a clock, by
	// TestThrottle_EvictionSamplesRatherThanScanning. What is left here is the
	// half that is genuinely ABOUT wall clock and cannot be counted: two paths
	// can perform identical operations and still be distinguishable by TIME,
	// which is what lets an attacker ask "is this a new key at capacity?".
	//
	// THREE THINGS MAKE THE MEASUREMENT ROBUST, and each closes a specific way
	// the earlier version could mislead:
	//
	//  1. The three sample streams are INTERLEAVED, a slice of each per round.
	//     Measuring all of one then all of another lets a burst of machine load
	//     land on one stream and not the others; a 28.2x reading was observed
	//     that way, under load, and did not reproduce.
	//  2. The statistic is a MEDIAN over rounds, not one long sum, so a single
	//     descheduled round cannot carry the result.
	//  3. The limit is DERIVED from the control measured in the same rounds,
	//     not hard-coded. A fixed 10x is a claim about the machine; the control
	//     is a measurement of it.
	//
	// The control also had to move. It used to be fullScan/notFull — the SAME
	// denominator as the assertion — so jitter in the cold sample scaled both,
	// and the guard reported a healthy instrument during exactly the run where
	// the instrument was unhealthy. Interleaving is what makes it independent.
	const (
		rounds  = 9
		perRoun = 40
	)
	empty := tracker(t, cap, Backoff{Threshold: 1_000_000, Base: time.Second, Max: time.Second, Forget: time.Hour})

	hotS := make([]float64, 0, rounds)
	coldS := make([]float64, 0, rounds)
	ctlS := make([]float64, 0, rounds)
	for r := 0; r < rounds; r++ {
		hotS = append(hotS, float64(timeInserts(t, full, perRoun, fmt.Sprintf("hot%d", r))))
		coldS = append(coldS, float64(timeInserts(t, empty, perRoun, fmt.Sprintf("cold%d", r))))
		ctlS = append(ctlS, float64(timeFullScan(full, perRoun)))
	}
	atCapacity, notFull, fullScan := median(hotS), median(coldS), median(ctlS)

	ratio := atCapacity / notFull
	controlRatio := fullScan / notFull
	t.Logf("medians over %d interleaved rounds: at-capacity %v, not-full %v (ratio %.1fx); "+
		"O(cap) control %v (ratio %.1fx)",
		rounds, time.Duration(atCapacity), time.Duration(notFull), ratio,
		time.Duration(fullScan), controlRatio)

	if controlRatio < 4 {
		t.Fatalf("the O(cap) control is only %.1fx a normal insert, so this measurement "+
			"could not detect an O(cap) regression and the assertion below would be "+
			"vacuous", controlRatio)
	}
	// DERIVED, not hard-coded: a sampled eviction does strictly more work than a
	// plain insert — a few map lookups and a delete — but it must sit far closer
	// to a normal insert than to a full scan. Half the measured control distance
	// is the line, so the bound scales with whatever this machine actually is.
	allowed := 1 + (controlRatio-1)/2
	if ratio > allowed {
		t.Errorf("inserting at capacity is %.1fx a normal insert, past the %.1fx derived "+
			"from this run's O(cap) control (%.1fx): eviction work looks proportional to "+
			"the table size, which is both a DoS amplifier and a timing signal for "+
			"\"new key at capacity\"", ratio, allowed, controlRatio)
	}
}

// median returns the middle value, sorting a copy so the caller's slice is
// untouched. A median rather than a mean because the failure mode being guarded
// against is one descheduled round, and a mean carries it.
func median(xs []float64) float64 {
	c := slices.Clone(xs)
	slices.Sort(c)
	return c[len(c)/2]
}

// timeInserts measures per-insert cost for keys that are misses.
func timeInserts(t *testing.T, m *MemTracker, n int, tag string) time.Duration {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	// Warm up, so the first-touch cost of the map does not land in the sample.
	for i := range 32 {
		if _, err := m.Fail(ctx, fmt.Sprintf("warm-%s-%d", tag, i), now); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	for i := range n {
		if _, err := m.Fail(ctx, fmt.Sprintf("bench-%s-%d", tag, i), now); err != nil {
			t.Fatal(err)
		}
	}
	return time.Since(start) / time.Duration(n)
}

// timeFullScan is the negative control: what an O(cap) implementation costs.
func timeFullScan(m *MemTracker, n int) time.Duration {
	start := time.Now()
	for range n {
		m.mu.Lock()
		var oldest time.Time
		var have bool
		for _, rec := range m.records {
			if !have || rec.lastSeen.Before(oldest) {
				oldest, have = rec.lastSeen, true
			}
		}
		m.mu.Unlock()
	}
	return time.Since(start) / time.Duration(n)
}

// --- address-only mode ------------------------------------------------------

// Address-only mode short-circuits when locked, and MUST, because a single-use
// credential factor consumes its credential on presentation: running it on a
// denied attempt destroys a valid ticket and throws the proof away.
func TestThrottle_AddressOnlyDoesNotBurnTheCredentialWhenLocked(t *testing.T) {
	t.Parallel()
	inner := &opaqueFactor{}
	mem := tracker(t, 64, fastBackoff())
	th, err := NewThrottle(inner, mem, AddressOnly())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := &Request{Peer: netip.MustParseAddrPort("10.0.0.4:1")}

	// Two real attempts lock the address.
	for range 2 {
		if _, err := th.Verify(ctx, r); !errors.Is(err, errWrong) {
			t.Fatalf("err = %v", err)
		}
	}
	consumedBefore := inner.consumed.Load()
	if _, err := th.Verify(ctx, r); !errors.Is(err, ErrThrottled) {
		t.Fatalf("err = %v, want ErrThrottled", err)
	}
	if got := inner.consumed.Load(); got != consumedBefore {
		t.Errorf("the factor consumed %d more credentials on a DENIED attempt — a valid "+
			"single-use ticket would have been destroyed and its proof discarded", got-consumedBefore)
	}
	// Still counted, so hammering a locked address does not reset it.
	if locked, _, _ := mem.Locked(ctx, trackerKey("a", "10.0.0.4"), time.Now()); !locked {
		t.Error("a locked attempt must still count as a failure")
	}
}

// Address-only mode keys nothing on a subject, so one address cannot lock out
// another.
func TestThrottle_AddressOnlyIsPerAddress(t *testing.T) {
	t.Parallel()
	inner := &opaqueFactor{}
	mem := tracker(t, 64, fastBackoff())
	th, err := NewThrottle(inner, mem, AddressOnly())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range 3 {
		_, _ = th.Verify(ctx, &Request{Peer: netip.MustParseAddrPort("10.0.0.5:1")})
	}
	if _, err := th.Verify(ctx, &Request{Peer: netip.MustParseAddrPort("10.0.0.6:1")}); !errors.Is(err, errWrong) {
		t.Errorf("err = %v: one address locked out another", err)
	}
}

// --- outage policy ----------------------------------------------------------

// A tracker that cannot answer cannot protect anything, so the default denies.
// Continuing would mean running with brute-force protection silently off.
func TestThrottle_TrackerOutageFailsClosedByDefault(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{succeed: func(*Request) bool { return true }}
	var reported []error
	rec := &recordingTracker{inner: tracker(t, 8, fastBackoff()), fail: errors.New("redis down")}
	th, err := NewThrottle(inner, rec, OnTrackerError(func(e error) { reported = append(reported, e) }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = th.Verify(context.Background(), attempt("alice", "10.0.0.7:1"))
	if !errors.Is(err, ErrTrackerUnavailable) {
		t.Fatalf("err = %v, want ErrTrackerUnavailable", err)
	}
	if inner.calls.Load() != 0 {
		t.Error("fail-closed must not run the factor at all")
	}
	if len(reported) == 0 {
		t.Error("a tracker outage that nobody is told about is invisible")
	}
}

// FailOpen is a deliberate availability-over-security choice and must actually
// admit.
func TestThrottle_FailOpenAdmits(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{succeed: func(*Request) bool { return true }}
	rec := &recordingTracker{inner: tracker(t, 8, fastBackoff()), fail: errors.New("redis down")}
	th, err := NewThrottle(inner, rec, FailOpen())
	if err != nil {
		t.Fatal(err)
	}
	got, err := th.Verify(context.Background(), attempt("alice", "10.0.0.8:1"))
	if err != nil {
		t.Fatalf("FailOpen must admit a correct credential during an outage: %v", err)
	}
	if got.Subject != "alice" {
		t.Errorf("contribution = %+v", got)
	}
	if inner.calls.Load() != 1 {
		t.Errorf("inner ran %d times, want 1", inner.calls.Load())
	}
}

// A tracker write failure on the SUCCESS path must not turn a correct credential
// into a rejection.
func TestThrottle_ResetFailureDoesNotFailAuth(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{succeed: func(*Request) bool { return true }}
	var reported []error
	th, err := NewThrottle(inner, &writeFailTracker{inner: tracker(t, 8, fastBackoff())},
		OnTrackerError(func(e error) { reported = append(reported, e) }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := th.Verify(context.Background(), attempt("alice", "10.0.0.9:1")); err != nil {
		t.Fatalf("a failed Reset must not fail the authentication: %v", err)
	}
	if len(reported) == 0 {
		t.Error("the failed write was never reported")
	}
}

type writeFailTracker struct{ inner Tracker }

func (w *writeFailTracker) Locked(ctx context.Context, k string, now time.Time) (bool, time.Duration, error) {
	return w.inner.Locked(ctx, k, now)
}
func (w *writeFailTracker) Fail(ctx context.Context, k string, now time.Time) (time.Duration, error) {
	return w.inner.Fail(ctx, k, now)
}
func (w *writeFailTracker) Reset(context.Context, string) error { return errors.New("write failed") }

// The seam must be able to honor the caller's deadline, which is why it takes a
// context at all.
func TestTracker_SeamCarriesContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ct := &ctxTracker{}
	th, err := NewThrottle(&countingFactor{}, ct)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := th.Verify(ctx, attempt("alice", "10.0.0.1:1")); !errors.Is(err, ErrTrackerUnavailable) {
		t.Errorf("err = %v: a network-backed tracker could not honor the deadline", err)
	}
	if !ct.sawContext {
		t.Error("the tracker never received the context")
	}
}

type ctxTracker struct{ sawContext bool }

func (c *ctxTracker) Locked(ctx context.Context, _ string, _ time.Time) (bool, time.Duration, error) {
	c.sawContext = true
	return false, 0, ctx.Err()
}
func (c *ctxTracker) Fail(ctx context.Context, _ string, _ time.Time) (time.Duration, error) {
	return 0, ctx.Err()
}
func (c *ctxTracker) Reset(ctx context.Context, _ string) error { return ctx.Err() }

// --- behavior ---------------------------------------------------------------

// A correct credential arriving DURING backoff must still be refused and must
// still count. If it reset the counter, the lockout would be bypassable by
// retrying — which is the whole thing it exists to prevent.
func TestThrottle_LockedRefusesEvenACorrectCredential(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{succeed: func(*Request) bool { return true }}
	mem := tracker(t, 16, Backoff{Threshold: 1, Base: time.Hour, Max: time.Hour, Forget: 2 * time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := trackerKey("s", "alice")
	now := time.Now()
	if _, err := mem.Fail(ctx, key, now); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Fail(ctx, key, now); err != nil {
		t.Fatal(err)
	}

	if _, err := th.Verify(ctx, attempt("alice", "10.0.0.1:1")); !errors.Is(err, ErrThrottled) {
		t.Fatalf("err = %v, want ErrThrottled", err)
	}
	if locked, _, _ := mem.Locked(ctx, key, time.Now()); !locked {
		t.Error("a locked attempt must count as a failure, or backoff is bypassable by retrying")
	}
}

func TestThrottle_SuccessResets(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{succeed: func(r *Request) bool { return r.Credentials["password"].Reveal() == "right" }}
	mem := tracker(t, 16, Backoff{Threshold: 3, Base: time.Minute, Max: time.Minute, Forget: time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range 2 {
		if _, err := th.Verify(ctx, attempt("alice", "10.0.0.1:1")); !errors.Is(err, errWrong) {
			t.Fatalf("err = %v, want the inner error passed through", err)
		}
	}
	good := attempt("alice", "10.0.0.1:1")
	good.Credentials["password"] = NewSecret("right")
	got, err := th.Verify(ctx, good)
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
	mem := tracker(t, 1024, Backoff{Threshold: 3, Base: time.Minute, Max: time.Minute, Forget: time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := range 4 {
		_, _ = th.Verify(ctx, attempt(fmt.Sprintf("user%d", i), "10.0.0.7:1"))
	}
	if locked, _, _ := mem.Locked(ctx, trackerKey("s", "user0"), time.Now()); locked {
		t.Error("no single subject should be locked after one failure each")
	}
	if locked, _, _ := mem.Locked(ctx, trackerKey("a", "10.0.0.7"), time.Now()); !locked {
		t.Error("the source address must be locked: a user-list walk accumulates nowhere else")
	}
}

// The port must not be part of the key: a fresh port per connection would give
// an attacker a fresh counter per attempt.
func TestThrottle_PortIsNotPartOfTheAddressKey(t *testing.T) {
	t.Parallel()
	inner := &countingFactor{}
	mem := tracker(t, 1024, Backoff{Threshold: 2, Base: time.Minute, Max: time.Minute, Forget: time.Hour})
	th, err := NewThrottle(inner, mem)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := range 3 {
		_, _ = th.Verify(ctx, attempt("alice", fmt.Sprintf("10.0.0.8:%d", 1000+i)))
	}
	if locked, _, _ := mem.Locked(ctx, trackerKey("a", "10.0.0.8"), time.Now()); !locked {
		t.Fatal("three attempts from three ports did not accumulate: the port is in the key")
	}
	if mem.Len() != 2 {
		t.Errorf("%d records for one subject and one address, want 2", mem.Len())
	}
}

func TestThrottle_PreservesKindAndPolicyValidity(t *testing.T) {
	t.Parallel()
	th, err := NewThrottle(&countingFactor{}, tracker(t, 0, Backoff{}))
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

// --- keys -------------------------------------------------------------------

// An entry cap bounds the NUMBER of records; it bounds MEMORY only if the keys
// are fixed width. Otherwise 16,384 arbitrarily long attacker-chosen strings sit
// inside the cap.
func TestTrackerKey_IsFixedWidth(t *testing.T) {
	t.Parallel()
	base := len(trackerKey("s", "a"))
	for _, v := range []string{"", "alice", "alice@example.com",
		fmt.Sprintf("%0*d", 1<<20, 7), string(make([]byte, 5<<20))} {
		if got := len(trackerKey("s", v)); got != base {
			t.Errorf("key for a %d-byte value is %d bytes, want the fixed %d", len(v), got, base)
		}
	}
	// Namespaces must not collide, or an address could lock out a subject of the
	// same name.
	if trackerKey("s", "x") == trackerKey("a", "x") {
		t.Error("namespaces collide")
	}
	if trackerKey("u", "x") == trackerKey("a", "x") {
		t.Error("the unattributed namespace collides with the address namespace")
	}
	if trackerKey("s", "x") == trackerKey("s", "y") {
		t.Error("distinct values collide")
	}
}

// --- MemTracker -------------------------------------------------------------

func TestMemTracker_BackoffGrowsAndForgets(t *testing.T) {
	t.Parallel()
	b := Backoff{Threshold: 2, Base: time.Second, Max: 8 * time.Second, Forget: time.Minute}
	m := tracker(t, 16, b)
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	for i := 1; i <= 2; i++ {
		if d, _ := m.Fail(ctx, "k", now); d != 0 {
			t.Fatalf("failure %d: backoff = %v, want 0 (below the threshold)", i, d)
		}
		if locked, _, _ := m.Locked(ctx, "k", now); locked {
			t.Fatalf("failure %d locked the key below the threshold", i)
		}
	}
	for i, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second} {
		if d, _ := m.Fail(ctx, "k", now); d != want {
			t.Errorf("failure %d past threshold: backoff = %v, want %v", i+1, d, want)
		}
	}
	locked, retry, _ := m.Locked(ctx, "k", now)
	if !locked || retry <= 0 {
		t.Fatalf("locked = %v retry = %v", locked, retry)
	}
	if locked, _, _ := m.Locked(ctx, "k", now.Add(9*time.Second)); locked {
		t.Error("still locked after the backoff elapsed")
	}
	if locked, _, _ := m.Locked(ctx, "k", now.Add(2*time.Minute)); locked {
		t.Error("a forgotten record must not report locked")
	}
	if d, _ := m.Fail(ctx, "k", now.Add(2*time.Minute)); d != 0 {
		t.Errorf("after Forget the count must restart: backoff = %v, want 0", d)
	}
}

func TestMemTracker_Reset(t *testing.T) {
	t.Parallel()
	m := tracker(t, 16, Backoff{Threshold: 0, Base: time.Minute, Max: time.Minute, Forget: time.Hour})
	ctx := context.Background()
	now := time.Now()
	if _, err := m.Fail(ctx, "k", now); err != nil {
		t.Fatal(err)
	}
	if locked, _, _ := m.Locked(ctx, "k", now); !locked {
		t.Fatal("threshold 0 must lock on the first failure")
	}
	if err := m.Reset(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if locked, _, _ := m.Locked(ctx, "k", now); locked {
		t.Error("Reset did not clear the record")
	}
	if m.Len() != 0 {
		t.Error("Reset left the record in place")
	}
}

func TestMemTracker_BoundedUnderFlood(t *testing.T) {
	t.Parallel()
	const size = 64
	m := tracker(t, size, Backoff{Threshold: 1, Base: time.Second, Max: time.Second, Forget: time.Hour})
	ctx := context.Background()
	now := time.Now()
	for i := range 100_000 {
		if _, err := m.Fail(ctx, fmt.Sprintf("s:flood-%d", i), now); err != nil {
			t.Fatal(err)
		}
		if n := m.Len(); n > size {
			t.Fatalf("after %d distinct keys the tracker holds %d records, cap is %d", i+1, n, size)
		}
	}
	if m.Len() == 0 {
		t.Error("the tracker evicted everything — no counting survives a flood at all")
	}
}

// An absolute-time sentinel (time.Unix(math.MaxInt32, 0)) selects no candidate
// once real records are dated past 2038, so nothing is evicted, the insert
// happens anyway, and the cap silently stops being enforced.
func TestMemTracker_BoundHoldsAfter2038(t *testing.T) {
	t.Parallel()
	const size = 2
	m := tracker(t, size, Backoff{Threshold: 1, Base: time.Second, Max: time.Second, Forget: 100 * 365 * 24 * time.Hour})
	ctx := context.Background()
	future := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 50 {
		if _, err := m.Fail(ctx, fmt.Sprintf("future-%d", i), future.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
		if n := m.Len(); n > size {
			t.Fatalf("with records dated 2040 the tracker grew to %d records, cap is %d "+
				"— eviction is using an absolute-time sentinel", n, size)
		}
	}
}

func TestMemTracker_SweepsExpiredBeforeEvicting(t *testing.T) {
	t.Parallel()
	const size = 4
	m := tracker(t, size, Backoff{Threshold: 1, Base: time.Second, Max: time.Second, Forget: time.Minute})
	ctx := context.Background()
	old := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for i := range size {
		if _, err := m.Fail(ctx, fmt.Sprintf("stale-%d", i), old); err != nil {
			t.Fatal(err)
		}
	}
	later := old.Add(time.Hour)
	if _, err := m.Fail(ctx, "victim", later); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	_, ok := m.records["victim"]
	n := len(m.records)
	m.mu.Unlock()
	if !ok {
		t.Fatal("the new record was not stored")
	}
	if n > size {
		t.Errorf("Len = %d, want at most %d", n, size)
	}
}

func TestMemTracker_ConcurrentUse(t *testing.T) {
	t.Parallel()
	m := tracker(t, 128, DefaultBackoff())
	ctx := context.Background()
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			now := time.Now()
			for i := range 200 {
				key := fmt.Sprintf("k%d", (g*i)%50)
				_, _ = m.Fail(ctx, key, now)
				_, _, _ = m.Locked(ctx, key, now)
				if i%7 == 0 {
					_ = m.Reset(ctx, key)
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
	b := Backoff{Threshold: 0, Base: time.Second, Max: time.Hour, Forget: time.Hour}
	for _, n := range []int{1, 10, 62, 63, 64, 1000, 1 << 20} {
		if d := b.delay(n); d < 0 || d > b.Max {
			t.Errorf("delay(%d) = %v, want within (0, %v]", n, d, b.Max)
		}
	}
	if d := b.delay(0); d != 0 {
		t.Errorf("delay(0) = %v, want 0", d)
	}
}

// Security configuration is not silently repaired: an operator who typo'd a
// lockout parameter must be told, not given different behavior than they wrote.
func TestNewMemTracker_RejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	if _, err := NewMemTracker(-1, DefaultBackoff()); err == nil {
		t.Error("a negative size must be refused")
	}
	bad := map[string]Backoff{
		"negative threshold": {Threshold: -1, Base: time.Second, Max: time.Minute, Forget: time.Hour},
		"zero base":          {Threshold: 1, Base: 0, Max: time.Minute, Forget: time.Hour},
		"negative base":      {Threshold: 1, Base: -time.Second, Max: time.Minute, Forget: time.Hour},
		"max below base":     {Threshold: 1, Base: time.Minute, Max: time.Second, Forget: time.Hour},
		"zero forget":        {Threshold: 1, Base: time.Second, Max: time.Minute, Forget: 0},
	}
	for name, b := range bad {
		if _, err := NewMemTracker(16, b); err == nil {
			t.Errorf("%s must be refused, not normalized", name)
		}
	}
	// The two documented shorthands still work.
	if _, err := NewMemTracker(0, Backoff{}); err != nil {
		t.Errorf("zero size and zero Backoff are the documented defaults: %v", err)
	}
	if d := DefaultBackoff(); d.Threshold != 5 || d.Base != time.Second || d.Max != 5*time.Minute || d.Forget != 15*time.Minute {
		t.Errorf("DefaultBackoff() = %+v", d)
	}
}

// A valid extreme configuration must saturate at Max and STAY there. Checking
// only the range misses the real failure: a large Base shifted far enough wraps
// to a small positive duration legitimately below Max, so the backoff reaches
// Max and then comes back DOWN — the most persistent attacker getting the
// shortest wait.
func TestBackoff_SaturatesMonotonically(t *testing.T) {
	t.Parallel()
	configs := map[string]Backoff{
		"ordinary":        {Threshold: 0, Base: time.Second, Max: time.Hour, Forget: time.Hour},
		"huge base":       {Threshold: 0, Base: time.Hour, Max: 24 * time.Hour, Forget: time.Hour},
		"base equals max": {Threshold: 0, Base: time.Minute, Max: time.Minute, Forget: time.Hour},
		"extreme max":     {Threshold: 0, Base: time.Hour, Max: 1 << 62, Forget: time.Hour},
		"with threshold":  {Threshold: 7, Base: 250 * time.Millisecond, Max: time.Minute, Forget: time.Hour},
	}
	for name, b := range configs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := b.validate(); err != nil {
				t.Fatalf("fixture is not a valid configuration: %v", err)
			}
			prev := time.Duration(-1)
			reachedMax := false
			for n := range 200 {
				d := b.delay(n)
				if d < 0 {
					t.Fatalf("delay(%d) = %v: negative durations read as \"not locked\"", n, d)
				}
				if d > b.Max {
					t.Fatalf("delay(%d) = %v exceeds Max %v", n, d, b.Max)
				}
				if d < prev {
					t.Fatalf("delay(%d) = %v is LESS than delay(%d) = %v: the backoff "+
						"decreases for a more persistent attacker", n, d, n-1, prev)
				}
				if d == b.Max {
					reachedMax = true
				} else if reachedMax {
					t.Fatalf("delay(%d) = %v dropped below Max after reaching it", n, d)
				}
				prev = d
			}
			if !reachedMax {
				t.Errorf("never reached Max %v within 200 failures", b.Max)
			}
		})
	}
}

// A raw digest is not valid UTF-8 and contains NUL, which the SQL and Redis
// trackers this seam exists for would have to escape or would truncate.
func TestTrackerKey_IsPrintable(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"", "alice", "alice@example.com", string([]byte{0, 1, 2, 255})} {
		k := trackerKey("s", v)
		if !utf8.ValidString(k) {
			t.Errorf("key for %q is not valid UTF-8", v)
		}
		for i, r := range k {
			if r < 0x20 || r == 0x7f {
				t.Errorf("key for %q has a control character at %d", v, i)
			}
		}
	}
}

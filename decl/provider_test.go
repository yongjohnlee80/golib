package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
)

// provider_test.go covers the subscription lifecycle.
//
// Every rule here exists because breaking it produces a defect with NO
// SYMPTOM at the point of the mistake: a lost first update looks like a stale
// value, a stale delivery looks like a flicker, and a leaked subscription looks
// like nothing at all until the process runs out of something.

// palette is a well-behaved provider: it snapshots synchronously, versions
// monotonically, and delivers its names as one atomic batch.
type palette struct {
	names []string
	now   map[string]string
	fn    func(decl.Update)
	ver   uint64

	cancelled int
	cancelErr error
	// breakContract makes the fake misbehave in a named way, so the engine's
	// refusal is tested against a provider that actually does it.
	skipSnapshot bool
	noCancel     bool
	subscribeErr error
	undeclared   string
}

func newPalette() *palette {
	return &palette{
		names: []string{"Theme.bg", "Theme.fg"},
		now:   map[string]string{"Theme.bg": "#111", "Theme.fg": "#eee"},
	}
}

func (p *palette) Subscribe(fn func(decl.Update)) ([]string, func() error, error) {
	if p.subscribeErr != nil {
		return nil, nil, p.subscribeErr
	}
	p.fn = fn
	if !p.skipSnapshot {
		vals := map[string]parse.SpecValue{}
		for n, v := range p.now {
			vals[n] = parse.SpecValue{Kind: parse.SpecValueString, Raw: v}
		}
		if p.undeclared != "" {
			vals[p.undeclared] = parse.SpecValue{Kind: parse.SpecValueString, Raw: "x"}
		}
		fn(decl.Update{Version: p.ver, Values: vals})
	}
	if p.noCancel {
		return p.names, nil, nil
	}
	return p.names, func() error { p.cancelled++; return p.cancelErr }, nil
}

// push delivers a later update at an explicit version.
func (p *palette) push(version uint64, kv map[string]string) {
	vals := map[string]parse.SpecValue{}
	for n, v := range kv {
		vals[n] = parse.SpecValue{Kind: parse.SpecValueString, Raw: v}
	}
	p.fn(decl.Update{Version: version, Values: vals})
}

// immediate runs scheduled work straight away, which is what the owning
// goroutine does when a delivery arrives while it is idle.
func immediate() func(func()) { return func(fn func()) { fn() } }

func subscribed(t *testing.T, p *palette, src string, opts ...decl.Option) (*decl.Tree, *reactor) {
	t.Helper()
	rec := newReactor()
	tr := decl.New(rec, append([]decl.Option{decl.WithScheduler(immediate())}, opts...)...)
	if err := tr.Subscribe(p); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := tr.Mount(qml(t, src)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	rec.trace = nil
	return tr, rec
}

// TestAProvidersNamesBecomeSourcesWithItsCurrentValues.
func TestAProvidersNamesBecomeSourcesWithItsCurrentValues(t *testing.T) {
	p := newPalette()
	tr, _ := subscribed(t, p, `Flex { Text { id: a text: Theme.bg } Text { id: b text: Theme.fg } }`)

	got, ok := tr.Source("Theme.bg")
	if !ok || got.Raw != "#111" {
		t.Errorf("Source(Theme.bg) = %+v, %v; want the provider's current value", got, ok)
	}
	if _, ok := tr.Source("Theme.fg"); !ok {
		t.Error("Theme.fg was not declared from the provider's names")
	}
}

// TestASubscriptionWithNoSynchronousSnapshotIsRefused.
//
// The window between "read the current values" and "start listening" is where a
// change is lost forever: it has already been delivered to nobody, and the
// engine will hold the stale value until something else happens to move it.
func TestASubscriptionWithNoSynchronousSnapshotIsRefused(t *testing.T) {
	p := newPalette()
	p.skipSnapshot = true
	tr := decl.New(newReactor(), decl.WithScheduler(immediate()))

	err := tr.Subscribe(p)
	if !errors.Is(err, decl.ErrProviderContract) {
		t.Fatalf("err = %v, want ErrProviderContract", err)
	}
	if !strings.Contains(err.Error(), "lost") {
		t.Errorf("diagnostic = %q, want it to say what the gap costs", err)
	}
	// And the refusal UNSUBSCRIBED: a provider left attached to a tree that
	// refused it goes on delivering into an engine with no sources for it.
	if p.cancelled != 1 {
		t.Errorf("cancelled %d times, want 1", p.cancelled)
	}
}

// TestARefusedSubscriptionReportsBothItsCauseAndItsCleanupFailure.
//
// First-wins would hide one of them, and which one it hid would depend on the
// order the code happened to be written in.
func TestARefusedSubscriptionReportsBothItsCauseAndItsCleanupFailure(t *testing.T) {
	p := newPalette()
	p.skipSnapshot = true
	p.cancelErr = errors.New("the socket was already closed")
	tr := decl.New(newReactor(), decl.WithScheduler(immediate()))

	err := tr.Subscribe(p)
	if !errors.Is(err, decl.ErrProviderContract) {
		t.Fatalf("err = %v, want the cause", err)
	}
	if !errors.Is(err, p.cancelErr) {
		t.Errorf("err = %v, want it to ALSO carry the cleanup failure", err)
	}
}

// TestAProviderWithNowhereToDeliverIsRefused.
//
// A provider's callback arrives from wherever the host's data lives. Without a
// scheduler it would mutate the tree underneath the goroutine that owns it, and
// the failure would be a rare corrupted frame rather than an error.
func TestAProviderWithNowhereToDeliverIsRefused(t *testing.T) {
	tr := decl.New(newReactor())
	err := tr.Subscribe(newPalette())
	if !errors.Is(err, decl.ErrNoScheduler) {
		t.Fatalf("err = %v, want ErrNoScheduler", err)
	}
	if !strings.Contains(err.Error(), "WithScheduler") {
		t.Errorf("diagnostic = %q, want it to name the remedy", err)
	}
}

// TestAProviderMustOfferAWayToUnsubscribe.
func TestAProviderMustOfferAWayToUnsubscribe(t *testing.T) {
	p := newPalette()
	p.noCancel = true
	tr := decl.New(newReactor(), decl.WithScheduler(immediate()))
	if err := tr.Subscribe(p); !errors.Is(err, decl.ErrProviderContract) {
		t.Fatalf("err = %v, want ErrProviderContract", err)
	}
}

// TestANameTheProviderNeverDeclaredIsRefused.
//
// The declared names are what become sources. A value delivered under some
// other name could never be bound by any schema, so accepting it would be
// storing something nothing can read.
func TestANameTheProviderNeverDeclaredIsRefused(t *testing.T) {
	p := newPalette()
	p.undeclared = "Theme.sneaky"
	tr := decl.New(newReactor(), decl.WithScheduler(immediate()))
	err := tr.Subscribe(p)
	if !errors.Is(err, decl.ErrProviderContract) {
		t.Fatalf("err = %v, want ErrProviderContract", err)
	}
	if !strings.Contains(err.Error(), "Theme.sneaky") {
		t.Errorf("diagnostic = %q, want it to name the offending name", err)
	}
	if p.cancelled != 1 {
		t.Errorf("cancelled %d times, want 1", p.cancelled)
	}
}

// TestAWholePaletteArrivesAsONEPropagation.
//
// The defect this prevents is a MIXED PALETTE. Applying a theme one name at a
// time leaves the screen half old and half new between the two calls, and a
// binding reading both is evaluated against a combination the host never had.
func TestAWholePaletteArrivesAsONEPropagation(t *testing.T) {
	p := newPalette()
	tr, rec := subscribed(t, p,
		`Flex { Text { id: a text: Theme.bg } Text { id: b text: Theme.fg } }`)
	_ = tr

	p.push(1, map[string]string{"Theme.bg": "#222", "Theme.fg": "#ddd"})

	got := applyLines(rec)
	if len(got) != 2 {
		t.Fatalf("applied %v, want both bindings updated", got)
	}
	// Neither application may carry a value from the OLD palette: that is what
	// "one propagation" means on the screen.
	for _, line := range got {
		if strings.Contains(line, "#111") || strings.Contains(line, "#eee") {
			t.Errorf("an old palette value was applied during the switch: %q", line)
		}
	}
}

// TestABindingReadingTwoChangedNamesRecomputesOnce.
func TestABindingReadingTwoChangedNamesRecomputesOnce(t *testing.T) {
	p := newPalette()
	rec := newReactor()
	tr := decl.New(rec, decl.WithScheduler(immediate()))
	if err := tr.Subscribe(p); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	var calls int
	if err := tr.DeclareFunc("pair", func(args []parse.SpecValue) (parse.SpecValue, error) {
		calls++
		return sv(args[0].Raw + "/" + args[1].Raw), nil
	}); err != nil {
		t.Fatalf("DeclareFunc: %v", err)
	}
	if err := tr.Mount(qml(t, `Text { id: a text: pair(Theme.bg, Theme.fg) }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	calls = 0
	rec.trace = nil

	p.push(1, map[string]string{"Theme.bg": "#222", "Theme.fg": "#ddd"})

	if calls != 1 {
		t.Errorf("the binding recomputed %d times, want 1 — a second pass would "+
			"apply an intermediate value to a real setter", calls)
	}
	if got := applyLines(rec); len(got) != 1 || !strings.Contains(got[0], "#222/#ddd") {
		t.Errorf("applied %v, want one application of the new pair", got)
	}
}

// TestAStaleDeliveryIsDropped.
//
// Deliveries reach the engine through a scheduler, so two in flight can arrive
// in either order. Without version ordering the OLDER one wins by arriving
// last, and the screen ends up showing a value the host has already replaced.
func TestAStaleDeliveryIsDropped(t *testing.T) {
	p := newPalette()
	tr, rec := subscribed(t, p, `Text { id: a text: Theme.bg }`)

	p.push(5, map[string]string{"Theme.bg": "#newer"})
	p.push(3, map[string]string{"Theme.bg": "#older"})

	got := applyLines(rec)
	if len(got) != 1 || !strings.Contains(got[0], "#newer") {
		t.Fatalf("applied %v, want only the newer delivery", got)
	}
	if cur, _ := tr.Source("Theme.bg"); cur.Raw != "#newer" {
		t.Errorf("source = %q, want the newer value to have won", cur.Raw)
	}
}

// TestARepeatedVersionIsDropped: "strictly increasing" means the same version
// twice is not news.
func TestARepeatedVersionIsDropped(t *testing.T) {
	p := newPalette()
	_, rec := subscribed(t, p, `Text { id: a text: Theme.bg }`)

	p.push(2, map[string]string{"Theme.bg": "#first"})
	p.push(2, map[string]string{"Theme.bg": "#second"})

	got := applyLines(rec)
	if len(got) != 1 || !strings.Contains(got[0], "#first") {
		t.Errorf("applied %v, want only the first delivery at that version", got)
	}
}

// TestAFailedDeliveryDoesNotAdvanceTheVersion.
//
// The same rule the quiet cache follows, for the same reason: a version
// advanced on a failure would make the provider's retry look stale and be
// dropped, and the failure would become permanent in silence.
func TestAFailedDeliveryDoesNotAdvanceTheVersion(t *testing.T) {
	p := newPalette()
	var sunk []error
	rec := newReactor()
	tr := decl.New(rec,
		decl.WithScheduler(immediate()),
		decl.WithProviderErrorSink(func(err error) { sunk = append(sunk, err) }))
	if err := tr.Subscribe(p); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := tr.Mount(qml(t, `Text { id: a text: Theme.bg }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	rec.trace = nil

	// The adapter refuses the next application.
	boom := errors.New("the setter refused")
	rec.applyErr["text"] = boom
	p.push(4, map[string]string{"Theme.bg": "#fails"})
	if len(sunk) != 1 || !errors.Is(sunk[0], boom) {
		t.Fatalf("sink got %v, want the delivery's failure", sunk)
	}

	// The SAME version retries and is not dropped as stale.
	rec.applyErr["text"] = nil
	rec.trace = nil
	p.push(4, map[string]string{"Theme.bg": "#fails"})
	if got := applyLines(rec); len(got) != 1 || !strings.Contains(got[0], "#fails") {
		t.Errorf("applied %v; the retry was dropped as stale and the failure is "+
			"now permanent", got)
	}
}

// TestDestroyUnsubscribesEveryProviderAndJoinsWhatTheyReport.
//
// A host told about one leak and not the other three has been told something
// worse than nothing.
func TestDestroyUnsubscribesEveryProviderAndJoinsWhatTheyReport(t *testing.T) {
	a, b, c := newPalette(), newPalette(), newPalette()
	b.names = []string{"Other.one"}
	b.now = map[string]string{"Other.one": "1"}
	c.names = []string{"Third.one"}
	c.now = map[string]string{"Third.one": "1"}
	aErr := errors.New("provider a could not close")
	cErr := errors.New("provider c could not close")
	a.cancelErr, c.cancelErr = aErr, cErr

	tr := decl.New(newReactor(), decl.WithScheduler(immediate()))
	for _, p := range []*palette{a, b, c} {
		if err := tr.Subscribe(p); err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
	}
	if err := tr.Mount(qml(t, `Text { id: a text: Theme.bg }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	err := tr.Destroy()
	for _, p := range []*palette{a, b, c} {
		if p.cancelled != 1 {
			t.Errorf("a provider was cancelled %d times, want 1", p.cancelled)
		}
	}
	if !errors.Is(err, aErr) || !errors.Is(err, cErr) {
		t.Errorf("Destroy = %v, want BOTH cleanup failures", err)
	}
}

// TestADeliveryAlreadyInFlightWhenTheTreeIsDestroyedIsDropped.
//
// cancel stops NEW deliveries. It cannot recall one already handed to the
// scheduler and waiting its turn, and that one would otherwise propagate into a
// tree with no nodes left.
func TestADeliveryAlreadyInFlightWhenTheTreeIsDestroyedIsDropped(t *testing.T) {
	p := newPalette()
	var queued []func()
	rec := newReactor()
	tr := decl.New(rec,
		decl.WithScheduler(func(fn func()) { queued = append(queued, fn) }),
		decl.WithProviderErrorSink(func(err error) { t.Errorf("unexpected: %v", err) }))
	if err := tr.Subscribe(p); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := tr.Mount(qml(t, `Text { id: a text: Theme.bg }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	// In flight: handed to the scheduler and NOT yet run.
	p.push(9, map[string]string{"Theme.bg": "#late"})
	if len(queued) != 1 {
		t.Fatalf("queued %d, want the delivery to be waiting", len(queued))
	}
	if err := tr.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	rec.trace = nil

	queued[0]() // the scheduler gets round to it, after the teardown

	if got := applyLines(rec); len(got) != 0 {
		t.Errorf("a delivery reached a destroyed tree: %v", got)
	}
}

// TestSubscribingAfterMountIsRefused.
func TestSubscribingAfterMountIsRefused(t *testing.T) {
	tr := decl.New(newReactor(), decl.WithScheduler(immediate()))
	if err := tr.Mount(qml(t, `Text { text: "hi" }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := tr.Subscribe(newPalette()); !errors.Is(err, decl.ErrPhase) {
		t.Errorf("err = %v, want ErrPhase", err)
	}
}

// TestALaterDeliveryUnderAnUndeclaredNameIsRefusedToo.
//
// The snapshot is checked, and so is every delivery after it. Checking only the
// first would let a provider widen its own surface after subscription: the name
// is not a declared source, so nothing can bind it, and storing it would create
// a value the engine can never hand to anyone — silently, since a delivery has
// no caller to complain to.
func TestALaterDeliveryUnderAnUndeclaredNameIsRefusedToo(t *testing.T) {
	p := newPalette()
	var sunk []error
	rec := newReactor()
	tr := decl.New(rec,
		decl.WithScheduler(immediate()),
		decl.WithProviderErrorSink(func(err error) { sunk = append(sunk, err) }))
	if err := tr.Subscribe(p); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := tr.Mount(qml(t, `Text { id: a text: Theme.bg }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	rec.trace = nil

	p.push(7, map[string]string{"Theme.bg": "#ok", "Theme.later": "#nope"})

	if len(sunk) != 1 || !errors.Is(sunk[0], decl.ErrProviderContract) {
		t.Fatalf("sink got %v, want ErrProviderContract", sunk)
	}
	if !strings.Contains(sunk[0].Error(), "Theme.later") {
		t.Errorf("diagnostic = %q, want it to name the undeclared name", sunk[0])
	}
	// And NOTHING from that delivery was applied: a batch is all or none, so
	// the legitimate half of a contract violation must not land either.
	if got := applyLines(rec); len(got) != 0 {
		t.Errorf("applied %v, want the whole delivery rejected", got)
	}
	if cur, _ := tr.Source("Theme.bg"); cur.Raw != "#111" {
		t.Errorf("Theme.bg = %q, want the delivery to have been rejected whole", cur.Raw)
	}
}

// TestARefusedSubscriptionLeavesNoSourcesBehind.
//
// The names were injected one at a time and the first refusal returned,
// leaving the earlier ones behind — so a schema could
// bind sources belonging to a provider that is NOT attached to anything, and
// nothing would ever update them, because the delivery path was torn down in
// the same breath.
func TestARefusedSubscriptionLeavesNoSourcesBehind(t *testing.T) {
	tr := decl.New(newReactor(), decl.WithScheduler(immediate()))
	// `Theme` is a constant, so `Theme.bad` cannot name something inside it.
	if err := tr.Inject("Theme", decl.Constant(sv("dark"))); err != nil {
		t.Fatalf("inject: %v", err)
	}
	p := &palette{
		names: []string{"Good", "Theme.bad"},
		now:   map[string]string{"Good": "ok", "Theme.bad": "bad"},
	}
	if err := tr.Subscribe(p); err == nil {
		t.Fatal("a provider naming a member of a constant was accepted")
	}
	if p.cancelled != 1 {
		t.Errorf("cancelled %d times, want 1", p.cancelled)
	}
	if _, ok := tr.Source("Good"); ok {
		t.Error("a refused subscription left Good registered as a source")
	}
	if _, ok := tr.Lookup("Good"); ok {
		t.Error("a refused subscription left Good in the registry")
	}
	// The name is free, so a corrected provider can claim it.
	q := &palette{names: []string{"Good"}, now: map[string]string{"Good": "ok"}}
	if err := tr.Subscribe(q); err != nil {
		t.Errorf("the tree was latched by a refused subscription: %v", err)
	}
}

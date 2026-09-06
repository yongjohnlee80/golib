package web

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

func hello() Hello {
	return Hello{Cols: 20, Rows: 5, Metrics: Metrics{CellW: 8, CellH: 16}}
}

// started returns a Backend whose Start has completed.
func started(t *testing.T, h Hello, opts ...Option) *Backend {
	t.Helper()
	b := New(opts...)
	done := make(chan error, 1)
	go func() { done <- b.Start(context.Background()) }()
	if err := b.Attach(h); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after a client attached")
	}
	t.Cleanup(func() { _ = b.Stop() })
	return b
}

// Start blocks until a client reports a size, which mirrors the terminal
// backend's probe fence: returning early would hand the App a size the server
// invented.
func TestBackend_StartWaitsForAClient(t *testing.T) {
	t.Parallel()
	b := New()
	t.Cleanup(func() { _ = b.Stop() })

	if _, err := b.Size(); !errors.Is(err, ErrNotStarted) {
		t.Errorf("Size before Start = %v, want ErrNotStarted: there is no size until a client reports one", err)
	}

	done := make(chan error, 1)
	go func() { done <- b.Start(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("Start returned %v before any client attached", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := b.Attach(hello()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return")
	}

	got, err := b.Size()
	if err != nil {
		t.Fatal(err)
	}
	if got != (tui.Size{W: 20, H: 5}) {
		t.Errorf("Size = %+v, want the client's measured 20x5", got)
	}
}

func TestBackend_StartRespectsContext(t *testing.T) {
	t.Parallel()
	b := New()
	t.Cleanup(func() { _ = b.Stop() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := b.Start(ctx)
	if !errors.Is(err, ErrNoClient) || !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want both ErrNoClient and context.Canceled", err)
	}
}

// A hello without a measured size or font metrics is refused: the server never
// guesses.
func TestBackend_AttachRequiresMeasurements(t *testing.T) {
	t.Parallel()
	for name, h := range map[string]Hello{
		"no cols":    {Rows: 5, Metrics: Metrics{CellW: 8, CellH: 16}},
		"no rows":    {Cols: 20, Metrics: Metrics{CellW: 8, CellH: 16}},
		"no metrics": {Cols: 20, Rows: 5},
		"zero width": {Cols: 20, Rows: 5, Metrics: Metrics{CellH: 16}},
		"negative":   {Cols: -1, Rows: 5, Metrics: Metrics{CellW: 8, CellH: 16}},
		"empty":      {},
	} {
		b := New()
		if err := b.Attach(h); err == nil {
			t.Errorf("%s: Attach accepted an unmeasured hello", name)
		}
		_ = b.Stop()
	}
}

// No capability is TriYes without a verifiable basis, and
// KittyKeyboard is false because a browser has no analogue.
func TestBackend_CapabilitiesAreHonest(t *testing.T) {
	t.Parallel()

	t.Run("minimal client", func(t *testing.T) {
		b := started(t, hello())
		caps := b.Capabilities()
		if caps.KittyKeyboard {
			t.Error("KittyKeyboard must be false: there is no browser analogue, and an " +
				"optimistic request is never reported as support")
		}
		if caps.Mouse != tui.TriNo {
			t.Errorf("Mouse = %v with no reported pointer, want TriNo", caps.Mouse)
		}
		if caps.UnicodeCore {
			t.Error("UnicodeCore must not be claimed without the client's font agreement")
		}
		if caps.Undercurl {
			t.Error("Undercurl must be false: no probe exists, and CSS text-decoration-style " +
				"is not the same feature")
		}
		if caps.DarkBackground {
			t.Error("DarkBackground must follow the client, not a default")
		}
		// These two are structurally true of this backend, not guesses.
		if !caps.SyncOutput {
			t.Error("SyncOutput is true by construction: the backend owns frame commit")
		}
		if !caps.InBandResize {
			t.Error("InBandResize is true: resize arrives on the same channel as everything else")
		}
		if caps.ColorProfile != tui.ProfileTrueColor {
			t.Errorf("ColorProfile = %v, want TrueColor", caps.ColorProfile)
		}
	})

	t.Run("client reporting support", func(t *testing.T) {
		h := hello()
		h.Pointer, h.PrefersDark, h.FontAgreement = true, true, true
		caps := started(t, h).Capabilities()
		if caps.Mouse != tui.TriYes {
			t.Errorf("Mouse = %v with a reported pointer, want TriYes", caps.Mouse)
		}
		if !caps.DarkBackground || !caps.UnicodeCore {
			t.Errorf("client-reported capabilities were ignored: %+v", caps)
		}
		if caps.KittyKeyboard {
			t.Error("KittyKeyboard must stay false no matter what a client claims")
		}
	})

	t.Run("constant after Start", func(t *testing.T) {
		b := started(t, hello())
		first := b.Capabilities()
		h := hello()
		h.Pointer = true
		if err := b.Attach(h); err != nil {
			t.Fatal(err)
		}
		if b.Capabilities() != first {
			t.Error("Capabilities changed after Start; the contract says constant")
		}
	})
}

// Flush must never block on a client. A vanished browser must not stall the UI.
func TestBackend_FlushNeverBlocks(t *testing.T) {
	t.Parallel()
	b := started(t, hello())

	// Take a frame and never acknowledge it: the client is gone.
	b.framer.publish(nil, cursorState{})
	if _, ok := b.NextFrame(); !ok {
		t.Fatal("no frame to strand")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 5000 {
			if err := b.Flush([]tui.CellUpdate{put(i%20, (i/20)%5, "x")}); err != nil {
				t.Errorf("Flush failed: %v", err)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Flush blocked on a client that never acknowledged")
	}
}

// Cursor state is latched and travels with the next frame, never immediately.
func TestBackend_CursorIsLatched(t *testing.T) {
	t.Parallel()
	b := started(t, hello())

	b.ShowCursor()
	b.SetCursor(4, 2)
	b.SetCursorShape(tui.CursorShapeUnderline)

	// Nothing is emitted until a Flush publishes it.
	if err := b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	fr, ok := b.NextFrame()
	if !ok {
		t.Fatal("no frame")
	}
	want := cursorState{Visible: true, X: 4, Y: 2, Shape: tui.CursorShapeUnderline}
	if fr.Cursor != want {
		t.Errorf("frame cursor = %+v, want %+v", fr.Cursor, want)
	}
	b.AckFrame(fr.Rev)

	b.HideCursor()
	if err := b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	fr, ok = b.NextFrame()
	if !ok {
		t.Fatal("hiding the cursor produced no frame")
	}
	if fr.Cursor.Visible {
		t.Error("HideCursor did not reach the next frame")
	}
}

// Criterion 7c: a sustained flood closes the connection instead of growing the
// queue. It must not drop silently and must not coalesce — both would make this
// backend behave differently from every other one.
func TestBackend_EventOverflowIsReportedNotDropped(t *testing.T) {
	t.Parallel()
	b := started(t, hello(), EventQueue(4))

	for i := range 4 {
		if err := b.Submit(tui.KeyEvent{Code: rune('a' + i)}); err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
	}
	err := b.Submit(tui.KeyEvent{Code: 'z'})
	if !errors.Is(err, ErrEventOverflow) {
		t.Fatalf("err = %v, want ErrEventOverflow — the transport must close the connection "+
			"rather than the backend dropping an event", err)
	}

	// Order is preserved and nothing was coalesced away.
	for i := range 4 {
		ev := <-b.Events()
		key, ok := ev.(tui.KeyEvent)
		if !ok || key.Code != rune('a'+i) {
			t.Fatalf("event %d = %#v, want the un-coalesced ordered stream", i, ev)
		}
	}
}

// Stop closes Events, is idempotent, and is safe from a deferred
// panic-recovery path.
func TestBackend_StopIsIdempotentAndClosesEvents(t *testing.T) {
	t.Parallel()
	b := started(t, hello())

	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-b.Events(); ok {
		t.Error("Events was not closed by Stop")
	}
	// Err is nil after a clean Stop, which is how the App loop distinguishes a
	// clean shutdown from a reader failure.
	if err := b.Err(); err != nil {
		t.Errorf("Err after a clean Stop = %v, want nil", err)
	}
	// Repeated Stops must not panic on a closed channel.
	for range 3 {
		if err := b.Stop(); err != nil {
			t.Errorf("repeated Stop: %v", err)
		}
	}
	// Post-Stop operations are refused rather than panicking.
	if err := b.Flush(nil); !errors.Is(err, ErrStopped) {
		t.Errorf("Flush after Stop = %v, want ErrStopped", err)
	}
	if err := b.Submit(tui.KeyEvent{}); !errors.Is(err, ErrStopped) {
		t.Errorf("Submit after Stop = %v, want ErrStopped", err)
	}
	if err := b.Attach(hello()); !errors.Is(err, ErrStopped) {
		t.Errorf("Attach after Stop = %v, want ErrStopped", err)
	}
}

// Concurrent Stops from several goroutines must be safe: Stop is documented as
// callable from a deferred panic-recovery path, which means races are expected.
func TestBackend_ConcurrentStop(t *testing.T) {
	t.Parallel()
	b := started(t, hello())
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Stop()
			_ = b.Flush(nil)
			_ = b.Submit(tui.KeyEvent{})
		}()
	}
	wg.Wait()
}

// Fail records the transport error, so the App loop can tell a dropped
// connection from a clean exit.
func TestBackend_FailRecordsTheTransportError(t *testing.T) {
	t.Parallel()
	b := started(t, hello())
	boom := errors.New("websocket closed abnormally")
	b.Fail(boom)

	if !errors.Is(b.Err(), boom) {
		t.Errorf("Err = %v, want the transport error", b.Err())
	}
	if _, ok := <-b.Events(); ok {
		t.Error("Fail did not stop the backend")
	}
	// The first error wins: a cascade of failures during teardown must not
	// overwrite the one that actually ended the session.
	b.Fail(errors.New("later noise"))
	if !errors.Is(b.Err(), boom) {
		t.Errorf("Err = %v, want the FIRST error", b.Err())
	}
}

// A reconnect resizes to whatever the new client measured and resyncs from
// scratch, because the new client holds nothing.
func TestBackend_ReattachResyncs(t *testing.T) {
	t.Parallel()
	b := started(t, hello())
	if err := b.Flush([]tui.CellUpdate{put(0, 0, "a")}); err != nil {
		t.Fatal(err)
	}
	fr, _ := b.NextFrame()
	b.AckFrame(fr.Rev)

	h := hello()
	h.Cols, h.Rows = 30, 8
	if err := b.Attach(h); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Size(); got != (tui.Size{W: 30, H: 8}) {
		t.Errorf("Size = %+v, want the reconnecting client's 30x8", got)
	}
	next, ok := b.NextFrame()
	if !ok {
		t.Fatal("no frame after reattach")
	}
	if !next.Full {
		t.Error("a reconnecting client holds nothing and must get a full snapshot")
	}
	// The resize is also reported as an event, so the App can relayout.
	select {
	case ev := <-b.Events():
		if r, ok := ev.(tui.ResizeEvent); !ok || r.W != 30 || r.H != 8 {
			t.Errorf("event = %#v, want ResizeEvent{30, 8}", ev)
		}
	default:
		t.Error("reattach did not emit a resize event")
	}
}

func TestBackend_ResizeEmitsEventAndChangesSize(t *testing.T) {
	t.Parallel()
	b := started(t, hello())
	b.Resize(40, 12)
	if got, _ := b.Size(); got != (tui.Size{W: 40, H: 12}) {
		t.Errorf("Size = %+v, want 40x12", got)
	}
	select {
	case ev := <-b.Events():
		if r, ok := ev.(tui.ResizeEvent); !ok || r.W != 40 || r.H != 12 {
			t.Errorf("event = %#v", ev)
		}
	default:
		t.Fatal("no resize event")
	}
	// A nonsense size is ignored rather than collapsing the grid.
	b.Resize(0, 0)
	b.Resize(-5, 3)
	if got, _ := b.Size(); got != (tui.Size{W: 40, H: 12}) {
		t.Errorf("Size = %+v after nonsense resizes, want 40x12 unchanged", got)
	}
}

// The logged session record must never carry user content.
func TestSessionEvent_CarriesNoUserContent(t *testing.T) {
	t.Parallel()
	got := sessionEvent{Kind: "start", Cols: 80, Rows: 24}.String()
	if got != "web session start size=80x24" {
		t.Errorf("String() = %q", got)
	}
	if got := (sessionEvent{Kind: "stop"}).String(); got != "web session stop" {
		t.Errorf("String() = %q", got)
	}
}

// A render that happens BEFORE the client attaches must not be lost.
//
// The fourth candidate in the ordering family, checked rather than assumed:
// Flush requires no size and publishes unconditionally, so an App that renders
// before Start returns lands cells in a grid no client has measured yet. If the
// attach-time resize reallocated that grid, the first thing the user ever sees
// would be blank until something else happened to repaint.
//
// It survives, for two independent reasons, and the test pins both: the framer
// starts at 80x24 rather than empty, so an in-range publish has somewhere to land;
// and the first frame any client receives is FULL, so it carries the grid rather
// than a diff against a baseline the client does not hold.
//
// Content outside the client's measured grid is still dropped, which is correct —
// the client cannot display a column it does not have — and the test states that
// too, so the property is not mistaken for "nothing is ever dropped".
func TestBackend_RenderBeforeAttachSurvivesIntoTheFirstFrame(t *testing.T) {
	t.Parallel()
	b := New()
	t.Cleanup(func() { _ = b.Stop() })

	// In range of the client's eventual 20x5, and out of range of it.
	if err := b.Flush([]tui.CellUpdate{
		{X: 0, Y: 0, Cell: tui.Cell{Content: "A", Width: 1}},
		{X: 19, Y: 4, Cell: tui.Cell{Content: "B", Width: 1}},
		{X: 40, Y: 10, Cell: tui.Cell{Content: "C", Width: 1}},
	}); err != nil {
		t.Fatalf("Flush before any client attached: %v", err)
	}

	started := make(chan error, 1)
	go func() { started <- b.Start(context.Background()) }()
	if err := b.Attach(hello()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after the client attached")
	}

	f, ok := b.framer.next()
	if !ok {
		t.Fatal("no frame for a newly attached client")
	}
	if !f.Full {
		t.Error("the first frame is a diff: a client holding no baseline cannot apply one")
	}
	got := map[string]bool{}
	for _, u := range f.Updates {
		if c := u.Cell.Content; c != " " && c != "" {
			got[c] = true
		}
	}
	if !got["A"] || !got["B"] {
		t.Errorf("first frame carries %v: a render that happened before the client "+
			"attached was lost, so the user's first screen is blank", got)
	}
	if got["C"] {
		t.Error("a cell outside the client's measured grid reached the frame")
	}
}

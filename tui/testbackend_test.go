package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestTestBackendInjectOrder: scripted input arrives on Events() in call
// order across event kinds (ADR-0002 §2.3, §2.9 ordering contract).
func TestTestBackendInjectOrder(t *testing.T) {
	b := NewTestBackend(10, 4)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	evs := []Event{
		KeyEvent{Kind: KeyPress, Code: 'a', Text: "a"},
		MouseEvent{Kind: MousePress, Button: MouseLeft, X: 3, Y: 2},
		PasteEvent{Text: "hi\n"},
		FocusEvent{Gained: true, Terminal: true},
	}
	if err := b.Inject(evs...); err != nil {
		t.Fatal(err)
	}
	for i, want := range evs {
		if got := <-b.Events(); got != want {
			t.Fatalf("event %d = %#v, want %#v", i, got, want)
		}
	}
}

// TestTestBackendInjectOverflow: Inject returns an error when the script
// exceeds the configured buffer instead of blocking — fail loud
// (ADR-0002 §2.3 rev 1).
func TestTestBackendInjectOverflow(t *testing.T) {
	b := NewTestBackend(10, 4, WithTestEventBuffer(2))
	err := b.Inject(
		KeyEvent{Code: '1'},
		KeyEvent{Code: '2'},
		KeyEvent{Code: '3'},
	)
	if err == nil {
		t.Fatal("Inject over the buffer returned nil, want error")
	}
	if !strings.Contains(err.Error(), "cap 2") {
		t.Errorf("overflow error %q does not name the buffer cap", err)
	}
	// The events that fit were still delivered, in order.
	for _, want := range []rune{'1', '2'} {
		if got := (<-b.Events()).(KeyEvent).Code; got != want {
			t.Fatalf("delivered %q, want %q", got, want)
		}
	}
}

// TestTestBackendStopAndErr: Stop closes Events() and is idempotent; Err()
// is nil after a clean Stop and returns the scripted error after SetErr
// (ADR-0002 §2.3, §5.7).
func TestTestBackendStopAndErr(t *testing.T) {
	t.Run("clean stop", func(t *testing.T) {
		b := NewTestBackend(4, 2)
		if err := b.Stop(); err != nil {
			t.Fatal(err)
		}
		if err := b.Stop(); err != nil { // double-Stop is a no-op (ADR-0002 §5.6)
			t.Fatalf("second Stop = %v", err)
		}
		if _, open := <-b.Events(); open {
			t.Fatal("Events() still open after Stop")
		}
		if err := b.Err(); err != nil {
			t.Fatalf("Err() = %v after clean Stop, want nil", err)
		}
		if err := b.Inject(KeyEvent{}); err == nil {
			t.Fatal("Inject after Stop returned nil, want error")
		}
	})

	t.Run("scripted reader error", func(t *testing.T) {
		b := NewTestBackend(4, 2)
		scripted := errors.New("reader failed")
		b.SetErr(scripted)
		if err := b.Stop(); err != nil {
			t.Fatal(err)
		}
		if _, open := <-b.Events(); open {
			t.Fatal("Events() still open after Stop")
		}
		if err := b.Err(); !errors.Is(err, scripted) {
			t.Fatalf("Err() = %v, want scripted %v", err, scripted)
		}
	})
}

// TestTestBackendSnapshotAndString: Flush applies the diff to the grid;
// Snapshot returns a deep copy; String renders one row per line with wide
// clusters standing for both columns.
func TestTestBackendSnapshotAndString(t *testing.T) {
	b := NewTestBackend(4, 2)
	if err := b.Flush([]CellUpdate{
		{X: 0, Y: 0, Cell: narrow("h")},
		{X: 1, Y: 0, Cell: narrow("i")},
		{X: 0, Y: 1, Cell: wide("世")},
	}); err != nil {
		t.Fatal(err)
	}

	if got, want := b.String(), "hi  \n世  "; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}

	snap := b.Snapshot()
	if snap[1][0].Width != 2 || !snap[1][1].Continuation() {
		t.Fatalf("snapshot wide pair wrong: %+v %+v", snap[1][0], snap[1][1])
	}
	snap[0][0] = narrow("X") // mutating the copy must not touch the backend
	if again := b.Snapshot(); again[0][0].Content != "h" {
		t.Fatal("Snapshot is not a deep copy")
	}
}

// TestTestBackendFlushPanicsOnOrphan: a diff that leaves an orphaned
// wide-cell half panics with a coordinate in the message (ADR-0002 §2.3 /
// ADR-0003 §2.3 W1, §5.3).
func TestTestBackendFlushPanicsOnOrphan(t *testing.T) {
	tests := []struct {
		name string
		prep []CellUpdate
		bad  []CellUpdate
	}{
		{
			name: "narrow over head orphans the continuation",
			prep: []CellUpdate{{X: 0, Y: 0, Cell: wide("世")}},
			bad:  []CellUpdate{{X: 0, Y: 0, Cell: narrow("a")}},
		},
		{
			name: "wide head in the last column (W3)",
			bad:  []CellUpdate{{X: 3, Y: 0, Cell: wide("世")}},
		},
		{
			name: "update outside the grid",
			bad:  []CellUpdate{{X: 9, Y: 9, Cell: narrow("a")}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewTestBackend(4, 1)
			if tt.prep != nil {
				if err := b.Flush(tt.prep); err != nil {
					t.Fatal(err)
				}
			}
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("Flush did not panic on an invariant violation")
				}
				if !strings.Contains(r.(string), "(") {
					t.Errorf("panic %q carries no coordinate", r)
				}
			}()
			_ = b.Flush(tt.bad)
		})
	}
}

// TestTestBackendInjectResize: InjectResize resizes and invalidates the
// grid and posts a ResizeEvent — the externally observable behavior of a
// real resize (ADR-0002 §2.3).
func TestTestBackendInjectResize(t *testing.T) {
	b := NewTestBackend(4, 2)
	if err := b.Flush([]CellUpdate{{X: 0, Y: 0, Cell: narrow("x")}}); err != nil {
		t.Fatal(err)
	}

	b.InjectResize(6, 3)
	if got := <-b.Events(); got != (ResizeEvent{W: 6, H: 3}) {
		t.Fatalf("resize event = %#v", got)
	}
	if s, _ := b.Size(); s != (Size{W: 6, H: 3}) {
		t.Fatalf("Size() = %+v after resize", s)
	}
	if got, want := b.String(), strings.TrimRight(strings.Repeat(strings.Repeat(" ", 6)+"\n", 3), "\n"); got != want {
		t.Fatalf("grid not invalidated after resize: %q", got)
	}
}

// TestTestBackendCursorLatching: cursor ops record desired state which only
// the next Flush applies — a frame is always one write (ADR-0002 §2.1).
func TestTestBackendCursorLatching(t *testing.T) {
	b := NewTestBackend(4, 2)
	b.SetCursor(2, 1)
	b.ShowCursor()
	b.SetCursorShape(CursorShapeBar)

	if x, y, vis := b.CursorPos(); x != 0 || y != 0 || vis {
		t.Fatalf("cursor applied before Flush: (%d, %d, %v)", x, y, vis)
	}
	if err := b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	if x, y, vis := b.CursorPos(); x != 2 || y != 1 || !vis {
		t.Fatalf("cursor after Flush = (%d, %d, %v), want (2, 1, true)", x, y, vis)
	}
	if got := b.CursorShape(); got != CursorShapeBar {
		t.Fatalf("cursor shape = %v, want bar", got)
	}
	if b.Flushes() != 1 {
		t.Fatalf("flushes = %d, want 1", b.Flushes())
	}
}

// TestTestBackendCapabilities: the default profile is everything-on; the
// option overrides it (ADR-0002 §2.3).
func TestTestBackendCapabilities(t *testing.T) {
	if caps := NewTestBackend(4, 2).Capabilities(); caps != fullCapabilities() {
		t.Fatalf("default caps = %+v", caps)
	}
	custom := Capabilities{ColorProfile: ProfileANSI16, Mouse: TriNo, DarkBackground: true}
	if caps := NewTestBackend(4, 2, WithTestCapabilities(custom)).Capabilities(); caps != custom {
		t.Fatalf("caps = %+v, want %+v", caps, custom)
	}
}

// TestTestBackendConstraintViolations: the ADR-0004 §2.7.1 rev 1 recording
// stub retains violations for assertion; FailOnViolations passes on a clean
// run.
func TestTestBackendConstraintViolations(t *testing.T) {
	b := NewTestBackend(4, 2)
	FailOnViolations(t, b) // clean run: must not fail t

	v := ConstraintViolation{
		Node: 7,
		Type: "*widget.List",
		Got:  Size{W: 100, H: 1},
		C:    Constraints{MinW: 0, MaxW: 10, MinH: 0, MaxH: 1},
	}
	b.RecordConstraintViolation(v)
	got := b.ConstraintViolations()
	if len(got) != 1 || got[0] != v {
		t.Fatalf("ConstraintViolations() = %+v, want [%+v]", got, v)
	}
}

// TestTestBackendConcurrentInjectAndDrain exercises the channel under -race:
// a producer goroutine injecting while the test drains.
func TestTestBackendConcurrentInjectAndDrain(t *testing.T) {
	b := NewTestBackend(4, 2, WithTestEventBuffer(8))
	const n = 200
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < n; {
			if err := b.Inject(KeyEvent{Code: rune(i)}); err == nil {
				i++
			}
		}
	}()
	for i := 0; i < n; i++ {
		if got := (<-b.Events()).(KeyEvent).Code; got != rune(i) {
			t.Fatalf("event %d = %d, out of order", i, got)
		}
	}
	<-done
	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
}

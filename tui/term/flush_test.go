package term

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// countingWriter records every Write, so the tests can assert write COUNTS
// and not just bytes.
type countingWriter struct {
	mu     sync.Mutex
	writes int
	buf    strings.Builder
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	w.buf.Write(p)
	return len(p), nil
}

func (w *countingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *countingWriter) Writes() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

func (w *countingWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = 0
	w.buf.Reset()
}

// flushBackend builds an unstarted harness backend with caps set directly —
// Flush needs no terminal acquisition.
func flushBackend(caps tui.Capabilities) (*Backend, *countingWriter) {
	w := &countingWriter{}
	b := newHarness(strings.NewReader(""), w)
	b.caps = caps
	return b, w
}

func cellAt(x, y int, s string, attrs tui.CellAttrs) tui.CellUpdate {
	return tui.CellUpdate{X: x, Y: y, Cell: tui.Cell{Content: s, Width: 1, Attrs: attrs}}
}

// shortWriter accepts at most chunk bytes per call — a non-blocking tty that
// takes only what fits its buffer and short-writes the rest (io.Writer-legal:
// n < len(p), nil error).
type shortWriter struct {
	chunk int
	buf   strings.Builder
	calls int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.calls++
	n := len(p)
	if n > w.chunk {
		n = w.chunk
	}
	w.buf.Write(p[:n])
	return n, nil
}

// TestFlushShortWritesReassembleFullFrame guards the regression where a single
// Flush emitted one output.Write and dropped a short count, truncating the
// frame on a non-blocking tty (the "only the top of the screen paints" bug).
// writeAll must loop until the whole frame lands.
func TestFlushShortWritesReassembleFullFrame(t *testing.T) {
	w := &shortWriter{chunk: 8}
	b := newHarness(strings.NewReader(""), w)
	b.caps = tui.Capabilities{}
	diff := make([]tui.CellUpdate, 0, 40)
	for y := 0; y < 40; y++ {
		diff = append(diff, cellAt(0, y, "X", tui.CellAttrs{}))
	}
	if err := b.Flush(diff); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(w.buf.String(), "X"); got != 40 {
		t.Fatalf("short-write flush truncated: got %d of 40 cells\n%q", got, w.buf.String())
	}
	if w.calls < 2 {
		t.Fatalf("shortWriter not exercised (calls=%d) — test is not meaningful", w.calls)
	}
}

func TestFlushEmptyDiffWritesZeroBytes(t *testing.T) {
	// Empty diff + unchanged cursor = zero bytes, zero Writes.
	b, w := flushBackend(tui.Capabilities{SyncOutput: true})
	if err := b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	if w.Writes() != 0 {
		t.Fatalf("empty flush wrote %d times: %q", w.Writes(), w.String())
	}
}

func TestFlushOneWritePerFrame(t *testing.T) {
	// Any non-empty frame is exactly ONE Write.
	b, w := flushBackend(tui.Capabilities{SyncOutput: true})
	diff := []tui.CellUpdate{
		cellAt(0, 0, "h", tui.CellAttrs{}),
		cellAt(1, 0, "i", tui.CellAttrs{}),
		cellAt(5, 3, "x", tui.CellAttrs{Mask: tui.AttrBold}),
	}
	b.SetCursor(2, 2)
	b.ShowCursor()
	if err := b.Flush(diff); err != nil {
		t.Fatal(err)
	}
	if w.Writes() != 1 {
		t.Fatalf("frame took %d writes, want 1", w.Writes())
	}
}

func TestFlushGoldenBasicRun(t *testing.T) {
	// Without mode 2026: hide cursor, single CUP for
	// a contiguous run, single SGR for uniform attrs, overwrite in place —
	// no ED/EL anywhere.
	b, w := flushBackend(tui.Capabilities{})
	err := b.Flush([]tui.CellUpdate{
		cellAt(0, 0, "h", tui.CellAttrs{}),
		cellAt(1, 0, "i", tui.CellAttrs{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?25l" + // R1: hide during paint
		"\x1b[1;1H" + // CUP once at run start
		"\x1b[0m" + // SGR once for the run
		"hi"
	if got := w.String(); got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(w.String(), "\x1b[2J") || strings.Contains(w.String(), "\x1b[K") {
		t.Fatal("R2 violated: clear sequence emitted")
	}
}

func TestFlushGoldenSyncBrackets(t *testing.T) {
	// Mode-2026 brackets the frame when SyncOutput is available.
	b, w := flushBackend(tui.Capabilities{SyncOutput: true})
	if err := b.Flush([]tui.CellUpdate{cellAt(0, 0, "x", tui.CellAttrs{})}); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?2026h\x1b[?25l\x1b[1;1H\x1b[0mx\x1b[?2026l"
	if got := w.String(); got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestFlushGoldenCUPOnlyOnDiscontinuity(t *testing.T) {
	b, w := flushBackend(tui.Capabilities{})
	err := b.Flush([]tui.CellUpdate{
		cellAt(0, 0, "a", tui.CellAttrs{}),
		cellAt(1, 0, "b", tui.CellAttrs{}),
		cellAt(5, 0, "c", tui.CellAttrs{}), // gap: needs CUP
		cellAt(0, 2, "d", tui.CellAttrs{}), // new row: needs CUP
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?25l\x1b[1;1H\x1b[0mab\x1b[1;6Hc\x1b[3;1Hd"
	if got := w.String(); got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestFlushGoldenSGROnlyOnChange(t *testing.T) {
	bold := tui.CellAttrs{Mask: tui.AttrBold}
	red := tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorANSI, Index: 1}}
	b, w := flushBackend(tui.Capabilities{})
	err := b.Flush([]tui.CellUpdate{
		cellAt(0, 0, "a", bold),
		cellAt(1, 0, "b", bold), // same attrs: no SGR
		cellAt(2, 0, "c", red),  // change: one SGR
		cellAt(3, 0, "d", red),  // same: none
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?25l\x1b[1;1H\x1b[0;1mab\x1b[0;31mcd"
	if got := w.String(); got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}

	// A second frame continuing with the same attrs emits no SGR at all.
	w.Reset()
	if err := b.Flush([]tui.CellUpdate{cellAt(4, 0, "e", red)}); err != nil {
		t.Fatal(err)
	}
	if got := w.String(); got != "e" {
		t.Fatalf("attr latch across frames broken: %q", got)
	}
}

func TestFlushGoldenSGRForms(t *testing.T) {
	cases := []struct {
		name  string
		attrs tui.CellAttrs
		want  string
	}{
		{"default", tui.CellAttrs{}, "\x1b[0m"},
		{"bold underline", tui.CellAttrs{Mask: tui.AttrBold | tui.AttrUnderline}, "\x1b[0;1;4m"},
		{"all attrs", tui.CellAttrs{
			Mask: tui.AttrBold | tui.AttrFaint | tui.AttrItalic | tui.AttrUnderline |
				tui.AttrBlink | tui.AttrReverse | tui.AttrStrikethrough,
		}, "\x1b[0;1;2;3;4;5;7;9m"},
		{"ansi fg", tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorANSI, Index: 2}}, "\x1b[0;32m"},
		{"ansi bright fg", tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorANSI, Index: 9}}, "\x1b[0;91m"},
		{"ansi bg", tui.CellAttrs{BG: tui.CellColor{Kind: tui.CellColorANSI, Index: 4}}, "\x1b[0;44m"},
		{"ansi bright bg", tui.CellAttrs{BG: tui.CellColor{Kind: tui.CellColorANSI, Index: 15}}, "\x1b[0;107m"},
		{"256 fg", tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorANSI256, Index: 208}}, "\x1b[0;38;5;208m"},
		{"256 bg", tui.CellAttrs{BG: tui.CellColor{Kind: tui.CellColorANSI256, Index: 17}}, "\x1b[0;48;5;17m"},
		{"rgb both", tui.CellAttrs{
			FG:   tui.CellColor{Kind: tui.CellColorRGB, R: 1, G: 2, B: 3},
			BG:   tui.CellColor{Kind: tui.CellColorRGB, R: 250, G: 251, B: 252},
			Mask: tui.AttrItalic,
		}, "\x1b[0;3;38;2;1;2;3;48;2;250;251;252m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, w := flushBackend(tui.Capabilities{})
			if err := b.Flush([]tui.CellUpdate{cellAt(0, 0, "x", tc.attrs)}); err != nil {
				t.Fatal(err)
			}
			want := "\x1b[?25l\x1b[1;1H" + tc.want + "x"
			if got := w.String(); got != want {
				t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestFlushWideCellDiscipline(t *testing.T) {
	// A wide head advances the pen 2 columns; the continuation entry (W2
	// pairs always dirty together) emits nothing and forces no CUP.
	b, w := flushBackend(tui.Capabilities{UnicodeCore: true})
	err := b.Flush([]tui.CellUpdate{
		{X: 0, Y: 0, Cell: tui.Cell{Content: "世", Width: 2}},
		{X: 1, Y: 0, Cell: tui.Cell{Content: "", Width: 0}}, // continuation
		{X: 2, Y: 0, Cell: tui.Cell{Content: "x", Width: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?25l\x1b[1;1H\x1b[0m世x"
	if got := w.String(); got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestFlushRiskyClusterReanchor(t *testing.T) {
	// Without mode 2027 the cursor is re-anchored absolutely
	// after a risky (ZWJ) cluster; with 2027 the advance is trusted.
	farmer := "\U0001F9D1‍\U0001F33E" // 🧑‍🌾

	b, w := flushBackend(tui.Capabilities{}) // no UnicodeCore
	err := b.Flush([]tui.CellUpdate{
		{X: 0, Y: 0, Cell: tui.Cell{Content: farmer, Width: 2}},
		{X: 2, Y: 0, Cell: tui.Cell{Content: "x", Width: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?25l\x1b[1;1H\x1b[0m" + farmer + "\x1b[1;3Hx"
	if got := w.String(); got != want {
		t.Fatalf("no-2027 golden mismatch\n got: %q\nwant: %q", got, want)
	}

	b2, w2 := flushBackend(tui.Capabilities{UnicodeCore: true})
	if err := b2.Flush([]tui.CellUpdate{
		{X: 0, Y: 0, Cell: tui.Cell{Content: farmer, Width: 2}},
		{X: 2, Y: 0, Cell: tui.Cell{Content: "x", Width: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	want2 := "\x1b[?25l\x1b[1;1H\x1b[0m" + farmer + "x"
	if got := w2.String(); got != want2 {
		t.Fatalf("2027 golden mismatch\n got: %q\nwant: %q", got, want2)
	}
}

func TestRiskyCluster(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"a", false},
		{"世", false}, // single-rune wide CJK: never risky
		{"🎉", false},
		{"\U0001F9D1‍\U0001F33E", true}, // ZWJ
		{"1️⃣", true},                   // VS16 keycap
		{"✂︎", true},                    // VS15
		{"\U0001F1E6\U0001F1FA", true},  // RI pair (flag)
		{"é", false},                   // plain combining accent: not risky
	}
	for _, tc := range cases {
		if got := riskyCluster(tc.s); got != tc.want {
			t.Errorf("riskyCluster(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestFlushLatchedCursor(t *testing.T) {
	// Cursor ops are latched and land in the same write as
	// the diff; cursor-only changes flush without cells.
	b, w := flushBackend(tui.Capabilities{})
	b.SetCursor(2, 1)
	b.SetCursorShape(tui.CursorShapeBar)
	b.ShowCursor()
	err := b.Flush([]tui.CellUpdate{cellAt(0, 0, "a", tui.CellAttrs{})})
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?25l\x1b[1;1H\x1b[0ma" +
		"\x1b[2;3H" + // latched position (1-based on the wire)
		"\x1b[6 q" + // DECSCUSR bar
		"\x1b[?25h" // latched visible
	if got := w.String(); got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
	if w.Writes() != 1 {
		t.Fatalf("latched cursor split the frame into %d writes", w.Writes())
	}

	// Unchanged latched state: next flush of the same cursor is zero bytes.
	w.Reset()
	if err := b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	if w.Writes() != 0 {
		t.Fatalf("unchanged cursor state still wrote: %q", w.String())
	}

	// Cursor-only change flushes without cells (one write). The latched
	// position is re-asserted absolutely, which the flush path does
	// unconditionally on every emitting frame.
	b.HideCursor()
	if err := b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	if got := w.String(); got != "\x1b[2;3H\x1b[?25l" {
		t.Fatalf("cursor-only flush: %q", got)
	}
	if w.Writes() != 1 {
		t.Fatalf("cursor-only flush took %d writes", w.Writes())
	}
}

func TestFlushAfterStopErrClosed(t *testing.T) {
	b, _ := flushBackend(tui.Capabilities{})
	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := b.Flush(nil); err != ErrClosed {
		t.Fatalf("Flush after Stop = %v, want ErrClosed", err)
	}
	if _, err := b.Size(); err != ErrClosed {
		t.Fatalf("Size after Stop = %v, want ErrClosed", err)
	}
}

package widget_test

// BufferView + Writer handle contract.

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func mountedView(t *testing.T, w, h int, opts ...widget.BufferViewOption) (*harness, *widget.BufferView, *shell) {
	t.Helper()
	v := widget.NewBufferView(opts...)
	sh := newShell(v)
	hh := startApp(t, sh, w, h)
	hh.inject(tab())
	hh.barrier(sh)
	return hh, v, sh
}

// write pushes s through the handle from the test goroutine and fails on
// error.
func write(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("Writer.Write: %v", err)
	}
}

// TestBufferViewANSIStyledCells asserts git-style SGR content through
// Writer() produces styled cells matching the escapes.
func TestBufferViewANSIStyledCells(t *testing.T) {
	h, v, _ := mountedView(t, 30, 5)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	write(t, w, "\x1b[31mred\x1b[0m plain \x1b[1;38;5;42mbold256\x1b[0m\n")
	h.waitFor("styled line", func() bool { return strings.Contains(h.grid(), "bold256") })

	// "red" in ANSI red (index 1).
	a := cellAttrs(h, 0, 0)
	if a.FG.Kind != tui.CellColorANSI || a.FG.Index != 1 {
		t.Fatalf("cell 'r' FG = %+v, want ANSI 1", a.FG)
	}
	// " plain " back at the terminal default.
	a = cellAttrs(h, 4, 0)
	if a.FG.Kind != tui.CellColorDefault || a.Mask != 0 {
		t.Fatalf("plain cell FG = %+v mask=%v, want default", a.FG, a.Mask)
	}
	// "bold256": bold + ANSI-256 index 42.
	a = cellAttrs(h, 10, 0)
	if a.FG.Kind != tui.CellColorANSI256 || a.FG.Index != 42 || a.Mask&tui.AttrBold == 0 {
		t.Fatalf("bold256 cell = %+v, want bold ANSI256(42)", a)
	}
}

// TestBufferViewPassthroughOff asserts that WithANSIPassthrough(false)
// strips escapes.
func TestBufferViewPassthroughOff(t *testing.T) {
	h, v, _ := mountedView(t, 30, 5, widget.WithANSIPassthrough(false))
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })
	write(t, w, "\x1b[31mred\x1b[0m plain\x1b]0;title\x07!\n")
	h.waitFor("stripped line", func() bool { return strings.Contains(h.grid(), "red plain!") })
	if a := cellAttrs(h, 0, 0); a.FG.Kind != tui.CellColorDefault {
		t.Fatalf("passthrough-off styled a cell: %+v", a.FG)
	}
}

// TestBufferViewFollowTail asserts follow keeps the tail pinned;
// scroll-up disengages (event); End re-engages (event).
func TestBufferViewFollowTail(t *testing.T) {
	h, v, sh := mountedView(t, 20, 3)
	follows := record[widget.FollowTailChangedEvent](h)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	for i := 1; i <= 6; i++ {
		write(t, w, fmt.Sprintf("line-%d\n", i))
	}
	h.waitFor("tail pinned", func() bool { return strings.Contains(h.grid(), "line-6") })
	h.wantNotContains("line-1")

	h.inject(key(tui.KeyUp))
	h.barrier(sh)
	if ev, ok := follows.last(); !ok || ev.Following {
		t.Fatalf("scroll-up did not disengage follow: %+v", ev)
	}
	// New writes no longer move the view.
	before := h.grid()
	write(t, w, "line-7\n")
	h.settle()
	if h.grid() != before {
		t.Fatalf("view moved while not following")
	}

	h.inject(key(tui.KeyEnd))
	h.barrier(sh)
	if ev, ok := follows.last(); !ok || !ev.Following {
		t.Fatalf("End did not re-engage follow: %+v", ev)
	}
	h.wantContains("line-7")
}

// TestBufferViewRing asserts the MaxLines ring drops the oldest
// lines.
func TestBufferViewRing(t *testing.T) {
	h, v, _ := mountedView(t, 20, 10, widget.WithMaxLines(3))
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })
	write(t, w, "a\nb\nc\nd\ne\n")
	h.waitFor("ring settled", func() bool { return strings.Contains(h.grid(), "e") })
	var n int
	h.onLoop(func() { n = v.LineCount() })
	if n != 3 {
		t.Fatalf("LineCount = %d, want MaxLines 3", n)
	}
	h.wantNotContains("a")
	h.wantNotContains("b")
	h.wantContains("d")
}

// TestBufferViewPartialLineAndCR: a partial trailing line renders and is
// extended by the next write; a bare \r overwrites the current line.
func TestBufferViewPartialLineAndCR(t *testing.T) {
	h, v, _ := mountedView(t, 20, 4)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	write(t, w, "prog: 10%")
	h.waitFor("partial line", func() bool { return strings.Contains(h.grid(), "prog: 10%") })
	write(t, w, "\rprog: 90%")
	h.waitFor("overwritten", func() bool { return strings.Contains(h.grid(), "prog: 90%") })
	h.wantNotContains("10%")
	write(t, w, " done\n")
	h.waitFor("extended", func() bool { return strings.Contains(h.grid(), "prog: 90% done") })
}

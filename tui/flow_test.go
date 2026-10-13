package tui

import (
	"strings"
	"testing"
)

// flow_test.go: Flow, Qt's wrapping row: children at their own size, left to right, a new line
// when the next would pass the right edge.

// flowOf is a Flow of probes, each w×h and filled with its letter.
func flowOf(spacing, rowSpacing int, sizes ...Size) (*Flow, []*probe) {
	f := NewFlow()
	f.SetSpacing(spacing)
	f.SetRowSpacing(rowSpacing)
	var ps []*probe
	for i, sz := range sizes {
		p := &probe{name: string(rune('a' + i)), pref: sz, fill: string(rune('a' + i))}
		ps = append(ps, p)
		f.Add(p)
	}
	return f, ps
}

func frame(rows ...string) string { return strings.Join(rows, "\n") }

func TestAFlowWithRoomIsOneLine(t *testing.T) {
	t.Parallel()
	f, _ := flowOf(1, 0, Size{W: 3, H: 1}, Size{W: 2, H: 1}, Size{W: 4, H: 1})
	h := startApp(t, f, 14, 2)
	if got, want := h.tb.String(), frame("aaa bb cccc   ", "              "); got != want {
		t.Fatalf("frame:\n%q\nwant\n%q", got, want)
	}
	FailOnViolations(t, h.tb)
}

func TestAFlowWrapsWhereTheNextChildWouldPassTheEdge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		width, rowSpacing int
		want              string
	}{
		// 4 + 1 + 4 is the width exactly: the second child fits, the third starts a line
		{"an exact fit stays on the line", 9, 0, frame("aaaa bbbb", "cccc     ", "         ", "         ")},
		// one cell short of the second: each child on its own line
		{"one cell short wraps", 8, 0, frame("aaaa    ", "bbbb    ", "cccc    ", "        ")},
		// rowSpacing follows spacing (-1): a row between the lines
		{"the row gap follows the spacing", 9, -1, frame("aaaa bbbb", "         ", "cccc     ", "         ")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, _ := flowOf(1, tc.rowSpacing, Size{W: 4, H: 1}, Size{W: 4, H: 1}, Size{W: 4, H: 1})
			h := startApp(t, f, tc.width, 4)
			if got := h.tb.String(); got != tc.want {
				t.Fatalf("frame:\n%q\nwant\n%q", got, tc.want)
			}
			FailOnViolations(t, h.tb)
		})
	}
}

// A line is as tall as its tallest child, and the next line starts below it.
func TestAFlowLineIsAsTallAsItsTallestChild(t *testing.T) {
	t.Parallel()
	f, ps := flowOf(0, 0, Size{W: 2, H: 1}, Size{W: 2, H: 3}, Size{W: 3, H: 1})
	h := startApp(t, f, 5, 5)
	rects := childRects(h, ps[0], ps[1], ps[2])
	want := []Rect{{X: 0, Y: 0, W: 2, H: 1}, {X: 2, Y: 0, W: 2, H: 3}, {X: 0, Y: 3, W: 3, H: 1}}
	for i := range want {
		if rects[i] != want[i] {
			t.Errorf("child %d at %+v, want %+v", i, rects[i], want[i])
		}
	}
}

// A child wider than the Flow is given the whole width, on a line of its own; the next child
// starts the line after it.
func TestAChildWiderThanTheFlowHasALineOfItsOwn(t *testing.T) {
	t.Parallel()
	f, ps := flowOf(1, 0, Size{W: 2, H: 1}, Size{W: 20, H: 1}, Size{W: 2, H: 1})
	h := startApp(t, f, 6, 4)
	rects := childRects(h, ps[0], ps[1], ps[2])
	want := []Rect{{X: 0, Y: 0, W: 2, H: 1}, {X: 0, Y: 1, W: 6, H: 1}, {X: 0, Y: 2, W: 2, H: 1}}
	for i := range want {
		if rects[i] != want[i] {
			t.Errorf("child %d at %+v, want %+v", i, rects[i], want[i])
		}
	}
	FailOnViolations(t, h.tb)
}

// hideableProbe is a probe that can be hidden.
type hideableProbe struct {
	*probe
	Visibility
}

// A hidden child takes no room: neither its width nor a gap.
func TestAHiddenFlowChildTakesNoRoom(t *testing.T) {
	t.Parallel()
	f := NewFlow()
	f.SetSpacing(1)
	a := &probe{name: "a", pref: Size{W: 2, H: 1}, fill: "a"}
	b := &hideableProbe{probe: &probe{name: "b", pref: Size{W: 2, H: 1}, fill: "b"}}
	b.Show(false)
	c := &probe{name: "c", pref: Size{W: 2, H: 1}, fill: "c"}
	f.Add(a, b, c)
	h := startApp(t, f, 8, 1)
	if got, want := h.tb.String(), "aa cc   "; got != want {
		t.Fatalf("frame %q, want %q", got, want)
	}
}

// In a column the Flow is laid out at the column's width and is as tall as its lines: the next
// child of the column (stretched across it, as a column does) starts under its last line.
func TestAFlowInAColumnTakesTheRowsItsLinesNeed(t *testing.T) {
	t.Parallel()
	f, _ := flowOf(1, 0, Size{W: 4, H: 1}, Size{W: 4, H: 1}, Size{W: 4, H: 1})
	after := &probe{name: "after", pref: Size{W: 3, H: 1}, fill: "z"}
	col := NewFlex(Vertical)
	col.Add(f, after)
	h := startApp(t, col, 9, 4)
	if got, want := h.tb.String(), frame("aaaa bbbb", "cccc     ", "zzzzzzzzz", "         "); got != want {
		t.Fatalf("frame:\n%q\nwant\n%q", got, want)
	}
	FailOnViolations(t, h.tb)
}

// SetSpacing and SetRowSpacing on a mounted Flow lay it out again.
func TestAFlowsSpacingChangesLive(t *testing.T) {
	t.Parallel()
	f, _ := flowOf(0, 0, Size{W: 2, H: 1}, Size{W: 2, H: 1})
	h := startApp(t, f, 6, 5)
	blank := "      "
	if got, want := h.tb.String(), frame("aabb  ", blank, blank, blank, blank); got != want {
		t.Fatalf("before: %q, want %q", got, want)
	}
	h.onLoop(func() { f.SetSpacing(3) })
	want := frame("aa    ", "bb    ", blank, blank, blank)
	waitFor(t, "the wider spacing wraps", func() bool { return h.tb.String() == want })
	h.onLoop(func() { f.SetRowSpacing(-1) }) // follows spacing again: three rows between the lines
	want = frame("aa    ", blank, blank, blank, "bb    ")
	waitFor(t, "three rows between the lines", func() bool { return h.tb.String() == want })
	var spacing, rows int
	h.onLoop(func() { spacing, rows = f.Spacing(), f.RowSpacing() })
	if spacing != 3 || rows != 3 {
		t.Errorf("spacing %d, row spacing %d; want 3 and 3", spacing, rows)
	}
	FailOnViolations(t, h.tb)
}

// A negative spacing is none: a Flow's gap is never less than touching.
func TestANegativeFlowSpacingIsNone(t *testing.T) {
	t.Parallel()
	f := NewFlow()
	f.SetSpacing(2)
	f.SetSpacing(-3)
	if f.Spacing() != 0 || f.RowSpacing() != 0 {
		t.Errorf("spacing %d, row spacing %d after SetSpacing(-3); want 0 and 0", f.Spacing(), f.RowSpacing())
	}
}

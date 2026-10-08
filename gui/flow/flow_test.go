package flow_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
)

func shaper() *gui.TextShaper {
	return gui.NewRecordingCanvas(gui.Size{W: 400, H: 400}, gui.Size{W: 8, H: 16}).Text()
}

func plain(s string) flow.Span { return flow.Span{Text: s, Font: gui.Font{Size: 16}, Line: -1} }

func TestMixedFontsShareABaseline(t *testing.T) {
	big := plain("Big ")
	big.Font.Size = 32
	p := flow.Lay([]flow.Span{plain("small "), big, plain("tail")}, flow.Options{}, shaper())
	if len(p.Lines) != 1 {
		t.Fatalf("lines: %d", len(p.Lines))
	}
	l := p.Lines[0]
	if l.H < 32 {
		t.Errorf("line height %v is less than the big font's size", l.H)
	}
	if len(l.Frags) != 3 || l.Frags[1].X <= l.Frags[0].X || l.Frags[2].X <= l.Frags[1].X {
		t.Fatalf("frags out of order: %+v", l.Frags)
	}
}

func TestWrapAtSpacesAndHangingSpace(t *testing.T) {
	sh := shaper()
	one := flow.Lay([]flow.Span{plain("alpha")}, flow.Options{}, sh).Width
	p := flow.Lay([]flow.Span{plain("alpha beta gamma")}, flow.Options{Width: one * 1.5}, sh)
	if len(p.Lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(p.Lines))
	}
	if p.Width > one*1.5 {
		t.Errorf("content wider than the line: %v > %v", p.Width, one*1.5)
	}
}

func TestLongWordBreaksBetweenClusters(t *testing.T) {
	sh := shaper()
	p := flow.Lay([]flow.Span{plain("supercalifragilistic")}, flow.Options{Width: 40}, sh)
	if len(p.Lines) < 3 {
		t.Fatalf("a long word in 40px: %d lines", len(p.Lines))
	}
	for _, l := range p.Lines {
		var w float32
		for _, f := range l.Frags {
			w = max(w, f.X+f.W)
		}
		if w > 40+0.5 && len(l.Frags) > 0 && l.Frags[0].To-l.Frags[0].From > 1 {
			t.Errorf("line %v wider than 40 with more than one cluster", w)
		}
	}
	if got := p.Text(flow.Pos{0, 0}, flow.Pos{0, len("supercalifragilistic")}); got != "supercalifragilistic" {
		t.Errorf("text across the breaks: %q", got)
	}
}

func TestCJKBreaksBetweenClusters(t *testing.T) {
	sh := shaper()
	s := "한국어문장입니다"
	w := flow.Lay([]flow.Span{plain("한국")}, flow.Options{}, sh).Width
	p := flow.Lay([]flow.Span{plain(s)}, flow.Options{Width: w * 1.6}, sh)
	if len(p.Lines) < 3 {
		t.Fatalf("CJK in a narrow line: %d lines", len(p.Lines))
	}
}

func TestCaretAndAtRoundTrip(t *testing.T) {
	sh := shaper()
	head := flow.Span{Text: "## ", Font: gui.Font{Size: 16}, Hidden: true}
	title := flow.Span{Text: "Heading words here", Font: gui.Font{Size: 24, Bold: true}, Col: 3}
	spans := []flow.Span{head, title}
	w := flow.Lay([]flow.Span{title}, flow.Options{}, sh).Width
	p := flow.Lay(spans, flow.Options{Width: w * 0.6, WhiteSpace: flow.PreWrap}, sh)
	if len(p.Lines) < 2 {
		t.Fatalf("expected a wrap, got %d lines", len(p.Lines))
	}
	// hidden marks take no width: the title starts at x 0
	if c := p.Caret(flow.Pos{1, 0}); c.X != 0 {
		t.Errorf("title's first caret at %v, want 0 past the hidden marks", c.X)
	}
	// a click at x 0 on the first line lands past the hidden marks
	if got := p.At(gui.Pt(0, 1)); got != (flow.Pos{1, 0}) {
		t.Errorf("At(0) = %+v, want the title's start", got)
	}
	// every boundary of the title round-trips through Caret and At
	text := title.Text
	for off := 0; off <= len(text); off++ {
		if off < len(text) && (text[off]&0xC0) == 0x80 {
			continue
		}
		c := p.Caret(flow.Pos{1, off})
		got := p.At(gui.Pt(c.X, c.Y+c.H/2))
		if got != (flow.Pos{1, off}) {
			// at a soft wrap the boundary is downstream: the hanging space's end and the next
			// line's start are one position
			if !(got.Span == 1 && text[min(off, len(text)-1)] == ' ') {
				t.Errorf("offset %d: caret %+v, At gives %+v", off, c, got)
			}
		}
	}
}

func TestWhiteSpaceModes(t *testing.T) {
	sh := shaper()
	n := flow.Lay([]flow.Span{plain("  a   b\n c ")}, flow.Options{}, sh)
	if got := n.Text(flow.Pos{0, 0}, flow.Pos{0, 11}); got != "a b c " {
		t.Errorf("Normal collapses to %q", got)
	}
	pre := flow.Lay([]flow.Span{plain("a  b\nc")}, flow.Options{Width: 10, WhiteSpace: flow.Pre}, sh)
	if len(pre.Lines) != 2 {
		t.Errorf("Pre: %d lines, want 2 (no wrapping at 10px)", len(pre.Lines))
	}
	if got := pre.Text(flow.Pos{0, 0}, flow.Pos{0, 6}); got != "a  b\nc" {
		t.Errorf("Pre keeps %q", got)
	}
	pw := flow.Lay([]flow.Span{plain("aaa bbb")}, flow.Options{Width: 30, WhiteSpace: flow.PreWrap}, sh)
	if len(pw.Lines) != 2 {
		t.Errorf("PreWrap: %d lines, want 2", len(pw.Lines))
	}
}

func TestAlignAndRects(t *testing.T) {
	sh := shaper()
	p := flow.Lay([]flow.Span{plain("mid")}, flow.Options{Width: 200, Align: flow.Center}, sh)
	f := p.Lines[0].Frags[0]
	if d := (f.X + f.W/2) - 100; d > 0.5 || d < -0.5 {
		t.Errorf("centred frag's middle at %v", f.X+f.W/2)
	}
	r := p.Rects(flow.Pos{0, 0}, flow.Pos{0, 3})
	if len(r) != 1 || r[0].W < f.W-0.5 {
		t.Errorf("selection rects %+v for a frag %v wide", r, f.W)
	}
}

func TestPaintDrawsVisibleFragsOnly(t *testing.T) {
	rc := gui.NewRecordingCanvas(gui.Size{W: 300, H: 40}, gui.Size{W: 8, H: 16})
	spans := []flow.Span{{Text: "**", Font: gui.Font{Size: 16}, Hidden: true}, {Text: "bold", Font: gui.Font{Size: 16, Bold: true}, Underline: true}}
	p := flow.Lay(spans, flow.Options{}, rc.Text())
	p.Paint(rc, gui.Pt(0, 0), spans)
	texts, rects := 0, 0
	for _, c := range rc.Calls {
		switch c.Op {
		case "DrawText":
			texts++
		case "FillRect":
			rects++
		}
	}
	if texts != 1 || rects != 1 {
		t.Errorf("drew %d texts and %d rects; want the bold text and its underline", texts, rects)
	}
}

func TestBlankLinesHoldABoundary(t *testing.T) {
	sh := shaper()
	for _, ws := range []flow.WhiteSpace{flow.Pre, flow.PreWrap} {
		p := flow.Lay([]flow.Span{plain("a\n\nb\n")}, flow.Options{Width: 200, WhiteSpace: ws}, sh)
		if len(p.Lines) != 4 {
			t.Fatalf("ws %d: %d lines, want 4 (a, blank, b, the empty last)", ws, len(p.Lines))
		}
		// the blank line between holds offset 2 (before its '\n'); the last holds 5 (after the final '\n')
		for _, c := range []struct {
			line int
			want flow.Pos
		}{{1, flow.Pos{0, 2}}, {3, flow.Pos{0, 5}}} {
			l := p.Lines[c.line]
			if got := p.At(gui.Pt(50, l.Y+l.H/2)); got != c.want {
				t.Errorf("ws %d: At on line %d = %+v, want %+v", ws, c.line, got, c.want)
			}
			if r := p.Caret(c.want); r.Y != l.Y {
				t.Errorf("ws %d: Caret(%+v) on y %v, want line %d's %v", ws, c.want, r.Y, c.line, l.Y)
			}
		}
	}
}

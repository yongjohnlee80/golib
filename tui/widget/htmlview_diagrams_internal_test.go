package widget

import (
	"context"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
	"github.com/yongjohnlee80/golib/tui/style"
)

// gridSurface is a tui.Surface over a plain grid, for reading what PaintCells drew.
type gridSurface struct {
	w, h int
	at   [][]rune
}

func newGridSurface(w, h int) *gridSurface {
	g := &gridSurface{w: w, h: h, at: make([][]rune, h)}
	for y := range g.at {
		g.at[y] = []rune(strings.Repeat(" ", w))
	}
	return g
}
func (g *gridSurface) SetCell(x, y int, content string, _ style.Style) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h || content == "" {
		return
	}
	g.at[y][x] = []rune(content)[0]
}
func (g *gridSurface) Fill(r tui.Rect, content string, st style.Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			g.SetCell(x, y, content, st)
		}
	}
}
func (g *gridSurface) Sub(tui.Rect) tui.Surface { return g }
func (g *gridSurface) Size() tui.Size           { return tui.Size{W: g.w, H: g.h} }
func (g *gridSurface) StringWidth(s string) int { return len([]rune(s)) }
func (g *gridSurface) Theme() *style.Theme      { return nil }
func (g *gridSurface) Caps() tui.Capabilities   { return tui.Capabilities{} }
func (g *gridSurface) text() string {
	var b strings.Builder
	for _, row := range g.at {
		b.WriteString(strings.TrimRight(string(row), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// The Rendered view draws a mind map fence as its diagram, in cells, between the text around it;
// one the terminal doesn't draw (one that doesn't parse: every kind the parser reads is drawn) is one
// muted line saying so, never its source; the host's own words replace that line's English.
func TestTheCellLayoutDrawsAMindmapAndSaysWhatItDoesnt(t *testing.T) {
	html := `<p>before</p><pre class="mermaid">mindmap
  root[Hub]
    a(One)
</pre><pre class="mermaid">notADiagram
  requirement Shelf
</pre><p>after</p>`
	c := layCells(t, html, 60)
	var gridRows, noticeRows int
	for _, r := range c.rows {
		if r.grid != nil {
			gridRows++
		}
	}
	rows := c.rowTexts()
	joined := strings.Join(rows, "\n")
	flat := strings.Join(strings.Fields(joined), " ") // the notice wraps to the room: read it as one line
	if !strings.Contains(flat, "▸ mermaid diagram: not drawn in the terminal (Raw shows its source)") {
		t.Fatalf("no notice for the diagram, whole:\n%s", joined)
	}
	for _, r := range rows {
		if strings.Contains(r, "▸ mermaid") {
			noticeRows++
		}
	}
	for i, r := range rows {
		if n := len([]rune(r)); n > 60 {
			t.Fatalf("row %d is %d cells wide, past the 60 the layout has: %q", i, n, r)
		}
	}
	if gridRows != 3 || noticeRows != 1 || strings.Contains(joined, "requirement Shelf") || strings.Contains(joined, "root[Hub]") {
		t.Fatalf("grid rows %d (want 3), notice rows %d (want 1), or a diagram's source shown:\n%s", gridRows, noticeRows, joined)
	}

	s := newGridSurface(60, len(c.rows))
	c.PaintCells(s, 0, 60, DocPos{}, DocPos{})
	got := s.text()
	for _, want := range []string{"before", "┌─────┐", "│ Hub ├──────┤ One │", "after"} {
		if !strings.Contains(got, want) {
			t.Errorf("the painted page has no %q:\n%s", want, got)
		}
	}

	c = layCells(t, `<pre class="mermaid">notADiagram
  requirement Shelf
</pre>`, 60)
	c.notice = func(kind string) string { return "图表未绘制: " + kind }
	c.layOut()
	if got := strings.Join(c.rowTexts(), "\n"); !strings.Contains(got, "▸ 图表未绘制: mermaid") {
		t.Fatalf("the host's notice wasn't used:\n%s", got)
	}

	// a view built first, then given the host's words (as AutoDoc does to a view its layout built):
	// the page is laid out again in them
	v := NewHTMLView()
	v.SetHTML([]byte(`<pre class="mermaid">notADiagram
  requirement Shelf
</pre>`))
	cl := v.layout.(*cellHTML)
	cl.SetWidth(60)
	if got := strings.Join(strings.Fields(strings.Join(cl.rowTexts(), " ")), " "); !strings.Contains(got, "▸ mermaid diagram: not drawn in the terminal") {
		t.Fatalf("before the host's words, the English isn't shown:\n%s", got)
	}
	WithDiagramNotice(func(kind string) string { return "图表未绘制: " + kind })(v)
	// a real view measures a CJK cluster two cells wide, so the row text has a gap after each:
	// read it without spaces
	if got := strings.Join(cl.rowTexts(), "\n"); !strings.Contains(strings.ReplaceAll(got, " ", ""), "▸图表未绘制:mermaid") {
		t.Fatalf("words set after the view was built weren't used:\n%s", got)
	}
}

// The Rendered view draws a flowchart that fits as its diagram, and fits one that doesn't where it
// can: one wider than the room that runs across (LR, RL) is laid running down (TB). A flowchart
// still wider is drawn all the same, the narrower of its two layouts, at its own width (the page
// pans it): never the notice. One with subgraphs is drawn too, each subgraph framed. Each case's
// precondition (how wide tui/mermaid lays it) is checked first, so the cell
// can't pass by its fixtures alone.
func TestTheCellLayoutDrawsAFlowchartThatFitsItsRoom(t *testing.T) {
	const room = 30
	runes := measureFunc(func(s string) int { return len([]rune(s)) })
	size := func(src string) tui.Size {
		t.Helper()
		d, err := pm.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		g, err := tuimermaid.Lay(context.Background(), d, runes)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		return g.Size
	}
	laid := func(src string) (*tuimermaid.Grid, string) {
		t.Helper()
		c := layCells(t, `<pre class="mermaid">`+src+`</pre>`, room)
		var g *tuimermaid.Grid
		for _, r := range c.rows {
			if r.grid != nil {
				g = r.grid
			}
		}
		return g, strings.Join(strings.Fields(strings.Join(c.rowTexts(), " ")), " ")
	}
	notice := "▸ flowchart diagram: not drawn in the terminal (Raw shows its source)"

	small := "flowchart TB\n  A[Start] --> B[End]\n"
	if g, text := laid(small); g == nil || strings.Contains(text, notice) {
		t.Fatalf("a flowchart that fits isn't drawn: %s", text)
	} else {
		s := newGridSurface(room, g.Size.H)
		c := layCells(t, `<pre class="mermaid">`+small+`</pre>`, room)
		c.PaintCells(s, 0, room, DocPos{}, DocPos{})
		if got := s.text(); !strings.Contains(got, "│ Start │") || !strings.Contains(got, "▼") {
			t.Fatalf("the flowchart's painted page:\n%s", got)
		}
	}

	across := "flowchart LR\n  A[first step] --> B[second step] --> C[third step]\n"
	if w := size(across).W; w <= room {
		t.Fatalf("precondition: the LR chain is %d cells wide, not past the room's %d", w, room)
	}
	down := size(strings.Replace(across, "LR", "TB", 1))
	if down.W > room {
		t.Fatalf("precondition: the chain laid TB is %d cells wide, past the room's %d", down.W, room)
	}
	if g, text := laid(across); g == nil || g.Size != down {
		t.Fatalf("the LR chain too wide for the room isn't drawn as it is laid TB (%+v): %+v, %s", down, g, text)
	}

	for name, c := range map[string]struct{ src, narrower string }{
		"TB":                 {"flowchart TB\n  A --> B[a long label one]\n  A --> C[a long label two]\n", ""},
		"LR, and TB as well": {"flowchart LR\n  A[a label as long as the room] --> B[and another one as long]\n  A --> C[and a third one as long]\n", "TB"},
	} {
		want := size(c.src)
		if c.narrower != "" {
			if alt := size(strings.Replace(c.src, "LR", c.narrower, 1)); alt.W < want.W {
				want = alt
			}
		}
		if want.W <= room {
			t.Fatalf("precondition: %s is laid %d cells wide at its narrowest, not past the room's %d", name, want.W, room)
		}
		if g, text := laid(c.src); g == nil || strings.Contains(text, notice) || g.Size != want {
			t.Errorf("%s, too wide for the room: drawn %+v; want it drawn at its narrowest, %+v, to pan, no notice: %s", name, g, want, text)
		}
	}

	if g, text := laid("flowchart TB\n  subgraph S [Group]\n    A --> B\n  end\n"); g == nil || strings.Contains(text, notice) {
		t.Errorf("a flowchart with a subgraph: drawn %v; want it drawn, framed: %s", g != nil, text)
	}
}

package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
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
// a kind the terminal doesn't draw is one muted line saying so, never its source; the host's own
// words replace that line's English.
func TestTheCellLayoutDrawsAMindmapAndSaysWhatItDoesnt(t *testing.T) {
	html := `<p>before</p><pre class="mermaid">mindmap
  root[Hub]
    a(One)
</pre><pre class="mermaid">flowchart LR
  a --> b
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
	if !strings.Contains(flat, "▸ flowchart diagram: not drawn in the terminal (Raw shows its source)") {
		t.Fatalf("no notice for the flowchart, whole:\n%s", joined)
	}
	for _, r := range rows {
		if strings.Contains(r, "▸ flowchart") {
			noticeRows++
		}
	}
	for i, r := range rows {
		if n := len([]rune(r)); n > 60 {
			t.Fatalf("row %d is %d cells wide, past the 60 the layout has: %q", i, n, r)
		}
	}
	if gridRows != 3 || noticeRows != 1 || strings.Contains(joined, "a --> b") || strings.Contains(joined, "root[Hub]") {
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

	c = layCells(t, `<pre class="mermaid">flowchart LR
  a --> b
</pre>`, 60)
	c.notice = func(kind string) string { return "图表未绘制: " + kind }
	c.layOut()
	if got := strings.Join(c.rowTexts(), "\n"); !strings.Contains(got, "▸ 图表未绘制: flowchart") {
		t.Fatalf("the host's notice wasn't used:\n%s", got)
	}

	// a view built first, then given the host's words (as AutoDoc does to a view its layout built):
	// the page is laid out again in them
	v := NewHTMLView()
	v.SetHTML([]byte(`<pre class="mermaid">flowchart LR
  a --> b
</pre>`))
	cl := v.layout.(*cellHTML)
	cl.SetWidth(60)
	if got := strings.Join(strings.Fields(strings.Join(cl.rowTexts(), " ")), " "); !strings.Contains(got, "▸ flowchart diagram: not drawn in the terminal") {
		t.Fatalf("before the host's words, the English isn't shown:\n%s", got)
	}
	WithDiagramNotice(func(kind string) string { return "图表未绘制: " + kind })(v)
	// a real view measures a CJK cluster two cells wide, so the row text has a gap after each:
	// read it without spaces
	if got := strings.Join(cl.rowTexts(), "\n"); !strings.Contains(strings.ReplaceAll(got, " ", ""), "▸图表未绘制:flowchart") {
		t.Fatalf("words set after the view was built weren't used:\n%s", got)
	}
}

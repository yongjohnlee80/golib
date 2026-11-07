package widget

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	mdhtml "github.com/yongjohnlee80/golib/parse/markdown/html"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
)

// wideSequence is a sequence diagram wider than navRoom, drawn in cells.
const wideSequence = "sequenceDiagram\n  Alice->>Bob: hello\n  Bob->>Carol: and on\n  Carol->>Dave: and on\n  Dave->>Erin: the end\n"

// navRoom is the page width the pan cells lay out at.
const navRoom = 30

// fence is src as the Rendered view's diagram block.
func navFence(src string) string { return `<pre class="mermaid">` + src + `</pre>` }

// gridWidth is how wide tui/mermaid lays src out, measured by runes as layCells is.
func gridWidth(t *testing.T, src string) int {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	g, err := tuimermaid.Lay(context.Background(), d, measureFunc(func(s string) int { return len([]rune(s)) }))
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return g.Size.W
}

// navRows is c painted from row top, h rows of the page's width, as text, one string a row.
func navRows(c *cellHTML, top, h int) []string {
	s := newRecordSurface(c.width, h)
	c.PaintCells(s, top, c.width, DocPos{}, DocPos{})
	out := make([]string, h)
	for y := range h {
		out[y] = s.row(y)
	}
	return out
}

// diagramBlock is the index of c's first block drawn as a diagram, and its first row.
func diagramBlock(t *testing.T, c *cellHTML) (bi, row int) {
	t.Helper()
	for ri, r := range c.rows {
		if r.grid != nil {
			return r.block, ri
		}
	}
	t.Fatal("no diagram drawn")
	return 0, 0
}

// A diagram wider than its room is drawn at its own width and pans sideways within the room, the
// room's two edge columns kept for the marks so a mark never covers the diagram: at 0 it shows its
// left and a » on its middle row; panned, its columns from the offset on, a « too; at its end (as
// far as it is wider than the room less those columns, and no further) a « only, its last column
// beside the right edge. The text around it never moves, and a pan past an end moves nothing.
func TestAWideDiagramPansWithinItsRoom(t *testing.T) {
	if w := gridWidth(t, wideSequence); w <= navRoom {
		t.Fatalf("precondition: the sequence is %d cells wide, not past the room's %d", w, navRoom)
	}
	c := layCells(t, "<p>before</p>"+navFence(wideSequence)+"<p>after</p>", navRoom)
	bi, first := diagramBlock(t, c)
	g := c.rows[first].grid.(mermaidPicture).g
	inner := navRoom - 2 // the room less its edge columns, the marks' while a block is wide
	over := g.Size.W - inner
	if got := c.over(bi); got != over {
		t.Fatalf("over = %d, want %d (the grid %d wide, the room %d less its 2 mark columns)", got, over, g.Size.W, navRoom)
	}
	// the grid as it lays out, whole: what each pan must show a window of
	whole := newRecordSurface(g.Size.W, g.Size.H)
	g.Paint(whole, 0, 0, -1, diagramTheme)
	mid := first + (g.Size.H-1)/2
	h := len(c.rows)
	text := func(rows []string) (out []string) { // the rows outside the diagram
		for ri, r := range rows {
			if c.rows[ri].grid == nil {
				out = append(out, r)
			}
		}
		return out
	}
	at0 := navRows(c, 0, h)
	check := func(name string, off int) {
		t.Helper()
		rows := navRows(c, 0, h)
		for y := range g.Size.H {
			row := []rune(rows[first+y] + strings.Repeat(" ", navRoom))[:navRoom]
			want := []rune(strings.Join(whole.text[y], ""))[off : off+inner]
			left, right := ' ', ' ' // the edge columns: blank, but on the middle row a mark where more lies
			if first+y == mid {
				if off > 0 {
					left = '«'
				}
				if off < over {
					right = '»'
				}
			}
			if row[0] != left || row[navRoom-1] != right {
				t.Errorf("%s: diagram row %d's edge columns are %q and %q, want %q and %q: %q", name, y, row[0], row[navRoom-1], left, right, string(row))
			}
			// between them the grid's columns from the offset on, whole: the cell beside a mark is
			// the diagram's own, never covered by it
			if got := row[1 : navRoom-1]; string(got) != string(want) {
				t.Errorf("%s: diagram row %d between its edges = %q, want the grid's columns %d to %d: %q", name, y, string(got), off, off+inner-1, string(want))
			}
		}
		if got, want := text(rows), text(at0); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: the text around the diagram moved:\n%s\nwant\n%s", name, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	check("at 0", 0)
	if !c.Pan(0, h, htmlPanStep) {
		t.Fatal("a pan right of a wide diagram moved nothing")
	}
	check("a step right", htmlPanStep)
	if c.Pan(0, h, 1000); c.offset(bi) != over {
		t.Fatalf("a long pan right went to %d, want the end, %d", c.offset(bi), over)
	}
	check("at the end", over)
	if c.Pan(0, h, 1) {
		t.Error("a pan right past the end moved")
	}
	if c.Pan(0, h, -1000); c.offset(bi) != 0 {
		t.Fatalf("a long pan left went to %d, want 0", c.offset(bi))
	}
	if c.Pan(0, h, -1) {
		t.Error("a pan left past 0 moved")
	}

	// one that fits doesn't pan, and shows no mark
	c = layCells(t, navFence("sequenceDiagram\n  A->>B: hi\n"), navRoom)
	bi, _ = diagramBlock(t, c)
	if c.over(bi) != 0 || c.Pan(0, len(c.rows), htmlPanStep) {
		t.Errorf("a diagram that fits: over %d, or it panned", c.over(bi))
	}
	if joined := strings.Join(navRows(c, 0, len(c.rows)), "\n"); strings.ContainsAny(joined, "«»") {
		t.Errorf("a diagram that fits shows a pan mark:\n%s", joined)
	}
}

// A panned diagram stays in its room: inside a quote it never paints over the quote's bar, and
// at the page's right it never paints past the room (the scroll indicator's column).
func TestAPannedDiagramStaysInItsRoom(t *testing.T) {
	c := layCells(t, "<blockquote>"+navFence(wideSequence)+"</blockquote>", navRoom)
	bi, first := diagramBlock(t, c)
	b := &c.doc[bi]
	if b.quote == 0 || c.over(bi) == 0 {
		t.Fatalf("precondition: the diagram is in a quote (%d) and wide (%d)", b.quote, c.over(bi))
	}
	c.Pan(0, len(c.rows), htmlPanStep)
	g := c.rows[first].grid.(mermaidPicture).g
	s := newRecordSurface(navRoom+5, len(c.rows))
	for y := range s.h {
		for x := range s.w {
			s.text[y][x] = "#" // what was there before the paint: kept outside the room
		}
	}
	c.PaintCells(s, 0, navRoom, DocPos{}, DocPos{})
	for y := first; y < first+g.Size.H; y++ {
		for x := range 2 * b.quote {
			if s.text[y][x] != "#" {
				t.Errorf("row %d: the diagram painted %q at column %d, left of its room (the quote's)", y, s.text[y][x], x)
			}
		}
		for x := navRoom; x < s.w; x++ {
			if s.text[y][x] != "#" {
				t.Errorf("row %d: the diagram painted %q at column %d, past the page's %d", y, s.text[y][x], x, navRoom)
			}
		}
	}
}

// navView is html in an HTMLView w by h cells, laid out and painted once.
func navView(t *testing.T, html string, w, h int) (*HTMLView, *cellHTML) {
	t.Helper()
	v := NewHTMLView()
	v.SetHTML([]byte(html))
	v.Layout(tui.Tight(tui.Size{W: w, H: h}))
	v.Render(newRecordSurface(w, h))
	c, ok := v.layout.(*cellHTML)
	if !ok {
		t.Fatalf("the view's layout is %T", v.layout)
	}
	return v, c
}

func navKey(code rune, text string) tui.KeyEvent {
	return tui.KeyEvent{Kind: tui.KeyPress, Code: code, Text: text}
}

// The view's Left/Right and h/l pan the wide block nearest the page's top: of two on screen, the
// first in the page's order (one that starts above the top and still shows is the nearest); with
// that one scrolled off, the next. Shift+wheel pans the block under the pointer, as the window's
// does. Each block keeps its own offset. A pan key with nothing wide on screen is still the page's
// (read, not passed on), and moves nothing.
func TestThePanKeysPanTheWideBlockNearestTheTop(t *testing.T) {
	page := "<p>top</p>" + navFence(wideSequence) + "<p>between</p>" + navFence(wideSequence) + "<p>bottom</p>"
	const viewH = 8
	v, c := navView(t, page, navRoom+1, viewH)
	var blocks, starts, ends []int
	for ri, r := range c.rows {
		if r.grid != nil && (ri == 0 || c.rows[ri-1].grid != r.grid) {
			blocks, starts, ends = append(blocks, r.block), append(starts, ri), append(ends, ri+r.grid.Size().H-1)
		}
	}
	if len(blocks) != 2 || c.over(blocks[0]) == 0 || c.over(blocks[1]) == 0 {
		t.Fatalf("precondition: two wide diagrams, got blocks %v (over %d, %d)", blocks, c.over(blocks[0]), c.over(blocks[1]))
	}
	offsets := func() [2]int { return [2]int{c.offset(blocks[0]), c.offset(blocks[1])} }
	for _, k := range []tui.KeyEvent{navKey(tui.KeyRight, ""), navKey('l', "l")} {
		before := offsets()
		if !v.HandleEvent(k) {
			t.Fatalf("%+v wasn't read", k)
		}
		if got := offsets(); got != [2]int{before[0] + htmlPanStep, 0} {
			t.Fatalf("%+v: offsets %v, want the first block a step on (%d) and the second at 0", k, got, before[0]+htmlPanStep)
		}
	}
	for _, k := range []tui.KeyEvent{navKey(tui.KeyLeft, ""), navKey('h', "h")} {
		before := offsets()
		v.HandleEvent(k)
		if got := offsets(); got != [2]int{before[0] - htmlPanStep, 0} {
			t.Fatalf("%+v: offsets %v, want the first block a step back", k, got)
		}
	}
	// both on screen, the first starting above the top: it is the nearest
	top := ends[0] - 1
	if top <= starts[0] || top+viewH-1 < starts[1] {
		t.Fatalf("precondition: rows %d to %d show the end of the first (%d-%d) and the start of the second (%d)", top, top+viewH-1, starts[0], ends[0], starts[1])
	}
	v.scrollTo(float32(top))
	if v.ScrollY() != float32(top) {
		t.Fatalf("precondition: the view scrolls to %d (it went to %v)", top, v.ScrollY())
	}
	v.HandleEvent(navKey(tui.KeyRight, ""))
	if got := offsets(); got != [2]int{htmlPanStep, 0} {
		t.Fatalf("both on screen, the first partly above the top: offsets %v, want the first panned", got)
	}
	// the first scrolled off: the second is the nearest
	v.scrollTo(float32(starts[1]))
	v.HandleEvent(navKey(tui.KeyRight, ""))
	if got := offsets(); got != [2]int{htmlPanStep, htmlPanStep} {
		t.Fatalf("with the first block off the top: offsets %v, want the second panned", got)
	}
	// Shift+wheel over the second block (its second row on screen) pans it, the first kept
	at := tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, Mods: tui.ModShift, X: 5, Y: 1}
	if !v.HandleEvent(at) {
		t.Fatal("Shift+wheel over a wide block wasn't read")
	}
	if got := offsets(); got != [2]int{htmlPanStep, htmlPanStep + 3} {
		t.Fatalf("Shift+wheel over the second block: offsets %v, want it 3 on", got)
	}

	// nothing wide: the keys are still read, and move nothing
	v, c = navView(t, "<p>just text</p>", navRoom, 10)
	for _, k := range []tui.KeyEvent{navKey(tui.KeyRight, ""), navKey('l', "l"), navKey(tui.KeyLeft, ""), navKey('h', "h")} {
		if !v.HandleEvent(k) {
			t.Errorf("%+v with nothing wide was passed on, not read", k)
		}
	}
	if c.pan != nil {
		t.Errorf("a pan with nothing wide recorded %v", c.pan)
	}
}

// navPage is a page with every kind of block a reader travels by, as Markdown.
const navPage = "# One\n\npara a\n\n- item 1\n- item 2\n\n| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n\n" +
	"## Two\n\npara b\n\n> quote 1\n>\n> quote 2\n\n---\n\npara c\n\npara d\n\npara e\n"

// markdownHTML is md rendered as the Rendered view gets it.
func markdownHTML(t *testing.T, md string) string {
	t.Helper()
	var b bytes.Buffer
	if err := mdhtml.Render(&b, markdown.Parse([]byte(md), markdown.WithGFM())); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// ] and [ travel by block (a table, a list and a quote each one block, whatever rows or
// paragraphs they hold; a heading its own), } and { by heading, each bringing its block's first
// row to the page's top; at the last (or first), nothing moves, and the key is still read.
func TestTheTravelKeysGoBlockByBlockAndHeadingByHeading(t *testing.T) {
	v, c := navView(t, markdownHTML(t, navPage), 40, 4)
	blocks, headings := c.Starts(false), c.Starts(true)
	firstRowWhere := func(what string, from float32, is func(row string) bool) float32 {
		t.Helper()
		for ri := int(from); ri < len(c.rows); ri++ {
			if is(navRows(c, ri, 1)[0]) {
				return float32(ri)
			}
		}
		t.Fatalf("no row shows %s", what)
		return 0
	}
	firstRow := func(want string) float32 {
		t.Helper()
		return firstRowWhere(want, 0, func(row string) bool { return strings.Contains(row, want) })
	}
	rule := func(row string) bool { // a row of ─ alone: the h2's rule, or the thematic break after the quote
		row = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(row, " "), "│"), " ")
		return row != "" && strings.Trim(row, "─") == ""
	}
	// one block each: the h1, a paragraph, the list, the table (its top rule), the h2, a paragraph,
	// the quote, the rule, three paragraphs
	tableTop := firstRow("┌")
	want := []float32{firstRow("One"), firstRow("para a"), firstRow("item 1"), tableTop, firstRow("Two"),
		firstRow("para b"), firstRow("quote 1"), firstRowWhere("the rule after the quote", firstRow("quote 1"), rule), firstRow("para c"), firstRow("para d"), firstRow("para e")}
	if len(blocks) != len(want) {
		t.Fatalf("blocks start at rows %v, want %v", blocks, want)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Fatalf("blocks start at rows %v, want %v", blocks, want)
		}
	}
	if len(headings) != 2 || headings[0] != want[0] || headings[1] != want[4] {
		t.Fatalf("headings start at rows %v, want %v and %v", headings, want[0], want[4])
	}
	maxTop := v.maxScroll()
	press := func(k rune) float32 {
		t.Helper()
		if !v.HandleEvent(navKey(k, string(k))) {
			t.Fatalf("%q wasn't read", k)
		}
		return v.ScrollY()
	}
	for i := 1; i < len(want); i++ {
		if got := press(']'); got != min(want[i], maxTop) {
			t.Fatalf("] number %d: top at %v, want %v", i, got, min(want[i], maxTop))
		}
		if want[i] >= maxTop {
			break
		}
	}
	if top := v.ScrollY(); press(']') != top {
		t.Fatal("] at the last block moved")
	}
	v.scrollTo(want[5]) // para b
	if got := press('['); got != want[4] {
		t.Fatalf("[ from para b: top at %v, want the h2's %v", got, want[4])
	}
	if got := press('['); got != want[3] {
		t.Fatalf("[ from the h2: top at %v, want the table's %v", got, want[3])
	}
	v.scrollTo(0)
	if got := press('}'); got != want[4] {
		t.Fatalf("} from the top: top at %v, want the h2's %v", got, want[4])
	}
	if got := press('}'); got != want[4] {
		t.Fatalf("} at the last heading moved to %v", got)
	}
	if got := press('{'); got != want[0] {
		t.Fatalf("{ from the h2: top at %v, want the h1's %v", got, want[0])
	}
	if got := press('{'); got != want[0] {
		t.Fatalf("{ at the first heading moved to %v", got)
	}
}

// A layout without a pan or travel (the window's, which scrolls its own wide boxes by the wheel)
// leaves those keys to the parent: the view doesn't read them.
func TestTheNavigationKeysAreTheLayoutsOnly(t *testing.T) {
	v := NewHTMLView()
	v.BindLayout(plainLayout{})
	for _, k := range []tui.KeyEvent{navKey(tui.KeyLeft, ""), navKey(tui.KeyRight, ""), navKey('h', "h"), navKey('l', "l"),
		navKey(']', "]"), navKey('[', "["), navKey('}', "}"), navKey('{', "{")} {
		if v.HandleEvent(k) {
			t.Errorf("%+v was read by a view whose layout has no pan or travel", k)
		}
	}
}

// plainLayout is an HTMLLayout with nothing but the interface: no pan, no travel.
type plainLayout struct{}

func (plainLayout) SetSource([]byte)               {}
func (plainLayout) Height() float32                { return 100 }
func (plainLayout) At(float32, float32) DocPos     { return DocPos{} }
func (plainLayout) Rects(DocPos, DocPos) []Rect32  { return nil }
func (plainLayout) Text(DocPos, DocPos) string     { return "" }
func (plainLayout) LinkAt(float32, float32) string { return "" }
func (plainLayout) BlockAt(float32) (int, float32) { return -1, 0 }
func (plainLayout) BlockTop(int) (float32, bool)   { return 0, false }

// The pan, as the page reads in cells (goldens with each run's style, testdata/htmlcells): a wide
// sequence at 0, a step on, and at its end; and the view after a ] and a } (block and heading
// travel), each from the top.
func TestThePageNavigationGoldens(t *testing.T) {
	html := "<p>before</p>" + navFence(wideSequence) + "<p>after</p>"
	c := layCells(t, html, navRoom)
	bi, _ := diagramBlock(t, c)
	for _, step := range []struct {
		name string
		off  int
	}{{"pan-0", 0}, {"pan-mid", htmlPanStep}, {"pan-max", c.over(bi)}} {
		c.pan = map[int]int{bi: step.off}
		s := newRecordSurface(navRoom, len(c.rows))
		c.PaintCells(s, 0, navRoom, DocPos{}, DocPos{})
		navGolden(t, step.name, surfaceGolden(s))
	}
	for _, step := range []struct {
		name string
		key  rune
	}{{"travel-block", ']'}, {"travel-heading", '}'}} {
		v, _ := navView(t, markdownHTML(t, navPage), 40, 6)
		v.HandleEvent(navKey(step.key, string(step.key)))
		s := newRecordSurface(40, 6)
		v.Render(s)
		navGolden(t, step.name, surfaceGolden(s))
	}
}

// surfaceGolden is s as a golden says it: each row's text, then its runs of one style.
func surfaceGolden(s *recordSurface) string {
	var b strings.Builder
	for y := range s.h {
		b.WriteString(goldenRow(s, y))
	}
	return b.String()
}

// navGolden compares got with testdata/htmlcells/<name>.golden (-update writes it).
func navGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "htmlcells", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (go test ./tui/widget -run %s -update)", path, err, t.Name())
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

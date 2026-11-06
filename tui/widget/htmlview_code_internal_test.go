package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// htmlview_code_internal_test.go: a code block in cells is boxed and never wrapped. A line wider than
// the room stays one line, and the block pans exactly as a wide diagram does.

const (
	codeRoom = 20                                                   // the page's width in these cells
	codeLine = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN" // 50 cells, each its own
)

// codeDoc is a code block of lines, in the language lang ("" none).
func codeDoc(lang string, lines ...string) string {
	class := ""
	if lang != "" {
		class = ` class="language-` + lang + `"`
	}
	return "<pre><code" + class + ">" + strings.Join(lines, "\n") + "</code></pre>"
}

// codeBlock is c's first code block's index.
func codeBlock(t *testing.T, c *cellHTML) int {
	t.Helper()
	for bi := range c.doc {
		if c.doc[bi].kind == hPre && c.doc[bi].diagram == "" {
			return bi
		}
	}
	t.Fatal("no code block")
	return -1
}

func wantRows(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n--- got\n%s\n--- want\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A code block is a box across its room: the language's name on its top edge, its lines inset between
// its sides, a tab to its stop.
func TestACodeBlockIsBoxedWithItsLanguageOnTop(t *testing.T) {
	c := layCells(t, codeDoc("go", "a := 1", "\tb()"), codeRoom)
	wantRows(t, "the box", navRows(c, 0, 4), []string{
		"┌─ go ─────────────┐",
		"│ a := 1           │",
		"│     b()          │",
		"└──────────────────┘",
	})
	s := newRecordSurface(codeRoom, 4)
	c.PaintCells(s, 0, codeRoom, DocPos{}, DocPos{})
	navGolden(t, "code-box", surfaceGolden(s))
}

// With no language the top edge is a plain rule; a name that doesn't fit its room is left off, the box
// still whole.
func TestACodeBlockWithNoLanguageOrNoRoomHasNoName(t *testing.T) {
	wantRows(t, "no language", navRows(layCells(t, codeDoc("", "x"), 8), 0, 3), []string{"┌──────┐", "│ x    │", "└──────┘"})
	wantRows(t, "a name too long", navRows(layCells(t, codeDoc("typescript", "x"), 8), 0, 3), []string{"┌──────┐", "│ x    │", "└──────┘"})
	// the narrowest room that holds it: "┌─ go ─┐", no less
	wantRows(t, "a name that just fits", navRows(layCells(t, codeDoc("go", "x"), 8), 0, 1), []string{"┌─ go ─┐"})
	wantRows(t, "a cell short of it", navRows(layCells(t, codeDoc("go", "x"), 7), 0, 1), []string{"┌─────┐"})
}

// A code block in a quote or a list keeps its indent: the box starts where the block's text would, the
// quote's bar and the indent outside it, and what is between its sides the page's own fill.
func TestACodeBlockKeepsItsIndentOnThePagesFill(t *testing.T) {
	for name, html := range map[string]string{
		"quote": "<blockquote>" + codeDoc("", "x") + "</blockquote>",
		"list":  "<ul><li><p>item</p>" + codeDoc("", "x") + "</li></ul>",
	} {
		t.Run(name, func(t *testing.T) {
			c := layCells(t, html, codeRoom)
			bi := codeBlock(t, c)
			b := &c.doc[bi]
			left := 2*b.quote + b.indent
			if left == 0 {
				t.Fatalf("precondition: the block is set in (quote %d, indent %d)", b.quote, b.indent)
			}
			top := c.tops[bi]
			s := newRecordSurface(codeRoom, len(c.rows))
			c.PaintCells(s, 0, codeRoom, DocPos{}, DocPos{})
			if s.text[top][left] != "┌" || s.text[top+1][left] != "│" || s.text[top+2][left] != "└" {
				t.Errorf("the box isn't at column %d:\n%s", left, strings.Join(navRows(c, 0, len(c.rows)), "\n"))
			}
			if s.text[top][codeRoom-1] != "┐" || s.text[top+1][codeRoom-1] != "│" || s.text[top+2][codeRoom-1] != "┘" {
				t.Errorf("the box doesn't end at the page's last column:\n%s", strings.Join(navRows(c, 0, len(c.rows)), "\n"))
			}
			// the interior is the page's own fill, not a shade: tokenTheme maps TokenSurface to
			// the page's base, so the shading the box once drew inside was invisible by
			// construction; between the sides the page's base is painted, so no cluster punches
			// the terminal's own colour through
			for x := left + 1; x < codeRoom-1; x++ {
				bg, ok := s.style[top+1][x].GetBackground()
				tok, isTok := bg.Token()
				if !ok || !isTok || tok != style.TokenBackground {
					t.Errorf("column %d, inside the box, is not on the page's fill (%v)", x, bg)
				}
			}
			for _, x := range []int{left, codeRoom - 1} {
				if got, want := s.style[top+1][x], style.PageText().Foreground(codeBoxTone); styleWords(got) != styleWords(want) {
					t.Errorf("the side at column %d wears %q, want the box's tone %q", x, styleWords(got), styleWords(want))
				}
			}
		})
	}
}

// A line wider than the room stays one line: the block pans, its sides and edges hold still, and a « or
// » stands in for a side where more of it lies that way, as a diagram's edge columns hold theirs. Each
// end stops: Pan reports no move there.
func TestALongCodeLineStaysOneRowAndPans(t *testing.T) {
	c := layCells(t, codeDoc("go", codeLine), codeRoom)
	if len(c.rows) != 3 {
		t.Fatalf("%d rows, want the edges and one line:\n%s", len(c.rows), strings.Join(navRows(c, 0, len(c.rows)), "\n"))
	}
	bi := codeBlock(t, c)
	if want := len(codeLine) + 2 - (codeRoom - 2); c.over(bi) != want {
		t.Fatalf("over = %d, want %d", c.over(bi), want)
	}
	top, bottom := "┌─ go ─────────────┐", "└──────────────────┘"
	wantRows(t, "unpanned", navRows(c, 0, 3), []string{top, "│ " + codeLine[:17] + "»", bottom})
	s := newRecordSurface(codeRoom, 3)
	c.PaintCells(s, 0, codeRoom, DocPos{}, DocPos{})
	navGolden(t, "pan-code-0", surfaceGolden(s))

	if !c.Pan(0, 3, htmlPanStep) {
		t.Fatal("Pan didn't move a wide code block")
	}
	wantRows(t, "panned 8", navRows(c, 0, 3), []string{top, "«" + codeLine[7:25] + "»", bottom})
	s = newRecordSurface(codeRoom, 3)
	c.PaintCells(s, 0, codeRoom, DocPos{}, DocPos{})
	navGolden(t, "pan-code-mid", surfaceGolden(s))

	c.Pan(0, 3, 1000)
	wantRows(t, "at the end", navRows(c, 0, 3), []string{top, "«" + codeLine[33:] + " │", bottom})
	s = newRecordSurface(codeRoom, 3)
	c.PaintCells(s, 0, codeRoom, DocPos{}, DocPos{})
	navGolden(t, "pan-code-max", surfaceGolden(s))
	if c.Pan(0, 3, 1) {
		t.Error("Pan moved past the line's end")
	}
	c.Pan(0, 3, -1000)
	if c.Pan(0, 3, -1) {
		t.Error("Pan moved before the line's start")
	}
}

// A block's marks are on its middle line on screen: all its lines pan together, the marks drawn over
// the sides of that one row.
func TestACodeBlocksMarksAreOnItsMiddleLine(t *testing.T) {
	c := layCells(t, codeDoc("", "a", codeLine, "b", "c", "d"), codeRoom)
	c.Pan(0, len(c.rows), htmlPanStep)
	got := navRows(c, 0, len(c.rows))
	wantRows(t, "the block", got, []string{
		"┌──────────────────┐",
		"│                  │", // "a" panned off
		"│" + codeLine[7:25] + "│",
		"«                  »", // lines 1..5 on screen: the middle one
		"│                  │",
		"│                  │",
		"└──────────────────┘",
	})
	// scrolled so only its last two lines show, the middle of what shows moves with them
	got = navRows(c, 4, 3)
	if !strings.HasPrefix(got[0], "«") || !strings.HasSuffix(got[0], "»") {
		t.Errorf("the marks aren't on the middle of the lines on screen:\n%s", strings.Join(got, "\n"))
	}
	// only its edge on screen: no line, no marks
	if got := navRows(c, 6, 1); strings.ContainsAny(got[0], "«»") {
		t.Errorf("marks on the box's edge: %q", got[0])
	}
}

// A block that fits is unchanged by a pan: nothing moves and no mark shows, to the last line that fits
// (its text, a space each side, and the two sides); a cell more and it pans.
func TestACodeBlockThatFitsDoesNotPan(t *testing.T) {
	fits := layCells(t, codeDoc("", codeLine[:16]), codeRoom)
	bi := codeBlock(t, fits)
	if fits.over(bi) != 0 {
		t.Errorf("a 16-cell line in 20: over = %d, want 0", fits.over(bi))
	}
	if fits.Pan(0, 3, htmlPanStep) || fits.ScrollX(0, 1, 3) {
		t.Error("a block that fits panned")
	}
	wantRows(t, "fits", navRows(fits, 0, 3), []string{"┌──────────────────┐", "│ " + codeLine[:16] + " │", "└──────────────────┘"})
	over := layCells(t, codeDoc("", codeLine[:17]), codeRoom)
	if got := over.over(codeBlock(t, over)); got != 1 {
		t.Errorf("a 17-cell line in 20: over = %d, want 1", got)
	}
}

// A wide cluster shows whole or not at all between the box's sides: panned onto either side, it leaves
// its cells blank (shaded), never half of itself, and never covers a side.
func TestAWideClusterShowsWholeBetweenTheSides(t *testing.T) {
	wide := func(s string) int {
		n := 0
		for _, r := range s {
			if r >= 0x4e00 {
				n += 2
			} else {
				n++
			}
		}
		return n
	}
	c := newCellHTML(wide)
	c.SetSource([]byte(codeDoc("", strings.Repeat("一", 20))))
	c.SetWidth(codeRoom)
	bi := codeBlock(t, c)
	for off := range c.over(bi) + 1 {
		c.pan = map[int]int{bi: off}
		s := newRecordSurface(codeRoom, 3)
		c.PaintCells(s, 0, codeRoom, DocPos{}, DocPos{})
		for x := 1; x < codeRoom-1; x++ {
			if s.text[1][x] == "一" && x+2 > codeRoom-1 {
				t.Errorf("pan %d: a wide cluster at column %d covers the right side", off, x)
			}
		}
		if s.text[1][0] == "一" || s.text[1][codeRoom-1] == "一" {
			t.Errorf("pan %d: a cluster on a side: %q", off, s.row(1))
		}
		// the clusters that show are the ones whose whole width is inside: logical 2+2i, on screen less off
		shown := 0
		for i := range 20 {
			if x := 2 + 2*i - off; x > 0 && x+2 <= codeRoom-1 {
				shown++
				if s.text[1][x] != "一" {
					t.Errorf("pan %d: cluster %d isn't at column %d: %q", off, i, x, s.row(1))
				}
			}
		}
		if n := strings.Count(s.row(1), "一"); n != shown {
			t.Errorf("pan %d: %d clusters on the row, want %d: %q", off, n, shown, s.row(1))
		}
	}
}

// A point on a panned line is the text shown there: At reads the shifted column, clamped between the
// box's sides, and a selection's rects are where the selection shows. Its copy is the whole text.
func TestAPannedLinesPointsAndSelectionAreTheShiftedColumns(t *testing.T) {
	c := layCells(t, codeDoc("", codeLine), codeRoom)
	bi := codeBlock(t, c)
	start := DocPos{Block: bi}
	upTo := func(p DocPos) string { return c.Text(start, p) }
	c.Pan(0, 3, htmlPanStep) // column x shows the line's cell x+8; its text starts at cell 2
	for x, want := range map[float32]int{5: 11, 1: 7, 18: 24} {
		if got := upTo(c.At(x, 1)); got != codeLine[:want] {
			t.Errorf("At(%v) is after %q, want %q", x, got, codeLine[:want])
		}
	}
	if got := upTo(c.At(0, 1)); got != codeLine[:7] {
		t.Errorf("At the left side is after %q, want the first shown (%q)", got, codeLine[:7])
	}
	if got := upTo(c.At(19, 1)); got != codeLine[:25] {
		t.Errorf("At the right side is after %q, want the last shown (%q)", got, codeLine[:25])
	}
	from, to := c.At(5, 1), c.At(8, 1) // what shows at columns 5, 6 and 7
	if got := c.Text(from, to); got != codeLine[11:14] {
		t.Fatalf("the selection's text = %q, want %q", got, codeLine[11:14])
	}
	rects := c.Rects(from, to)
	if len(rects) != 1 || rects[0].X != 5 || rects[0].W != 3 || rects[0].Y != 1 {
		t.Errorf("the selection's rects = %+v, want columns 5 to 7 of row 1", rects)
	}
	s := newRecordSurface(codeRoom, 3)
	c.PaintCells(s, 0, codeRoom, from, to)
	navGolden(t, "pan-code-selected", surfaceGolden(s))
	for x := range codeRoom {
		rev, _ := s.style[1][x].GetReverse()
		if want := x >= 5 && x < 8; rev != want {
			t.Errorf("column %d reversed = %v, want %v: %q", x, rev, want, s.row(1))
		}
	}
	// a selection panned off shows nothing, and is still the text
	off := DocPos{Block: bi, Span: from.Span, Offset: 3}
	if r := c.Rects(start, off); len(r) != 0 {
		t.Errorf("a selection left of the room has rects %+v", r)
	}
	if got := c.Text(start, c.blockEnd(bi)); got != codeLine {
		t.Errorf("the block's copy = %q, want its whole line", got)
	}
}

// A point on the box's edges is its block's start and end, a selection dragged onto them taking the
// whole block, from a quote too (a quote bar's position isn't the document's start).
func TestAPointOnACodeBlocksEdgesIsItsEnds(t *testing.T) {
	c := layCells(t, "<p>before</p><blockquote>"+codeDoc("", "one", "", "two")+"</blockquote>", codeRoom)
	bi := codeBlock(t, c)
	top := c.tops[bi]
	if got := c.At(4, float32(top)); got != (DocPos{Block: bi}) {
		t.Errorf("the top edge is %+v, want the block's start", got)
	}
	if got := c.At(4, float32(top+4)); got != c.blockEnd(bi) {
		t.Errorf("the bottom edge is %+v, want the block's end %+v", got, c.blockEnd(bi))
	}
	if got := c.At(6, float32(top+2)); got.Block != bi {
		t.Errorf("a blank line is %+v, not in the block", got)
	}
}

// A link on a panned line is where it shows: under the pointer there, its rect there, and gone while it
// is panned off.
func TestAPannedLinesLinksAreTheShiftedColumns(t *testing.T) {
	c := layCells(t, `<pre><code>`+strings.Repeat("a", 30)+`<a href="x.md">link</a></code></pre>`, codeRoom)
	for x := range codeRoom {
		if got := c.LinkAt(float32(x), 1); got != "" {
			t.Fatalf("unpanned, column %d is the link %q: it lies past the room", x, got)
		}
	}
	if r := c.LinkRects(0, 3, nil); len(r) != 0 {
		t.Fatalf("unpanned, the link's rects %+v: it lies past the room", r)
	}
	c.Pan(0, 3, 1000) // over: 36 less 18; the link's first cell, 32, shows at 14
	if got := c.LinkAt(13, 1); got != "" {
		t.Errorf("column 13, before the link: %q", got)
	}
	for x := float32(14); x < 18; x++ {
		if got := c.LinkAt(x, 1); got != "x.md" {
			t.Errorf("column %v, on the link: %q", x, got)
		}
	}
	if got := c.LinkAt(18, 1); got != "" {
		t.Errorf("column 18, past the link: %q", got)
	}
	r := c.LinkRects(0, 3, nil)
	if len(r) != 1 || r[0].X != 14 || r[0].W != 4 || r[0].Y != 1 {
		t.Errorf("the link's rects %+v, want columns 14 to 17 of row 1", r)
	}
}

// Find brings the current match into view on a panned line: a mark past the room pans the block to it,
// one before the room pans back, its cells marked at the shifted columns. One in view doesn't pan.
func TestFindPansAWideCodeBlockToTheCurrentMark(t *testing.T) {
	line := "needle" + strings.Repeat("-", 30) + "needle" + strings.Repeat("-", 30)
	c := layCells(t, codeDoc("", line), codeRoom)
	bi := codeBlock(t, c)
	if n := c.SetMarks(func(text string) [][2]int { return [][2]int{{0, 6}, {36, 42}} }); n != 2 {
		t.Fatalf("%d marks", n)
	}
	shows := func(what string) {
		t.Helper()
		if got := navRows(c, 0, 3)[1]; !strings.Contains(got, "needle") {
			t.Errorf("%s: the mark isn't on screen: %q (pan %d)", what, got, c.offset(bi))
		}
	}
	c.SetCurrentMark(1)
	shows("the second mark")
	s := newRecordSurface(codeRoom, 3)
	c.PaintCells(s, 0, codeRoom, DocPos{}, DocPos{})
	navGolden(t, "pan-code-find", surfaceGolden(s))
	x := len([]rune(s.row(1)[:strings.Index(s.row(1), "needle")])) // in cells: « is two bytes, one cell
	for i := x; i < x+6; i++ {
		if rev, _ := s.style[1][i].GetReverse(); !rev {
			t.Errorf("column %d, on the current mark, isn't drawn as current", i)
		}
	}
	if x+6 > codeRoom-1 {
		t.Errorf("the mark ends at %d, past the right side", x+6)
	}
	before := c.offset(bi)
	c.SetCurrentMark(1)
	if c.offset(bi) != before {
		t.Errorf("a mark in view panned: %d, was %d", c.offset(bi), before)
	}
	c.SetCurrentMark(0)
	shows("the first mark")
	if c.offset(bi) != 0 {
		t.Errorf("back at the first mark, pan %d, want 0: its start is the line's", c.offset(bi))
	}
}

// The view's keys and wheel pan a code block as they do a diagram: Right/Left and, under Vim, l/h pan
// the wide block nearest the page's top, past a block that fits, each block its own; the wheel the one
// under the pointer.
func TestTheKeysAndWheelPanACodeBlock(t *testing.T) {
	html := codeDoc("", "fits") + "<p>x</p>" + codeDoc("", codeLine) + "<p>x</p>" + codeDoc("", codeLine)
	v, c := navView(t, html, codeRoom, 30)
	var blocks []int
	for bi := range c.doc {
		if c.doc[bi].kind == hPre {
			blocks = append(blocks, bi)
		}
	}
	if len(blocks) != 3 {
		t.Fatalf("%d code blocks", len(blocks))
	}
	v.HandleEvent(navKey(tui.KeyRight, ""))
	if c.offset(blocks[0]) != 0 || c.offset(blocks[1]) != htmlPanStep || c.offset(blocks[2]) != 0 {
		t.Errorf("Right panned %d, %d, %d: want only the first wide block, by a step", c.offset(blocks[0]), c.offset(blocks[1]), c.offset(blocks[2]))
	}
	v.HandleEvent(navKey(tui.KeyLeft, ""))
	if c.offset(blocks[1]) != 0 {
		t.Errorf("Left left it at %d", c.offset(blocks[1]))
	}
	if !c.ScrollX(0, float32(c.tops[blocks[2]]+1), 3) || c.offset(blocks[2]) != 3 || c.offset(blocks[1]) != 0 {
		t.Errorf("the wheel over the second wide block panned %d and %d", c.offset(blocks[1]), c.offset(blocks[2]))
	}
	if c.ScrollX(0, float32(c.tops[blocks[0]]+1), 3) {
		t.Error("the wheel panned a block that fits")
	}
}

// A narrower page makes a block wide, a wider one fits it again, its pan kept within what it has.
func TestACodeBlocksPanFollowsThePagesWidth(t *testing.T) {
	c := layCells(t, codeDoc("", codeLine), codeRoom)
	bi := codeBlock(t, c)
	c.Pan(0, 3, 1000)
	c.SetWidth(float32(len(codeLine) + 4))
	if c.over(bi) != 0 || c.offset(bi) != 0 {
		t.Errorf("wide enough: over %d, offset %d", c.over(bi), c.offset(bi))
	}
	wantRows(t, "fits again", navRows(c, 0, 3)[1:2], []string{"│ " + codeLine + " │"})
	c.SetWidth(codeRoom)
	if c.offset(bi) != c.over(bi) {
		t.Errorf("narrow again: offset %d, over %d (the pan kept)", c.offset(bi), c.over(bi))
	}
}

// A panned code block stays in its room: nothing drawn left of its box, in a quote, or past the page's
// width, the scroll indicator's column.
func TestAPannedCodeBlockStaysInItsRoom(t *testing.T) {
	c := layCells(t, "<blockquote>"+codeDoc("", codeLine)+"</blockquote>", codeRoom)
	bi := codeBlock(t, c)
	c.Pan(0, len(c.rows), htmlPanStep)
	top, left := c.tops[bi], 2*c.doc[bi].quote
	s := newRecordSurface(codeRoom+5, len(c.rows))
	for y := range s.h {
		for x := range s.w {
			s.text[y][x] = "#"
		}
	}
	c.PaintCells(s, 0, codeRoom, DocPos{}, DocPos{})
	for x := codeRoom; x < s.w; x++ {
		if s.text[top+1][x] != "#" {
			t.Errorf("column %d, past the page's %d: %q", x, codeRoom, s.text[top+1][x])
		}
	}
	if s.text[top+1][0] != "▎" || s.text[top+1][left-1] != " " {
		t.Errorf("the quote's bar and space aren't kept: %q", s.row(top+1))
	}
}

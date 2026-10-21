package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/vt"
)

// The Terminal's rendering: its cells drawn from the screen, a vt cell's style, and its cursor.

// Render paints the visible lines: the live screen in input mode, and the
// normal-mode view over the scrollback in normal mode. With a view it draws
// them off the window, at the terminal's own size, and hands the view that
// frame and the caret; nothing is drawn in s.
func (t *Terminal) Render(s tui.Surface) {
	ctx := t.Context()
	if t.view == nil || ctx == nil {
		t.draw(s)
		return
	}
	cells := ctx.RenderOffscreen(&t.off, tui.Size{W: t.cols, H: t.rows}, t.CellFace(), t.draw)
	x, y, ok := t.cursor()
	t.view.Frame(cells, t.cols, t.rows, tui.Point{X: x, Y: y}, t.CursorShape(), ok && ctx.Focused())
}

// draw is the terminal's drawing on s: Render's, on the window or off it.
func (t *Terminal) draw(s tui.Surface) {
	size := s.Size()
	top := t.scr.Scrollback()
	if t.mode == TerminalNormal {
		top = t.normal.top
	}
	for y := 0; y < size.H; y++ {
		line := top + y
		cells := t.lineCells(line)
		for x := 0; x < size.W; x++ {
			var c vt.Cell
			if x < len(cells) {
				c = cells[x]
			} else {
				c = vt.Cell{Width: 1}
			}
			if c.Width == 0 {
				continue // painted with its wide cluster
			}
			content := c.Content
			if content == "" || c.Attr&vt.AttrInvisible != 0 {
				content = " "
			}
			st := t.cellStyle(c)
			if t.mode == TerminalNormal && t.normal.selected(line, x) {
				st = st.Reverse(c.Attr&vt.AttrReverse == 0)
			}
			s.SetCell(x, y, content, st)
		}
	}
	if t.mode == TerminalNormal {
		t.normal.render(t, s)
	}
}

// lineCells is absolute line i: scrollback lines first, then the screen.
func (t *Terminal) lineCells(i int) []vt.Cell {
	sb := t.scr.Scrollback()
	if i < 0 {
		return nil
	}
	if i < sb {
		return t.scr.Line(i)
	}
	row := i - sb
	rows, cols := t.scr.Size()
	if row >= rows {
		return nil
	}
	cells := make([]vt.Cell, cols)
	for c := range cells {
		cells[c] = t.scr.Cell(row, c)
	}
	return cells
}

// lineCount is the number of absolute lines: the scrollback and the screen.
func (t *Terminal) lineCount() int {
	rows, _ := t.scr.Size()
	return t.scr.Scrollback() + rows
}

func (t *Terminal) cellStyle(c vt.Cell) style.Style {
	st := style.New()
	switch {
	case !c.FG.IsDefault():
		st = st.Foreground(c.FG)
	case t.cfg.themeColors:
		if fg, ok := t.look.GetForeground(); ok {
			st = st.Foreground(fg)
		} else {
			st = st.Foreground(style.TokenForeground)
		}
	}
	switch {
	case !c.BG.IsDefault():
		st = st.Background(c.BG)
	case t.cfg.themeColors:
		if bg, ok := t.look.GetBackground(); ok {
			st = st.Background(bg)
		} else {
			st = st.Background(style.TokenBackground)
		}
	}
	// a foreground raised to the minimum contrast against the background it is drawn on
	if t.minContrast > 1 && c.Attr&vt.AttrReverse == 0 {
		fg, fgSet := st.GetForeground()
		bg, bgSet := st.GetBackground()
		if !bgSet {
			bg, bgSet = t.look.GetBackground()
		}
		if fgSet && bgSet {
			st = st.Foreground(t.legible(fg, bg))
		}
	}
	return st.WithAttrs(c.Attr) // vt's cells and a Style share the bits (invisible is not a Style's)
}

// Cursor is the program's cursor in input mode (hidden when the program
// hides it), and the normal-mode cursor in normal mode. With a view there is
// none in the window's cells: the view draws the caret (Frame).
func (t *Terminal) Cursor() (x, y int, ok bool) {
	if t.view != nil {
		return 0, 0, false
	}
	return t.cursor()
}

// cursor is the terminal's own cursor in its cells, as Cursor reports it with no view.
func (t *Terminal) cursor() (x, y int, ok bool) {
	if t.mode == TerminalNormal {
		return t.normal.cursor(t)
	}
	row, col, visible, _ := t.scr.Cursor()
	return col, row, visible && (t.running || !t.exited)
}

// CursorShape shows the mode, as the Editor's does: a block in normal mode, and in input mode a
// thin bar, the Editor's while typing, unless the program asked for a shape of its own with
// DECSCUSR (vim's block, an underline). DECSCUSR 0 asks for the host's own, which is the bar.
func (t *Terminal) CursorShape() tui.CursorShape {
	if t.mode == TerminalNormal {
		return tui.CursorShapeBlock
	}
	_, _, _, shape := t.scr.Cursor() // vt's shapes are tui's
	if shape == tui.CursorShapeDefault {
		return tui.CursorShapeBar
	}
	return shape
}

// SetCellFace draws the Terminal's cells in family, a monospace font of its own, at the window's
// cell size (tui.CellFacer); "" is the window's own. A backend that draws no fonts — a terminal —
// ignores it, its terminal owning the font. A menu or tooltip over it keeps the window's.
func (t *Terminal) SetCellFace(family string) {
	if t.cellFace == family {
		return
	}
	t.cellFace = family
	t.MarkDirty()
}

// CellFace is SetCellFace's family: tui.CellFacer.
func (t *Terminal) CellFace() string { return t.cellFace }

// SetCursorColor gives the hardware cursor a colour of its own, as the Editor's SetCursorColor
// does, so the two carets look alike; the terminal's own colour otherwise.
func (t *Terminal) SetCursorColor(c style.Color) {
	t.cursorColor, t.cursorColored = c, true
	t.MarkDirty()
}

// CursorColor implements tui.CursorColorer.
func (t *Terminal) CursorColor() (style.Color, bool) { return t.cursorColor, t.cursorColored }

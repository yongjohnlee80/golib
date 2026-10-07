package gui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// renderer draws the grid with Gio. It runs on tui's loop goroutine, inside Flush, and is the
// only user of its shaper (text.Shaper is not safe for concurrent use).
//
// Every row is recorded once into its own op.Ops and replayed by later frames until it changes.
// A recorded row is never written again: a frame handed to the Gio goroutine may still be drawing
// it, so a changed row gets a NEW op.Ops and the old one is left to the garbage collector.
type renderer struct {
	shaper   *text.Shaper
	typeface font.Typeface
	theme    Theme

	m         metrics     // the metrics the row cache was recorded under
	pageBG    color.NRGBA // the grid's most common background: the frame around it (drawMargins)
	pageBGSet bool
	rows      []op.CallOp
	glyphs    map[glyphKey]glyphShape // cleared with the row cache when the font size changes
}

type glyphKey struct {
	content      string
	bold, italic bool
}

type glyphShape struct {
	path    clip.PathSpec
	bitmaps op.CallOp // colour glyphs (emoji); empty for outline-only text
	advance int       // device pixels
	empty   bool      // nothing to draw (a space)
}

func newRenderer(shaper *text.Shaper, typeface string, theme Theme) *renderer {
	return &renderer{shaper: shaper, typeface: font.Typeface(typeface), theme: theme, glyphs: map[glyphKey]glyphShape{}}
}

// cursorState is the cursor as the App latched it (Backend.ShowCursor, SetCursor…).
type cursorState struct {
	visible bool
	x, y    int
	shape   tui.CursorShape
	color   color.NRGBA
	colored bool // color is set; otherwise the theme's cursor colour
}

// placedImage is a decoded image and where the App placed it.
type placedImage struct {
	tui.ImagePlacement
	op paint.ImageOp
}

// frame records one complete frame as a macro: every row, the native views over their cells,
// the images, the cursor. g's dirty rows are recorded afresh and marked clean. Rows are cached apart from the
// window's size, so a window resized within the same grid re-records nothing; only the margins
// past the last whole cell are drawn for every frame.
func (r *renderer) frame(g *grid, m metrics, cur cursorState, natives []tui.NativePlacement, images []placedImage, focused bool) op.CallOp {
	if !r.m.sameCells(m) || len(r.rows) != g.h {
		if r.m.ppem != m.ppem {
			clear(r.glyphs)
		}
		r.m = m
		r.rows = make([]op.CallOp, g.h)
		g.touchAll()
	}
	changed := false
	for y := range g.h {
		if g.dirty[y] {
			r.rows[y] = r.recordRow(g, y)
			g.dirty[y] = false
			changed = true
		}
	}
	if changed || !r.pageBGSet {
		r.pageBG, r.pageBGSet = r.pageBackground(g), true // only when a row changed: a full pass over the cells
	}

	ops := new(op.Ops) // owned by the frame from here on
	rec := op.Record(ops)
	r.drawMargins(ops, g, m)
	for _, row := range r.rows {
		row.Add(ops)
	}
	r.drawNatives(ops, g, m, natives)
	for _, img := range images {
		r.drawImage(ops, img)
	}
	if cur.visible {
		r.drawCursor(ops, g, cur, focused)
	}
	return rec.Stop()
}

// drawMargins fills everything around the grid (the window's padding, and the half-cells of
// leftover on each side) with the page colour, so the grid sits in an even frame, as a terminal's
// padding does. A bar on the top or bottom row stays exactly one cell tall, its text centred, and
// stops at the frame instead of running into the window's border.
func (r *renderer) drawMargins(ops *op.Ops, g *grid, m metrics) {
	grid := m.cellRect(0, 0, g.w, g.h)
	win := image.Rectangle{Max: m.window}
	bg := r.pageBG
	fillRect(ops, image.Rect(win.Min.X, win.Min.Y, win.Max.X, grid.Min.Y), bg)   // top
	fillRect(ops, image.Rect(win.Min.X, grid.Max.Y, win.Max.X, win.Max.Y), bg)   // bottom
	fillRect(ops, image.Rect(win.Min.X, grid.Min.Y, grid.Min.X, grid.Max.Y), bg) // left
	fillRect(ops, image.Rect(grid.Max.X, grid.Min.Y, win.Max.X, grid.Max.Y), bg) // right
}

// pageBackground is the background most cells of the grid have: the app's page or backdrop, not a
// bar's or a panel's. Ties go to the colour seen first, row by row. With no cells, the theme's.
func (r *renderer) pageBackground(g *grid) color.NRGBA {
	counts := map[color.NRGBA]int{}
	best, bestN := r.theme.BG, 0
	for _, c := range g.cells {
		_, bg := r.colors(c.Attrs)
		counts[bg]++
		if n := counts[bg]; n > bestN {
			best, bestN = bg, n
		}
	}
	return best
}

// recordRow records row y: backgrounds first, merged into runs, then glyphs and decorations.
func (r *renderer) recordRow(g *grid, y int) op.CallOp {
	ops := new(op.Ops)
	rec := op.Record(ops)
	m := r.m
	for x := 0; x < g.w; {
		_, bg := r.colors(g.at(x, y).Attrs)
		run := x + 1
		for run < g.w {
			_, next := r.colors(g.at(run, y).Attrs)
			if next != bg {
				break
			}
			run++
		}
		if bg != r.theme.BG {
			fillRect(ops, m.cellRect(x, y, run-x, 1), bg)
		}
		x = run
	}
	for x := range g.w {
		c := g.at(x, y)
		if c.Continuation() {
			continue
		}
		fg, _ := r.colors(c.Attrs)
		r.drawCell(ops, c, x, y, fg)
	}
	return rec.Stop()
}

// colors is a cell's foreground and background as drawn: reverse swaps them, faint dims the text.
func (r *renderer) colors(a tui.CellAttrs) (fg, bg color.NRGBA) {
	fg, bg = r.theme.resolve(a.FG, true), r.theme.resolve(a.BG, false)
	if a.Mask&tui.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	if a.Mask&tui.AttrFaint != 0 {
		fg.A = fg.A / 5 * 3
	}
	return fg, bg
}

// drawCell draws one cell's glyph and decorations in fg, without its background.
func (r *renderer) drawCell(ops *op.Ops, c tui.Cell, x, y int, fg color.NRGBA) {
	m := r.m
	span := max(int(c.Width), 1)
	rect := m.cellRect(x, y, span, 1)
	if ru, size := utf8.DecodeRuneInString(c.Content); size == len(c.Content) && drawable(ru) {
		drawBox(ops, ru, m.cellRect(x, y, 1, 1), fg, m.scale)
	} else if c.Content != "" && c.Content != " " {
		gs := r.shape(c.Content, c.Attrs.Mask&tui.AttrBold != 0, c.Attrs.Mask&tui.AttrItalic != 0)
		if !gs.empty {
			cl := clip.Rect(rect).Push(ops)
			tr := op.Affine(glyphPlacement(rect, m, gs.advance)).Push(ops)
			outline := clip.Outline{Path: gs.path}.Op().Push(ops)
			paint.ColorOp{Color: fg}.Add(ops)
			paint.PaintOp{}.Add(ops)
			outline.Pop()
			gs.bitmaps.Add(ops)
			tr.Pop()
			cl.Pop()
		}
	}
	lw := max(1, int(m.scale+0.5))
	if c.Attrs.Mask&tui.AttrUnderline != 0 {
		uy := rect.Min.Y + min(m.baseline+lw, m.cell.Y-lw)
		fillRect(ops, image.Rect(rect.Min.X, uy, rect.Max.X, uy+lw), fg)
	}
	if c.Attrs.Mask&tui.AttrStrikethrough != 0 {
		sy := rect.Min.Y + m.cell.Y/2
		fillRect(ops, image.Rect(rect.Min.X, sy, rect.Max.X, sy+lw), fg)
	}
}

// glyphPlacement puts a glyph of the given advance on the baseline of its cell span rect. A glyph
// that fits is centred in the span. One wider than the span (a colour emoji or an icon from a
// fallback font, in a one-cell span) is scaled down to the span's width about the cell's middle,
// as Ghostty and kitty fit symbols to their cells, rather than clipped to half a glyph.
func glyphPlacement(rect image.Rectangle, m metrics, advance int) f32.Affine2D {
	x, base := float32(rect.Min.X), float32(rect.Min.Y+m.baseline)
	if advance <= rect.Dx() {
		return f32.Affine2D{}.Offset(f32.Pt(x+float32((rect.Dx()-advance)/2), base))
	}
	s := float32(rect.Dx()) / float32(advance)
	mid := float32(rect.Min.Y) + float32(m.cell.Y)/2
	return f32.Affine2D{}.Offset(f32.Pt(x, base)).Scale(f32.Pt(x, mid), f32.Pt(s, s))
}

// shape is the outline and advance of one grapheme cluster in the cell font, cached.
func (r *renderer) shape(content string, bold, italic bool) glyphShape {
	k := glyphKey{content, bold, italic}
	if gs, ok := r.glyphs[k]; ok {
		return gs
	}
	f := font.Font{Typeface: r.typeface}
	if bold {
		f.Weight = font.Bold
	}
	if italic {
		f.Style = font.Italic
	}
	r.shaper.LayoutString(text.Parameters{
		Font:     f,
		PxPerEm:  fixed.Int26_6(r.m.ppem * 64),
		MaxWidth: 1 << 20,
	}, content)
	var glyphs []text.Glyph
	var advance fixed.Int26_6
	for {
		g, ok := r.shaper.NextGlyph()
		if !ok {
			break
		}
		glyphs = append(glyphs, g)
		advance += g.Advance
	}
	gs := glyphShape{advance: advance.Round(), empty: len(glyphs) == 0}
	if !gs.empty {
		gs.path = r.shaper.Shape(glyphs)
		gs.bitmaps = r.shaper.Bitmaps(glyphs)
	}
	r.glyphs[k] = gs
	return gs
}

// drawImage draws an image over its cells, its Clip (in image pixels) scaled to fill them.
func (r *renderer) drawImage(ops *op.Ops, img placedImage) {
	dst := r.m.cellRect(img.X, img.Y, img.Cols, img.Rows)
	src := image.Rect(img.Clip.X, img.Clip.Y, img.Clip.X+img.Clip.W, img.Clip.Y+img.Clip.H)
	if src.Empty() {
		src = image.Rectangle{Max: img.op.Size()}
	}
	if dst.Empty() || src.Empty() {
		return
	}
	sx := float32(dst.Dx()) / float32(src.Dx())
	sy := float32(dst.Dy()) / float32(src.Dy())
	cl := clip.Rect(dst).Push(ops)
	tr := op.Affine(f32.Affine2D{}.
		Offset(f32.Pt(-float32(src.Min.X), -float32(src.Min.Y))).
		Scale(f32.Point{}, f32.Pt(sx, sy)).
		Offset(f32.Pt(float32(dst.Min.X), float32(dst.Min.Y)))).Push(ops)
	img.op.Add(ops)
	paint.PaintOp{}.Add(ops)
	tr.Pop()
	cl.Pop()
}

// drawCursor draws the caret at the latched cell: a block shows the cell's glyph in the
// background colour over the caret colour; an unfocused window shows a hollow block.
func (r *renderer) drawCursor(ops *op.Ops, g *grid, cur cursorState, focused bool) {
	m := r.m
	if cur.x < 0 || cur.y < 0 || cur.x >= g.w || cur.y >= g.h {
		return
	}
	cc := r.theme.Cursor
	if cur.colored {
		cc = cur.color
	}
	c := g.at(cur.x, cur.y)
	rect := m.cellRect(cur.x, cur.y, max(int(c.Width), 1), 1)
	lw := max(1, int(m.scale+0.5))
	switch {
	case !focused:
		fillRect(ops, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+lw), cc)
		fillRect(ops, image.Rect(rect.Min.X, rect.Max.Y-lw, rect.Max.X, rect.Max.Y), cc)
		fillRect(ops, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+lw, rect.Max.Y), cc)
		fillRect(ops, image.Rect(rect.Max.X-lw, rect.Min.Y, rect.Max.X, rect.Max.Y), cc)
	case cur.shape == tui.CursorShapeBar:
		fillRect(ops, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+2*lw, rect.Max.Y), cc)
	case cur.shape == tui.CursorShapeUnderline:
		fillRect(ops, image.Rect(rect.Min.X, rect.Max.Y-2*lw, rect.Max.X, rect.Max.Y), cc)
	default:
		fillRect(ops, rect, cc)
		_, bg := r.colors(c.Attrs)
		r.drawCell(ops, c, cur.x, cur.y, bg)
	}
}

// decodeImage decodes an App's PNG for drawing.
func decodeImage(p tui.ImagePlacement) (placedImage, bool) {
	src, err := png.Decode(bytes.NewReader(p.PNG))
	if err != nil {
		return placedImage{}, false
	}
	return placedImage{ImagePlacement: p, op: paint.NewImageOp(src)}, true
}

// paintCells draws the cells in r (grid coordinates, clamped to the grid) as the rows draw them:
// over a native view, the cells written after it, and for a native style, the text it keeps. A
// wide cell cut by r's left edge is drawn from its head, clipped to r. glyphsOnly skips the
// backgrounds. Unlike recordRow, a background equal to the theme's is filled too: these cells
// are drawn over a view, not over the window's background.
func (r *renderer) paintCells(ops *op.Ops, g *grid, cr CellRect, glyphsOnly bool) {
	x0, y0 := max(cr.X, 0), max(cr.Y, 0)
	x1, y1 := min(cr.X+cr.W, g.w), min(cr.Y+cr.H, g.h)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	area := clip.Rect(r.m.cellRect(x0, y0, x1-x0, y1-y0)).Push(ops)
	defer area.Pop()
	for y := y0; y < y1; y++ {
		if !glyphsOnly {
			for x := x0; x < x1; x++ {
				_, bg := r.colors(g.at(x, y).Attrs)
				fillRect(ops, r.m.cellRect(x, y, 1, 1), bg)
			}
		}
		x := x0
		if g.at(x, y).Continuation() && x > 0 {
			x-- // the head of a wide cell whose second half is in r
		}
		for ; x < x1; x++ {
			c := g.at(x, y)
			if c.Continuation() {
				continue
			}
			fg, _ := r.colors(c.Attrs)
			r.drawCell(ops, c, x, y, fg)
		}
	}
}

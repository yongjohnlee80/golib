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

	m      metrics // the metrics the row cache was recorded under
	rows   []op.CallOp
	glyphs map[glyphKey]glyphShape // cleared with the row cache when the font size changes
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

// frame records one complete frame as a macro: every row, the images over their cells, the
// cursor. g's dirty rows are recorded afresh and marked clean. Rows are cached apart from the
// window's size, so a window resized within the same grid re-records nothing; only the margins
// past the last whole cell are drawn for every frame.
func (r *renderer) frame(g *grid, m metrics, cur cursorState, images []placedImage, focused bool) op.CallOp {
	if !r.m.sameCells(m) || len(r.rows) != g.h {
		if r.m.ppem != m.ppem {
			clear(r.glyphs)
		}
		r.m = m
		r.rows = make([]op.CallOp, g.h)
		g.touchAll()
	}
	for y := range g.h {
		if g.dirty[y] {
			r.rows[y] = r.recordRow(g, y)
			g.dirty[y] = false
		}
	}

	ops := new(op.Ops) // owned by the frame from here on
	rec := op.Record(ops)
	r.drawMargins(ops, g, m)
	for _, row := range r.rows {
		row.Add(ops)
	}
	for _, img := range images {
		r.drawImage(ops, img)
	}
	if cur.visible {
		r.drawCursor(ops, g, cur, focused)
	}
	return rec.Stop()
}

// drawMargins fills everything around the grid (the window's padding, and the strip past the
// last whole cell) with the background of the edge cell beside it, as terminals extend their
// edge cells: an app that paints its own background shows no band of the window's default colour.
func (r *renderer) drawMargins(ops *op.Ops, g *grid, m metrics) {
	grid := m.cellRect(0, 0, g.w, g.h)
	win := image.Rectangle{Max: m.window}
	bgAt := func(x, y int) color.NRGBA { _, bg := r.colors(g.at(x, y).Attrs); return bg }
	for y := range g.h { // left and right of each row
		row := m.cellRect(0, y, g.w, 1)
		fillRect(ops, image.Rect(win.Min.X, row.Min.Y, grid.Min.X, row.Max.Y), bgAt(0, y))
		fillRect(ops, image.Rect(grid.Max.X, row.Min.Y, win.Max.X, row.Max.Y), bgAt(g.w-1, y))
	}
	for x := range g.w { // above and below each column
		col := m.cellRect(x, 0, 1, g.h)
		fillRect(ops, image.Rect(col.Min.X, win.Min.Y, col.Max.X, grid.Min.Y), bgAt(x, 0))
		fillRect(ops, image.Rect(col.Min.X, grid.Max.Y, col.Max.X, win.Max.Y), bgAt(x, g.h-1))
	}
	// the four corners, from the corner cells
	fillRect(ops, image.Rect(win.Min.X, win.Min.Y, grid.Min.X, grid.Min.Y), bgAt(0, 0))
	fillRect(ops, image.Rect(grid.Max.X, win.Min.Y, win.Max.X, grid.Min.Y), bgAt(g.w-1, 0))
	fillRect(ops, image.Rect(win.Min.X, grid.Max.Y, grid.Min.X, win.Max.Y), bgAt(0, g.h-1))
	fillRect(ops, image.Rect(grid.Max.X, grid.Max.Y, win.Max.X, win.Max.Y), bgAt(g.w-1, g.h-1))
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

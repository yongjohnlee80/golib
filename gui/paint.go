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

// drawMargins fills the strip past the last whole cell, right and below, with the background of
// the edge cell beside it, as terminals extend their edge cells: an app that paints its own
// background shows no band of the window's default colour.
func (r *renderer) drawMargins(ops *op.Ops, g *grid, m metrics) {
	right, bottom := g.w*m.cell.X, g.h*m.cell.Y
	if m.window.X > right {
		for y := range g.h {
			_, bg := r.colors(g.at(g.w-1, y).Attrs)
			fillRect(ops, image.Rect(right, y*m.cell.Y, m.window.X, (y+1)*m.cell.Y), bg)
		}
	}
	if m.window.Y > bottom {
		for x := range g.w {
			_, bg := r.colors(g.at(x, g.h-1).Attrs)
			fillRect(ops, image.Rect(x*m.cell.X, bottom, (x+1)*m.cell.X, m.window.Y), bg)
		}
		if m.window.X > right {
			_, bg := r.colors(g.at(g.w-1, g.h-1).Attrs)
			fillRect(ops, image.Rect(right, bottom, m.window.X, m.window.Y), bg)
		}
	}
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
			// Centre the glyph in its span; a fallback glyph wider than the span is clipped to it
			// rather than drawn over its neighbour.
			dx := (rect.Dx() - gs.advance) / 2
			cl := clip.Rect(rect).Push(ops)
			off := op.Offset(image.Pt(rect.Min.X+max(dx, 0), rect.Min.Y+m.baseline)).Push(ops)
			outline := clip.Outline{Path: gs.path}.Op().Push(ops)
			paint.ColorOp{Color: fg}.Add(ops)
			paint.PaintOp{}.Add(ops)
			outline.Pop()
			gs.bitmaps.Add(ops)
			off.Pop()
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

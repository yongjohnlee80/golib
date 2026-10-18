package gui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"unicode/utf8"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/op/paint"
	"github.com/yongjohnlee80/gio/text"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// Rendering Pipeline and Op Macro Architecture:
//
//	+-----------------------+     Row Dirty Flags      +------------------------+
//	| Active Grid & Metrics | -----------------------> | Incremental Row Cache  |
//	+-----------------------+                          | (r.rows []op.CallOp)   |
//	           |                                       +------------------------+
//	           v                                                   |
//	+-----------------------+       Draw Margins & Replay          v
//	| Full Frame Assembly   | <------------------------------------+
//	+-----------------------+
//	           |
//	           +---> 1. Page Margin Fill (r.drawMargins)
//	           +---> 2. Cached Rows Add(ops) (Background runs, line arms, glyph batching)
//	           +---> 3. Compositing Layer (Native views & Kitty PNG images)
//	           +---> 4. Hardware Caret (Block, Underline, Bar, or Hollow focus frame)
//	           v
//	+-----------------------+
//	| op.Record -> CallOp   |  (Latching handoff to Gio presentation loop)
//	+-----------------------+
//
// renderer translates the active [grid] into optimized Gio vector drawing operations.
//
// Concurrency & Lifecycle:
//   - Executes exclusively on the TUI event loop goroutine within [Backend.Flush].
//   - Owns a private [text.Shaper] instance, avoiding concurrency hazards since [text.Shaper]
//     is not thread-safe.
//
// Performance & Caching Strategy:
//   - Each row is recorded into an independent [op.Ops] macro stored in rows as [op.CallOp].
//   - As long as a row's dirty flag remains false, the recorded macro is replayed without
//     re-shaping glyphs or recalculating box borders.
//   - When a row is modified, a fresh [op.Ops] instance is allocated; previous macros handed to
//     the Gio display thread are never mutated in place.
type renderer struct {
	shaper   *text.Shaper
	paths    *shapeCache // the shaper's outline paths, kept while on screen, for the cells and the native views
	typeface font.Typeface
	theme    Theme

	m         metrics     // the metrics the row cache was recorded under
	pageBG    color.NRGBA // the grid's most common background: the frame around it (drawMargins)
	pageBGSet bool
	rows      []op.CallOp
	// glyphs is cleared with the row cache when the font size or the window's face changes. It
	// holds one entry per (cluster, bold, italic, face) drawn since: bounded by the faces in the
	// table (at most tui's maxCellFaces) times the clusters actually on screen, not by frames.
	// The frame being recorded, cut at stream slots (frameParts).
	cutOps  *op.Ops
	cutRec  op.MacroOp
	cuts    []framePart
	cutting bool
	glyphs  map[glyphKey]glyphShape
	faces   []font.Typeface // the cell faces (tui.FaceHost), index 0 unused: the window's typeface
	fonts   Fonts           // the window's families, for the native views (Canvas.Fonts)
}

type glyphKey struct {
	content      string
	bold, italic bool
	face         uint16 // tui.Cell.Face: 0 the window's
}

type glyphShape struct {
	path    clip.PathSpec
	bitmaps op.CallOp // colour glyphs (emoji); empty for outline-only text
	advance int       // device pixels
	empty   bool      // nothing to draw (a space)
	glyphs  []text.Glyph
	colour  bool // it has colour glyphs (bitmaps): drawn alone, not in a row's batch
}

// newRenderer initializes a text and cell renderer with glyph cache and fonts.
func newRenderer(shaper *text.Shaper, typeface string, theme Theme) *renderer {
	return &renderer{shaper: shaper, paths: newShapeCache(shaper), typeface: font.Typeface(typeface), theme: theme, glyphs: map[glyphKey]glyphShape{},
		fonts: Fonts{Prose: uiFamily, Mono: typeface}}
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

// frame records one complete frame as a single macro: frameParts with its segments joined.
func (r *renderer) frame(g *grid, m metrics, cur cursorState, natives []tui.NativePlacement, images []placedImage, focused bool) op.CallOp {
	draw, parts := r.frameParts(g, m, cur, natives, images, focused)
	if len(parts) == 0 {
		return draw
	}
	// Stream slots draw nothing here: a caller wanting one macro gets the segments joined.
	ops := new(op.Ops)
	rec := op.Record(ops)
	draw.Add(ops)
	for _, p := range parts {
		p.seg.Add(ops)
	}
	return rec.Stop()
}

// framePart is a stream slot and the drawing that follows it, up to the next slot.
type framePart struct {
	slot streamSlot
	seg  op.CallOp
}

// frameParts records the frame: every row, then the native views and images over their cells in
// paint order, then the cursor. g's dirty rows are recorded afresh and marked clean. Rows are
// cached apart from the window's size, so a window resized within the same grid re-records
// nothing; only the margins past the last whole cell are drawn for every frame.
//
// It returns a first segment and, for each stream slot a view's DrawStream recorded, the slot and
// the drawing after it. present puts each stream's newest picture in its slot, in paint order, at
// every vsync, with no tui flush and no row recorded again.
func (r *renderer) frameParts(g *grid, m metrics, cur cursorState, natives []tui.NativePlacement, images []placedImage, focused bool) (op.CallOp, []framePart) {
	if !r.m.sameCells(m) || len(r.rows) != g.h {
		if r.m.ppem != m.ppem || r.m.fontGen != m.fontGen { // another size, or another face
			clear(r.glyphs)
			r.paths.clear()
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
	r.cutOps, r.cutRec, r.cuts, r.cutting = ops, op.Record(ops), r.cuts[:0], true
	r.drawMargins(ops, g, m)
	for _, row := range r.rows {
		row.Add(ops)
	}
	r.composite(ops, g, m, natives, images)
	if cur.visible {
		r.drawCursor(ops, g, cur, focused)
	}
	r.cutting = false
	last := r.cutRec.Stop()
	r.paths.endFrame()
	if len(r.cuts) == 0 {
		return last, nil
	}
	// cuts[0].seg is the drawing before the first slot; each later seg follows the slot before.
	first := r.cuts[0].seg
	parts := make([]framePart, len(r.cuts))
	for i := range r.cuts {
		parts[i].slot = r.cuts[i].slot
		if i+1 < len(r.cuts) {
			parts[i].seg = r.cuts[i+1].seg
		} else {
			parts[i].seg = last
		}
	}
	return first, parts
}

// cut ends the drawing so far at a stream slot and starts the next segment.
func (r *renderer) cut(slot streamSlot) {
	r.cuts = append(r.cuts, framePart{slot: slot, seg: r.cutRec.Stop()})
	r.cutRec = op.Record(r.cutOps)
}

// drawMargins fills everything around the grid (the window's padding, and the half-cells of
// leftover on each side) with the page colour, so the grid sits in an even frame, as a terminal's
// padding does. A bar on the top or bottom row stays exactly one cell tall, its text centred, and
// stops at the frame instead of running into the window's border.
func (r *renderer) drawMargins(ops *op.Ops, g *grid, m metrics) {
	if m.layout != nil {
		r.drawStageMargins(ops, m)
		return
	}
	grid := m.cellRect(0, 0, g.w, g.h)
	win := image.Rectangle{Max: m.window}
	bg := r.pageBG
	fillRect(ops, image.Rect(win.Min.X, win.Min.Y, win.Max.X, grid.Min.Y), bg)   // top
	fillRect(ops, image.Rect(win.Min.X, grid.Max.Y, win.Max.X, win.Max.Y), bg)   // bottom
	fillRect(ops, image.Rect(win.Min.X, grid.Min.Y, grid.Min.X, grid.Max.Y), bg) // left
	fillRect(ops, image.Rect(grid.Max.X, grid.Min.Y, win.Max.X, grid.Max.Y), bg) // right
}

// drawStageMargins fills what no pane's cells cover with the page colour: the padding around
// the panes, and each cell pane's leftover (the pixels under one cell, right and bottom). A
// pixel pane's cells cover it (its band is rounded up).
func (r *renderer) drawStageMargins(ops *op.Ops, m metrics) {
	bg := r.pageBG
	win, in := image.Rectangle{Max: m.window}, m.layout.inner
	fillRect(ops, image.Rect(win.Min.X, win.Min.Y, win.Max.X, in.Min.Y), bg)
	fillRect(ops, image.Rect(win.Min.X, in.Max.Y, win.Max.X, win.Max.Y), bg)
	fillRect(ops, image.Rect(win.Min.X, in.Min.Y, in.Min.X, in.Max.Y), bg)
	fillRect(ops, image.Rect(in.Max.X, in.Min.Y, win.Max.X, in.Max.Y), bg)
	for _, gap := range m.layout.gaps {
		fillRect(ops, gap, bg)
	}
	for _, p := range m.layout.panes {
		cells := m.cellRect(p.band.X, p.band.Y, p.band.W, p.band.H).Intersect(p.clip)
		if cells.Empty() {
			fillRect(ops, p.clip, bg)
			continue
		}
		fillRect(ops, image.Rect(cells.Max.X, p.clip.Min.Y, p.clip.Max.X, p.clip.Max.Y), bg) // right
		fillRect(ops, image.Rect(p.clip.Min.X, cells.Max.Y, cells.Max.X, p.clip.Max.Y), bg)  // bottom
	}
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
// In a Panes window the row is one segment per band it crosses, each under its pane's clip;
// cells in a hole are not drawn.
func (r *renderer) recordRow(g *grid, y int) op.CallOp {
	countRow()
	ops := new(op.Ops)
	rec := op.Record(ops)
	if r.m.layout == nil {
		r.drawSpan(ops, g, y, 0, g.w, bgOffTheme)
		return rec.Stop()
	}
	for _, p := range r.m.layout.panes {
		b := p.band
		if y < b.Y || y >= b.Y+b.H || b.W <= 0 {
			continue
		}
		x0, x1 := max(b.X, 0), min(b.X+b.W, g.w)
		if x0 >= x1 {
			continue
		}
		cl := clip.Rect(p.clip).Push(ops)
		r.drawSpan(ops, g, y, x0, x1, bgOffTheme)
		cl.Pop()
	}
	return rec.Stop()
}

// bgMode is which backgrounds drawSpan fills.
type bgMode uint8

const (
	bgOffTheme bgMode = iota // the runs not in the theme's own background (the frame's fill shows there)
	bgAll                    // every run: over a native view, which the cells must cover
	bgNone                   // none: glyphs only
)

// drawSpan translates a horizontal range of cells [x0, x1) in row y into batched Gio operations.
//
// Optimization & Batching Mechanics:
//  1. Background Coalescing: Cells with identical background colors are grouped into contiguous
//     horizontal runs and filled via [fillRect].
//  2. Line Segment Merging: Box-drawing characters (such as borders and divider lines) are converted
//     into directional arms and merged across adjoining cells via [mergeRects] to eliminate border seams
//     and minimize rectangle fill ops.
//  3. Glyph Batching: Text glyphs sharing foreground attributes are accumulated into a single
//     compound vector outline path per color via [clip.Outline] and rasterized in one draw call.
//     Glyphs requiring individual scaling or containing multi-color bitmaps (emoji) bypass
//     batching and are drawn individually.
func (r *renderer) drawSpan(ops *op.Ops, g *grid, y, x0, x1 int, bgs bgMode) {
	m := r.m
	for x := x0; x < x1 && bgs != bgNone; {
		_, bg := r.colors(g.at(x, y).Attrs)
		run := x + 1
		for run < x1 {
			_, next := r.colors(g.at(run, y).Attrs)
			if next != bg {
				break
			}
			run++
		}
		if bgs == bgAll || bg != r.theme.BG {
			fillRect(ops, m.cellRect(x, y, run-x, 1), bg)
		}
		x = run
	}
	batch := map[color.NRGBA][]text.Glyph{}
	var order []color.NRGBA
	var lines []colouredRect
	x := x0
	if g.at(x, y).Continuation() && x > 0 {
		x-- // the head of a wide cell whose second half is in the span
	}
	for ; x < x1; x++ {
		c := g.at(x, y)
		if c.Continuation() {
			continue
		}
		fg, _ := r.colors(c.Attrs)
		if ru, size := utf8.DecodeRuneInString(c.Content); size == len(c.Content) && size > 0 {
			if a, ok := lineArms[ru]; ok {
				lw := max(1, int(m.scale+0.5))
				for _, rc := range armRects(a, m.cellRect(x, y, 1, 1), lw) {
					lines = append(lines, colouredRect{rc, fg})
				}
				lines = r.decorationRects(lines, c, x, y, fg)
				continue
			}
		}
		if gl, ok := r.batchable(c, x, y); ok {
			if _, seen := batch[fg]; !seen {
				order = append(order, fg)
			}
			batch[fg] = append(batch[fg], gl...)
			lines = r.decorationRects(lines, c, x, y, fg)
			continue
		}
		r.drawCell(ops, c, x, y, fg)
	}
	for _, cr := range mergeRects(lines) {
		fillRect(ops, cr.r, cr.c)
	}
	// Shape places each glyph by its X from the first one's, on one baseline at the path's origin:
	// the path is moved there, to the row's baseline
	base := float32(m.cellRect(x0, y, 1, 1).Min.Y + m.baseline) // the segment's own band
	for _, fg := range order {
		gs := batch[fg]
		tr := op.Affine(f32.Affine2D{}.Offset(f32.Pt(float32(gs[0].X)/64, base))).Push(ops)
		countBatch()
		outline := clip.Outline{Path: r.paths.Shape(gs)}.Op().Push(ops)
		paint.ColorOp{Color: fg}.Add(ops)
		paint.PaintOp{}.Add(ops)
		outline.Pop()
		tr.Pop()
	}
}

// mayBeColour reports a cluster that may draw as a colour glyph (an emoji): one with a variation
// selector, or a code point in the emoji and symbol ranges. Gio's glyphs carry no colour flag, so
// this errs wide: a plain glyph taken for one is only drawn on its own, as every glyph once was.
func mayBeColour(s string) bool {
	for _, r := range s {
		switch {
		case r == 0xFE0F, r == 0x200D, r >= 0x1F000, r >= 0x2300 && r <= 0x2BFF, r == 0x00A9, r == 0x00AE, r == 0x2122:
			return true
		}
	}
	return false
}

// batchable is a cell's glyphs placed in the row, when the cell can be drawn in its row's batch:
// shaped text (not a box-drawing character) that fits its cell, with no colour glyphs. Each
// glyph is moved along the row by what glyphPlacement would have moved it.
func (r *renderer) batchable(c tui.Cell, x, y int) ([]text.Glyph, bool) {
	if c.Content == "" || c.Content == " " {
		return nil, false
	}
	if ru, size := utf8.DecodeRuneInString(c.Content); size == len(c.Content) && drawable(ru) {
		return nil, false
	}
	gs := r.shape(c.Content, c.Attrs.Mask&tui.AttrBold != 0, c.Attrs.Mask&tui.AttrItalic != 0, c.Face)
	rect := r.m.cellRect(x, y, max(int(c.Width), 1), 1)
	if gs.empty || gs.colour || gs.advance > rect.Dx() {
		return nil, false
	}
	dx := fixed.I(rect.Min.X + (rect.Dx()-gs.advance)/2)
	out := make([]text.Glyph, len(gs.glyphs))
	for i, g := range gs.glyphs {
		g.X += dx // the row's x; the row's baseline is the batch's transform
		out[i] = g
	}
	return out, true
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
		countCell()
		drawBox(ops, ru, m.cellRect(x, y, 1, 1), fg, m.scale)
	} else if c.Content != "" && c.Content != " " {
		gs := r.shape(c.Content, c.Attrs.Mask&tui.AttrBold != 0, c.Attrs.Mask&tui.AttrItalic != 0, c.Face)
		if !gs.empty {
			countCell()
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
	r.drawDecorations(ops, c, x, y, fg)
}

// colouredRect is a rectangle to fill, and its colour.
type colouredRect struct {
	r image.Rectangle
	c color.NRGBA
}

// mergeRects joins each rectangle to the one before it where they meet edge to edge on the same
// rows, in the same colour: a run of line pieces becomes one. Order is kept, so what overlaps
// still paints in the order given.
func mergeRects(in []colouredRect) []colouredRect {
	var out []colouredRect
	for _, cr := range in {
		if n := len(out); n > 0 {
			last := &out[n-1]
			if last.c == cr.c && last.r.Min.Y == cr.r.Min.Y && last.r.Max.Y == cr.r.Max.Y && last.r.Max.X >= cr.r.Min.X && last.r.Min.X <= cr.r.Min.X {
				last.r.Max.X = max(last.r.Max.X, cr.r.Max.X)
				continue
			}
		}
		out = append(out, cr)
	}
	return out
}

// decorationRects appends a cell's underline and strikethrough to rs, to be merged with the row's.
func (r *renderer) decorationRects(rs []colouredRect, c tui.Cell, x, y int, fg color.NRGBA) []colouredRect {
	m := r.m
	rect := m.cellRect(x, y, max(int(c.Width), 1), 1)
	lw := max(1, int(m.scale+0.5))
	if c.Attrs.Mask&tui.AttrUnderline != 0 {
		uy := rect.Min.Y + min(m.baseline+lw, m.cell.Y-lw)
		rs = append(rs, colouredRect{image.Rect(rect.Min.X, uy, rect.Max.X, uy+lw), fg})
	}
	if c.Attrs.Mask&tui.AttrStrikethrough != 0 {
		sy := rect.Min.Y + m.cell.Y/2
		rs = append(rs, colouredRect{image.Rect(rect.Min.X, sy, rect.Max.X, sy+lw), fg})
	}
	return rs
}

// drawDecorations draws a cell's underline and strikethrough in fg.
func (r *renderer) drawDecorations(ops *op.Ops, c tui.Cell, x, y int, fg color.NRGBA) {
	m := r.m
	rect := m.cellRect(x, y, max(int(c.Width), 1), 1)
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
func (r *renderer) shape(content string, bold, italic bool, face uint16) glyphShape {
	k := glyphKey{content, bold, italic, face}
	if gs, ok := r.glyphs[k]; ok {
		return gs
	}
	f := font.Font{Typeface: covered(r.shaper, r.typefaceOf(face), content)}
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
	gs := glyphShape{advance: advance.Round(), empty: len(glyphs) == 0, glyphs: glyphs}
	if !gs.empty {
		gs.path = r.shaper.Shape(glyphs)
		gs.bitmaps = r.shaper.Bitmaps(glyphs)
		gs.colour = mayBeColour(content)
	}
	r.glyphs[k] = gs
	return gs
}

// typefaceOf is the typeface cells of face are drawn in: the table's, or the window's for 0 and
// for an index the table does not have.
func (r *renderer) typefaceOf(face uint16) font.Typeface {
	if face != 0 && int(face) < len(r.faces) {
		return r.faces[face]
	}
	return r.typeface
}

// drawImage draws an image over its cells, its Clip (in image pixels) scaled to fill them.
func (r *renderer) drawImage(ops *op.Ops, img placedImage) {
	dst, within, ok := r.m.anchored(tui.Rect{X: img.X, Y: img.Y, W: img.Cols, H: img.Rows})
	if !ok {
		return
	}
	src := image.Rect(img.Clip.X, img.Clip.Y, img.Clip.X+img.Clip.W, img.Clip.Y+img.Clip.H)
	if src.Empty() {
		src = image.Rectangle{Max: img.op.Size()}
	}
	if dst.Empty() || src.Empty() {
		return
	}
	sx := float32(dst.Dx()) / float32(src.Dx())
	sy := float32(dst.Dy()) / float32(src.Dy())
	cl := clip.Rect(dst.Intersect(within)).Push(ops)
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
	// The app's caret colour first, then the theme's. With neither, the caret is the text's
	// colour under it, as many terminals draw it, so it shows on the app's own page whatever
	// that is (a fixed light caret vanishes on a light page).
	c := g.at(cur.x, cur.y)
	cc, _ := r.colors(c.Attrs)
	if r.theme.Cursor.A != 0 {
		cc = r.theme.Cursor
	}
	if cur.colored {
		cc = cur.color
	}
	p, in := m.pane(cur.x, cur.y)
	if !in {
		return // a hole: no pixels
	}
	if m.layout != nil {
		defer clip.Rect(p.clip).Push(ops).Pop()
	}
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
	bgs := bgAll
	if glyphsOnly {
		bgs = bgNone
	}
	if r.m.layout == nil {
		area := clip.Rect(r.m.cellRect(x0, y0, x1-x0, y1-y0)).Push(ops)
		defer area.Pop()
		for y := y0; y < y1; y++ {
			r.drawSpan(ops, g, y, x0, x1, bgs)
		}
		return
	}
	// Each band's part is drawn by that band, under its own clip.
	for _, f := range r.m.fragments(tui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}) {
		area := clip.Rect(f.dev.Intersect(f.clip)).Push(ops)
		for y := f.cells.Y; y < f.cells.Y+f.cells.H; y++ {
			r.drawSpan(ops, g, y, f.cells.X, f.cells.X+f.cells.W, bgs)
		}
		area.Pop()
	}
}

// backdrop is the most common background of the cells bordering v (outside it), ties to the
// first seen; the page background when none borders it (v fills the grid).
func (r *renderer) backdrop(g *grid, v CellRect) color.NRGBA {
	counts := map[color.NRGBA]int{}
	best, bestN := r.pageBG, 0
	look := func(x, y int) {
		if x < 0 || y < 0 || x >= g.w || y >= g.h {
			return
		}
		_, bg := r.colors(g.at(x, y).Attrs)
		counts[bg]++
		if n := counts[bg]; n > bestN {
			best, bestN = bg, n
		}
	}
	for x := v.X - 1; x <= v.X+v.W; x++ {
		look(x, v.Y-1)
		look(x, v.Y+v.H)
	}
	for y := v.Y; y < v.Y+v.H; y++ {
		look(v.X-1, y)
		look(v.X+v.W, y)
	}
	return best
}

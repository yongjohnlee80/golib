package gui

import (
	"image/color"
	"math"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// NativeStyle is golib's painter set: push buttons, check boxes, text fields, progress bars, tab
// bars, scrollbars on lists and trees, a dialog's shadow, and a real dim behind it. Each painter takes its colours
// from the cells the widget painted, so the app's theme and the widget's state (focused, armed,
// disabled) carry over, and it draws text at the cells tui gave it. It returns a new Style each
// call, for a consumer to change.
func NativeStyle() *Style {
	s := NewStyle()
	s.ForRole(tui.RolePushButton, paintButton)
	s.ForRole(tui.RoleCheckBox, paintCheckBox)
	s.ForRole(tui.RoleEditableText, paintTextField)
	s.ForRole(tui.RoleProgressBar, paintProgress)
	s.ForRole(tui.RolePageTabList, paintTabs)
	s.ForRole(tui.RoleList, paintScrollbar)
	s.ForRole(tui.RoleTree, paintScrollbar)
	s.ForRole(tui.RoleDialog, paintDialog)
	ForType(s, paintScrim)
	return s
}

// mix is a blended toward b by t in [0, 1].
func mix(a, b color.NRGBA, t float32) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5) }
	return color.NRGBA{R: l(a.R, b.R), G: l(a.G, b.G), B: l(a.B, b.B), A: l(a.A, b.A)}
}

// alpha is c at opacity a.
func alpha(c color.NRGBA, a float32) color.NRGBA {
	c.A = uint8(float32(c.A)*a + 0.5)
	return c
}

// cols and rows are the canvas's size in whole cells.
func cells(c Canvas) (cols, rows int) {
	cs := c.CellSize()
	if cs.W <= 0 || cs.H <= 0 {
		return 0, 0
	}
	sz := c.Size()
	return int(math.Round(float64(sz.W / cs.W))), int(math.Round(float64(sz.H / cs.H)))
}

// focusedNode reports whether the component holds the keyboard focus.
func focusedNode(c tui.Component) bool {
	if b, ok := c.(interface{ Context() *tui.Context }); ok {
		if ctx := b.Context(); ctx != nil {
			return ctx.Focused()
		}
	}
	return false
}

// hovered reports whether the pointer is over the component.
func hovered(c tui.Component) bool {
	if b, ok := c.(interface{ Context() *tui.Context }); ok {
		if ctx := b.Context(); ctx != nil {
			return ctx.Hovered()
		}
	}
	return false
}

// labelFont is the UI font at about the cell text's size.
func labelFont(c Canvas) Font { return Font{Size: c.CellSize().H * 0.78} }

// paintButton draws a rounded button in the button's own colours, lighter under the pointer and
// darker while pressed, with its label centred in the UI font. A button's label is not
// selectable or pointed into, so it need not keep the cells' columns.
func paintButton(comp tui.Component) (View, tui.NativeScope, bool) {
	b, ok := comp.(*widget.Button)
	if !ok {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) {
		fg, bg := c.CellColors(0, 0)
		r := Rect{W: c.Size().W, H: c.Size().H}
		radius := min(r.H/2, 6)
		fill := bg
		switch {
		case b.State() == widget.WidgetStateArmed:
			fill = mix(bg, fg, 0.22)
		case b.Hovered():
			fill = mix(bg, fg, 0.1)
		}
		c.FillRect(r, Solid(c.Backdrop())) // the corners show what is around the button
		c.FillRRect(r, radius, Solid(fill))
		border := alpha(fg, 0.35)
		width := float32(1)
		if b.State() == widget.WidgetStateFocused {
			border, width = alpha(fg, 0.9), 1.5
		}
		c.StrokeRRect(r.Inset(width/2), radius, width, Solid(border))
		i, ok := b.MnemonicIndex()
		if !ok {
			i = -1
		}
		drawLabel(c, b.Label(), i, labelFont(c), fg, readableOn(keyColor(c, b.Label(), i, fg), fill, fg))
	}), tui.ScopeSubtree, true
}

// keyColor is the colour of the mnemonic's cell, as the button's cells paint it: a theme's accent
// when it sets one (ButtonStyle.Hotkey), else fg. The cell is the key's cluster wearing a colour
// other than the label's.
func keyColor(c Canvas, label string, key int, fg color.NRGBA) color.NRGBA {
	if key < 0 {
		return fg
	}
	var cluster string
	i := 0
	for cl := range tui.Graphemes(label) {
		if i == key {
			cluster = cl
			break
		}
		i++
	}
	cols, _ := cells(c)
	for x := range cols {
		if c.CellText(x, 0) != cluster {
			continue
		}
		if kfg, _ := c.CellColors(x, 0); kfg != fg {
			return kfg
		}
	}
	return fg
}

// readableOn is key, the mnemonic's colour, where it reads on fill, else the label's fg: a theme's
// accent picked for the button's surface (a red) vanished on a focused button's fill (a brown).
// The underline still marks the key, as a lit menu row's does.
func readableOn(key, fill, fg color.NRGBA) color.NRGBA {
	if r, ok := style.ContrastRatio(style.RGB(key.R, key.G, key.B), style.RGB(fill.R, fill.G, fill.B)); ok && r < 3 {
		return fg
	}
	return key
}

// drawLabel draws label centred in c, with cluster key (the button's mnemonic; -1 for none)
// underlined in keyFg, at the label's own weight: the line and the colour mark it, and a bold
// letter in the middle of a word reads as a blot.
func drawLabel(c Canvas, label string, key int, f Font, fg, keyFg color.NRGBA) {
	var parts [3]strings.Builder // before the key, the key, after it
	n := 0
	for cl := range tui.Graphemes(label) {
		switch {
		case key < 0 || n < key:
			parts[0].WriteString(cl)
		case n == key:
			parts[1].WriteString(cl)
		default:
			parts[2].WriteString(cl)
		}
		n++
	}
	runs := [3]*TextLayout{
		c.Text().Layout(parts[0].String(), f, 0),
		c.Text().Layout(parts[1].String(), f, 0),
		c.Text().Layout(parts[2].String(), f, 0),
	}
	var width, ascent, descent float32
	for _, r := range runs {
		width += r.Width
		ascent, descent = max(ascent, r.Ascent), max(descent, r.Descent)
	}
	x := (c.Size().W - width) / 2
	y := (c.Size().H - (ascent + descent)) / 2
	for i, r := range runs {
		col := fg
		if i == 1 {
			col = keyFg
		}
		if r.Width > 0 {
			c.DrawText(r, Pt(x, y+ascent-r.Ascent), Solid(col))
		}
		if i == 1 && r.Width > 0 {
			c.FillRect(Rect{X: x, Y: y + ascent + max(descent*0.35, 1), W: r.Width, H: max(f.Size/14, 1)}, Solid(col))
		}
		x += r.Width
	}
}

// paintCheckBox draws the box as a rounded square over the "[x]" or "[ ]" cells, checked with a
// tick, and keeps every other cell (the text, and any space the label is centred in) as tui
// drew it.
func paintCheckBox(comp tui.Component) (View, tui.NativeScope, bool) {
	cb, ok := comp.(interface{ Checked() bool })
	if !ok {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) {
		cols, rows := cells(c)
		at := boxCells(c, cols)
		if at < 0 {
			c.PaintCells(CellRect{W: cols, H: rows}, false) // no box in the cells: show them as they are
			return
		}
		c.PaintCells(CellRect{W: at, H: rows}, false)
		c.PaintCells(CellRect{X: at + 3, W: cols - at - 3, H: rows}, false)
		cs := c.CellSize()
		fg, bg := c.CellColors(at, 0)
		x0 := float32(at) * cs.W
		c.FillRect(Rect{X: x0, W: 3 * cs.W, H: cs.H}, Solid(bg)) // the text's own background, as the cells beside it
		side := min(3*cs.W, cs.H) - 4
		box := Rect{X: x0 + (3*cs.W-side)/2, Y: (cs.H - side) / 2, W: side, H: side}
		if hovered(comp) {
			c.FillRRect(box.Inset(-2), 5, Solid(alpha(fg, 0.12)))
		}
		if cb.Checked() {
			c.FillRRect(box, 3, Solid(fg))
			tick := new(Path).
				MoveTo(Pt(box.X+side*0.22, box.Y+side*0.52)).
				LineTo(Pt(box.X+side*0.42, box.Y+side*0.72)).
				LineTo(Pt(box.X+side*0.78, box.Y+side*0.3))
			c.StrokePath(tick, max(side*0.12, 1.5), Solid(bg))
		} else {
			c.StrokeRRect(box.Inset(0.75), 3, 1.5, Solid(alpha(fg, 0.7)))
		}
	}), tui.ScopeSubtree, true
}

// boxCells is the first column of the "[x]" or "[ ]" on the first row, or -1.
func boxCells(c Canvas, cols int) int {
	for x := 0; x+2 < cols; x++ {
		if c.CellText(x, 0) == "[" && c.CellText(x+2, 0) == "]" {
			return x
		}
	}
	return -1
}

// paintTextField draws a square-edged field over its whole row, its border brighter while focused,
// beneath the text. The text, the placeholder and the selection are tui's own cells drawn over
// it: the glyphs over the native fill, and the selected cells whole, so every cluster stays at
// the column tui hit-tests and no line crosses a glyph.
func paintTextField(comp tui.Component) (View, tui.NativeScope, bool) {
	ti, ok := comp.(*widget.TextInput)
	if !ok {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) {
		cols, rows := cells(c)
		fg, bg := c.CellColors(0, 0)
		r := Rect{W: c.Size().W, H: c.Size().H}
		border, width := alpha(fg, 0.3), float32(1)
		if focusedNode(ti) {
			border, width = alpha(fg, 0.7), 1.5
		}
		c.FillRect(r, Solid(bg))
		c.StrokeRRect(r.Inset(width/2), 0, width, Solid(border))
		c.PaintCells(CellRect{W: cols, H: rows}, true)
		if lo, hi, sel := ti.Selection(); sel && focusedNode(ti) {
			x0, x1 := selectionCells(ti, lo, hi)
			x0, x1 = max(x0, ti.Padding()), min(x1, cols-ti.Padding()) // the frame's end cells stay the frame's
			if x1 > x0 {
				c.PaintCells(CellRect{X: x0, W: x1 - x0, H: 1}, false)
			}
		}
	}), tui.ScopeSubtree, true
}

// selectionCells is the cell span of clusters [lo, hi) on screen: the columns TextInput.Render
// writes them at, after its padding and its scroll.
func selectionCells(ti *widget.TextInput, lo, hi int) (x0, x1 int) {
	ctx := ti.Context()
	if ctx == nil {
		return 0, 0
	}
	x := ti.Padding() - ti.Scroll()
	i := 0
	x0, x1 = -1, -1
	for cl := range tui.Graphemes(ti.Value()) {
		if ti.Masked() {
			cl = string(ti.Mask())
		}
		if i == lo {
			x0 = x
		}
		x += ctx.StringWidth(cl)
		i++
		if i == hi {
			x1 = x
		}
	}
	return x0, x1
}

// paintProgress draws a determinate bar as a rounded track and fill in the bar's colours; an
// indeterminate one keeps its animated cells.
func paintProgress(comp tui.Component) (View, tui.NativeScope, bool) {
	p, ok := comp.(*widget.ProgressBar)
	if !ok {
		return nil, 0, false
	}
	if _, det := p.Progress(); !det {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) {
		f, _ := p.Progress()
		cols, _ := cells(c)
		filled, _ := c.CellColors(0, 0)
		emptyFG, emptyBG := c.CellColors(max(cols-1, 0), 0)
		sz := c.Size()
		c.FillRect(Rect{W: sz.W, H: sz.H}, Solid(c.Backdrop()))
		h := max(sz.H*0.4, 3)
		track := Rect{Y: (sz.H - h) / 2, W: sz.W, H: h}
		c.FillRRect(track, h/2, Solid(mix(emptyBG, emptyFG, 0.25)))
		if f > 0 {
			fill := track
			fill.W = max(float32(f)*track.W, h)
			c.FillRRect(fill, h/2, Solid(filled))
		}
	}), tui.ScopeSubtree, true
}

// paintTabs draws the bar with the active tab raised, its labels kept in their cells. The
// content below the bar is the active tab's child and draws over this view, so the scope is
// the chrome.
func paintTabs(comp tui.Component) (View, tui.NativeScope, bool) {
	t, ok := comp.(*widget.Tabs)
	if !ok {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) {
		cols, _ := cells(c)
		cs := c.CellSize()
		spans := t.TabSpans()
		if cols <= 0 || len(spans) == 0 {
			return
		}
		_, barBG := c.CellColors(cols-1, 0)
		c.FillRect(Rect{W: c.Size().W, H: cs.H}, Solid(barBG))
		for i, s := range spans {
			if s.X >= cols {
				break
			}
			fg, bg := c.CellColors(s.X, 0)
			r := Rect{X: float32(s.X)*cs.W - 2, Y: 1, W: float32(s.W)*cs.W + 4, H: cs.H - 1}
			c.FillRRect(r, 4, Solid(bg))
			if i == t.Active() {
				c.FillRect(Rect{X: r.X + 2, Y: cs.H - 2, W: r.W - 4, H: 2}, Solid(fg))
			}
		}
		c.PaintCells(CellRect{W: cols, H: 1}, true)
	}), tui.ScopeChrome, true
}

// paintScrollbar draws a thin rounded thumb along the right edge of a list or tree whose
// content is taller than its viewport, over its rows.
func paintScrollbar(comp tui.Component) (View, tui.NativeScope, bool) {
	s, ok := comp.(tui.Scroller)
	if !ok {
		return nil, 0, false
	}
	if _, view, content := s.ScrollState(); content <= view || view <= 0 {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) {
		off, view, content := s.ScrollState()
		sz := c.Size()
		fg, _ := c.CellColors(0, 0)
		w := float32(4)
		h := max(sz.H*float32(view)/float32(content), 12)
		y := (sz.H - h) * float32(off) / float32(max(content-view, 1))
		c.FillRRect(Rect{X: sz.W - w - 2, Y: y, W: w, H: h}, w/2, Solid(alpha(fg, 0.4)))
	}), tui.ScopeChrome, true
}

// paintDialog draws a soft shadow around the dialog's card. The card and the scrim are tui's
// cells, over and under it.
func paintDialog(comp tui.Component) (View, tui.NativeScope, bool) {
	m, ok := comp.(*widget.Modal)
	if !ok {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) {
		cr := m.CardRect()
		if cr.W <= 0 || cr.H <= 0 {
			return
		}
		cs := c.CellSize()
		card := Rect{X: float32(cr.X) * cs.W, Y: float32(cr.Y) * cs.H, W: float32(cr.W) * cs.W, H: float32(cr.H) * cs.H}
		shadow := color.NRGBA{A: 0xff}
		for i := float32(1); i <= 8; i++ {
			c.FillRRect(Rect{X: card.X - i, Y: card.Y - i + 3, W: card.W + 2*i, H: card.H + 2*i}, 6+i, Solid(alpha(shadow, 0.05)))
		}
	}), tui.ScopeChrome, true
}

// scrimDim is how dark the scrim makes what is beneath a dialog.
const scrimDim = 0.45

// paintScrim dims what is beneath a dialog, which stays visible through it: drawn natively, the
// scrim writes no cells of its own.
func paintScrim(*widget.Scrim) (View, tui.NativeScope, bool) {
	return ViewFunc(func(c Canvas) {
		c.FillRect(Rect{W: c.Size().W, H: c.Size().H}, Solid(alpha(color.NRGBA{A: 0xff}, scrimDim)))
	}), tui.ScopeChrome, true
}

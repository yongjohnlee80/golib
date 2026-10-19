package gui

import (
	"image/color"
	"math"
	"slices"
	"strings"
	"sync"
	"weak"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Native Styling and Painter Pipeline:
//
//	+------------------+     Cell Grid      +--------------------+
//	| tui.Component    | -----------------> | Canvas Interrogation|
//	| (Button, Dialog, |                    | (CellColors, Size, |
//	|  TextInput, etc.)|                    |  CellSize, Bounds) |
//	+------------------+                    +--------------------+
//	         |                                        |
//	         | Registered Painter                     |
//	         v                                        v
//	+------------------+    Vector Shapes   +--------------------+
//	| Style Registry   | -----------------> | Smooth GPU Prims   |
//	| (Role / Type)    |                    | (RRect, Shadow,    |
//	|                  |                    |  Alpha Mix, Scrim) |
//	+------------------+                    +--------------------+
//
// NativeStyle returns golib's standard vector painter configuration.
// It maps higher-level widget roles (push buttons, check boxes, text fields,
// progress bars, tab bars, scrollbars, a dialog's shadow and a floating panel's,
// a real dim behind a dialog, HTML views and data tables) to hardware-accelerated vector rendering routines.
//
// Each painter inspects the cell colors, text content, and theme attributes chosen by
// the underlying terminal widget, preserving active theme styling, focused/armed states,
// and mnemonic highlights while elevating them to anti-aliased vector graphics.
// It constructs and returns a newly allocated [Style] instance on each invocation,
// permitting caller customization.
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
	s.ForRole(tui.RoleWindow, paintWindow)
	ForType(s, paintScrim)
	ForType(s, paintHTMLView)
	ForType(s, paintDataTable)
	ForType(s, paintScrollView)
	ForType(s, paintTerminal)
	return s
}

// paintHTMLView draws an HTMLView whose bound layout is a native view (gui/widget.BindHTML binds
// one): that layout, over the widget's whole subtree. An HTMLView still laid out in cells is left
// to its cells.
func paintHTMLView(v *widget.HTMLView) (View, tui.NativeScope, bool) {
	view, ok := v.BoundLayout().(View)
	if !ok {
		return nil, 0, false
	}
	return view, tui.ScopeSubtree, true
}

// paintDataTable draws a DataTable whose bound layout is a native view (gui/widget.BindTable binds
// one): that layout, over the widget's whole subtree. A DataTable still laid out in cells is left
// to its cells.
func paintDataTable(t *widget.DataTable) (View, tui.NativeScope, bool) {
	view, ok := t.BoundLayout().(View)
	if !ok {
		return nil, 0, false
	}
	return view, tui.ScopeSubtree, true
}

// paintTerminal draws a Terminal whose view is a native view (gui/widget.TerminalViews makes them):
// that view, over the terminal. A Terminal with no view, or one that is not a View, is left to its
// cells.
func paintTerminal(t *widget.Terminal) (View, tui.NativeScope, bool) {
	view, ok := t.TerminalView().(View)
	if !ok {
		return nil, 0, false
	}
	return view, tui.ScopeSubtree, true
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

// openColors is the colours of the first of the cells xs on row that nothing covers, and whether
// one is open. A painter reads its widget's colours from the cells, which are the composed screen:
// a raised panel over one lends it the panel's colours, so a covered cell is never read. With none
// open the painter skips what it would have filled, as the host paints the covering cells over it.
func openColors(c Canvas, row int, xs ...int) (fg, bg color.NRGBA, ok bool) {
	for _, x := range xs {
		if !CellCovered(c, x, row) {
			fg, bg = c.CellColors(x, row)
			return fg, bg, true
		}
	}
	return color.NRGBA{}, color.NRGBA{}, false
}

// span is the columns [from, to) ascending, or descending when from > to: to excluded either way.
func span(from, to int) []int {
	var xs []int
	for x := from; x != to; {
		xs = append(xs, x)
		if from < to {
			x++
		} else {
			x--
		}
	}
	return xs
}

// endsFirst is the columns [0, cols) with the two end cells first: a widget's frame or padding,
// in its own colours where the cells between may wear others (a mnemonic, a selection).
func endsFirst(cols int) []int {
	if cols <= 0 {
		return nil
	}
	if cols == 1 {
		return []int{0}
	}
	return append([]int{0, cols - 1}, span(1, cols-1)...)
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
		i, ok := b.MnemonicIndex()
		if !ok {
			i = -1
		}
		fg, bg, open := buttonColors(c, b.Label(), i)
		if !open {
			return // wholly covered: the host paints the covering cells
		}
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
		drawLabel(c, b.Label(), i, labelFont(c), fg, readableOn(keyColor(c, b.Label(), i, fg), fill, fg))
	}), tui.ScopeSubtree, true
}

// buttonColors are the button's own colours: its first open cell, the ends first, never the
// mnemonic's (a theme's accent there would become the whole label's).
func buttonColors(c Canvas, label string, key int) (fg, bg color.NRGBA, ok bool) {
	cluster := keyCluster(label, key)
	cols, _ := cells(c)
	var xs []int
	for _, x := range endsFirst(max(cols, 1)) {
		if cluster == "" || c.CellText(x, 0) != cluster {
			xs = append(xs, x)
		}
	}
	return openColors(c, 0, xs...)
}

// keyCluster is the label's cluster at index key, or "" for none.
func keyCluster(label string, key int) string {
	if key < 0 {
		return ""
	}
	i := 0
	for cl := range tui.Graphemes(label) {
		if i == key {
			return cl
		}
		i++
	}
	return ""
}

// keyColor is the colour of the mnemonic's cell, as the button's cells paint it: a theme's accent
// when it sets one (ButtonStyle.Hotkey), else fg. The cell is the key's cluster wearing a colour
// other than the label's.
func keyColor(c Canvas, label string, key int, fg color.NRGBA) color.NRGBA {
	cluster := keyCluster(label, key)
	if cluster == "" {
		return fg
	}
	cols, _ := cells(c)
	for x := range cols {
		if CellCovered(c, x, 0) || c.CellText(x, 0) != cluster {
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

// boxCells is the first column of the "[x]" or "[ ]" on the first row, or -1. The box is the first
// one: a check box draws it before its text, which may hold brackets of its own ("[x] On"). A later
// layer over a cell before it is found may hide it, and a box further on would be the text's, so
// there is none to draw: the cells are shown as they are, and the host paints the layer over them.
func boxCells(c Canvas, cols int) int {
	for x := 0; x+2 < cols; x++ {
		if CellCovered(c, x, 0) || CellCovered(c, x+1, 0) || CellCovered(c, x+2, 0) {
			return -1
		}
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
		fg, bg, open := fieldColors(c, ti, cols)
		if !open {
			return // wholly covered: the host paints the covering cells
		}
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

// fieldColors are the field's own colours: its first open cell, the frame's end cells first, and
// never a selected cell, which wears the selection's.
func fieldColors(c Canvas, ti *widget.TextInput, cols int) (fg, bg color.NRGBA, ok bool) {
	x0, x1 := -1, -1
	if lo, hi, sel := ti.Selection(); sel && focusedNode(ti) {
		x0, x1 = selectionCells(ti, lo, hi)
	}
	var xs []int
	for _, x := range endsFirst(max(cols, 1)) {
		if x < x0 || x >= x1 {
			xs = append(xs, x)
		}
	}
	return openColors(c, 0, xs...)
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
		n := filledCells(f, cols)
		filled, _, fillOpen := openColors(c, 0, span(0, max(n, 1))...) // none filled yet: the first cell, as before
		last := max(cols, 1) - 1
		emptyFG, emptyBG, emptyOpen := openColors(c, 0, span(last, min(n, last)-1)...)
		sz := c.Size()
		c.FillRect(Rect{W: sz.W, H: sz.H}, Solid(c.Backdrop()))
		h := max(sz.H*0.4, 3)
		track := Rect{Y: (sz.H - h) / 2, W: sz.W, H: h}
		if emptyOpen {
			c.FillRRect(track, h/2, Solid(mix(emptyBG, emptyFG, 0.25)))
		}
		if f > 0 && fillOpen {
			fill := track
			fill.W = max(float32(f)*track.W, h)
			c.FillRRect(fill, h/2, Solid(filled))
		}
	}), tui.ScopeSubtree, true
}

// filledCells is how many cells of a determinate bar of cols cells ProgressBar.Render fills at
// fraction f: the full ones and the partial one.
func filledCells(f float64, cols int) int {
	exact := f * float64(cols)
	full := int(exact)
	if full < cols && int((exact-float64(full))*8) > 0 {
		full++
	}
	return min(full, cols)
}

// panelBarStrips are the tab strips a panel's title bar holds (OnPanelBar), each with the bar's
// colour. Weak, so a strip dropped with its editor leaves nothing behind.
var panelBarStrips struct {
	sync.Mutex
	m map[weak.Pointer[widget.TabStrip]]func() (color.NRGBA, bool)
}

// OnPanelBar marks t as drawn on a panel's title bar (gui/widget's Editor.Tabs), whose colour bar
// answers (false before the panel's first paint). t's painter fills its bar in that colour, the
// panel's own, not its cells': the strip's cells cover the panel's bar, and in their colour the title
// bar was two shades, the strip's band and the panel's tint beyond it (Johno: one or the other).
func OnPanelBar(t *widget.TabStrip, bar func() (color.NRGBA, bool)) {
	panelBarStrips.Lock()
	defer panelBarStrips.Unlock()
	if panelBarStrips.m == nil {
		panelBarStrips.m = map[weak.Pointer[widget.TabStrip]]func() (color.NRGBA, bool){}
	}
	for k := range panelBarStrips.m {
		if k.Value() == nil {
			delete(panelBarStrips.m, k)
		}
	}
	panelBarStrips.m[weak.Make(t)] = bar
}

// panelBar is the colour of the panel's bar comp is on (OnPanelBar), when it is one and the panel
// has painted it.
func panelBar(comp tui.Component) (color.NRGBA, bool) {
	t, ok := comp.(*widget.TabStrip)
	if !ok {
		return color.NRGBA{}, false
	}
	panelBarStrips.Lock()
	bar := panelBarStrips.m[weak.Make(t)]
	panelBarStrips.Unlock()
	if bar == nil {
		return color.NRGBA{}, false
	}
	return bar()
}

// paintTabs draws the bar with the active tab raised, a divider between neighbours, its labels kept
// in their cells. The
// content below the bar is the active tab's child and draws over this view, so the scope is
// the chrome.
func paintTabs(comp tui.Component) (View, tui.NativeScope, bool) {
	var tabSpans func() []widget.TabSpan
	var active func() int
	switch t := comp.(type) {
	case *widget.Tabs:
		tabSpans, active = t.TabSpans, t.Active
	case interface {
		TabSpans() []widget.TabSpan
		ActiveTabIndex() int
	}:
		// Runtime terminal tabs keep IDs for operations; the painter needs
		// only the strip's spans and current presentation index. This seam
		// also builds against the released root module, before its next tag.
		tabSpans, active = t.TabSpans, t.ActiveTabIndex
	default:
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) {
		cols, _ := cells(c)
		cs := c.CellSize()
		spans := tabSpans()
		if cols <= 0 || len(spans) == 0 {
			return
		}
		if fill, ok := panelBar(comp); ok { // on a panel's title bar: the panel's own colour
			c.FillRect(Rect{W: c.Size().W, H: cs.H}, Solid(fill))
		} else if _, barBG, ok := openColors(c, 0, barCells(spans, cols)...); ok {
			c.FillRect(Rect{W: c.Size().W, H: cs.H}, Solid(barBG))
		}
		// The boxes and the gaps between them sit on whole device pixels, and every gap is the same
		// width: a cell is rarely a whole number of pixels wide (9.6 at some sizes), and boxes placed
		// by their cells drifted a pixel wider or narrower from one gap to the next. A box with a
		// neighbour one cell on ends one gap before that neighbour's box; the others end by their cells.
		px := max(c.Scale(), 1)
		snap := func(v float32) float32 { return float32(math.Round(float64(v*px))) / px }
		gapW := snap(cs.W - 4)
		boxes := make([]Rect, len(spans))
		for i, s := range spans {
			left, right := snap(float32(s.X)*cs.W-2), snap(float32(s.X+s.W)*cs.W+2)
			if i+1 < len(spans) && spans[i+1].X == s.X+s.W+1 {
				right = snap(float32(spans[i+1].X)*cs.W-2) - gapW
			}
			boxes[i] = Rect{X: left, Y: 1, W: right - left, H: cs.H - 1}
		}
		for i, s := range spans {
			if s.X >= cols {
				break
			}
			fg, bg, ok := openColors(c, 0, span(s.X, min(s.X+s.W, cols))...)
			if !ok {
				continue // the tab is wholly covered
			}
			r := boxes[i]
			c.FillRRect(r, 4, Solid(bg))
			if i == active() {
				c.FillRect(Rect{X: r.X + 2, Y: cs.H - 2, W: r.W - 4, H: 2}, Solid(fg))
			}
		}
		// a divider in the middle of each gap between two neighbours, so a tab's × reads as its own
		// and not the one before's: one device pixel wide at any scale, in the bar's ground toward
		// its text (darker on a light bar, lighter on a dark one), the same in every gap; none in a
		// cell a panel covers
		for i := 1; i < len(spans); i++ {
			gap := spans[i-1].X + spans[i-1].W
			if spans[i].X != gap+1 || gap >= cols {
				continue
			}
			fg, bg, ok := openColors(c, 0, gap)
			if !ok {
				continue
			}
			prev := boxes[i-1]
			x := prev.X + prev.W + float32(math.Floor(float64(gapW*px/2)))/px
			c.FillRect(Rect{X: x, Y: cs.H * 0.2, W: 1 / px, H: cs.H * 0.6}, Solid(mix(bg, fg, 0.55)))
		}
		c.PaintCells(CellRect{W: cols, H: 1}, true)
	}), tui.ScopeChrome, true
}

// barCells are the cells the bar's colour is read from: its last, then the others in no tab's
// span, from the right. The last is the bar's own unless the tabs overflow it.
func barCells(spans []widget.TabSpan, cols int) []int {
	if cols <= 0 {
		return nil
	}
	xs := []int{cols - 1}
	for x := cols - 2; x >= 0; x-- {
		if !slices.ContainsFunc(spans, func(s widget.TabSpan) bool { return x >= s.X && x < s.X+s.W }) {
			xs = append(xs, x)
		}
	}
	return xs
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
		cols, rows := cells(c)
		fg, _, ok := OpenCellColors(c, CellRect{W: max(cols, 1), H: max(rows, 1)})
		if !ok {
			return // wholly covered
		}
		w := float32(4)
		h := max(sz.H*float32(view)/float32(content), 12)
		y := (sz.H - h) * float32(off) / float32(max(content-view, 1))
		c.FillRRect(Rect{X: sz.W - w - 2, Y: y, W: w, H: h}, w/2, Solid(alpha(fg, 0.4)))
	}), tui.ScopeChrome, true
}

// paintScrollView draws a ScrollView's thumb as a list's: in the column it reserves for it, which
// its child, laid out a column narrower, never covers.
func paintScrollView(v *widget.ScrollView) (View, tui.NativeScope, bool) { return paintScrollbar(v) }

// paintDialog draws a soft shadow around the dialog's card. The card and the scrim are tui's
// cells, over and under it.
func paintDialog(comp tui.Component) (View, tui.NativeScope, bool) {
	m, ok := comp.(*widget.Modal)
	if !ok {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) { softShadow(c, m.CardRect(), dialogShadow) }), tui.ScopeChrome, true
}

// paintWindow draws a softer shadow around a floating panel (a Float placed free of the window's
// edges: centred, or moved or resized off its edge). Its content is tui's cells, drawn over the
// shadow, so the shadow shows only outside the border, as a dialog's does. A panel docked at an
// edge or a corner gets none: it is part of the window's frame, its border already parts it from
// the page, and a shadow on its inner side alone would make it look lifted off the edge it is
// attached to. The shadow is what tells a floating panel from a docked one.
func paintWindow(comp tui.Component) (View, tui.NativeScope, bool) {
	f, ok := comp.(*widget.Float)
	if !ok || !f.Floating() || fillsArea(f) {
		return nil, 0, false
	}
	return ViewFunc(func(c Canvas) { softShadow(c, f.ContentRect(), panelShadow) }), tui.ScopeChrome, true
}

// fillsArea reports whether f's content covers its whole area (a panel maximised to Center at
// 100%): no shadow is seen there, and declining keeps the full-window native view out of the frame.
// The painter is asked every frame, so a panel taken back from maximised gets its shadow again.
func fillsArea(f *widget.Float) bool {
	cr, a := f.ContentRect(), f.Area()
	return a.W > 0 && a.H > 0 && cr.X <= 0 && cr.Y <= 0 && cr.X+cr.W >= a.W && cr.Y+cr.H >= a.H
}

// shadowLook is a soft shadow: rings of a translucent black around a card, each a pixel wider than
// the one inside it, the whole dropped a few pixels.
type shadowLook struct {
	rings int
	alpha float32 // each ring's opacity; where all of them overlap, about 1-(1-alpha)^rings
	drop  float32
}

var (
	// dialogShadow: a dialog is the highest thing on the screen, over a dimmed page.
	dialogShadow = shadowLook{rings: 8, alpha: 0.05, drop: 3}
	// panelShadow: a floating panel sits lower than a dialog, over the page undimmed, where the
	// dialog's shadow would read heavy.
	panelShadow = shadowLook{rings: 6, alpha: 0.04, drop: 2}
)

// softShadow draws look's shadow around the cells of cr, in the canvas's cells.
func softShadow(c Canvas, cr tui.Rect, look shadowLook) {
	if cr.W <= 0 || cr.H <= 0 {
		return
	}
	cs := c.CellSize()
	card := Rect{X: float32(cr.X) * cs.W, Y: float32(cr.Y) * cs.H, W: float32(cr.W) * cs.W, H: float32(cr.H) * cs.H}
	shadow := color.NRGBA{A: 0xff}
	for i := float32(1); i <= float32(look.rings); i++ {
		c.FillRRect(Rect{X: card.X - i, Y: card.Y - i + look.drop, W: card.W + 2*i, H: card.H + 2*i}, 6+i, Solid(alpha(shadow, look.alpha)))
	}
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

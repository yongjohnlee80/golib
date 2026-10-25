package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// member is a GovernedWindow as a program builds one: a window (a WindowMod) in a Float in the
// overlay, at its own anchor; governed, it is anchored live over its tile, docked at its edge when
// the tile has no cells, and its core takes the tile's target.
type member struct {
	float     *widget.Float
	mod       *widget.WindowMod
	own, dock widget.Anchor
}

func (m *member) Govern(rect func() (tui.Rect, bool), target any) {
	m.float.SetAnchor(widget.AtLive(rect, m.dock))
	m.mod.Core().Retarget(target)
}

func (m *member) Release() {
	m.float.SetAnchor(m.own)
	m.mod.Core().Retarget(nil)
}

func (m *member) WindowBehavior() *widget.WindowCore { return m.mod.Core() }

// newMember is a window titled title at own, docked at dock while governed without cells.
func newMember(title string, own, dock widget.Anchor) *member {
	mod := widget.NewWindowMod(widget.NewBox(widget.NewText(""), widget.WithTitle(title)),
		widget.WithWindowMove(), widget.WithWindowResize(), widget.WithWindowMinimize(widget.NewMinimizedWindows()))
	return &member{float: widget.NewFloat(mod, widget.WithAnchor(own)), mod: mod, own: own, dock: dock}
}

// boxAt is where the box titled title is drawn: its top-left corner and its width; -1s when it is
// not on screen.
func boxAt(h *harness, title string) (x, y, w int) {
	for row, line := range strings.Split(h.tb.String(), "\n") {
		i := strings.Index(line, title)
		if i < 0 {
			continue
		}
		rs := []rune(line)
		col := len([]rune(line[:i]))
		left := col
		for left > 0 && rs[left] != '┌' {
			left--
		}
		if rs[left] != '┌' {
			continue
		}
		right := col
		for right < len(rs) && rs[right] != '┐' {
			right++
		}
		return left, row, right - left + 1
	}
	return -1, -1, -1
}

// tilesRig is a manager over the overlay with members attached and shown.
type tilesRig struct {
	h       *harness
	tiles   *widget.Tiles
	members map[string]*member
}

func startTiles(t *testing.T, w, ht int, tiles *widget.Tiles, base tui.Component, ms map[string]*member) *tilesRig {
	t.Helper()
	host := widget.NewOverlayHost(base)
	for _, m := range ms {
		host.Attach(m.float)
	}
	h := startApp(t, host, w, ht)
	t.Cleanup(h.stop)
	h.onLoop(func() {
		for _, m := range ms {
			m.float.Show()
		}
	})
	h.settle()
	return &tilesRig{h: h, tiles: tiles, members: ms}
}

func (r *tilesRig) governed(key string) bool {
	var on bool
	r.h.onLoop(func() { on = r.tiles.Tile(key).Governed() })
	return on
}

// A member that wants to be tiled is anchored over its tile, docked; one that stops wanting is
// released to its own placement, exactly where it was, and the page fills the manager again.
func TestAWantingMemberIsGovernedOverItsTileAndReleasedToItsOwnPlace(t *testing.T) {
	tiles := widget.NewTiles()
	tiles.Add("page", widget.NewBox(widget.NewText(""), widget.WithTitle("PG")), widget.TilePlace{Edge: tui.DockCenter, Min: 5})
	a := newMember("AA", widget.AtRect(tui.Rect{X: 20, Y: 3, W: 12, H: 4}), widget.Left)
	tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a})
	if x, y, w := boxAt(r.h, "AA"); x != 0 || y != 0 || w != 10 {
		t.Fatalf("governed: the member at (%d,%d) %d wide, want its tile's (0,0) 10 wide:\n%s", x, y, w, r.h.tb.String())
	}
	if x, _, _ := boxAt(r.h, "PG"); x != 10 {
		t.Fatalf("the page at column %d, want 10, beside the member's tile:\n%s", x, r.h.tb.String())
	}
	var floating bool
	r.h.onLoop(func() { floating = a.float.Floating() })
	if floating {
		t.Error("a governed member reads floating: its fallback must be docked")
	}
	r.h.onLoop(func() { tiles.SetWants("a", false) })
	r.h.settle()
	if x, y, w := boxAt(r.h, "AA"); x != 20 || y != 3 || w != 12 {
		t.Fatalf("released: the member at (%d,%d) %d wide, want its own (20,3) 12 wide:\n%s", x, y, w, r.h.tb.String())
	}
	if r.governed("a") {
		t.Error("released, yet governed")
	}
	if x, _, w := boxAt(r.h, "PG"); x != 0 || w != 40 {
		t.Fatalf("the page at %d, %d wide, want the whole manager:\n%s", x, w, r.h.tb.String())
	}
}

// When the tiled set no longer fits, the member tiled most recently is released, whichever was
// declared first, and it is tiled again once there is room.
func TestTheMostRecentlyTiledMemberIsReleasedWhenTheSetNoLongerFits(t *testing.T) {
	for _, first := range []string{"a", "b"} {
		t.Run(first+" tiled first", func(t *testing.T) {
			tiles := widget.NewTiles()
			tiles.Add("page", widget.NewText("page"), widget.TilePlace{Edge: tui.DockCenter, Min: 10})
			a := newMember("AA", widget.AtRect(tui.Rect{X: 2, Y: 6, W: 8, H: 3}), widget.Left)
			b := newMember("BB", widget.AtRect(tui.Rect{X: 12, Y: 6, W: 8, H: 3}), widget.Right)
			tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 10}, false)
			tiles.AddMember("b", b, widget.TilePlace{Edge: tui.DockRight, Size: 25, Min: 10}, false)
			second := map[string]string{"a": "b", "b": "a"}[first]
			tiles.SetWants(first, true)
			tiles.SetWants(second, true)
			r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a, "b": b})
			if !r.governed("a") || !r.governed("b") {
				t.Fatalf("40 wide fits both (10 + 10 + the page's 10)")
			}
			r.h.tb.InjectResize(25, 10)
			r.h.settle()
			if !r.governed(first) || r.governed(second) {
				t.Fatalf("25 wide: %s governed %v, %s governed %v; want the one tiled last (%s) released", first, r.governed(first), second, r.governed(second), second)
			}
			r.h.tb.InjectResize(40, 10)
			r.h.settle()
			if !r.governed("a") || !r.governed("b") {
				t.Fatal("widened to 40, both tile again")
			}
		})
	}
}

// With nothing tiled the manager hibernates: no centre is needed, nothing is governed, and it draws
// nothing; a member that does not want to be tiled floats where it is.
func TestAManagerWithNothingTiledHibernates(t *testing.T) {
	tiles := widget.NewTiles()
	a := newMember("AA", widget.AtRect(tui.Rect{X: 5, Y: 2, W: 10, H: 4}), widget.Left)
	tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Size: 30, Min: 5}, false)
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a})
	if r.governed("a") {
		t.Fatal("a member that does not want to be tiled is governed")
	}
	if x, y, _ := boxAt(r.h, "AA"); x != 5 || y != 2 {
		t.Fatalf("the member at (%d,%d), want its own (5,2):\n%s", x, y, r.h.tb.String())
	}
	for i, line := range strings.Split(r.h.tb.String(), "\n") {
		if i >= 2 && i < 6 {
			continue // the member's own rows
		}
		if strings.TrimSpace(line) != "" {
			t.Fatalf("a hibernating manager drew row %d: %q", i, line)
		}
	}
}

// With no centre tiled the strips fill the manager: the last tiled strip takes the remainder, and no
// cell is left over or shared.
func TestWithNoCentreTiledTheStripsFillTheManager(t *testing.T) {
	tiles := widget.NewTiles()
	a := newMember("AA", widget.AtRect(tui.Rect{X: 0, Y: 6, W: 8, H: 3}), widget.Left)
	b := newMember("BB", widget.AtRect(tui.Rect{X: 10, Y: 6, W: 8, H: 3}), widget.Right)
	tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
	tiles.AddMember("b", b, widget.TilePlace{Edge: tui.DockRight, Size: 25, Min: 5}, true)
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a, "b": b})
	ax, _, aw := boxAt(r.h, "AA")
	bx, _, bw := boxAt(r.h, "BB")
	if ax != 0 || ax+aw != bx || bx+bw != 40 {
		t.Fatalf("a at %d %d wide, b at %d %d wide: want them to fill 0..40 with nothing between:\n%s", ax, aw, bx, bw, r.h.tb.String())
	}
}

// A governed member is never moved on its own: an Alt-left drag into another edge it may take does
// not re-dock it. Alt-right on its shared border resizes its tile, the page following.
func TestAMemberRefusesAMoveAndResizesByItsSharedBorder(t *testing.T) {
	tiles := widget.NewTiles()
	tiles.Add("page", widget.NewBox(widget.NewText(""), widget.WithTitle("PG")), widget.TilePlace{Edge: tui.DockCenter, Min: 5})
	a := newMember("AA", widget.AtRect(tui.Rect{X: 20, Y: 3, W: 12, H: 4}), widget.Left)
	// it may sit at the right edge too, so a move it took would land somewhere: a re-dock there
	tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Edges: []tui.DockEdge{tui.DockLeft, tui.DockRight}, Size: 25, Min: 5}, true)
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a})
	drag := func(btn tui.MouseButton, x0, y0, x1, y1 int) {
		r.h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: btn, Mods: tui.ModAlt, X: x0, Y: y0})
		r.h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: btn, Mods: tui.ModAlt, X: x1, Y: y1})
		r.h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: btn, Mods: tui.ModAlt, X: x1, Y: y1})
		r.h.settle()
	}
	drag(tui.MouseLeft, 3, 4, 38, 4) // into the right edge's band
	if x, y, w := boxAt(r.h, "AA"); x != 0 || y != 0 || w != 10 {
		t.Fatalf("after an Alt-left drag the member is at (%d,%d) %d wide, want its tile's (0,0) 10 wide:\n%s", x, y, w, r.h.tb.String())
	}
	drag(tui.MouseRight, 9, 4, 15, 4)
	if _, _, w := boxAt(r.h, "AA"); w != 16 {
		t.Fatalf("after an Alt-right drag of its border to column 15 the member is %d wide, want 16:\n%s", w, r.h.tb.String())
	}
	if x, _, _ := boxAt(r.h, "PG"); x != 16 {
		t.Fatalf("the page at column %d, want 16, beside the wider tile:\n%s", x, r.h.tb.String())
	}
}

// A member minimized through its own window takes no cells and stays governed; restored, it is back
// over its tile.
func TestAHiddenMemberTakesNoCellsAndStaysGoverned(t *testing.T) {
	tiles := widget.NewTiles()
	tiles.Add("page", widget.NewBox(widget.NewText(""), widget.WithTitle("PG")), widget.TilePlace{Edge: tui.DockCenter, Min: 5})
	a := newMember("AA", widget.AtRect(tui.Rect{X: 20, Y: 3, W: 12, H: 4}), widget.Left)
	tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a})
	r.h.onLoop(func() { a.mod.Core().Minimize() })
	r.h.settle()
	var hidden bool
	r.h.onLoop(func() { hidden = tiles.Tile("a").Hidden() })
	if !hidden || !r.governed("a") {
		t.Fatalf("minimized: hidden %v, governed %v; want both", hidden, r.governed("a"))
	}
	if x, _, w := boxAt(r.h, "PG"); x != 0 || w != 40 {
		t.Fatalf("the page at %d, %d wide, want the whole manager while the member is hidden:\n%s", x, w, r.h.tb.String())
	}
	r.h.onLoop(func() { a.mod.Core().Restore() })
	r.h.settle()
	if x, y, w := boxAt(r.h, "AA"); x != 0 || y != 0 || w != 10 {
		t.Fatalf("restored: the member at (%d,%d) %d wide, want its tile's (0,0) 10 wide:\n%s", x, y, w, r.h.tb.String())
	}
}

// bar is a docked bar of rows rows, the full width.
type bar struct {
	widget.Base
	rows int
}

func (b *bar) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{W: c.MaxW, H: b.rows}) }
func (b *bar) Render(tui.Surface)                {}

// barred is a manager under a docked bar of n rows, its origin reported from where the dock placed
// it, as a decl Window wires it.
func barred(n int) (*tui.Dock, *bar, *widget.Tiles) {
	top := &bar{rows: n}
	var origin tui.Point
	tiles := widget.NewTiles(widget.WithTilesOrigin(func() tui.Point { return origin }))
	dock := tui.NewDock()
	dock.OnPlaced(func(c tui.Component, r tui.Rect) {
		if c == tiles {
			origin = tui.Point{X: r.X, Y: r.Y}
		}
	})
	dock.Pin(tui.DockTop, top)
	dock.Pin(tui.DockCenter, tiles)
	return dock, top, tiles
}

// A manager below a docked bar puts its member at the bar's height plus its tile's Y, and the origin
// follows the bar's height when it changes.
func TestAMemberUnderADockedBarIsPlacedBelowItAndFollowsIt(t *testing.T) {
	dock, top, tiles := barred(2)
	a := newMember("AA", widget.AtRect(tui.Rect{X: 20, Y: 6, W: 12, H: 3}), widget.Left)
	tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
	r := startTiles(t, 40, 12, tiles, dock, map[string]*member{"a": a})
	if x, y, _ := boxAt(r.h, "AA"); x != 0 || y != 2 {
		t.Fatalf("under a 2-row bar the member is at (%d,%d), want (0,2):\n%s", x, y, r.h.tb.String())
	}
	r.h.onLoop(func() { top.rows = 3; top.RequestLayout() })
	r.h.settle()
	if x, y, _ := boxAt(r.h, "AA"); x != 0 || y != 3 {
		t.Fatalf("the bar grown to 3 rows: the member is at (%d,%d), want (0,3):\n%s", x, y, r.h.tb.String())
	}
}

// Without WithTilesOrigin a manager is at the overlay's origin: a bare manager's member is at its
// tile's own rect.
func TestABareManagersMemberIsAtItsTile(t *testing.T) {
	tiles := widget.NewTiles()
	a := newMember("AA", widget.AtRect(tui.Rect{X: 20, Y: 6, W: 12, H: 3}), widget.Top)
	tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockTop, Size: 40, Min: 3}, true)
	tiles.Add("page", widget.NewText("page"), widget.TilePlace{Edge: tui.DockCenter, Min: 3})
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a})
	if x, y, w := boxAt(r.h, "AA"); x != 0 || y != 0 || w != 40 {
		t.Fatalf("a bare manager's top member is at (%d,%d) %d wide, want (0,0) 40 wide:\n%s", x, y, w, r.h.tb.String())
	}
}

// fixedTarget is a window target at a fixed rect that keeps what is asked of it: geometry and
// visibility, as a tile's target has.
type fixedTarget struct {
	r       tui.Rect
	asked   []tui.Rect
	visible bool
}

func (f *fixedTarget) Bounds() tui.Rect          { return f.r }
func (f *fixedTarget) RequestedBounds() tui.Rect { return f.r }
func (f *fixedTarget) Area() tui.Rect            { return tui.Rect{W: 40, H: 10} }
func (f *fixedTarget) SetBounds(r tui.Rect) bool { f.asked = append(f.asked, r); return true }
func (f *fixedTarget) Checkpoint() func()        { return func() {} }
func (f *fixedTarget) SetWindowVisible(v bool) bool {
	f.visible = v
	return true
}
func (f *fixedTarget) WindowVisible() bool { return f.visible }
func (f *fixedTarget) Modal() bool         { return false }

// floatWindow is a movable, minimizable window in a Float at r, shown over a blank base.
func floatWindow(t *testing.T, r tui.Rect) (*harness, *widget.Float, *widget.WindowMod) {
	t.Helper()
	mod := widget.NewWindowMod(widget.NewBox(widget.NewText(""), widget.WithTitle("WW")),
		widget.WithWindowMove(), widget.WithWindowMinimize(widget.NewMinimizedWindows()))
	f := widget.NewFloat(mod, widget.WithAnchor(widget.AtRect(r)))
	host := widget.NewOverlayHost(widget.NewText(""))
	host.Attach(f)
	h := startApp(t, host, 40, 10)
	t.Cleanup(h.stop)
	h.onLoop(f.Show)
	h.settle()
	return h, f, mod
}

// Retarget probes the new target, as Bind does: the core's geometry is the target's and a move goes
// to it, the Float staying where it is. The window's state is kept. Retarget(nil) gives back the
// Float's own placement, which a move moves again.
func TestRetargetProbesTheNewTargetAndNilGivesBackTheFloats(t *testing.T) {
	h, _, mod := floatWindow(t, tui.Rect{X: 5, Y: 2, W: 10, H: 4})
	target := &fixedTarget{r: tui.Rect{X: 1, Y: 1, W: 6, H: 3}, visible: true}
	step := func() {
		mod.Core().HandleAction(tui.ActionInvocation{Action: widget.WindowMoveByAction{DX: 1}, Origin: tui.OriginProgrammatic})
	}
	var bounds tui.Rect
	h.onLoop(func() { mod.Core().Retarget(target); bounds = mod.Core().Bounds(); step() })
	h.settle()
	if bounds != target.r {
		t.Fatalf("retargeted, the core's bounds are %v, want the target's %v", bounds, target.r)
	}
	if len(target.asked) != 1 || target.asked[0].X != 2 {
		t.Fatalf("a move step went to the target as %v, want one rect at X 2", target.asked)
	}
	if x, y, _ := boxAt(h, "WW"); x != 5 || y != 2 {
		t.Fatalf("the Float moved to (%d,%d) on a step its core's target took:\n%s", x, y, h.tb.String())
	}
	var minimized bool
	h.onLoop(func() {
		mod.Core().Minimize()
		mod.Core().Retarget(&fixedTarget{r: target.r, visible: false})
		minimized = mod.Core().State().Minimized
	})
	if !minimized {
		t.Fatal("a retarget lost the window's minimized state")
	}
	h.onLoop(func() { mod.Core().Restore(); mod.Core().Retarget(nil); step() })
	h.settle()
	if x, y, _ := boxAt(h, "WW"); x != 6 || y != 2 {
		t.Fatalf("Retarget(nil) and a step: the Float at (%d,%d), want (6,2), moved by its own target again:\n%s", x, y, h.tb.String())
	}
}

// A retarget during a drag cancels the drag first: its checkpoint puts the Float back where the drag
// began, and the drag's later motion moves nothing.
func TestRetargetCancelsADragInFlight(t *testing.T) {
	h, _, mod := floatWindow(t, tui.Rect{X: 5, Y: 2, W: 10, H: 4})
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 7, Y: 3})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 17, Y: 5})
	h.settle()
	if x, _, _ := boxAt(h, "WW"); x != 15 {
		t.Fatalf("the drag should have moved the window to column 15; it is at %d:\n%s", x, h.tb.String())
	}
	h.onLoop(func() { mod.Core().Retarget(&fixedTarget{r: tui.Rect{X: 1, Y: 1, W: 6, H: 3}, visible: true}) })
	h.settle()
	if x, y, _ := boxAt(h, "WW"); x != 5 || y != 2 {
		t.Fatalf("the retarget should have cancelled the drag back to (5,2); the window is at (%d,%d):\n%s", x, y, h.tb.String())
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 25, Y: 7})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 25, Y: 7})
	h.settle()
	if x, y, _ := boxAt(h, "WW"); x != 5 || y != 2 {
		t.Fatalf("the cancelled drag's later motion moved the window to (%d,%d):\n%s", x, y, h.tb.String())
	}
}

// A member's completed gesture commits through its tile, as a Tile child's does, whoever built its
// window's core (WindowChangeHandler): an Alt-right drag of its border keeps the size it reached after
// another layout, and the manager publishes it once. Its minimize and restore are published too.
func TestAMembersGesturesCommitThroughItsTile(t *testing.T) {
	var got []widget.TilesChangedEvent
	tiles := widget.NewTiles(widget.WithTilesChanged(func(e widget.TilesChangedEvent) { got = append(got, e) }))
	tiles.Add("page", widget.NewBox(widget.NewText(""), widget.WithTitle("PG")), widget.TilePlace{Edge: tui.DockCenter, Min: 5})
	a := newMember("AA", widget.AtRect(tui.Rect{X: 20, Y: 3, W: 12, H: 4}), widget.Left)
	tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a})
	r.h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 9, Y: 4})
	r.h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, Mods: tui.ModAlt, X: 15, Y: 4})
	r.h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, Mods: tui.ModAlt, X: 15, Y: 4})
	r.h.settle()
	var size float64
	var events []widget.TilesChangedEvent
	r.h.onLoop(func() { size = tiles.Tile("a").Place().Size; events = append(events, got...) })
	if size != 40 {
		t.Fatalf("after the drag the member's committed size is %v%%, want 40%% (16 of 40 cells)", size)
	}
	if len(events) != 1 || events[0].Key != "a" || events[0].Operation != widget.WindowResize || events[0].Size != 40 {
		t.Fatalf("published %+v, want one resize of a to 40%%", events)
	}
	r.h.tb.InjectResize(40, 9) // another layout: a committed size is kept, a staged one would be dropped
	r.h.settle()
	if _, _, w := boxAt(r.h, "AA"); w != 16 {
		t.Fatalf("after another layout the member is %d wide, want the 16 it reached:\n%s", w, r.h.tb.String())
	}
	r.h.onLoop(func() { got = nil; a.mod.Core().Minimize() })
	r.h.settle()
	r.h.onLoop(func() { a.mod.Core().Restore() })
	r.h.settle()
	r.h.onLoop(func() { events = append([]widget.TilesChangedEvent(nil), got...) })
	if len(events) != 2 || events[0].Operation != widget.WindowMinimize || !events[0].Hidden || events[1].Operation != widget.WindowRestore || events[1].Hidden {
		t.Fatalf("minimize then restore published %+v, want a hidden minimize then a shown restore", events)
	}
}

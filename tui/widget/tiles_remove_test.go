package widget_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// MEMBERS ADDED AND REMOVED WHILE THE MANAGER RUNS (Tiles.RemoveMember, Tiles.AddMemberBefore): a
// program whose set of windows changes (a decl reload adding or dropping one) adds and removes
// members of a mounted manager. A removed member's cells go back to the others, it is released to
// its own placement, and nothing is published; a gesture in progress, in any tile, is cancelled
// first, so neither its release nor its Escape acts on a layout that no longer holds the member.

// changes records a manager's TilesChangedEvents.
type changes struct {
	mu  sync.Mutex
	got []string
}

func (c *changes) record(e core.TilesChangedEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, e.Key+" "+e.Operation.String())
}

func (c *changes) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.got)
}

// maximizableMember is newMember whose window also maximizes.
func maximizableMember(title string, own, dock core.Anchor) *member {
	collector := core.NewMinimizedWindows()
	mod := core.NewWindowMod(core.NewBox(core.NewText(""), core.WithTitle(title)),
		core.WithWindowMove(), core.WithWindowResize(), core.WithWindowMinimize(collector), core.WithWindowMaximize())
	return &member{float: core.NewFloat(mod, core.WithAnchor(own)), mod: mod, own: own, dock: dock, dock2: collector}
}

// twoMembers is a 40×10 manager: the page at the centre, member a a quarter down the left, member
// b a quarter down the right, both tiled; b maximizes.
func twoMembers(t *testing.T) (*tilesRig, *changes) {
	t.Helper()
	ch := &changes{}
	tiles := core.NewTiles(core.WithTilesChanged(ch.record))
	tiles.Add("page", core.NewBox(core.NewText(""), core.WithTitle("PG")), core.TilePlace{Edge: tui.DockCenter, Min: 5})
	a := newMember("AA", core.AtRect(tui.Rect{X: 2, Y: 6, W: 10, H: 3}), core.Left)
	b := maximizableMember("BB", core.AtRect(tui.Rect{X: 24, Y: 6, W: 12, H: 3}), core.Right)
	tiles.AddMember("a", a, core.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
	tiles.AddMember("b", b, core.TilePlace{Edge: tui.DockRight, Size: 25, Min: 5}, true)
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a, "b": b})
	if x, _, w := boxAt(r.h, "PG"); x != 10 || w != 20 {
		t.Fatalf("the page at %d, %d wide, want 10, 20 between the two members:\n%s", x, w, r.h.grid())
	}
	return r, ch
}

// remove removes key on the loop, and lets the frame settle.
func (r *tilesRig) remove(t *testing.T, key string) bool {
	t.Helper()
	var ok bool
	r.h.onLoop(func() { ok = r.tiles.RemoveMember(key) })
	r.h.settle()
	return ok
}

// A removed member's cells go back to the page, it is released to its own placement, it is no
// longer the manager's, and nothing is published. A key no member has, or a Tile child's, is
// refused. The mutant, named here: RemoveMember leaving the tile in the manager's order (the page
// stays 20 wide).
func TestARemovedMemberGivesItsCellsBackAndIsReleased(t *testing.T) {
	r, ch := twoMembers(t)
	if !r.remove(t, "b") {
		t.Fatal("RemoveMember refused member b")
	}
	if x, _, w := boxAt(r.h, "PG"); x != 10 || w != 30 {
		t.Fatalf("after b's removal the page at %d, %d wide, want 10, 30:\n%s", x, w, r.h.grid())
	}
	if x, y, w := boxAt(r.h, "BB"); x != 24 || y != 6 || w != 12 {
		t.Fatalf("the removed member at (%d,%d) %d wide, want released to its own (24,6) 12 wide:\n%s", x, y, w, r.h.grid())
	}
	var gone bool
	r.h.onLoop(func() { gone = r.tiles.Tile("b") == nil })
	if !gone {
		t.Error("the removed member is still the manager's tile")
	}
	var again, tile bool
	r.h.onLoop(func() { again, tile = r.tiles.RemoveMember("b"), r.tiles.RemoveMember("page") })
	if again || tile {
		t.Errorf("RemoveMember took a key no member has (%v) or a Tile child (%v)", again, tile)
	}
	if got := ch.all(); len(got) != 0 {
		t.Errorf("the removal published %q: a program's removal is no operation of the user's", got)
	}
}

// A gesture in progress when a member is removed is cancelled first, whichever tile it began in and
// however far it got: staged in the removed member, staged in the other (its Escape would restore a
// checkpoint holding the removed member), or only pressed. Its release then changes nothing and
// publishes nothing, Escape leaves the member removed, and the pointer is free: an Alt-right drag
// on the other member's border afterwards resizes it. The mutant, named here: RemoveMember not
// cancelling the gesture (the release commits a resize of the removed strip, or Escape brings the
// member back, or the stale drag holds the pointer).
func TestRemovingAMemberCancelsAGestureInProgress(t *testing.T) {
	press := func(x int) tui.Event {
		return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: x, Y: 4}
	}
	motion := func(x int) tui.Event {
		return tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, Mods: tui.ModAlt, X: x, Y: 4}
	}
	release := func(x int) tui.Event {
		return tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, Mods: tui.ModAlt, X: x, Y: 4}
	}
	for _, c := range []struct {
		name string
		mid  []tui.Event // the gesture before the removal
		end  []tui.Event // after it
	}{
		{"staged in the removed member", []tui.Event{press(30), motion(25)}, []tui.Event{release(25)}},
		{"staged in the other member", []tui.Event{press(9), motion(14)}, []tui.Event{release(14)}},
		{"staged in the other, then Escape", []tui.Event{press(9), motion(14)}, []tui.Event{tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}, release(14)}},
		{"only pressed in the removed member", []tui.Event{press(30)}, []tui.Event{motion(25), release(25)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, ch := twoMembers(t)
			r.h.inject(c.mid...)
			r.h.settle()
			if !r.remove(t, "b") {
				t.Fatal("RemoveMember refused member b")
			}
			r.h.inject(c.end...)
			r.h.settle()
			var back bool
			r.h.onLoop(func() { back = r.tiles.Tile("b") != nil })
			if back {
				t.Fatal("the removed member is the manager's again: a checkpoint from before the removal was restored")
			}
			if x, _, w := boxAt(r.h, "AA"); x != 0 || w != 10 {
				t.Fatalf("member a at %d, %d wide, want 0, 10: the cancelled gesture changed it:\n%s", x, w, r.h.grid())
			}
			if x, _, w := boxAt(r.h, "PG"); x != 10 || w != 30 {
				t.Fatalf("the page at %d, %d wide, want 10, 30:\n%s", x, w, r.h.grid())
			}
			if got := ch.all(); len(got) != 0 {
				t.Fatalf("the cancelled gesture published %q", got)
			}
			r.h.inject(press(9), motion(15), release(15))
			r.h.settle()
			if _, _, w := boxAt(r.h, "AA"); w != 16 {
				t.Fatalf("a drag of member a's border after the removal left it %d wide, want 16: the pointer is still held:\n%s", w, r.h.grid())
			}
		})
	}
}

// A maximized member removed ends the maximize: the page and the other member are shown again in
// their places. The mutant, named here: RemoveMember leaving the zoom on the removed tile (nothing
// is shown).
func TestRemovingTheMaximizedMemberShowsTheOthers(t *testing.T) {
	r, _ := twoMembers(t)
	var ok bool
	r.h.onLoop(func() { ok = r.tiles.Maximize("b") })
	r.h.settle()
	if !ok {
		t.Fatal("member b did not maximize")
	}
	if x, _, _ := boxAt(r.h, "PG"); x >= 0 {
		t.Fatalf("the page is shown while b is maximized:\n%s", r.h.grid())
	}
	r.remove(t, "b")
	var zoomed bool
	r.h.onLoop(func() { zoomed = r.tiles.Zoomed() != nil })
	if zoomed {
		t.Error("the manager is still zoomed on a removed member")
	}
	if x, _, w := boxAt(r.h, "PG"); x != 10 || w != 30 {
		t.Fatalf("the page at %d, %d wide, want 10, 30:\n%s", x, w, r.h.grid())
	}
	if x, _, w := boxAt(r.h, "AA"); x != 0 || w != 10 {
		t.Fatalf("member a at %d, %d wide, want 0, 10:\n%s", x, w, r.h.grid())
	}
}

// A member added while the manager runs takes its place in the manager's order before the tile
// AddMemberBefore names, as a manager built with it there lays it out: added before a, on the same
// edge, it is the outer strip; before no tile's key, it is appended, the inner one. The mutant,
// named here: AddMemberBefore appending always (the first case lays c inside a).
func TestAMemberAddedWhileRunningTakesItsPlaceInTheOrder(t *testing.T) {
	for _, c := range []struct {
		before   string
		cx, ax   int
		ordering string
	}{
		{"a", 0, 10, "before a: the outer strip"},
		{"nosuch", 10, 0, "appended: the inner strip"},
	} {
		t.Run(c.before, func(t *testing.T) {
			tiles := core.NewTiles()
			tiles.Add("page", core.NewBox(core.NewText(""), core.WithTitle("PG")), core.TilePlace{Edge: tui.DockCenter, Min: 5})
			a := newMember("AA", core.AtRect(tui.Rect{X: 2, Y: 6, W: 10, H: 3}), core.Left)
			cm := newMember("CC", core.AtRect(tui.Rect{X: 24, Y: 6, W: 12, H: 3}), core.Left)
			tiles.AddMember("a", a, core.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
			r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a, "c": cm})
			r.h.onLoop(func() {
				tiles.AddMemberBefore(c.before, "c", cm, core.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
			})
			r.h.settle()
			if x, _, _ := boxAt(r.h, "CC"); x != c.cx {
				t.Fatalf("%s: the added member at %d, want %d:\n%s", c.ordering, x, c.cx, r.h.grid())
			}
			if x, _, _ := boxAt(r.h, "AA"); x != c.ax {
				t.Fatalf("%s: member a at %d, want %d:\n%s", c.ordering, x, c.ax, r.h.grid())
			}
		})
	}
}

// A member added while a gesture is in progress cancels it first, as a removal does: the gesture's
// Escape would restore a checkpoint without the added member. The mutant, named here: AddMember not
// cancelling the gesture (Escape takes the added member out again).
func TestAddingAMemberCancelsAGestureInProgress(t *testing.T) {
	tiles := core.NewTiles()
	tiles.Add("page", core.NewBox(core.NewText(""), core.WithTitle("PG")), core.TilePlace{Edge: tui.DockCenter, Min: 5})
	a := newMember("AA", core.AtRect(tui.Rect{X: 2, Y: 6, W: 10, H: 3}), core.Left)
	cm := newMember("CC", core.AtRect(tui.Rect{X: 24, Y: 6, W: 12, H: 3}), core.Right)
	tiles.AddMember("a", a, core.TilePlace{Edge: tui.DockLeft, Size: 25, Min: 5}, true)
	r := startTiles(t, 40, 10, tiles, tiles, map[string]*member{"a": a, "c": cm})
	r.h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 9, Y: 4},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, Mods: tui.ModAlt, X: 14, Y: 4})
	r.h.settle()
	r.h.onLoop(func() { tiles.AddMember("c", cm, core.TilePlace{Edge: tui.DockRight, Size: 25, Min: 5}, true) })
	r.h.settle()
	r.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, Mods: tui.ModAlt, X: 14, Y: 4})
	r.h.settle()
	var there bool
	r.h.onLoop(func() { there = tiles.Tile("c") != nil })
	if !there {
		t.Fatal("Escape after the add took the added member out: the gesture's checkpoint predates it")
	}
	if x, _, w := boxAt(r.h, "CC"); x != 30 || w != 10 {
		t.Fatalf("the added member at %d, %d wide, want over its tile at 30, 10:\n%s", x, w, r.h.grid())
	}
	if x, _, w := boxAt(r.h, "AA"); x != 0 || w != 10 {
		t.Fatalf("member a at %d, %d wide, want 0, 10: the cancelled gesture changed it:\n%s", x, w, r.h.grid())
	}
}

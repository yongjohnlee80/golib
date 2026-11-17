package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// tiles_splice_test.go: a reload that adds or drops a TileManager's members splices them, as a
// program whose set of windows changes while it runs reloads its document. The members it keeps
// keep their nodes and state (an Editor's text, cursor and focus), a new one is placed where the
// document declares it, a dropped one leaves the manager, and the live TileManager.* bindings follow
// their members, never their indexes. A reload that touches a Tile still rebuilds the manager.

// spliceDoc is an 80×24 Window whose TileManager governs the Drawer ex, a quarter down the left
// with an Editor in it, beside the page; before and after are members declared around ex.
func spliceDoc(before, after string) string {
	return `import tui 1.0
import demo 1.0
Window {
 TileManager {
` + before + `
  Drawer { id: ex; modal: false; windowResize: true; minimizable: true; edge: Tui.Right; size: 30; closeOnQ: false
    TileManager.edge: Tui.Left; TileManager.edges: "left right"; TileManager.size: App.exSize; TileManager.minimumSize: 5
    Frame { title: "EXPLORER"; Editor { id: exEd } } }
` + after + `
  Tile { id: pg; edge: Tui.Center; Frame { title: "PAGE"; Text { text: "p" } } }
  onTileChanged: App.log("tile", key, operation, edge, size, tiled)
 }
}`
}

// auxMember is a member a reload adds: a Drawer tiled down the left, App.auxSize percent across,
// with an Editor of its own.
const auxMember = `  Drawer { id: aux; modal: false; windowResize: true; edge: Tui.Right; size: 20; closeOnQ: false
    TileManager.edge: Tui.Left; TileManager.size: App.auxSize; TileManager.minimumSize: 5
    Frame { title: "AUX"; Editor { id: auxEd } } }`

// pageTile is a Tile a reload adds: a strip along the bottom.
const pageTile = `  Tile { id: foot; edge: Tui.Bottom; size: 20; Frame { title: "FOOT"; Text { text: "f" } } }`

func runSplice(t *testing.T, doc string) (*decltest.Screen, *calls) {
	t.Helper()
	rec := &calls{}
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.exSize": 25, "App.auxSize": 25}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(core.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	return s, rec
}

// reloadResult reloads the screen with doc and returns what the reconcile did.
func reloadResult(t *testing.T, s *decltest.Screen, doc string) decl.Result {
	t.Helper()
	var res decl.Result
	var err error
	onScreenLoop(t, s, func() { res, err = s.Program.Reload([]byte(doc)) })
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return res
}

// editorState is an Editor's node, text, cursor and focus, read on the loop.
type editorState struct {
	node    tui.NodeID
	lines   string
	cursor  editor.TextPos
	focused bool
}

func stateOf(t *testing.T, s *decltest.Screen, id string) editorState {
	t.Helper()
	var st editorState
	onScreenLoop(t, s, func() {
		c, ok := s.Program.Find(id)
		if !ok {
			t.Errorf("no node %s", id)
			return
		}
		e := c.(*editor.Editor)
		st = editorState{e.NodeID(), strings.Join(e.Core().Lines(), "\n"), e.Core().Cursor(), e.Context().Focused()}
	})
	return st
}

// typeInto gives the Editor id its text and its cursor, and the keyboard.
func typeInto(t *testing.T, s *decltest.Screen, id, text string, at editor.TextPos) {
	t.Helper()
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find(id)
		e := c.(*editor.Editor)
		e.SetValue(text)
		e.Core().SetCursor(at)
		e.Context().RequestFocus()
	})
	s.WaitFor(t, id+" focused", func(string) bool { return stateOf(t, s, id).focused })
}

// A reload that adds a member after ex, then one that drops it, splices it in and out: no node is
// rebuilt, ex is the same Drawer, its Editor the same node with its text, its cursor and the
// keyboard; the new member opens over its tile, beside ex as a manager built with it lays it out,
// and once dropped its cells are the page's again and it is gone from the screen. The mutants,
// named here: the manager refusing to arrange (today's rebuild: ex's Editor is a new, empty node),
// and arrange keeping a dropped member in the manager (the page stays narrow).
func TestAReloadSplicesMembersInAndOutLeavingTheOthersAsTheyWere(t *testing.T) {
	s, _ := runSplice(t, spliceDoc("", ""))
	call(t, s, "ex", "open")
	titleAt(t, s, "PAGE", 22, 0)
	typeInto(t, s, "exEd", "alpha\nbeta gamma", editor.TextPos{Line: 1, Col: 5})
	drawer, _ := s.Program.Find("ex")
	before := stateOf(t, s, "exEd")
	// kept checks ex and its Editor are as they were; the keyboard too while nothing else took it
	kept := func(when string, focus bool) {
		t.Helper()
		if now, _ := s.Program.Find("ex"); now != drawer {
			t.Fatalf("%s: ex is another Drawer", when)
		}
		now := stateOf(t, s, "exEd")
		if !focus {
			now.focused = before.focused
		}
		if now != before {
			t.Fatalf("%s: ex's Editor is %+v, want it as it was, %+v", when, now, before)
		}
	}

	if res := reloadResult(t, s, spliceDoc("", auxMember)); len(res.Rebuilt) != 0 {
		t.Fatalf("adding a member rebuilt %v", res.Rebuilt)
	}
	kept("after aux was added", true)
	titleAt(t, s, "PAGE", 22, 0) // aux not open yet: it takes no cells
	call(t, s, "aux", "open")
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "AUX", 22, 0)
	titleAt(t, s, "PAGE", 42, 0)

	if res := reloadResult(t, s, spliceDoc("", "")); len(res.Rebuilt) != 0 {
		t.Fatalf("dropping a member rebuilt %v", res.Rebuilt)
	}
	titleAt(t, s, "PAGE", 22, 0)
	s.WaitFor(t, "AUX gone from the screen", func(sc string) bool { return !strings.Contains(sc, "AUX") })
	if _, ok := s.Program.Find("aux"); ok {
		t.Error("the dropped member is still found")
	}
	kept("after aux was dropped", false) // opening aux may have taken the keyboard
}

// A member a reload adds before ex is laid out before it, as a manager built with it there lays
// it out: on the same edge, the outer strip. Each binding drives its own member's tile, the inserted
// one's and the one whose index the insertion shifted: App.exSize moves ex's border, App.auxSize
// aux's. The mutants, named here: AddMember for AddMemberBefore (aux inside ex), and the receivers
// not given again after an arrange (aux's binding is refused, or drives the wrong tile).
func TestAMemberSplicedInBeforeAnotherKeepsEachBindingOnItsOwnTile(t *testing.T) {
	s, _ := runSplice(t, spliceDoc("", ""))
	call(t, s, "ex", "open")
	titleAt(t, s, "PAGE", 22, 0)
	if res := reloadResult(t, s, spliceDoc(auxMember, "")); len(res.Rebuilt) != 0 {
		t.Fatalf("adding a member rebuilt %v", res.Rebuilt)
	}
	call(t, s, "aux", "open")
	titleAt(t, s, "AUX", 2, 0)
	titleAt(t, s, "EXPLORER", 22, 0)
	titleAt(t, s, "PAGE", 42, 0)
	set(t, s, "App.exSize", 30) // ex 24 across: from 20 to 43
	titleAt(t, s, "EXPLORER", 22, 0)
	titleAt(t, s, "PAGE", 46, 0)
	set(t, s, "App.auxSize", 30) // aux 24 across: ex from 24
	titleAt(t, s, "AUX", 2, 0)
	titleAt(t, s, "EXPLORER", 26, 0)
	titleAt(t, s, "PAGE", 50, 0)
}

// The focus inside a member a reload drops goes to a node still on the screen: ex's Editor, the
// one left that takes it. The mutant, named here: arrange keeping the dropped member (its Editor
// keeps the keyboard, on a node no longer declared).
func TestTheFocusInADroppedMemberGoesToALiveNode(t *testing.T) {
	s, _ := runSplice(t, spliceDoc("", auxMember))
	call(t, s, "ex", "open")
	call(t, s, "aux", "open")
	titleAt(t, s, "PAGE", 42, 0)
	typeInto(t, s, "auxEd", "aux text", editor.TextPos{})
	reloadResult(t, s, spliceDoc("", ""))
	s.WaitFor(t, "ex's Editor focused", func(string) bool { return stateOf(t, s, "exEd").focused })
}

// A reload that drops a member while an Alt-right drag of its border is held cancels the drag: its
// release changes nothing and reports nothing, and the pointer is free, so a drag of ex's border
// afterwards resizes ex. The mutant, named here: RemoveMember not cancelling the gesture (the
// release commits a resize of the dropped strip, or the drag holds the pointer).
func TestDroppingAMemberMidDragCancelsTheDrag(t *testing.T) {
	s, rec := runSplice(t, spliceDoc("", auxMember))
	call(t, s, "ex", "open")
	call(t, s, "aux", "open")
	titleAt(t, s, "PAGE", 42, 0)
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 39, Y: 10},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, Mods: tui.ModAlt, X: 45, Y: 10})
	titleAt(t, s, "PAGE", 48, 0) // the drag is live: aux's border follows it
	reloadResult(t, s, spliceDoc("", ""))
	s.Keys(t, tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, Mods: tui.ModAlt, X: 45, Y: 10})
	titleAt(t, s, "PAGE", 22, 0)
	for _, c := range rec.all() {
		if strings.HasPrefix(c, "tile aux") {
			t.Fatalf("the cancelled drag reported %q", c)
		}
	}
	altDrag(t, s, tui.MouseRight, 19, 10, 31, 10)
	titleAt(t, s, "PAGE", 34, 0)
}

// A reload that adds a Tile rebuilds the manager, as before: a Tile is fixed once its manager is
// built (canSplice), and the rebuilt manager lays the new Tile out with the rest. The mutant, named
// here: canSplice taking every child (arrange refuses the new Tile, and the reload fails).
func TestAReloadAddingATileStillRebuildsTheManager(t *testing.T) {
	s, _ := runSplice(t, spliceDoc("", ""))
	res := reloadResult(t, s, spliceDoc("", pageTile))
	rebuilt := false
	for _, r := range res.Rebuilt {
		if r.Type == "TileManager" {
			rebuilt = true
		}
	}
	if !rebuilt {
		t.Fatalf("adding a Tile did not rebuild the manager: %v", res.Rebuilt)
	}
	s.WaitForText(t, "FOOT")
	call(t, s, "ex", "open")
	titleAt(t, s, "EXPLORER", 2, 0)
}

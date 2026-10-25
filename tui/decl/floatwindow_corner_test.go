package decl_test

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// floatwindow_corner_test.go: a component a Go program puts over a FloatWindow's content
// (AddAligned), as AutoDoc's find card is, at the window's top-right.

// cornerDoc is an 80×24 Window: the FloatWindow fw floating at columns 20 to 43 (tiled by
// App.fwTiled, the fill member), its Editor's own context menu on; and a floating Drawer dr over the
// right, whose Editor can take the keyboard.
const cornerDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager {
  FloatWindow { id: fw; movable: true; resizable: true; minimizable: true
    floatX: 25; floatY: 20; floatWidth: 30; floatHeight: 60
    TileManager.edge: Tui.Center; TileManager.tiled: App.fwTiled
    Frame { title: "FW"; Editor { id: ed; contextMenu: true; text: "one two three four five six seven" } } }
 }
 Drawer { id: dr; modal: false; edge: Tui.Right; size: 70; Frame { title: "DR"; Editor { id: dred; text: "drawer" } } }
}`

// cornerCard is AutoDoc's find card's shape: a framed count with an X, its clicks counted.
type cornerCard struct {
	box    *widget.Box
	x      *widget.Button
	clicks atomic.Int64
}

// runCorner runs cornerDoc with a card added at fw's top-right.
func runCorner(t *testing.T) (*decltest.Screen, *cornerCard) {
	t.Helper()
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(cornerDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.fwTiled": false}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "FW")
	c := &cornerCard{x: widget.NewButton("X")}
	row := tui.NewFlex(tui.Horizontal)
	row.Add(widget.NewText("3 of 9 "), c.x)
	c.box = widget.NewBox(row, widget.WithTitle("CARD"))
	onScreenLoop(t, s, func() {
		n, _ := s.Program.Find("fw")
		n.(interface {
			AddAligned(tui.Component, tui.Align)
		}).AddAligned(c.box, tui.AlignTopRight)
		tui.Subscribe(s.Program.App().Bus(), func(e tui.ControlActivatedEvent) {
			if e.Owner == c.x.Context().ID() {
				c.clicks.Add(1)
			}
		})
	})
	return s, c
}

// cardAt waits for the card to show at (x, y), or, with x < 0, to show nowhere.
func cardAt(t *testing.T, s *decltest.Screen, what string, x, y int) {
	t.Helper()
	s.WaitFor(t, what, func(sc string) bool {
		cx, cy, ok := at(sc, "CARD")
		if x < 0 {
			return !ok
		}
		return ok && cx == x && cy == y
	})
}

// The card is inside its window, at the window's top-right and z-order: a floating Drawer that holds
// the keyboard over it covers it, and the editor given the keyboard brings the window forward, card
// and all. Its X takes a click. It minimizes and restores with the window, follows it tiled, and
// SetVisible hides it.
func TestAFloatWindowsAlignedComponentIsInsideItAtItsZOrder(t *testing.T) {
	s, c := runCorner(t)
	cardAt(t, s, "the card at the window's top-right", 32, 5)
	call(t, s, "dr", "open")
	call(t, s, "dred", "forceActiveFocus")
	cardAt(t, s, "covered by the Drawer holding the keyboard", -1, 0)
	call(t, s, "ed", "forceActiveFocus")
	cardAt(t, s, "the window forward with its card", 32, 5)
	call(t, s, "dr", "close")
	x, y, _ := at(s.String(), "3 of 9")
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x + len("3 of 9 "), Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x + len("3 of 9 "), Y: y})
	s.WaitFor(t, "the X clicked", func(string) bool { return c.clicks.Load() == 1 })
	call(t, s, "fw", "minimize")
	cardAt(t, s, "minimized with the window", -1, 0)
	call(t, s, "fw", "restore")
	cardAt(t, s, "restored with it", 32, 5)
	set(t, s, "App.fwTiled", true)
	cardAt(t, s, "tiled, at the tile's top-right", 68, 0)
	set(t, s, "App.fwTiled", false)
	cardAt(t, s, "released, back with the window", 32, 5)
	onScreenLoop(t, s, func() { c.box.SetVisible(false) })
	cardAt(t, s, "SetVisible(false)", -1, 0)
}

// The window's content still opens its own popups over the Window, not inside the window: the
// editor's context menu, pressed near the window's right edge (column 43), reaches past it.
func TestAFloatWindowsContentStillOpensItsMenuOverTheWindow(t *testing.T) {
	s, _ := runCorner(t)
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 40, Y: 10})
	s.WaitForText(t, "Undo")
	widest := 0
	for _, line := range strings.Split(s.String(), "\n") {
		widest = max(widest, len([]rune(strings.TrimRight(line, " "))))
	}
	if widest-1 <= 43 {
		t.Fatalf("the menu stops at column %d, inside the window (its right edge 43): it opened in the window, not over the Window:\n%s", widest-1, s.String())
	}
}

// sizeProbe is a component that asks for 3×1 and records the constraints it is laid out with.
type sizeProbe struct {
	widget.Base
	mu   sync.Mutex
	last tui.Constraints
}

func (p *sizeProbe) Layout(c tui.Constraints) tui.Size {
	p.mu.Lock()
	p.last = c
	p.mu.Unlock()
	return c.Constrain(tui.Size{W: 3, H: 1})
}
func (p *sizeProbe) Render(tui.Surface) {}
func (p *sizeProbe) got() tui.Constraints {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

// A FloatWindow's content is laid out at the window's whole size, as it was before a program could
// put anything over it: a content that asks for 3×1 is given the window's rect, tight.
func TestAFloatWindowsContentIsLaidOutAtTheWholeWindow(t *testing.T) {
	probe := &sizeProbe{}
	probeType := tuidecl.Type{Name: "SizeProbe", Children: tuidecl.NoChildren(),
		Build: func(tuidecl.Build) (tui.Component, []string, error) { return probe, nil, nil }}
	doc := `import tui 1.0
Window { Text { text: "page" }
 FloatWindow { id: fw; floatX: 25; floatY: 20; floatWidth: 30; floatHeight: 60; SizeProbe {} } }`
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(doc)), tuidecl.AdapterOptions(tuidecl.WithTypes(probeType)))
	s.WaitFor(t, "the content laid out", func(string) bool { return probe.got().MaxW > 0 })
	if c := probe.got(); c.MinW != c.MaxW || c.MinH != c.MaxH || c.MaxW < 20 || c.MaxH < 10 {
		t.Fatalf("the content laid out with %+v, want the window's rect, tight (about 24×14)", c)
	}
}

// A FloatWindow's focus order puts its content before what a program adds over it, so the find card
// never takes the window's keyboard first: the window's first focus lands in its content (the
// button, not the card's X), Tab from the content reaches the X while the card shows, and a hidden
// card's X is never a Tab stop.
func TestAFloatWindowsContentComesBeforeItsAlignedComponentInFocusOrder(t *testing.T) {
	doc := `import tui 1.0
Window { Text { text: "page" }
 FloatWindow { id: fw; floatX: 25; floatY: 20; floatWidth: 50; floatHeight: 60
  Frame { title: "FW"; Button { id: one; text: "One" } } } }`
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(doc)))
	s.WaitForText(t, "One")
	x := widget.NewButton("X")
	row := tui.NewFlex(tui.Horizontal)
	row.Add(widget.NewText("3 of 9 "), x) // wide enough for the box's title
	card := widget.NewBox(row, widget.WithTitle("CARD"))
	onScreenLoop(t, s, func() {
		n, _ := s.Program.Find("fw")
		n.(interface {
			AddAligned(tui.Component, tui.Align)
		}).AddAligned(card, tui.AlignTopRight)
	})
	onScreenLoop(t, s, func() {}) // laid out; not waited on by its drawing, which the order also decides
	focused := func() (one, xx bool) {
		onScreenLoop(t, s, func() {
			b, _ := s.Program.Find("one")
			one, xx = s.Program.App().FocusWithin(b), s.Program.App().FocusWithin(x)
		})
		return one, xx
	}
	call(t, s, "fw", "forceActiveFocus")
	if one, xx := focused(); !one || xx {
		t.Fatalf("the window's first focus: in its content %v, in the card's X %v; want the content", one, xx)
	}
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	s.WaitFor(t, "Tab from the content to the card's X", func(string) bool { _, xx := focused(); return xx })
	onScreenLoop(t, s, func() { card.SetVisible(false) })
	call(t, s, "one", "forceActiveFocus")
	for range 3 {
		s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
		if _, xx := focused(); xx {
			t.Fatal("Tab reached the hidden card's X")
		}
	}
}

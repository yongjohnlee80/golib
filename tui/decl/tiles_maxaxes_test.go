package decl_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tiles_maxaxes_test.go: the centre's maximum. A page as wide as its ruler takes at most
// App.pageW columns; the strips beside it take the rest, two sides splitting it, and with none the
// page is centred at its maximum, the columns either side of it no window's.

// maxAxesDoc is an 80×24 Window: the page, a FloatWindow at the centre, capped at App.pageW (its
// Frame too, as AutoDoc's page is, centred in what it is given); an
// agent Drawer at the right and an explorer Drawer at the left, each tiled while its source says.
const maxAxesDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager { id: tm
  FloatWindow { id: pg; movable: true; resizable: true
    TileManager.edge: Tui.Center; TileManager.maximumWidth: App.pageW; TileManager.minimumWidth: App.pageMinW
    Frame { title: "PAGE"; maximumWidth: App.pageW; Editor { id: ped; text: "page" } } }
  Drawer { id: ag; modal: false; windowResize: true; movable: true; edge: Tui.Right; size: 30; closeOnQ: false
    TileManager.edge: Tui.Right; TileManager.size: 25; TileManager.tiled: App.agTiled; TileManager.minimumWidth: App.agMinW
    Frame { title: "AGENT"; Editor { id: aged; text: "agent" } } }
  Drawer { id: ex; modal: false; windowResize: true; movable: true; edge: Tui.Left; size: 30; closeOnQ: false
    TileManager.edge: Tui.Left; TileManager.size: 25; TileManager.tiled: App.exTiled
    Frame { title: "EXPLORER"; Editor { id: exed; text: "explorer" } } }
 }
}`

// runMaxAxes runs maxAxesDoc with sources over its defaults: the page capped at 40, both panels
// tiled, minimums of 5.
func runMaxAxes(t *testing.T, sources map[string]any) *decltest.Screen {
	t.Helper()
	all := map[string]any{"App.pageW": 40, "App.pageMinW": 5, "App.agTiled": true, "App.agMinW": 5, "App.exTiled": true}
	for k, v := range sources {
		all[k] = v
	}
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(maxAxesDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(all),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	return s
}

// waitScreenRect waits for id to be drawn at want.
func waitScreenRect(t *testing.T, s *decltest.Screen, id string, want tui.Rect) {
	t.Helper()
	s.WaitFor(t, fmt.Sprintf("%s at %+v", id, want), func(string) bool {
		r, on := screenRect(t, s, id)
		return on && r == want
	})
}

// keyboardIn reports whether the keyboard is inside the node id.
func keyboardIn(t *testing.T, s *decltest.Screen, id string) bool {
	t.Helper()
	var in bool
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find(id)
		in = s.Program.App().FocusWithin(c)
	})
	return in
}

// The agent tiled at the right takes what the page can't: it asks a quarter, 20 columns, but the
// page takes at most 40 of the 80, so the agent is 40 wide, no gap between them.
func TestTheStripBesideTheCentreTakesWhatItsMaximumLeaves(t *testing.T) {
	s := runMaxAxes(t, nil)
	call(t, s, "ag", "open")
	waitScreenRect(t, s, "ag", tui.Rect{X: 40, Y: 0, W: 40, H: 24})
	waitScreenRect(t, s, "pg", tui.Rect{X: 0, Y: 0, W: 40, H: 24})
}

// Two sides split it: the page capped at 31, the explorer and the agent each asking 20, so the 9 left
// over go 4 to the explorer and 5 to the agent, the odd one to the right, and the page sits between.
func TestTwoStripsBesideTheCentreSplitWhatItsMaximumLeaves(t *testing.T) {
	s := runMaxAxes(t, map[string]any{"App.pageW": 31})
	call(t, s, "ex", "open")
	call(t, s, "ag", "open")
	waitScreenRect(t, s, "ex", tui.Rect{X: 0, Y: 0, W: 24, H: 24})
	waitScreenRect(t, s, "pg", tui.Rect{X: 24, Y: 0, W: 31, H: 24})
	waitScreenRect(t, s, "ag", tui.Rect{X: 55, Y: 0, W: 25, H: 24})
}

// With no strip, the page is centred at its maximum, and its tile is its rect: the window is no wider
// than the page. The maximum is live: at 50 the page is 50 wide, centred again.
func TestTheCentreAloneIsCentredAtItsMaximum(t *testing.T) {
	s := runMaxAxes(t, nil)
	waitScreenRect(t, s, "pg", tui.Rect{X: 20, Y: 0, W: 40, H: 24})
	if b := coreBounds(t, s, "pg"); b != (tui.Rect{X: 20, Y: 0, W: 40, H: 24}) {
		t.Fatalf("the page's tile %v: want its own rect, (20,0) 40x24", b)
	}
	set(t, s, "App.pageW", 50)
	waitScreenRect(t, s, "pg", tui.Rect{X: 15, Y: 0, W: 50, H: 24})
}

// Johno's click-back: the page alone, centred, has the keyboard and is drawn over the explorer, which
// floats at the left. A click on the explorer beside the page takes the keyboard: no window lies
// over it there. (With the page's tile the whole manager, the press landed on the page's window.)
func TestAClickBesideTheCappedCentreReachesTheWindowBeneath(t *testing.T) {
	s := runMaxAxes(t, map[string]any{"App.exTiled": false})
	call(t, s, "ex", "open")
	s.WaitForText(t, "EXPLORER")
	call(t, s, "ped", "forceActiveFocus")
	s.WaitFor(t, "the page's keyboard", func(string) bool { return keyboardIn(t, s, "ped") })
	var x, y int
	s.WaitFor(t, "the explorer drawn beside the page", func(sc string) bool {
		var ok bool
		x, y, ok = at(sc, "explorer")
		return ok && x < 20
	})
	if x >= 20 {
		t.Fatalf("the explorer's text isn't drawn beside the page (at %d,%d):\n%s", x, y, s.String())
	}
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x + 1, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x + 1, Y: y})
	if !keyboardIn(t, s, "exed") {
		t.Fatalf("a click on the explorer beside the page left the keyboard where it was:\n%s", s.String())
	}
}

// What fits is the minimums' alone, the maximum never: the agent keeping 55 and the page 5 fit 80 (the
// page's maximum, 40, would not), so the agent is tiled; keeping 76 they don't, and it is released.
func TestTheCentresMaximumDoesNotDecideWhatFits(t *testing.T) {
	s := runMaxAxes(t, map[string]any{"App.agMinW": 55})
	call(t, s, "ag", "open")
	s.WaitFor(t, "the agent over its tile", func(string) bool { return governed(t, s, "ag") })
	set(t, s, "App.agMinW", 76)
	s.WaitFor(t, "the agent released", func(string) bool { return !governed(t, s, "ag") })
	if !strings.Contains(s.String(), "AGENT") {
		t.Fatalf("the released agent isn't drawn at its own place:\n%s", s.String())
	}
}

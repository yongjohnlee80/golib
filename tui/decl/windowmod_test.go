package decl_test

import (
	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
	"strings"
	"testing"
)

func TestWindowModDeclarationHousesAnExistingZoomPolicy(t *testing.T) {
	target := &windowZoomFixture{}
	s := decltest.Run(t, 40, 12, tuidecl.LayoutSource("window.qml", []byte(`import tui 1.0
Window { WindowMod { id: pane; maximizable: true; key: "pane"
    Flex { direction: Tui.Vertical; WindowButtons { target: "pane" } Text { text: "document" } }
} }`)), tuidecl.AdapterOptions(tuidecl.WithWindowTargets(map[string]any{"pane": target})))
	s.WaitForText(t, "document")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("pane", "toggleMaximize"); err != nil {
			t.Error(err)
		}
		if !target.zoomed || target.calls != 1 {
			t.Errorf("zoom policy not shared: %+v", target)
		}
	})
}

type windowZoomFixture struct {
	zoomed bool
	calls  int
}

func (z *windowZoomFixture) ToggleMaximize() bool { z.zoomed = !z.zoomed; z.calls++; return true }
func (z *windowZoomFixture) Maximized() bool      { return z.zoomed }

func TestDrawerWindowResizeHasNoGripAndKeepsTheFinalProportions(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 60, 20, tuidecl.LayoutSource("window.qml", []byte(`import tui 1.0
import demo 1.0
Window { Text { text: "page" }
Drawer { id: d; modal: false; windowResize: true; edge: Tui.Bottom; size: 30; length: 100
    dragModifier: WindowMod.Alt | WindowMod.Control; onResized: App.log(size, length)
    Frame { title: "panel"; Editor { id: editor } }
} }`)), tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "panel")
	if _, _, ok := find(s, "□"); ok {
		t.Fatal("new window resize created a visible legacy grip")
	}
	s.Keys(t,
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt | tui.ModCtrl, X: 40, Y: 16},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, X: 40, Y: 12},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 40, Y: 12})
	s.WaitFor(t, "one completed proportion write", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all(); got[0].Raw != "50" || got[1].Raw != "100" {
		t.Fatalf("resized: %+v", got)
	}
	s.Keys(t, tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 40, Y: 12})
	if len(rec.all()) != 2 {
		t.Error("an extra release emitted a second resize completion")
	}
}

func TestDrawerCollectorMinimizeNeedsNoDefaultTaskbar(t *testing.T) {
	model := widget.NewMinimizedWindows()
	s := decltest.Run(t, 40, 15, tuidecl.LayoutSource("window.qml", []byte(`import tui 1.0
Window { Button { text: "base" }
Drawer { id: d; modal: false; minimizable: true; closable: true; label: "Notes"
    Frame { title: "Notes"; Text { text: "inside" } }
} }`)), tuidecl.AdapterOptions(tuidecl.WithWindowCollector(model)))
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "inside")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "minimize"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "hidden without taskbar", func(sc string) bool { return !strings.Contains(sc, "inside") })
	onScreenLoop(t, s, func() {
		entries := model.Entries()
		if len(entries) != 1 {
			t.Error("no collector entry")
			return
		}
		if !entries[0].Restore() {
			t.Error("application restore refused")
		}
	})
	s.WaitForText(t, "inside")
}

func TestDrawerResizeByGrowsFromBottomWithoutPointerReporting(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 40, 20, tuidecl.LayoutSource("keys.qml", []byte(`import tui 1.0
import demo 1.0
Window { Button { text: "base" }
Drawer { id: d; modal: false; windowResize: true; edge: Tui.Bottom; size: 25
onResized: App.log(size,length)
Frame { title: "panel"; Editor {} }
} }`)), tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "panel")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "resizeBy", 0, 2); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "one keyboard completion", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all(); got[0].Raw != "35" {
		t.Fatalf("bottom keyboard growth: %+v", got)
	}
}

func TestWindowMissingCollectorIsABuildError(t *testing.T) {
	p, err := tuidecl.NewProgram(tuidecl.LayoutSource("bad.qml", []byte(`import tui 1.0
Window { WindowMod { minimizable: true; Text { text: "missing collector" } } }`)))
	if err == nil {
		if p != nil {
			_ = p.Tree().Destroy()
		}
		t.Fatal("missing collector accepted")
	}
	if !strings.Contains(err.Error(), "WithWindowCollector") {
		t.Fatalf("unhelpful collector error: %v", err)
	}
}

func TestDrawerWindowResizeEscapeEmitsNoPersistenceCompletion(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 40, 20, tuidecl.LayoutSource("cancel.qml", []byte(`import tui 1.0
import demo 1.0
Window { Button { text: "base" }
Drawer { id: d; modal: false; windowResize: true; edge: Tui.Bottom; size: 30
onResized: App.log(size,length)
Frame { title: "panel"; Editor {} }
} }`)), tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "panel")
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 20, Y: 16},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, X: 20, Y: 12}, tui.KeyEvent{Code: tui.KeyEscape})
	s.WaitFor(t, "checkpoint restored", func(string) bool { return panelTop(s) == 14 })
	if len(rec.all()) != 0 {
		t.Errorf("cancel persisted a size: %+v", rec.all())
	}
}

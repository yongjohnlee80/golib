package decl_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

const windowChangedDoc = `import tui 1.0
import demo 1.0
Window {
 WindowMod { id: wm; key: "wm"; maximizable: true
  onChanged: App.log("wm", operation, key, x, y, width, height, maximized, minimized)
  Text { text: "page" } }
 Drawer { id: dr; key: "dr"; modal: false; maximizable: true
  onChanged: App.log("dr", operation, key, x, y, width, height, maximized, minimized)
  Text { text: "drawer" } }
 Drawer { id: plain; Text { text: "plain" } }
}`

// A WindowMod and a Drawer with window controls raise changed alike: the operation, the window's
// key, its bounds and its two states, in the signal's order, from the same method. A Drawer with
// none refuses the method as unavailable. This pins the shape decl-D9's shared declaration
// (windowModConfig.options, windowChangedArgs, drawerWindow) must keep.
func TestAWindowModAndADrawerRaiseChangedAlike(t *testing.T) {
	rec := &calls{}
	s := decltest.Run(t, 60, 20, tuidecl.LayoutSource("main.qml", []byte(windowChangedDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		// a maximizable WindowMod zooms a target (WindowCore.Bind requires one); a Drawer is its own
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows()),
			tuidecl.WithWindowTargets(map[string]any{"wm": &windowZoomFixture{}})))
	s.WaitForText(t, "page")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("dr", "open"); err != nil {
			t.Errorf("dr.open(): %v", err)
		}
	})
	s.WaitForText(t, "drawer")
	for _, id := range []string{"wm", "dr"} {
		onScreenLoop(t, s, func() {
			if err := s.Program.Call(id, "toggleMaximize"); err != nil {
				t.Errorf("%s.toggleMaximize(): %v", id, err)
			}
		})
	}
	s.WaitFor(t, "both maximized", func(string) bool { return len(rec.all()) >= 2 })
	for i, id := range []string{"wm", "dr"} {
		f := strings.Fields(rec.all()[i])
		if len(f) != 9 || f[0] != id || f[1] != "maximize" || f[2] != id || f[7] != "true" || f[8] != "false" {
			t.Errorf("%s changed: %q; want %s maximize %s x y width height true false", id, rec.all()[i], id, id)
			continue
		}
		for _, n := range f[3:7] {
			if _, err := strconv.Atoi(n); err != nil {
				t.Errorf("%s changed: bound %q is not a whole number (%q)", id, n, rec.all()[i])
			}
		}
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("plain", "toggleMaximize"); !errors.Is(err, tuidecl.ErrWindowOperationUnavailable) {
			t.Errorf("plain.toggleMaximize(): %v; want ErrWindowOperationUnavailable", err)
		}
	})
}

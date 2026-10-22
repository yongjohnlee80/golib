package widget

import (
	"context"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// The GUI Editor's menu key follows the key map, as the terminal Editor's does: rebound to F2,
// F2 opens the menu and Shift+F10 no longer does. Before, the GUI Editor matched the fixed
// Shift+F10 and Menu keys, so a rebound key did nothing and the old one still opened the menu.
func TestTheGUIEditorsMenuKeyFollowsTheKeymap(t *testing.T) {
	scheme, err := keymap.ParseScheme(strings.NewReader(`<mapping>
<shortcut id="tui.editor.contextMenu"><key value="F2"/></shortcut>
</mapping>`), "keymap.xml")
	if err != nil {
		t.Fatal(err)
	}
	km, err := keymap.Build(tuiwidget.KeyCommands(), scheme)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEditor(WithCore(tuiwidget.CoreInitialText("hello")))
	e.SetContextMenuRows(nil)
	tb := tui.NewTestBackend(60, 14, tui.WithTestCapabilities(tui.Capabilities{NativeViews: true}))
	sh := &shell{child: tuiwidget.NewOverlayHost(e)}
	app := tui.NewApp(sh, tui.WithBackend(&nativeTB{TestBackend: tb}), tui.WithKeymap(km))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	h := &edHarness{t: t, app: app, tb: tb, sh: sh, e: e, cell: gui.Size{W: cellW, H: cellH}}
	h.until("a frame", func() bool { return tb.Flushes() > 0 })
	_ = tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 0, Y: 1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 0, Y: 1})
	h.barrier()

	_ = tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10, Mods: tui.ModShift})
	h.barrier()
	if strings.Contains(tb.String(), "Paste") {
		t.Fatal("Shift+F10 opened the menu, though the key map moved it to F2")
	}
	_ = tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF2})
	h.until("the menu on F2", func() bool { return strings.Contains(tb.String(), "Paste") })
}

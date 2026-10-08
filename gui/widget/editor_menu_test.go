package widget

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// An Editor built without a menu (as a QML style builds it) gets its rows afterwards: a
// consumer's CoreMenuAction row, appended to the stock rows, runs on the core when chosen.
func TestContextMenuRowsSetAfterConstruction(t *testing.T) {
	e := NewEditor(WithCore(tuiwidget.CoreInitialText("hello world")))
	var ran atomic.Int32
	e.SetContextMenuRows(func(c *tuiwidget.EditorCore) []tuiwidget.MenuItemModel {
		rows := tuiwidget.CoreContextItems(c)
		return append(rows, tuiwidget.NewCommand("upper", "Upper", tuiwidget.CoreMenuAction{ID: "upper",
			Run: func(c *tuiwidget.EditorCore) { ran.Add(1); c.SetValue(strings.ToUpper(c.Value())) }}))
	})
	tb := tui.NewTestBackend(60, 14, tui.WithTestCapabilities(tui.Capabilities{NativeViews: true}))
	sh := &shell{child: tuiwidget.NewOverlayHost(e)}
	app := tui.NewApp(sh, tui.WithBackend(&nativeTB{TestBackend: tb}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	h := &edHarness{t: t, app: app, tb: tb, sh: sh, e: e, cell: gui.Size{W: cellW, H: cellH}}
	h.until("a frame", func() bool { return tb.Flushes() > 0 })
	h.barrier()

	// a right press on the text (the body starts under the title row)
	_ = tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 2, Y: 1})
	// no barrier while the menu is open: it holds the keys, the barrier's included
	h.until("the menu", func() bool { return strings.Contains(tb.String(), "Upper") })
	// nothing to undo, no selection, an empty register: Upper is the only enabled row
	_ = tb.Inject(tui.KeyEvent{Code: tui.KeyEnter})
	h.until("the row ran", func() bool { return ran.Load() == 1 })
	var v string
	h.onLoop(func() { v = e.Core().Value() })
	if v != "HELLO WORLD" {
		t.Errorf("after the row: %q", v)
	}

	// nil goes back to the stock rows
	h.until("the menu closed", func() bool { return !strings.Contains(tb.String(), "Upper") })
	h.onLoop(func() { e.SetContextMenuRows(nil) })
	_ = tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 2, Y: 1})
	h.until("the stock menu", func() bool { return strings.Contains(tb.String(), "Paste") })
	if strings.Contains(tb.String(), "Upper") {
		t.Error("nil rows kept the consumer's row")
	}
}

// Shift+F10 opens the menu at the native caret, as a right press on the caret's cell does; with
// the menu off it opens nothing and the key goes on.
func TestShiftF10OpensTheMenuAtTheNativeCaret(t *testing.T) {
	e := NewEditor(WithCore(tuiwidget.CoreInitialText("hello world\nsecond line")))
	e.SetContextMenuRows(nil)
	tb := tui.NewTestBackend(60, 14, tui.WithTestCapabilities(tui.Capabilities{NativeViews: true}))
	sh := &shell{child: tuiwidget.NewOverlayHost(e)}
	app := tui.NewApp(sh, tui.WithBackend(&nativeTB{TestBackend: tb}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	h := &edHarness{t: t, app: app, tb: tb, sh: sh, e: e, cell: gui.Size{W: cellW, H: cellH}}
	h.until("a frame", func() bool { return tb.Flushes() > 0 })
	_ = tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 0, Y: 2},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 0, Y: 2})
	h.barrier()
	h.paint() // the native caret: the menu opens from its pixels, not the cells'
	h.onLoop(func() {
		if _, _, ok := e.body.caretCell(); !ok || !e.body.caretOn {
			t.Error("no native caret after a paint")
		}
	})
	_ = tb.Inject(tui.KeyEvent{Code: tui.KeyF10, Mods: tui.ModShift})
	h.until("the menu", func() bool { return strings.Contains(tb.String(), "Paste") })
	// the caret is on the second line, the body's row 1 (screen row 2): the panel's border on row
	// 3, Undo on row 4
	for y, line := range strings.Split(tb.String(), "\n") {
		if strings.Contains(line, "Undo") && y != 4 {
			t.Errorf("Undo on row %d, want 4, below the caret's line:\n%s", y, tb.String())
		}
	}
	_ = tb.Inject(tui.KeyEvent{Code: tui.KeyEscape})
	h.until("the menu closed", func() bool { return !strings.Contains(tb.String(), "Paste") })

	h.onLoop(func() { e.SetContextMenu(false) })
	_ = tb.Inject(tui.KeyEvent{Code: tui.KeyF10, Mods: tui.ModShift})
	h.barrier()
	if strings.Contains(tb.String(), "Paste") {
		t.Error("Shift+F10 opened a menu that is off")
	}
}

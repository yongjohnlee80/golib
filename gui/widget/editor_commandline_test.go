package widget

import (
	"context"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// ':' opens the shared "VIM Commands" prompt over the native editor, and a line runs on its core;
// off, ':' goes on to the application.
func TestColonOpensThePromptOverTheNativeEditor(t *testing.T) {
	e := NewEditor(WithCore(tuiwidget.CoreInitialText("one\ntwo\nthree")))
	tb := tui.NewTestBackend(70, 16, tui.WithTestCapabilities(tui.Capabilities{NativeViews: true}))
	sh := &shell{child: tuiwidget.NewOverlayHost(e)}
	app := tui.NewApp(sh, tui.WithBackend(&nativeTB{TestBackend: tb}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	h := &edHarness{t: t, app: app, tb: tb, sh: sh, e: e, cell: gui.Size{W: cellW, H: cellH}}
	h.until("a frame", func() bool { return tb.Flushes() > 0 })
	_ = tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 0, Y: 1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 0, Y: 1})
	h.barrier()

	_ = tb.Inject(tui.KeyEvent{Code: ':', Text: ":"})
	h.until("the prompt", func() bool { return strings.Contains(tb.String(), "VIM Commands") })
	_ = tb.Inject(tui.KeyEvent{Code: '3', Text: "3"}, tui.KeyEvent{Code: tui.KeyEnter})
	h.until("the prompt closed", func() bool { return !strings.Contains(tb.String(), "VIM Commands") })
	var at tuiwidget.TextPos
	h.onLoop(func() { at = e.Core().Cursor() })
	if at.Line != 2 {
		t.Fatalf("after :3 the cursor is on line %d", at.Line+1)
	}

	h.onLoop(func() { e.SetCommandLineEnabled(false) })
	_ = tb.Inject(tui.KeyEvent{Code: ':', Text: ":"})
	h.barrier()
	if strings.Contains(tb.String(), "VIM Commands") {
		t.Fatal("':' opened a prompt that is off")
	}
}

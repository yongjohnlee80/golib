package decl_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// overlay_test.go holds WithOverlay to ADR D4's rows: a QML dialog opened
// over a screen built in Go, on the Go program's own overlay, handing the
// keyboard back to the Go widget that had it.

// goScreen is a Go program with one QML part: a TextInput of its own beside
// whatever the document builds, under the program's OverlayHost.
type goScreen struct {
	t       *testing.T
	app     *tui.App
	be      *tui.TestBackend
	tree    *decl.Tree
	adapter *tuidecl.Adapter
	input   *widget.TextInput
	yes, no atomic.Int32
}

func runGoScreen(t *testing.T, doc string) *goScreen {
	t.Helper()
	g := &goScreen{t: t, be: tui.NewTestBackend(50, 12), input: widget.NewTextInput()}
	// The overlay the dialogs open on is the Go program's, so it exists
	// before the adapter is given it; the QML part joins its base below.
	base := tui.NewFlex(tui.Vertical)
	host := widget.NewOverlayHost(base)
	g.adapter = tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithErrorSink(func(err error) { t.Errorf("handler: %v", err) }),
		tuidecl.WithOverlay(host))...)
	g.tree = decl.New(g.adapter, decl.WithScheduler(func(fn func()) { g.app.Update(fn) }))
	if err := tuidecl.InjectHosts(g.tree, tuidecl.HostFuncs{
		"yes": func() error { g.yes.Add(1); return nil },
		"no":  func() error { g.no.Add(1); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	spec, err := qml.QML{File: "part.qml"}.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := g.tree.Mount(spec); err != nil {
		t.Fatal(err)
	}
	base.Add(g.input)
	if root, _ := g.adapter.Component(g.tree.Root()); !isDialog(root) {
		base.Add(root)
	}
	g.app = tui.NewApp(host, tui.WithBackend(g.be), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.app.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("the app did not stop")
		}
	})
	return g
}

// isDialog reports a component that is opened rather than laid out: the
// document's root when the document is only a dialog.
func isDialog(c tui.Component) bool {
	return strings.HasSuffix(fmt.Sprintf("%T", c), ".dialogNode")
}

func (g *goScreen) onLoop(fn func()) {
	done := make(chan struct{})
	g.app.Update(func() { fn(); close(done) })
	<-done
}

func (g *goScreen) open(id string) {
	g.t.Helper()
	g.onLoop(func() {
		n, ok := g.tree.NodeByID(id)
		if !ok {
			g.t.Errorf("no node %q", id)
			return
		}
		if err := g.adapter.Invoke(n, "open", nil); err != nil {
			g.t.Errorf("open: %v", err)
		}
	})
}

func (g *goScreen) waitFor(what string, cond func(string) bool) {
	g.t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !cond(g.be.String()); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			g.t.Fatalf("%s never happened:\n%s", what, g.be.String())
		}
	}
}

func (g *goScreen) keys(evs ...tui.Event) {
	g.t.Helper()
	if err := g.be.Inject(evs...); err != nil {
		g.t.Fatal(err)
	}
}

const askDoc = "import tui 1.0\nFlex {\n Text { text: \"the qml part\" }\n" +
	" Dialog { id: ask; title: \"Ask\"; standardButtons: Dialog.Yes | Dialog.No\n" +
	"  onAccepted: yes(); onRejected: no()\n  Text { text: \"sure?\" } }\n}"

// H1 + H2 — the dialog opens on the Go program's overlay, answers, closes on a
// letter and on Escape, and hands the keyboard back to the Go TextInput.
func TestH1H2AQMLDialogOpensOverAGoScreenAndGivesTheKeyboardBack(t *testing.T) {
	g := runGoScreen(t, askDoc)
	g.waitFor("the qml part", func(s string) bool { return strings.Contains(s, "the qml part") })
	g.onLoop(func() { g.input.Context().RequestFocus() })
	g.keys(decltest.Type("ab")...)
	g.waitFor("typing into the Go input", func(string) bool {
		var v string
		g.onLoop(func() { v = g.input.Value() })
		return v == "ab"
	})

	g.open("ask")
	g.waitFor("the dialog", func(s string) bool { return strings.Contains(s, "sure?") })
	g.keys(decltest.Rune('y'))
	g.waitFor("the dialog closing on y", func(s string) bool { return !strings.Contains(s, "sure?") })
	if g.yes.Load() != 1 {
		t.Errorf("accepted ran %d times, want 1", g.yes.Load())
	}
	g.keys(decltest.Rune('c'))
	g.waitFor("the keyboard back in the Go input", func(string) bool {
		var v string
		g.onLoop(func() { v = g.input.Value() })
		return v == "abc"
	})

	g.open("ask")
	g.waitFor("the dialog again", func(s string) bool { return strings.Contains(s, "sure?") })
	g.keys(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	g.waitFor("the dialog closing on Escape", func(s string) bool { return !strings.Contains(s, "sure?") })
	if g.no.Load() != 1 {
		t.Errorf("rejected ran %d times, want 1", g.no.Load())
	}
}

// H3 — a document that is only a dialog needs no Window.
func TestH3ADocumentThatIsOnlyADialog(t *testing.T) {
	g := runGoScreen(t, "import tui 1.0\nDialog { id: alone; title: \"Solo\"; standardButtons: Dialog.Ok\n Text { text: \"on its own\" } }")
	g.open("alone")
	g.waitFor("the dialog", func(s string) bool { return strings.Contains(s, "on its own") })
}

// H4 — under a Window, a dialog opens on the Window's host, not the adapter's.
func TestH4AWindowsDialogsStillOpenOnTheWindow(t *testing.T) {
	elsewhere := widget.NewOverlayHost(widget.NewText("never mounted"))
	s := decltest.Run(t, 40, 8, tuidecl.LayoutSource("m.qml", []byte("import tui 1.0\nWindow {\n Text { text: \"body\" }\n"+
		" Dialog { id: d; title: \"Here\"; Text { text: \"on the window\" } } }")),
		tuidecl.AdapterOptions(tuidecl.WithOverlay(elsewhere)))
	s.WaitForText(t, "body")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "on the window")
}

// H5 — without WithOverlay, outside a Window, open() refuses and says what to do.
func TestH5WithoutAnOverlayOpenNamesTheRemedy(t *testing.T) {
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(), tuidecl.WithErrorSink(func(error) {}))...)
	tr := decl.New(a)
	spec, _ := qml.QML{}.Parse([]byte("import tui 1.0\nDialog { id: d; Text { } }"))
	if err := tr.Mount(spec); err != nil {
		t.Fatal(err)
	}
	id, _ := tr.NodeByID("d")
	if err := a.Invoke(id, "open", nil); err == nil || !strings.Contains(err.Error(), "WithOverlay") {
		t.Fatalf("err = %v, want WithOverlay named", err)
	}
}

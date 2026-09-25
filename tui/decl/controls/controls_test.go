package controls_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// controls_test.go holds TextField and Popup to the trial's rows. The DUAL
// PATH is the point: the same typing into a native widget.TextInput and into
// a QML TextField must give the same screen and the same answers, or the QML
// type is not the widget — only something that looks like it.

// recorder keeps what the handlers were called with.
type recorder struct {
	mu    sync.Mutex
	calls map[string][]string
}

func (r *recorder) handler(name string) decl.HandlerFunc {
	return func(args []qml.SpecValue) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.calls == nil {
			r.calls = map[string][]string{}
		}
		v := ""
		if len(args) > 0 {
			v = args[0].Raw
		}
		r.calls[name] = append(r.calls[name], v)
		return nil
	}
}

func (r *recorder) got(name string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls[name]...)
}

func runQML(t *testing.T, doc string, rec *recorder) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Types(controls.Types()...),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{
			"App.run":  rec.handler("run"),
			"App.edit": rec.handler("edit"),
		}))
}

// runNative runs a widget.TextInput as a Go program would.
func runNative(t *testing.T, input *widget.TextInput) *tui.TestBackend {
	t.Helper()
	be := tui.NewTestBackend(40, 8)
	app := tui.NewApp(input, tui.WithBackend(be), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return be
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
	}
}

func firstRow(screen string) string {
	return strings.TrimRight(strings.SplitN(screen, "\n", 2)[0], " ")
}

const fieldDoc = "import tui 1.0\nimport demo 1.0\nTextField { focus: true; onAccepted: App.run(text); onTextEdited: App.edit(text) }"

// W2 — the same typing, natively and from QML: the same row on screen, the
// same accepted value, one edit reported per keystroke.
func TestW2ATextFieldIsTheTextInputOnBothPaths(t *testing.T) {
	// The native callbacks run on the App's loop; the test reads under a lock.
	var mu sync.Mutex
	var nativeAccepted []string
	var edits int
	native := widget.NewTextInput(
		widget.WithOnSubmit(func(v string) { mu.Lock(); nativeAccepted = append(nativeAccepted, v); mu.Unlock() }),
		widget.WithOnEdit(func(string) { mu.Lock(); edits++; mu.Unlock() }))
	be := runNative(t, native)
	accepted := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), nativeAccepted...) }

	rec := &recorder{}
	s := runQML(t, fieldDoc, rec)

	typing := append(decltest.Type("hello"), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	if err := be.Inject(typing...); err != nil {
		t.Fatal(err)
	}
	s.Keys(t, typing...)

	waitFor(t, "the native accept", func() bool { return len(accepted()) == 1 })
	waitFor(t, "the QML accept", func() bool { return len(rec.got("run")) == 1 })
	if got := rec.got("run")[0]; got != accepted()[0] || got != "hello" {
		t.Errorf("accepted: QML %q, native %q, want hello", got, accepted()[0])
	}
	// The accept runs inside the key's handler, BEFORE the frame that paints
	// the last keystroke: compare the rows once both screens show the text.
	waitFor(t, "both screens painted", func() bool {
		return strings.Contains(firstRow(s.String()), "hello") && strings.Contains(firstRow(be.String()), "hello")
	})
	if firstRow(s.String()) != firstRow(be.String()) {
		t.Errorf("the rows differ:\nQML    %q\nnative %q", firstRow(s.String()), firstRow(be.String()))
	}
	mu.Lock()
	nativeEdits := edits
	mu.Unlock()
	if got := rec.got("edit"); len(got) != nativeEdits || got[len(got)-1] != "hello" {
		t.Errorf("textEdited: %q, native edits %d", got, nativeEdits)
	}
}

// W3 — Password masks as WithMask does; the placeholder shows while empty.
func TestW3EchoModeAndPlaceholderMatchTheNativeOptions(t *testing.T) {
	native := widget.NewTextInput(widget.WithMask('•'))
	be := runNative(t, native)
	rec := &recorder{}
	s := runQML(t, "import tui 1.0\nTextField { focus: true; echoMode: TextInput.Password }", rec)
	be.Inject(decltest.Type("abc")...)
	s.Keys(t, decltest.Type("abc")...)
	s.WaitForText(t, "•••")
	waitFor(t, "the native mask", func() bool { return strings.Contains(be.String(), "•••") })
	if strings.Contains(s.String(), "abc") {
		t.Errorf("Password echoed the text:\n%s", s.String())
	}
	if firstRow(s.String()) != firstRow(be.String()) {
		t.Errorf("the rows differ:\nQML    %q\nnative %q", firstRow(s.String()), firstRow(be.String()))
	}

	p := runQML(t, "import tui 1.0\nTextField { placeholderText: \"type here\" }", rec)
	p.WaitForText(t, "type here")
}

// W3b — `text` is a runtime property: bound, it follows its source.
func TestW3TextFollowsItsBinding(t *testing.T) {
	s := decltest.Run(t, 40, 4,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nTextField { text: App.value }")),
		tuidecl.Types(controls.Types()...),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.value": "first"}))
	s.WaitForText(t, "first")
	done := make(chan struct{})
	s.Program.Post(func() {
		if err := s.Program.Set("App.value", "second"); err != nil {
			t.Error(err)
		}
		close(done)
	})
	<-done
	s.WaitForText(t, "second")
}

const promptDoc = "import tui 1.0\nimport demo 1.0\nWindow {\n Editor { id: ed; focus: true }\n" +
	" Shortcut { sequence: \"Ctrl+P\"; onActivated: prompt.open() }\n" +
	" Popup { id: prompt; modal: true; onClosed: App.edit(\"closed\")\n  TextField { placeholderText: \"command\"; onAccepted: App.run(text) } } }"

// W4 — a command prompt: a modal Popup holding a TextField, opened by a key,
// taking the keyboard, answering, and giving the keyboard back to the editor.
func TestW4APopupIsACommandPrompt(t *testing.T) {
	rec := &recorder{}
	s := runQML(t, promptDoc, rec)
	s.Keys(t, decltest.Type("ix")...)
	s.WaitForText(t, "x")

	s.Keys(t, decltest.Ctrl('p'))
	s.WaitForText(t, "command")
	s.Keys(t, decltest.Type("wq")...)
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	waitFor(t, "the command", func() bool { return len(rec.got("run")) == 1 })
	if got := rec.got("run")[0]; got != "wq" {
		t.Errorf("the prompt ran %q, want wq", got)
	}

	// Escape closes it, and the keyboard is the editor's again.
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "the prompt closing", func(sc string) bool { return !strings.Contains(sc, "command") })
	// Closed by Escape is closed: the signal says so, as close() would.
	waitFor(t, "closed raised by Escape", func() bool { return len(rec.got("edit")) == 1 })
	s.Keys(t, decltest.Type("iy")...)
	var value string
	waitFor(t, "typing in the editor again", func() bool {
		done := make(chan struct{})
		s.Program.Post(func() {
			ed, _ := tuidecl.FindAs[*widget.Editor](s.Program, "ed")
			value = ed.Value()
			close(done)
		})
		<-done
		return strings.Contains(value, "y")
	})
	if strings.Contains(value, "wq") {
		t.Errorf("the prompt's keys reached the editor: %q", value)
	}
}

// W4b — open and close by id, and the signals they raise.
func TestW4APopupOpensAndClosesByID(t *testing.T) {
	rec := &recorder{}
	s := runQML(t, "import tui 1.0\nimport demo 1.0\nWindow {\n Text { text: \"body\" }\n"+
		" Popup { id: p; onOpened: App.run(\"opened\"); onClosed: App.edit(\"closed\")\n  Text { text: \"floating\" } } }", rec)
	s.WaitForText(t, "body")
	call := func(m string) {
		done := make(chan struct{})
		s.Program.Post(func() {
			if err := s.Program.Call("p", m); err != nil {
				t.Error(err)
			}
			close(done)
		})
		<-done
	}
	call("open")
	s.WaitForText(t, "floating")
	call("open") // opening an open popup is nothing
	call("close")
	s.WaitFor(t, "the popup closing", func(sc string) bool { return !strings.Contains(sc, "floating") })
	if o, c := rec.got("run"), rec.got("edit"); len(o) != 1 || len(c) != 1 {
		t.Errorf("opened %v, closed %v: want one each", o, c)
	}
}

// W5 — refusals, by name and position.
func TestW5WhatTheControlsRefuse(t *testing.T) {
	for doc, want := range map[string]string{
		"TextField { colour: 1 }":                             "colour",
		"TextField { echoMode: TextInput.Pasword }":           "TextInput.Pasword",
		"TextField { echoMode: \"Password\" }":                "TextInput.Normal, TextInput.Password",
		"TextField { onFoo: App.run() }":                      "has no signal foo",
		"TextField { Text { } }":                              "takes no children",
		"Window { Text { }\n Popup { Text { }\n Text { } } }": "exactly one child",
		"Window { Text { }\n Popup { modal: 1\n Text { } } }": "modal",
	} {
		_, err := tuidecl.NewProgram(
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+doc)),
			tuidecl.Types(controls.Types()...),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Handlers(map[string]decl.HandlerFunc{"App.run": (&recorder{}).handler("run")}),
			tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(20, 4))))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", doc, err, want)
		}
		if err != nil && !strings.Contains(err.Error(), "main.qml:") {
			t.Errorf("%s: the refusal is not placed in the file: %v", doc, err)
		}
	}
}

// TestAPopupOutsideAWindowNeedsAnOverlay: as a Dialog does.
func TestAPopupOutsideAWindowNeedsAnOverlay(t *testing.T) {
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithErrorSink(func(error) {}), tuidecl.WithTypes(controls.Types()...))...)
	tr := decl.New(a)
	spec, _ := qml.QML{}.Parse([]byte("import tui 1.0\nPopup { id: p; Text { } }"))
	if err := tr.Mount(spec); err != nil {
		t.Fatal(err)
	}
	id, _ := tr.NodeByID("p")
	if err := a.Invoke(id, "open", nil); err == nil || !strings.Contains(err.Error(), "WithOverlay") {
		t.Fatalf("err = %v", err)
	}
}

// TestTheControlsWearTheirParentsPalette: a TextField in a coloured card wears
// the card's base — the trap palette propagation exists for, closed for a
// consumer type through Type.Restyle.
func TestTheControlsWearTheirParentsPalette(t *testing.T) {
	s := runQML(t, "import tui 1.0\nFlex { palette.base: \"blue\"; palette.text: \"white\"\n TextField { text: \"in the card\" } }", &recorder{})
	s.WaitForText(t, "in the card")
	s.WaitFor(t, "the field on blue", func(string) bool {
		row := s.Backend.Snapshot()[0]
		return row[0].Attrs.BG == tui.CellColor{Kind: tui.CellColorANSI, Index: 4}
	})
}

// TestARefusedPopupLeavesNoLayerOnTheOverlay: the Popup attaches its layer
// while it is being built; a declaration refused after that (a signal it
// does not raise) must release it, or every refused mount or reload leaves
// one more layer on the host.
func TestARefusedPopupLeavesNoLayerOnTheOverlay(t *testing.T) {
	host := widget.NewOverlayHost(widget.NewText("base"))
	layers := func() int {
		n := 0
		for range host.All() {
			n++
		}
		return n
	}
	before := layers()
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithErrorSink(func(error) {}), tuidecl.WithOverlay(host), tuidecl.WithTypes(controls.Types()...))...)
	tr := decl.New(a)
	if err := tuidecl.InjectHosts(tr, tuidecl.HostFuncs{"go": func() error { return nil }}); err != nil {
		t.Fatal(err)
	}
	spec, _ := qml.QML{}.Parse([]byte("import tui 1.0\nPopup { onFoo: go(); Text { text: \"x\" } }"))
	if err := tr.Mount(spec); err == nil || !strings.Contains(err.Error(), "has no signal foo") {
		t.Fatalf("mount: %v", err)
	}
	if got := layers(); got != before {
		t.Fatalf("the overlay has %d layers after a refused Popup, want %d", got, before)
	}
}

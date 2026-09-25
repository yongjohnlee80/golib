package decl_test

import (
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// component_ids_test.go: ids inside a component are the component's own —
// Qt's component scope — and a handler reads a field's property by its id.

// signIn is a component with ids of its own: a field it reads when Enter is
// pressed in it, through the id.
var signIn = fstest.MapFS{
	"ui/SignIn.qml": {Data: []byte("Flex { direction: Tui.Vertical\n" +
		" TextField { id: user; onAccepted: App.login(user.text) } }")},
}

// logins records the sign-ins the handler saw; the handler runs on the UI
// loop and the test reads from its own goroutine.
type logins struct {
	mu  sync.Mutex
	got []string
}

func (l *logins) add(s string) { l.mu.Lock(); l.got = append(l.got, s); l.mu.Unlock() }
func (l *logins) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

const twoSignIns = "import tui 1.0\nimport demo 1.0\nimport demo.ui 1.0\n" +
	"Flex { direction: Tui.Vertical\n SignIn { id: first }\n SignIn { id: second } }"

func runSignIns(t *testing.T, got *logins) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 30, 6,
		tuidecl.LayoutSource("main.qml", []byte(twoSignIns)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Components(signIn, "ui", "demo.ui", "1.0"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.login": func(args []qml.SpecValue) error {
			got.add(args[0].Raw)
			return nil
		}}))
}

// TestEachInstanceReadsItsOwnField: two uses of one component, each with its
// own `user` — Enter in either signs in with THAT field's text.
func TestEachInstanceReadsItsOwnField(t *testing.T) {
	got := &logins{}
	s := runSignIns(t, got)
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}) // into the first field
	s.Keys(t, decltest.Type("ann")...)
	s.WaitForText(t, "ann")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "the first sign-in", func(string) bool { return len(got.all()) == 1 })
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	s.Keys(t, decltest.Type("bob")...)
	s.WaitForText(t, "bob")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "the second sign-in", func(string) bool { return len(got.all()) == 2 })
	if g := got.all(); g[0] != "ann" || g[1] != "bob" {
		t.Fatalf("signed in as %q, want each field's own text [ann bob]", g)
	}
}

// TestTheDocumentCannotReachAnIdInsideAComponent: `user` is SignIn's; the
// document using SignIn names only the instance.
func TestTheDocumentCannotReachAnIdInsideAComponent(t *testing.T) {
	err := tuidecl.Check(
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nimport demo.ui 1.0\n"+
			"Flex { SignIn { id: first }\n Button { text: \"x\"; onClicked: App.login(user.text) } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Components(signIn, "ui", "demo.ui", "1.0"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.login": func([]qml.SpecValue) error { return nil }}))
	if err == nil || !strings.Contains(err.Error(), "user.text") {
		t.Fatalf("err = %v, want the document's read of a component's id refused", err)
	}
}

// TestAComponentsRootIdIsItsInstance: a Dialog component naming itself `me`
// closes THIS instance from inside, whatever the document calls it.
func TestAComponentsRootIdIsItsInstance(t *testing.T) {
	ui := fstest.MapFS{"ui/Note.qml": {Data: []byte("Dialog { id: me; title: \"note\"\n" +
		" Text { text: \"body\" }\n Shortcut { sequence: \"x\"; onActivated: me.close() } }")}}
	s := decltest.Run(t, 40, 10,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo.ui 1.0\n"+
			"Window {\n Text { text: \"under\" }\n Note { id: a }\n Note { } }")),
		tuidecl.Components(ui, "ui", "demo.ui", "1.0"))
	s.WaitForText(t, "under")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("a", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "┌ note ")
	s.Keys(t, decltest.Rune('x'))
	s.WaitFor(t, "the instance closed itself", func(sc string) bool { return !strings.Contains(sc, "┌ note ") })
}

// TestReadingAPropertyTheTypeDoesNotOfferIsRefused names what can be read.
func TestReadingAPropertyTheTypeDoesNotOfferIsRefused(t *testing.T) {
	err := tuidecl.Check(
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"Flex { TextField { id: f }\n Button { text: \"x\"; onClicked: App.go(f.placeholderText) } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.go": func([]qml.SpecValue) error { return nil }}))
	if err == nil || !strings.Contains(err.Error(), "readable properties text") {
		t.Fatalf("err = %v, want the unreadable property refused, naming text", err)
	}
}

// TestAUseAddedBeforeANamedInstanceLeavesItsFieldsAlone: the ids inside a
// named instance come from its name, so a reload that adds a use BEFORE it
// patches the instance rather than rebuilding it — what was typed stays.
func TestAUseAddedBeforeANamedInstanceLeavesItsFieldsAlone(t *testing.T) {
	got := &logins{}
	s := runSignIns(t, got)
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	s.Keys(t, decltest.Type("kept")...)
	s.WaitForText(t, "kept")
	var rebuilt int
	onScreenLoop(t, s, func() {
		res, err := s.Program.Reload([]byte("import tui 1.0\nimport demo 1.0\nimport demo.ui 1.0\n" +
			"Flex { direction: Tui.Vertical\n SignIn { id: zero }\n SignIn { id: first }\n SignIn { id: second } }"))
		if err != nil {
			t.Error(err)
		}
		rebuilt = len(res.Rebuilt)
	})
	s.WaitFor(t, "three fields, the typed text kept", func(sc string) bool { return strings.Contains(sc, "kept") })
	time.Sleep(20 * time.Millisecond)
	if !strings.Contains(s.String(), "kept") || rebuilt != 0 {
		t.Fatalf("rebuilt %d nodes; the typed text %v:\n%s", rebuilt, strings.Contains(s.String(), "kept"), s.String())
	}
}

package decl_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// methods_test.go covers `id.method()` in a handler, and the `|` operator for
// flags. Both are what a QML Dialog is written with:
//
//	MenuItem { onTriggered: quitDialog.open() }
//	Dialog { id: quitDialog; standardButtons: Dialog.Yes | Dialog.No }

// methodAdapter is the recorder with methods: every "Dialog" has open and close.
type methodAdapter struct {
	*recorder
	calls []string
}

func (m *methodAdapter) MethodsOf(typeName string) []string {
	if typeName == "Dialog" {
		return []string{"open", "close"}
	}
	return nil
}

func (m *methodAdapter) Invoke(node decl.NodeID, method string, args []qml.SpecValue) error {
	var vals []string
	for _, a := range args {
		vals = append(vals, a.Raw)
	}
	m.calls = append(m.calls, fmt.Sprintf("%s %d(%s)", method, node, strings.Join(vals, ",")))
	return nil
}

func methodTree(t *testing.T, src string) (*decl.Tree, *methodAdapter, error) {
	t.Helper()
	a := &methodAdapter{recorder: newRecorder()}
	tr := decl.New(a)
	return tr, a, tr.Mount(qmlDoc(t, src))
}

// TestAHandlerCallsAMethodOnTheNodeItsIDNames: the call reaches the adapter,
// for the node with that id, with its arguments evaluated.
func TestAHandlerCallsAMethodOnTheNodeItsIDNames(t *testing.T) {
	tr, a, err := methodTree(t, "Flex {\n Button { onClicked: quit.open(\"now\") }\n Dialog { id: quit }\n}")
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	dlg, ok := tr.NodeByID("quit")
	if !ok {
		t.Fatal("no node has the id quit")
	}
	var button decl.NodeID
	for node, em := range a.emitters {
		if em["clicked"] != nil {
			button = node
		}
	}
	if len(a.calls) != 0 {
		t.Fatalf("a method ran before its signal: %v", a.calls)
	}
	if err := a.emitters[button]["clicked"](); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if want := fmt.Sprintf("open %d(now)", dlg); len(a.calls) != 1 || a.calls[0] != want {
		t.Fatalf("calls = %v, want [%s]", a.calls, want)
	}
}

// TestAMethodTheTypeLacksIsRefusedAtMount, naming what it does have — found
// while the tree is intact, not when the user first presses the button.
func TestAMethodTheTypeLacksIsRefusedAtMount(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"Flex {\n Button { onClicked: quit.shake() }\n Dialog { id: quit }\n}", "open, close"},
		{"Flex {\n Button { onClicked: box.open() }\n Box { id: box }\n}", "no methods"},
		{"Flex {\n Button { onClicked: quit.x.open() }\n Dialog { id: quit }\n}", "quit.<method>()"},
	} {
		tr, a, err := methodTree(t, c.src)
		if !errors.Is(err, decl.ErrNoMethod) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want ErrNoMethod mentioning %q", c.src, err, c.want)
		}
		if tr.Root() != decl.NoNode || len(a.calls) != 0 {
			t.Errorf("%q: the refused document built something", c.src)
		}
	}
}

// TestAnIDMayNotShadowAReachableName: `x.open()` must not mean whichever the
// resolver tried first.
func TestAnIDMayNotShadowAReachableName(t *testing.T) {
	a := &methodAdapter{recorder: newRecorder()}
	tr := decl.New(a)
	if err := tr.Inject("quit.open", decl.Handle(func([]qml.SpecValue) error { return nil })); err != nil {
		t.Fatal(err)
	}
	err := tr.Mount(qmlDoc(t, "Flex {\n Button { onClicked: quit.open() }\n Dialog { id: quit }\n}"))
	if !errors.Is(err, decl.ErrAmbiguousName) {
		t.Fatalf("err = %v, want ErrAmbiguousName", err)
	}
	_, _, err = methodTree(t, "import tui 1.0\nFlex {\n Dialog { id: Tui }\n}")
	if !errors.Is(err, decl.ErrAmbiguousName) {
		t.Fatalf("an id spelling an import: err = %v, want ErrAmbiguousName", err)
	}
}

// TestAMethodCallFollowsTheIDAcrossAReload: resolved when the signal fires, so
// the node the id names NOW is the one called.
func TestAMethodCallFollowsTheIDAcrossAReload(t *testing.T) {
	tr, a, err := methodTree(t, "Flex {\n Button { id: go; onClicked: quit.open() }\n Dialog { id: quit }\n}")
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	before, _ := tr.NodeByID("quit")
	// The dialog changes type-for-type in place of a Box: a NEW node under the
	// same id, which a call resolved at compile time would miss.
	if _, err := tr.Reconcile(qmlDoc(t, "Flex {\n Button { id: go; onClicked: quit.open() }\n Box { }\n Dialog { id: quit }\n}")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, err := tr.Reconcile(qmlDoc(t, "Flex {\n Button { id: go; onClicked: quit.open() }\n Dialog { id: quit }\n Box { }\n}")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	dlg, _ := tr.NodeByID("quit")
	if dlg == before {
		t.Fatalf("fixture: the dialog kept node %d across the reloads; this test needs a new node", dlg)
	}
	btn, _ := tr.NodeByID("go")
	a.calls = nil
	if err := a.emitters[btn]["clicked"](); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if want := fmt.Sprintf("open %d()", dlg); len(a.calls) != 1 || a.calls[0] != want {
		t.Fatalf("calls = %v, want [%s]", a.calls, want)
	}
}

// ---------------------------------------------------------------- `|`

func flagTree(t *testing.T, src string) (*decl.Tree, *recorder, error) {
	t.Helper()
	a := newReactor()
	r := a.recorder
	tr := decl.New(a)
	num := func(s string) qml.SpecValue { return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: s} }
	for name, v := range map[string]qml.SpecValue{
		"Flag.A": num("1"), "Flag.B": num("4"), "Flag.C": num("0x10"),
		"Flag.Half": num("0.5"), "Flag.Name": sv("x"),
	} {
		if err := tr.Inject(name, decl.Constant(v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.DeclareSource("mask", num("2")); err != nil {
		t.Fatal(err)
	}
	return tr, r, tr.Mount(qmlDoc(t, src))
}

func TestFlagsCombineWithBitwiseOr(t *testing.T) {
	for src, want := range map[string]string{
		`Text { text: Flag.A | Flag.B }`:          "number(5)",
		`Text { text: Flag.A | Flag.B | Flag.C }`: "number(21)",
		`Text { text: Flag.B | 1 }`:               "number(5)",
	} {
		_, r, err := flagTree(t, src)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if !strings.Contains(strings.Join(r.trace, "\n"), "text="+want) {
			t.Errorf("%s: trace %v, want text=%s", src, r.trace, want)
		}
	}
}

// TestASourceInAFlagOperandIsTracked: the operand reads a source, so the whole
// value is a binding and moves when the source does.
func TestASourceInAFlagOperandIsTracked(t *testing.T) {
	tr, r, err := flagTree(t, `Text { text: Flag.A | mask }`)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	r.trace = nil
	if _, err := tr.SetSource("mask", qml.SpecValue{Kind: qml.SpecValueNumber, Raw: "8"}); err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	if !strings.Contains(strings.Join(r.trace, "\n"), "text=number(9)") {
		t.Fatalf("after mask=8, trace %v, want text=number(9)", r.trace)
	}
}

func TestOnlyIntegersCombine(t *testing.T) {
	for src, want := range map[string]string{
		`Text { text: Flag.A | Flag.Half }`: "not an integer",
		`Text { text: Flag.A | Flag.Name }`: "string",
		`Text { text: Flag.A | "x" }`:       "string",
	} {
		_, _, err := flagTree(t, src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", src, err, want)
		}
	}
	// Every other operator is still the evaluator's limit, reported as such.
	if _, _, err := flagTree(t, `Text { text: Flag.A + Flag.B }`); !errors.Is(err, decl.ErrExpressionValue) {
		t.Errorf("`+`: err = %v, want ErrExpressionValue", err)
	}
}

package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// attached_test.go covers ATTACHED PROPERTIES — `Dock.edge` written on a child
// and read by its parent.
//
// Every refusal is asserted with the tree untouched, because the whole point
// of judging these during planning is that a misplaced one is found before
// anything is built.

func mountDoc(t *testing.T, src string) (*decl.Tree, error) {
	t.Helper()
	spec, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithErrorSink(func(error) {}))...)
	tr := decl.New(a)
	return tr, tr.Mount(spec)
}

func TestAnAttachedPropertyIsJudgedAgainstItsParentBeforeAnythingIsBuilt(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{
			name:    "a parent that does not read Dock.*",
			src:     "import tui 1.0\nFlex {\n  Text { Dock.edge: Tui.Top }\n}",
			wantMsg: "does not read Dock.*",
		},
		{
			name:    "the root, which has no parent to read it",
			src:     "import tui 1.0\nWindow {\n  Dock.edge: Tui.Top\n  Text { }\n}",
			wantMsg: "root node",
		},
		{
			name:    "a member the schema does not declare",
			src:     "import tui 1.0\nWindow {\n  Text { Dock.side: Tui.Top }\n}",
			wantMsg: `"Dock" has no attached property "side"`,
		},
		{
			name:    "a schema nobody declared",
			src:     "import tui 1.0\nWindow {\n  Text { Layout.fillWidth: true }\n}",
			wantMsg: `no attaching schema named "Layout"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr, err := mountDoc(t, c.src)
			if !errors.Is(err, decl.ErrAttached) {
				t.Fatalf("err = %v, want ErrAttached", err)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("diagnostic = %q, want it to contain %q", err, c.wantMsg)
			}
			if tr.Len() != 0 {
				t.Errorf("%d nodes were built for a document refused at planning", tr.Len())
			}
		})
	}
}

// TestAnAttachedPropertyCannotBeBound.
//
// The parent reads it once, when it is built, so a binding could never
// propagate. Refused rather than accepted and silently frozen.
func TestAnAttachedPropertyCannotBeBound(t *testing.T) {
	spec, err := qml.QML{}.Parse([]byte("import tui 1.0\nWindow {\n  Text { Dock.edge: where }\n}"))
	if err != nil {
		t.Fatal(err)
	}
	a := tuidecl.New(tuidecl.StdRegistry(), tuidecl.StdProperties()...)
	tr := decl.New(a)
	if err := tr.Inject("where", decl.SourceValue(qml.SpecValue{Kind: qml.SpecValueString, Raw: "top"})); err != nil {
		t.Fatal(err)
	}
	err = tr.Mount(spec)
	if !errors.Is(err, decl.ErrAttached) {
		t.Fatalf("err = %v, want ErrAttached", err)
	}
	if !strings.Contains(err.Error(), "cannot be bound") {
		t.Errorf("diagnostic = %q, want it to say why", err)
	}
}

// TestAnAttachedPropertyIsNotAppliedToTheChild: the child's builder never sees
// it and no setter is looked for, which is what lets a Text carry Dock.edge
// without having a property of that name.
func TestAnAttachedPropertyIsNotAppliedToTheChild(t *testing.T) {
	_, err := mountDoc(t, "import tui 1.0\nWindow {\n  Text { Dock.edge: Tui.Bottom; text: \"hi\" }\n}")
	if err != nil {
		t.Fatalf("a Text docked by an attached property did not mount: %v", err)
	}
}

// TestAMisplacedAttachedPropertyIsRefusedOnReloadToo — the second entry point.
func TestAMisplacedAttachedPropertyIsRefusedOnReloadToo(t *testing.T) {
	tr, err := mountDoc(t, "import tui 1.0\nWindow {\n  Text { Dock.edge: Tui.Top }\n}")
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	spec, _ := qml.QML{}.Parse([]byte("import tui 1.0\nWindow {\n  Flex { Text { Dock.edge: Tui.Top } }\n}"))
	if _, err := tr.Reconcile(spec); !errors.Is(err, decl.ErrAttached) {
		t.Errorf("Reconcile: err = %v, want ErrAttached", err)
	}
}

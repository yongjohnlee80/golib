package decl_test

import (
	"errors"
	"github.com/yongjohnlee80/golib/parse/qml"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// acceptance_test.go holds ONE claim, stated as the requirement was given:
//
//	configure our TUI ui elements using QML syntax, given that we provide
//	custom modules from TUI, but the syntax must be correct in QML format; we
//	will provide a custom tui import so that it fits and works natively with
//	our own TUI package.
//
// Every other test in this package proves a mechanism. This one proves the
// PRODUCT: a document an author would actually write, mounted, painted on a
// real backend, reacting to a real event.
//
// It deliberately does not reach for a fake anywhere. A test that asserts on
// widget fields can pass while nothing reaches the screen, and "the tree was
// constructed" has never been the claim.

// theScreen is written the way QML is written: an import line, a qualified
// enum from that module, styling taken from an injected object rather than
// hardcoded, and a handler that calls a function the host provided.
const theScreen = `import tui 1.0
import myapp.theme 1.0

Split {
    id: root
    orientation: Tui.Horizontal

    Flex {
        direction: Tui.Vertical
        Text { text: Theme.heading }
        Text { text: Theme.body }
        Button {
            text: "Save"
            enabled: true
            onClicked: save()
        }
    }

    Text { text: "detail pane" }
}`

// TestAQMLDocumentConfiguresARealScreen is the acceptance cell.
func TestAQMLDocumentConfiguresARealScreen(t *testing.T) {
	spec, err := qml.QML{}.Parse([]byte(theScreen))
	if err != nil {
		t.Fatalf("a correct QML document did not parse: %v", err)
	}

	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithErrorSink(func(err error) { t.Errorf("handler error: %v", err) }),
	)...)
	tr := decl.New(a)

	// THE QML WAY OF STYLING. A palette is a singleton published by a module,
	// and a document imports the module and reads its properties. There is no
	// stylesheet and no sigil — the sigil this grammar used to have was a
	// second spelling for exactly this.
	if err := tr.DeclareModule(decl.Module{
		Name: "myapp.theme", Version: "1.0", Exports: []string{"Theme"},
	}); err != nil {
		t.Fatalf("DeclareModule: %v", err)
	}

	// The host hands over exactly what the document may reach: two styling
	// values and one effect. Nothing else in the host is nameable from QML,
	// and that is the boundary rather than a convention.
	var saved int
	for name, v := range map[string]string{
		"Theme.heading": "Project Atlas",
		"Theme.body":    "three files changed",
	} {
		if err := tr.Inject(name, decl.SourceValue(qml.SpecValue{
			Kind: qml.SpecValueString, Raw: v,
		})); err != nil {
			t.Fatalf("inject %s: %v", name, err)
		}
	}
	if err := tuidecl.InjectHosts(tr, tuidecl.HostFuncs{
		"save": func() error { saved++; return nil },
	}); err != nil {
		t.Fatalf("InjectHosts: %v", err)
	}

	if err := tr.Mount(spec); err != nil {
		t.Fatalf("the document did not mount: %v", err)
	}

	be, app := startApp(t, mustRoot(t, tr, a))
	waitFor(t, func() bool { return strings.Contains(be.String(), "Project Atlas") })

	// 1. The document is on the screen — including the values that came from
	//    the injected object rather than from the file.
	painted := be.String()
	for _, want := range []string{"Project Atlas", "three files changed", "Save", "detail pane"} {
		if !strings.Contains(painted, want) {
			t.Errorf("the screen does not show %q:\n%s", want, painted)
		}
	}

	// 2. A real activation on the real widget runs the host's function. A
	//    builder that wired the emitter to nothing passes every structural
	//    assertion above and fails here.
	btn := findButton(t, tr, a)
	var activated bool
	onLoop(t, app, func() { activated = btn.Activate(tui.OriginProgrammatic) })
	if !activated {
		t.Fatal("the button refused a programmatic activation")
	}
	if saved != 1 {
		t.Fatalf("the host function ran %d times, want 1", saved)
	}

	// 3. Styling is LIVE. Moving the injected value repaints the screen, which
	//    is what "imported or injected in a QML way" has to mean if it is to be
	//    worth more than a constant substituted once at mount.
	var res decl.PropagationResult
	onLoop(t, app, func() {
		res, err = tr.SetSource("Theme.heading", qml.SpecValue{
			Kind: qml.SpecValueString, Raw: "Project Borealis",
		})
	})
	if err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	if res.Applied != 1 {
		t.Fatalf("PropagationResult = %+v, want one application", res)
	}
	waitFor(t, func() bool { return strings.Contains(be.String(), "Project Borealis") })

	repainted := be.String()
	if strings.Contains(repainted, "Project Atlas") {
		t.Errorf("the old heading is still on the screen:\n%s", repainted)
	}
	// The value that did NOT change is still there — a repaint that redrew
	// everything would hide a propagation that fanned out too widely.
	if !strings.Contains(repainted, "three files changed") {
		t.Errorf("the untouched text vanished on a repaint:\n%s", repainted)
	}
}

// TestTheDocumentIsHELDToQMLRules is the other half of the acceptance claim.
//
// "The syntax must be correct in QML format" is only a requirement if being
// incorrect is refused. Each row below is a document that a real QML runtime
// would reject, and the assertion is that this stack rejects it too — with a
// diagnostic naming the line rather than a generic failure.
func TestTheDocumentIsHELDToQMLRules(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr error
		wantMsg string
	}{
		{
			name:    "a singleton used without importing its module",
			src:     "Flex {\n  direction: Tui.Vertical\n}",
			wantErr: decl.ErrNotImported,
			wantMsg: "import tui",
		},
		{
			name:    "a module no host provides",
			src:     "import QtQuick 2.15\nFlex { }",
			wantErr: decl.ErrUndefinedModule,
			wantMsg: "QtQuick",
		},
		{
			name:    "a handler named instead of called",
			src:     "import tui 1.0\nButton {\n  onClicked: save\n}",
			wantErr: decl.ErrHandlerBody,
			wantMsg: "save()",
		},
		{
			name:    "a name the host never handed over",
			src:     "import tui 1.0\nText {\n  text: secrets.apiKey\n}",
			wantErr: decl.ErrNotInjected,
			wantMsg: "secrets",
		},
		{
			name:    "an effect where a value belongs",
			src:     "import tui 1.0\nText {\n  text: save()\n}",
			wantErr: decl.ErrWrongKind,
			wantMsg: "a handler performs an effect",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec, err := qml.QML{}.Parse([]byte(c.src))
			if err != nil {
				t.Fatalf("the fixture must be well-FORMED QML; only its meaning is wrong: %v", err)
			}
			a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
				tuidecl.WithErrorSink(func(error) {}),
			)...)
			tr := decl.New(a)
			if err := tuidecl.InjectHosts(tr, tuidecl.HostFuncs{
				"save": func() error { return nil },
			}); err != nil {
				t.Fatalf("InjectHosts: %v", err)
			}

			err = tr.Mount(spec)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("diagnostic = %q, want it to contain %q", err, c.wantMsg)
			}
			var se decl.SchemaError
			if !errors.As(err, &se) || se.Pos.Line == 0 {
				t.Errorf("err = %v, want a position an author can navigate to", err)
			}
		})
	}
}

package decl_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// The screen this package exists to prove it can build.
//
// Every element is here because it breaks something: the Split cannot be built
// after the fact, the Button's callback has no setter, the Flex takes children
// only afterwards, and the Text carries a property that must travel the runtime
// path instead of the constructor.
const screen = `Split {
    id: root
    orientation: horizontal
    Flex {
        direction: vertical
        Text { text: "left pane" }
        Button {
            label: "Save"
            enabled: true
            onClicked: save
        }
    }
    Text { text: "right pane" }
}`

func mount(t *testing.T, src string, hosts tuidecl.HostFuncs, sink func(error)) (*decl.Tree, *tuidecl.Adapter) {
	t.Helper()
	spec, err := parse.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("schema does not parse: %v", err)
	}
	opts := append(tuidecl.StdSetters(),
		tuidecl.WithHostFuncs(hosts),
		tuidecl.WithErrorSink(sink),
	)
	a := tuidecl.New(tuidecl.StdRegistry(), opts...)
	tr := decl.New(a)
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	return tr, a
}

// TestARealScreenBuildsAndRenders is the claim the whole seam rests on: a
// schema becomes live widgets that a real App will lay out and paint.
//
// It renders through the actual runtime rather than asserting on the widget
// values, because "the tree was constructed" and "the tree can be shown" are
// different claims and only the second one matters to a user.
func TestARealScreenBuildsAndRenders(t *testing.T) {
	var saved int
	tr, a := mount(t, screen,
		tuidecl.HostFuncs{"save": func() error { saved++; return nil }},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })

	root, ok := a.Component(tr.Root())
	if !ok {
		t.Fatal("the root node built no component")
	}
	if _, isSplit := root.(*widget.Split); !isSplit {
		t.Fatalf("root is %T, want *widget.Split — the constructor that needs its children up front", root)
	}

	// Drive it through a real App and a real backend.
	be, app := startApp(t, root)
	waitFor(t, func() bool { return strings.Contains(be.String(), "left pane") })
	_ = app

	painted := be.String()
	for _, want := range []string{"left pane", "right pane", "Save"} {
		if !strings.Contains(painted, want) {
			t.Errorf("the screen does not show %q:\n%s", want, painted)
		}
	}
	_ = saved
}

// TestAWidgetEventReachesTheSchemaHandler closes the loop: a real activation on
// a real Button runs the function the schema named.
//
// This is the half a construction-only test cannot reach. A builder could wire
// the emitter to nothing and every structural assertion above would still pass.
func TestAWidgetEventReachesTheSchemaHandler(t *testing.T) {
	var ran int
	tr, a := mount(t, screen,
		tuidecl.HostFuncs{"save": func() error { ran++; return nil }},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })

	btn := findButton(t, tr, a)
	_, app := startApp(t, mustRoot(t, tr, a))

	// On the LOOP GOROUTINE. Activate mutates loop-owned state, and calling it
	// from the test goroutine while the loop runs is a data race — the same one
	// this repository had to fix in its own widget tests.
	var activated bool
	onLoop(t, app, func() { activated = btn.Activate(tui.OriginProgrammatic) })
	if !activated {
		t.Fatal("the button refused a programmatic activation")
	}
	if ran != 1 {
		t.Errorf("the schema handler ran %d times, want 1 — the widget event never reached it", ran)
	}
}

// TestAHandlerErrorReachesTheSink. A toolkit callback is shaped func() and has
// nowhere to put an error, so without an explicit destination a failing handler
// is indistinguishable from one that worked.
func TestAHandlerErrorReachesTheSink(t *testing.T) {
	boom := errors.New("the host refused")
	var got []error
	tr, a := mount(t, screen,
		tuidecl.HostFuncs{"save": func() error { return boom }},
		func(err error) { got = append(got, err) })

	btn := findButton(t, tr, a)
	_, app := startApp(t, mustRoot(t, tr, a))
	onLoop(t, app, func() { btn.Activate(tui.OriginProgrammatic) })

	if len(got) != 1 {
		t.Fatalf("the sink received %d errors, want 1 — a failing handler was silently dropped", len(got))
	}
	if !errors.Is(got[0], boom) {
		t.Errorf("sink got %v, want the host's error", got[0])
	}
}

// TestConstructorOnlyPropertiesAreNotReApplied. Split's orientation has no
// setter at all, so the builder must report it consumed and the engine must
// skip it. If either half slips, the mount fails outright — which is what makes
// this a real check rather than a restatement of the code.
func TestConstructorOnlyPropertiesAreNotReApplied(t *testing.T) {
	// A registry whose Split builder FORGETS to report the consumed property.
	forgetful := tuidecl.NewRegistry()
	tuidecl.Register(forgetful, "Split", func(b tuidecl.Build) (tui.Component, []string, error) {
		if len(b.Children) != 2 {
			return nil, nil, errors.New("need two children")
		}
		return widget.NewSplit(widget.Horizontal, b.Children[0], b.Children[1]), nil, nil
	})
	tuidecl.Register(forgetful, "Text", func(b tuidecl.Build) (tui.Component, []string, error) {
		return widget.NewText(""), nil, nil
	})

	spec, err := parse.QML{}.Parse([]byte(`Split { orientation: horizontal Text { } Text { } }`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := tuidecl.New(forgetful, tuidecl.StdSetters()...)
	if err := decl.New(a).Mount(spec); err == nil {
		t.Error("a constructor-only property that was not reported as consumed still mounted; " +
			"it should have been applied and found no setter")
	}

	// The real registry reports it, so the same schema mounts.
	if _, _ = mount(t, `Split { orientation: horizontal Text { } Text { } }`,
		tuidecl.HostFuncs{}, func(error) {}); true {
		// mount fatals on failure; reaching here is the assertion.
	}
}

// TestAnUnregisteredTypeIsAPositionedError: a schema is input, so naming a type
// nobody registered is the author's mistake to see, with the line.
func TestAnUnregisteredTypeIsAPositionedError(t *testing.T) {
	spec, err := parse.QML{}.Parse([]byte("Split {\n  Nope { }\n  Text { }\n}"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := tuidecl.New(tuidecl.StdRegistry(), tuidecl.WithHostFuncs(tuidecl.HostFuncs{}))
	mountErr := decl.New(a).Mount(spec)
	if mountErr == nil {
		t.Fatal("an unregistered type mounted")
	}
	var se decl.SchemaError
	if !errors.As(mountErr, &se) {
		t.Fatalf("error %T, want decl.SchemaError", mountErr)
	}
	if se.Pos.Line != 2 {
		t.Errorf("error at line %d, want 2 — where Nope is written", se.Pos.Line)
	}
	if !strings.Contains(mountErr.Error(), "Nope") {
		t.Errorf("error does not name the type: %v", mountErr)
	}
}

func findButton(t *testing.T, tr *decl.Tree, a *tuidecl.Adapter) *widget.Button {
	t.Helper()
	for id := decl.NodeID(1); id <= decl.NodeID(tr.Len()+4); id++ {
		if c, ok := a.Component(id); ok {
			if b, isBtn := c.(*widget.Button); isBtn {
				return b
			}
		}
	}
	t.Fatal("no Button was built")
	return nil
}

func mustRoot(t *testing.T, tr *decl.Tree, a *tuidecl.Adapter) tui.Component {
	t.Helper()
	c, ok := a.Component(tr.Root())
	if !ok {
		t.Fatal("no root component")
	}
	return c
}

// startApp runs a real App over a TestBackend and stops it at cleanup.
func startApp(t *testing.T, root tui.Component) (*tui.TestBackend, *tui.App) {
	t.Helper()
	be := tui.NewTestBackend(60, 12)
	app := tui.NewApp(root, tui.WithBackend(be), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("the app did not stop")
		}
	})
	return be, app
}

// onLoop runs fn on the App's loop goroutine and waits, which is the only legal
// way for a test to touch component state while the loop is running.
func onLoop(t *testing.T, app *tui.App, fn func()) {
	t.Helper()
	done := make(chan struct{})
	app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the loop did not run the update")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 200 {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the screen never painted")
}

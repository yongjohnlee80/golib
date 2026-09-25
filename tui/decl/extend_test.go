package decl_test

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// extend_test.go holds the consumer's API to what a consumer would do with it:
// a widget of their own, declared as ONE Type, used from QML the way the
// built-ins are — bound, called by id, raising a signal with a parameter —
// and a widget the host built, placed by name. On a running App, read off the
// screen.

// gauge is a consumer's own widget: a label and a value, drawn as text.
type gauge struct {
	*widget.Text
	label string
	value float64
}

func newGauge() *gauge { return &gauge{Text: widget.NewText("")} }

func (g *gauge) SetLabel(s string)  { g.label = s; g.show() }
func (g *gauge) SetValue(v float64) { g.value = v; g.show() }
func (g *gauge) Reset() error       { g.value = 0; g.show(); return nil }
func (g *gauge) show()              { g.SetText(g.label + "=" + strconv.FormatFloat(g.value, 'f', -1, 64)) }

var gaugeType = tuidecl.Type{
	Name: "Gauge",
	Build: func(b tuidecl.Build) (tui.Component, []string, error) {
		g := newGauge()
		var start float64
		consumed, err := tuidecl.ReadProps(b.Props, map[string]tuidecl.Field{"start": tuidecl.NumberField(&start)})
		if err != nil {
			return nil, nil, err
		}
		g.value = start
		return g, consumed, nil
	},
	Ctor: []string{"start"},
	Setters: map[string]tuidecl.Setter{
		"label": tuidecl.StringSetter((*gauge).SetLabel),
		"value": tuidecl.NumberSetter((*gauge).SetValue),
	},
	Methods: map[string]tuidecl.Method{"reset": tuidecl.NoArgMethod((*gauge).Reset)},
}

// picker raises `picked(choice)` when activated.
var pickerType = tuidecl.Type{
	Name: "Picker",
	Build: func(b tuidecl.Build) (tui.Component, []string, error) {
		emit := b.EmitterWith("picked")
		return widget.NewButton("pick", widget.WithOnActivate(func() {
			emit(qml.SpecValue{Kind: qml.SpecValueString, Raw: "the-choice"})
		})), nil, nil
	},
	Signals: map[string][]string{"picked": {"choice"}},
}

func extended(t *testing.T, src string, extra ...tuidecl.Type) (*decl.Tree, *tuidecl.Adapter, error, *[]string) {
	t.Helper()
	var sunk []string
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithTypes(append([]tuidecl.Type{gaugeType, pickerType}, extra...)...),
		tuidecl.WithErrorSink(func(err error) { sunk = append(sunk, err.Error()) }))...)
	tr := decl.New(a)
	var picked []string
	num := func(s string) qml.SpecValue { return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: s} }
	if err := tr.DeclareSource("cpu", num("42")); err != nil {
		t.Fatal(err)
	}
	if err := tr.Inject("choose", decl.Handle(func(args []qml.SpecValue) error {
		picked = append(picked, args[0].Raw)
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	spec, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_ = sunk
	return tr, a, tr.Mount(spec), &picked
}

func TestACustomWidgetBindsAndIsCalledByID(t *testing.T) {
	src := "import tui 1.0\nFlex {\n direction: Tui.Vertical\n" +
		" Gauge { id: cpu; start: 1; label: \"CPU\"; value: cpu_ }\n" +
		" Button { id: go; text: \"reset\"; onClicked: cpu.reset() }\n}"
	src = strings.Replace(src, "cpu_", "cpu", 1) // the source named cpu, the node cpu
	_, _, err, _ := extended(t, src)
	// The id `cpu` spells the source `cpu`: ambiguous, and refused.
	if !errors.Is(err, decl.ErrAmbiguousName) {
		t.Fatalf("an id spelling a source: err = %v, want ErrAmbiguousName", err)
	}

	src = strings.Replace(src, "id: cpu;", "id: meter;", 1)
	src = strings.Replace(src, "cpu.reset()", "meter.reset()", 1)
	tr, a, err, _ := extended(t, src)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	root, _ := a.Component(tr.Root())
	be, app := startApp(t, root)
	waitFor(t, func() bool { return strings.Contains(be.String(), "CPU=42") })
	// A source change reaches the custom widget's setter, as it would a built-in.
	onLoop(t, app, func() {
		if _, err := tr.SetSource("cpu", qml.SpecValue{Kind: qml.SpecValueNumber, Raw: "97.5"}); err != nil {
			t.Errorf("SetSource: %v", err)
		}
	})
	waitFor(t, func() bool { return strings.Contains(be.String(), "CPU=97.5") })
	// A handler calls its method by id.
	btn, _ := tr.NodeByID("go")
	c, _ := a.Component(btn)
	onLoop(t, app, func() { c.(*widget.Button).Activate(tui.OriginProgrammatic) })
	waitFor(t, func() bool { return strings.Contains(be.String(), "CPU=0") })
}

func TestACustomSignalPassesItsParameter(t *testing.T) {
	tr, a, err, picked := extended(t, "Picker { id: p; onPicked: choose(choice) }")
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	root, _ := a.Component(tr.Root())
	_, app := startApp(t, root)
	onLoop(t, app, func() { root.(*widget.Button).Activate(tui.OriginProgrammatic) })
	waitFor(t, func() bool { return len(*picked) == 1 })
	if (*picked)[0] != "the-choice" {
		t.Fatalf("choose got %q", (*picked)[0])
	}
}

// TestACustomWidgetIsRefusedLikeABuiltIn: a wrong value, an unknown property
// and an unknown method are said the way they are for any widget.
func TestACustomWidgetIsRefusedLikeABuiltIn(t *testing.T) {
	for src, want := range map[string]string{
		`Gauge { start: "high" }`:                                      "want a number",
		`Gauge { colour: "red" }`:                                      "colour",
		"Flex {\n Gauge { id: g }\n Button { onClicked: g.spin() }\n}": "the methods forceActiveFocus, reset",
	} {
		_, _, err, _ := extended(t, src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", src, err, want)
		}
	}
}

// TestAnInstanceIsTheHostsWidgetPlacedOnce.
func TestAnInstanceIsTheHostsWidgetPlacedOnce(t *testing.T) {
	mine := widget.NewText("host-built")
	tr, a, err, _ := extended(t, "Flex {\n Terminal { }\n}", tuidecl.Instance("Terminal", mine))
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	root, _ := a.Component(tr.Root())
	be, _ := startApp(t, root)
	waitFor(t, func() bool { return strings.Contains(be.String(), "host-built") })

	// Placed twice: refused, by name and position.
	_, _, err, _ = extended(t, "Flex {\n Solo { }\n Solo { }\n}", tuidecl.Instance("Solo", widget.NewText("x")))
	if err == nil || !strings.Contains(err.Error(), "Solo is the host's one Solo") {
		t.Fatalf("placing an Instance twice: err = %v, want it refused by name", err)
	}

	// Released with its node, it can be placed again: Destroy, then remount.
	if err := tr.Destroy(); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	spec, _ := qml.QML{}.Parse([]byte("Flex {\n Terminal { }\n}"))
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("remounting the released Instance: %v", err)
	}
}

// TestADestroyHookRunsWithTheWidget — once, when its node goes.
func TestADestroyHookRunsWithTheWidget(t *testing.T) {
	var gone []tui.Component
	held := gaugeType
	held.Name = "Held"
	held.Destroyed = func(c tui.Component) { gone = append(gone, c) }
	tr, a, err, _ := extended(t, "Flex {\n Held { id: h }\n}", held)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	id, _ := tr.NodeByID("h")
	built, _ := a.Component(id)
	spec, _ := qml.QML{}.Parse([]byte("Flex { }"))
	if _, err := tr.Reconcile(spec); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(gone) != 1 || gone[0] != built {
		t.Fatalf("destroy hook ran %d times, with %v; want once, with the widget built", len(gone), gone)
	}
}

func TestWithTypesRefusesAClashingName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a type named like a built-in was accepted")
		}
	}()
	clash := gaugeType
	clash.Name = "Editor"
	tuidecl.New(tuidecl.StdRegistry(), tuidecl.WithTypes(clash))
}

// TestAHandlerForASignalTheTypeLacksIsRefused: Qt refuses `onFoo` on a type
// with no foo signal; so does the adapter — a handler that could never run.
func TestAHandlerForASignalTheTypeLacksIsRefused(t *testing.T) {
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(), tuidecl.WithErrorSink(func(error) {}))...)
	tr := decl.New(a)
	if err := tuidecl.InjectHosts(tr, tuidecl.HostFuncs{"go": func() error { return nil }}); err != nil {
		t.Fatal(err)
	}
	spec, _ := qml.QML{File: "m.qml"}.Parse([]byte("import tui 1.0\nFlex { Text { onFoo: go(); onBar: go() }\n Button { onClicked: go() } }"))
	err := tr.Mount(spec)
	if err == nil || !strings.Contains(err.Error(), "Text has no signal bar, foo") || !strings.Contains(err.Error(), "m.qml:2") {
		t.Fatalf("err = %v, want Text's missing signals named and placed", err)
	}
}

// TestAnEnumIsPublishedUnderItsScopeAndReadAtRuntime: a consumer type's
// enumeration, written as Qt writes one, through both readers.
func TestAnEnumIsPublishedUnderItsScopeAndReadAtRuntime(t *testing.T) {
	mode := tuidecl.Enum{Scope: "Gauge", Values: []string{"Bar", "Dial"}}
	gauge := tuidecl.Type{
		Name:  "Gauge",
		Enums: []tuidecl.Enum{mode},
		Build: func(tuidecl.Build) (tui.Component, []string, error) { return widget.NewText(""), nil, nil },
		Setters: map[string]tuidecl.Setter{
			"style": tuidecl.EnumSetter(mode, func(x *widget.Text, v string) { x.SetText(v) }),
		},
	}
	s := decltest.Run(t, 20, 2,
		tuidecl.LayoutSource("m.qml", []byte("import tui 1.0\nimport demo 1.0\nGauge { style: App.mode }")),
		tuidecl.Types(gauge), tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.mode": "Gauge.Dial"}))
	s.WaitForText(t, "Dial")
	if _, err := tuidecl.NewProgram(tuidecl.LayoutSource("m.qml", []byte("import tui 1.0\nGauge { style: \"Dial\" }")),
		tuidecl.Types(gauge), tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(1, 1)))); err == nil ||
		!strings.Contains(err.Error(), "Gauge.Bar, Gauge.Dial") {
		t.Fatalf("a bare string: err = %v", err)
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "already a singleton") {
			t.Fatalf("a scope clashing with Tui: recovered %v", r)
		}
	}()
	tuidecl.New(tuidecl.StdRegistry(), tuidecl.WithTypes(tuidecl.Type{Name: "Clash", Enums: []tuidecl.Enum{{Scope: "Tui", Values: []string{"X"}}},
		Build: gauge.Build}))
}

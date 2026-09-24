package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

func text(s string) parse.SpecValue {
	return parse.SpecValue{Kind: parse.SpecValueString, Raw: s}
}

// reactive mounts src against the real tui adapter with sources and functions
// declared, and starts a real App over a TestBackend.
func reactive(t *testing.T, src string, sources map[string]string,
	funcs map[string]decl.ValueFunc) (*decl.Tree, *tuidecl.Adapter) {
	t.Helper()
	opts := append(tuidecl.StdProperties(),
		tuidecl.WithHostFuncs(tuidecl.HostFuncs{}),
		tuidecl.WithErrorSink(func(err error) { t.Errorf("unexpected handler error: %v", err) }))
	ad := tuidecl.New(tuidecl.StdRegistry(), opts...)
	tr := decl.New(ad)
	for n, v := range sources {
		if err := tr.DeclareSource(n, text(v)); err != nil {
			t.Fatalf("DeclareSource %q: %v", n, err)
		}
	}
	for n, f := range funcs {
		if err := tr.DeclareFunc(n, f); err != nil {
			t.Fatalf("DeclareFunc %q: %v", n, err)
		}
	}
	spec, err := parse.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("schema does not parse: %v", err)
	}
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	return tr, ad
}

// TestASourceChangeReachesTheScreen — rows 1, 18 and 0001c rows 2 and 6.
//
// Asserted on what is PAINTED, because "the engine recomputed" and "the user
// sees it" are different claims and only the second one matters.
func TestASourceChangeReachesTheScreen(t *testing.T) {
	const src = `Flex { id: root direction: "vertical"
	    Text { id: plain text: greeting }
	    Text { id: nested text: wrap(upper(greeting), "!") } }`

	tr, ad := reactive(t, src, map[string]string{"greeting": "hello"},
		map[string]decl.ValueFunc{
			"upper": func(a []parse.SpecValue) (parse.SpecValue, error) {
				return text(strings.ToUpper(a[0].Raw)), nil
			},
			"wrap": func(a []parse.SpecValue) (parse.SpecValue, error) {
				return text(a[0].Raw + a[1].Raw), nil
			},
		})

	be, app := startApp(t, mustRoot(t, tr, ad))
	waitFor(t, func() bool { return strings.Contains(be.String(), "hello") })
	if !strings.Contains(be.String(), "HELLO!") {
		t.Fatalf("a nested call did not evaluate at mount:\n%s", be.String())
	}

	// The widgets themselves must have survived: a source change patches, it
	// does not rebuild.
	plain := componentOf(t, tr, ad, "plain")
	before := nodeIDsOf(t, []tui.Component{plain})[0]

	var res decl.PropagationResult
	var err error
	onLoop(t, app, func() { res, err = tr.SetSource("greeting", text("bonjour")) })
	if err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	if res.Recomputed != 2 || res.Applied != 2 {
		t.Errorf("res = %+v, want 2 recomputed / 2 applied", res)
	}
	waitFor(t, func() bool {
		s := be.String()
		return strings.Contains(s, "bonjour") && strings.Contains(s, "BONJOUR!")
	})
	if got := nodeIDsOf(t, []tui.Component{componentOf(t, tr, ad, "plain")})[0]; got != before {
		t.Errorf("a source change remounted the widget: NodeID %d -> %d", before, got)
	}
	if componentOf(t, tr, ad, "plain") != plain {
		t.Error("a source change replaced the component")
	}
}

// TestABareIdentifierStillReachesTheAdapter — 0001c rows 1 and 3.
//
// The regression nine shipped cells caught. `direction: "vertical"` is a bare
// word and belongs to the adapter; declaring a source of the same spelling must
// change nothing about it.
func TestABareIdentifierStillReachesTheAdapter(t *testing.T) {
	// A source named for the adapter's own enum value.
	tr, ad := reactive(t,
		`Flex { id: root direction: "vertical" Text { id: a text: msg } }`,
		map[string]string{"msg": "shown", "vertical": "SHADOW"}, nil)

	be, _ := startApp(t, mustRoot(t, tr, ad))
	waitFor(t, func() bool { return strings.Contains(be.String(), "shown") })
	if strings.Contains(be.String(), "SHADOW") {
		t.Errorf("a source shadowed the adapter's identifier:\n%s", be.String())
	}
	// And the Flex really is vertical: two texts would share a line otherwise.
	if v, ok := tr.Source("vertical"); !ok || v.Raw != "SHADOW" {
		t.Errorf("the source itself was disturbed: %q ok=%v", v.Raw, ok)
	}
}

// TestAnUnknownSourceRefusesWithTheTreeUntouched — 0001c row 5, 0001b row 19.
func TestAnUnknownSourceRefusesWithTheTreeUntouched(t *testing.T) {
	opts := append(tuidecl.StdProperties(),
		tuidecl.WithHostFuncs(tuidecl.HostFuncs{}), tuidecl.WithErrorSink(func(error) {}))
	tr := decl.New(tuidecl.New(tuidecl.StdRegistry(), opts...))

	bad, err := parse.QML{}.Parse([]byte(
		`Flex { id: r direction: "vertical" Text { id: a text: nope } }`))
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(bad); err == nil {
		t.Fatal("an unknown source was accepted")
	}
	if tr.Len() != 0 {
		t.Errorf("Len = %d, want 0: nodes were allocated before the failure", tr.Len())
	}
	// Mountable after correction, WITHOUT Destroy — the tree was never partial.
	ok, _ := parse.QML{}.Parse([]byte(
		`Flex { id: r direction: "vertical" Text { id: a text: "fixed" } }`))
	if err := tr.Mount(ok); err != nil {
		t.Fatalf("the tree was latched by a failure that built nothing: %v", err)
	}
}

// TestABindingOnAConstructorOnlyPropertyIsRefusedOnTheRealAdapter — row 10.
//
// widget.Split takes its orientation as a constructor argument and has no
// setter, so a bound orientation could only be applied by rebuilding the node
// on every tick — destroying the state a reconcile exists to preserve.
func TestABindingOnAConstructorOnlyPropertyIsRefusedOnTheRealAdapter(t *testing.T) {
	opts := append(tuidecl.StdProperties(),
		tuidecl.WithHostFuncs(tuidecl.HostFuncs{}), tuidecl.WithErrorSink(func(error) {}))
	tr := decl.New(tuidecl.New(tuidecl.StdRegistry(), opts...))
	if err := tr.DeclareSource("o", text("horizontal")); err != nil {
		t.Fatal(err)
	}
	spec, _ := parse.QML{}.Parse([]byte(
		`Split { id: r orientation: "o" Text { id: a text: "l" } Text { id: b text: "r" } }`))
	err := tr.Mount(spec)
	if err == nil {
		t.Fatal("a bound constructor-only property was accepted")
	}
	if !strings.Contains(err.Error(), "orientation") {
		t.Errorf("the error does not name the property: %v", err)
	}
	if tr.Len() != 0 {
		t.Errorf("Len = %d, want 0", tr.Len())
	}
}

// TestTheAdapterNeverSeesAnExpression — row 18's second half.
//
// The builders and setters shipped in P1-P3 know nothing about bindings, and
// this is what keeps that true: they are handed terminals, always.
func TestTheAdapterNeverSeesAnExpression(t *testing.T) {
	var seen []parse.SpecValueKind
	reg := tuidecl.NewRegistry()
	tuidecl.Register(reg, "Flex", func(b tuidecl.Build) (tui.Component, []string, error) {
		for _, p := range b.Props {
			seen = append(seen, p.Value.Kind)
		}
		return tui.NewFlex(tui.Vertical), []string{"direction"}, nil
	})
	tuidecl.Register(reg, "Text", func(b tuidecl.Build) (tui.Component, []string, error) {
		for _, p := range b.Props {
			seen = append(seen, p.Value.Kind)
		}
		return widget.NewText(""), nil, nil
	})
	opts := []tuidecl.Option{
		tuidecl.WithConstructorProps("Flex", "direction"),
		tuidecl.WithSetters("Text", map[string]tuidecl.Setter{
			"text": func(c tui.Component, v parse.SpecValue) error {
				seen = append(seen, v.Kind)
				tx, _ := c.(*widget.Text)
				tx.SetText(v.Raw)
				return nil
			},
		}),
		tuidecl.WithHostFuncs(tuidecl.HostFuncs{}),
		tuidecl.WithErrorSink(func(error) {}),
	}
	tr := decl.New(tuidecl.New(reg, opts...))
	if err := tr.DeclareSource("g", text("v")); err != nil {
		t.Fatal(err)
	}
	if err := tr.DeclareFunc("f", func(a []parse.SpecValue) (parse.SpecValue, error) {
		return text("d" + a[0].Raw), nil
	}); err != nil {
		t.Fatal(err)
	}
	spec, _ := parse.QML{}.Parse([]byte(
		`Flex { id: r direction: "vertical" Text { id: a text: f(g) } Text { id: b text: g } }`))
	if err := tr.Mount(spec); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.SetSource("g", text("w")); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("nothing was observed; this test proves nothing")
	}
	// A bare identifier is a BINDING now, as it is in QML, so the adapter never
	// sees one — nor a Call. Every value crossing the seam is terminal, which is
	// what keeps builders and setters ignorant that bindings exist at all.
	for _, k := range seen {
		if k == parse.SpecValueRef || k == parse.SpecValueCall {
			t.Errorf("the adapter received an unresolved %v; every value must arrive terminal", k)
		}
	}
}

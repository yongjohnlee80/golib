package decl_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// check_test.go holds Check to its promise: every document a program can load
// is judged, including the ones its layout never reads — and each problem is
// reported against what was mounted to find it.

const checkLayout = "import tui 1.0\nimport demo 1.0\nimport demo.theme.dark 1.0\nimport demo.ui 1.0\n" +
	"Window {\n Text { text: App.status; palette.window: Theme.bg; palette.windowText: Theme.fg }\n" +
	" Greeting { }\n}"

// checkFiles is a program with nothing wrong in it: two themes that define the
// same names, and two components — one of them unused.
func checkFiles() fstest.MapFS {
	return fstest.MapFS{
		"main.qml":         {Data: []byte(checkLayout)},
		"themes/dark.qml":  {Data: []byte(`Theme { bg: "black"; fg: "white" }`)},
		"themes/light.qml": {Data: []byte(`Theme { bg: "white"; fg: "black" }`)},
		"ui/Greeting.qml":  {Data: []byte(`Text { text: "hello" }`)},
		"ui/Farewell.qml":  {Data: []byte(`Text { text: App.status }`)},
		"extra/Other.qml":  {Data: []byte(`Text { text: "other" }`)},
	}
}

func checkOptions(files fstest.MapFS) []tuidecl.ProgramOption {
	return []tuidecl.ProgramOption{
		tuidecl.Layout(files, "main.qml"),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.status": "ready"}),
		tuidecl.Themes(files, "themes", "demo.theme", "1.0"),
		tuidecl.Components(files, "ui", "demo.ui", "1.0"),
		tuidecl.Components(files, "extra", "demo.extra", "1.0"),
	}
}

// checkFinds breaks one file, and requires that NewProgram — which reads only
// what the layout imports — still succeeds, while Check reports the file.
// Without the first half a test could pass because the layout itself broke.
func checkFinds(t *testing.T, file, src string, want ...string) {
	t.Helper()
	files := checkFiles()
	files[file] = &fstest.MapFile{Data: []byte(src)}
	opts := checkOptions(files)

	p, err := tuidecl.NewProgram(append(opts, tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(40, 6))))...)
	if err != nil {
		t.Fatalf("NewProgram refused the program, so this is not a file only Check reads: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = p.Run(ctx) // releases the tree

	err = tuidecl.Check(opts...)
	if err == nil {
		t.Fatalf("Check passed a program whose %s is broken", file)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("Check's report lacks %q:\n%v", w, err)
		}
	}
}

func TestCheckPassesASoundProgram(t *testing.T) {
	if err := tuidecl.Check(checkOptions(checkFiles())...); err != nil {
		t.Fatalf("Check refused a sound program: %v", err)
	}
}

// TestCheckMountsTheLayoutUnderTheOtherTheme: the theme the layout does not
// import lacks a name the layout reads. Switching to it is one import line,
// and that line would break the screen.
func TestCheckMountsTheLayoutUnderTheOtherTheme(t *testing.T) {
	checkFinds(t, "themes/light.qml", `Theme { bg: "white" }`,
		"main.qml with import demo.theme.light in place of demo.theme.dark", "Theme.fg")
}

// TestCheckPlacesAThemesSyntaxErrorInItsFile: a theme is data, and a broken one
// is reported where it is broken.
func TestCheckPlacesAThemesSyntaxErrorInItsFile(t *testing.T) {
	checkFinds(t, "themes/light.qml", `Theme { bg: "white"`,
		"module demo.theme.light", "themes/light.qml:1:")
}

// TestCheckMountsAComponentNothingUsesYet: Farewell is in a module the layout
// imports, but no screen uses it, so a mount never expands it.
func TestCheckMountsAComponentNothingUsesYet(t *testing.T) {
	checkFinds(t, "ui/Farewell.qml", `Text { txt: App.status }`,
		"component Farewell of demo.ui, which main.qml does not use", "txt")
}

// TestCheckResolvesAnUnusedComponentsNamesInTheLayoutsImports: a component
// imports nothing; its names are the using document's. One reading a name the
// layout's imports do not bring into scope fails where it would once used.
func TestCheckResolvesAnUnusedComponentsNamesInTheLayoutsImports(t *testing.T) {
	checkFinds(t, "ui/Farewell.qml", `Text { text: Nobody.status }`,
		"component Farewell of demo.ui", "Nobody")
}

// TestCheckLoadsAModuleNothingImports: demo.extra is imported by no document
// and replaces nothing; its own rules must still hold.
func TestCheckLoadsAModuleNothingImports(t *testing.T) {
	checkFinds(t, "extra/Other.qml", `Text { id: mine }`,
		"module demo.extra", "extra/Other.qml")
}

// TestCheckReportsEveryProblem: two broken files, two reports — a lint that
// stopped at the first would make fixing them a loop.
func TestCheckReportsEveryProblem(t *testing.T) {
	files := checkFiles()
	files["themes/light.qml"] = &fstest.MapFile{Data: []byte(`Theme { bg: "white" }`)}
	files["ui/Farewell.qml"] = &fstest.MapFile{Data: []byte(`Text { txt: "x" }`)}
	err := tuidecl.Check(checkOptions(files)...)
	if err == nil {
		t.Fatal("Check passed two broken files")
	}
	for _, w := range []string{"demo.theme.light in place of", "component Farewell"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("report lacks %q:\n%v", w, err)
		}
	}
}

// TestCheckRefusesABrokenLayout: the layout itself is judged too, and a
// configuration NewProgram would refuse, Check refuses.
func TestCheckRefusesABrokenLayout(t *testing.T) {
	files := checkFiles()
	files["main.qml"] = &fstest.MapFile{Data: []byte(strings.Replace(checkLayout, "text: App.status", "txt: App.status", 1))}
	err := tuidecl.Check(checkOptions(files)...)
	if err == nil || !strings.Contains(err.Error(), "main.qml") {
		t.Fatalf("Check on a broken layout: %v", err)
	}
	if err := tuidecl.Check(); err == nil {
		t.Fatal("Check with no layout passed")
	}
	files["main.qml"] = &fstest.MapFile{Data: []byte("Window {")}
	if err := tuidecl.Check(checkOptions(files)...); err == nil {
		t.Fatal("Check passed a layout that does not parse")
	}
}

// TestCheckReleasesWhatItMounts: every mount's providers are released — Check
// runs nothing, and leaves nothing running.
func TestCheckReleasesWhatItMounts(t *testing.T) {
	c := &countingProvider{}
	opts := append(checkOptions(checkFiles()), tuidecl.Providers(c))
	if err := tuidecl.Check(opts...); err != nil {
		t.Fatal(err)
	}
	// The layout, the light theme in place of the dark, Farewell alone, and
	// demo.extra's Other alone.
	if got := c.subs.Load(); got != 4 {
		t.Errorf("Check mounted %d times, want 4", got)
	}
	if c.subs.Load() != c.cancels.Load() {
		t.Errorf("%d subscriptions, %d released", c.subs.Load(), c.cancels.Load())
	}
}

// TestCheckMountsTheComponentsOfAModuleNothingImports: a module no document
// imports yet is still a module the program can load, and its components are
// judged — not only parsed.
func TestCheckMountsTheComponentsOfAModuleNothingImports(t *testing.T) {
	checkFinds(t, "extra/Other.qml", `Text { txt: "invalid" }`,
		"component Other of demo.extra, which main.qml does not use", "txt")
}

// TestCheckHostsAnUnusedComponentInANeutralRoot: the layout's root has
// construction rules of its own — a Split takes exactly two children — and an
// unused component must not be judged by them.
func TestCheckHostsAnUnusedComponentInANeutralRoot(t *testing.T) {
	files := fstest.MapFS{
		"main.qml":      {Data: []byte("import tui 1.0\nimport demo.ui 1.0\nSplit {\n Text { text: \"a\" }\n Text { text: \"b\" }\n}")},
		"ui/Unused.qml": {Data: []byte(`Text { text: "u" }`)},
	}
	opts := []tuidecl.ProgramOption{tuidecl.Layout(files, "main.qml"), tuidecl.Components(files, "ui", "demo.ui", "1.0")}
	if err := tuidecl.Check(opts...); err != nil {
		t.Fatalf("Check refused a sound Split layout: %v", err)
	}
	files["ui/Unused.qml"] = &fstest.MapFile{Data: []byte(`Text { txt: "u" }`)}
	if err := tuidecl.Check(opts...); err == nil || !strings.Contains(err.Error(), "component Unused") {
		t.Fatalf("a broken unused component under a Split layout: %v", err)
	}
	// A bar docked to an edge needs a parent to read Dock.edge — and a Split
	// layout has none to lend it.
	files["ui/Unused.qml"] = &fstest.MapFile{Data: []byte(`StatusBar { Dock.edge: Tui.Bottom }`)}
	if err := tuidecl.Check(opts...); err != nil {
		t.Fatalf("Check refused a sound docked bar: %v", err)
	}
	// And a Menu belongs in a MenuBar, never directly in a Window: alone, it is
	// sound.
	files["ui/Unused.qml"] = &fstest.MapFile{Data: []byte(`Menu { title: "&File" }`)}
	if err := tuidecl.Check(opts...); err != nil {
		t.Fatalf("Check refused a sound Menu component: %v", err)
	}
	// Refused everywhere, both refusals are reported.
	files["ui/Unused.qml"] = &fstest.MapFile{Data: []byte(`StatusBar { Dock.edge: Tui.Bottom; txt: "x" }`)}
	err := tuidecl.Check(opts...)
	if err == nil || !strings.Contains(err.Error(), "alone:") || !strings.Contains(err.Error(), "in a Window:") {
		t.Fatalf("a component refused in every placement: %v", err)
	}
}

// TestCheckReplacesEveryImportAnAlternativeClashesWith: a module can take the
// place of two imports at once. Swapping only one would leave the other beside
// it, exporting a name they share — refused by the engine, for Check's reasons.
func TestCheckReplacesEveryImportAnAlternativeClashesWith(t *testing.T) {
	files := fstest.MapFS{
		"main.qml": {Data: []byte("import tui 1.0\nimport demo.colours 1.0\nimport demo.shapes 1.0\n" +
			"Window { Text { text: Shapes.name; palette.window: Colours.bg } }")},
		"colours.qml": {Data: []byte(`Colours { bg: "black" }`)},
		"shapes.qml":  {Data: []byte(`Shapes { name: "square" }`)},
		"both.qml":    {Data: []byte("Colours { bg: \"white\" }")},
	}
	both := func() (decl.ModuleContents, error) {
		cols, err := decl.ValueFile(files, "both.qml")()
		if err != nil {
			return cols, err
		}
		shapes, err := decl.ValueFile(files, "shapes.qml")()
		if err != nil {
			return cols, err
		}
		for k, v := range shapes.Values {
			cols.Values[k] = v
		}
		cols.Exports = append(cols.Exports, shapes.Exports...)
		return cols, nil
	}
	err := tuidecl.Check(
		tuidecl.Layout(files, "main.qml"),
		tuidecl.Offer("demo.colours", "1.0", decl.ValueFile(files, "colours.qml")),
		tuidecl.Offer("demo.shapes", "1.0", decl.ValueFile(files, "shapes.qml")),
		tuidecl.Offer("demo.both", "1.0", both),
	)
	if err != nil {
		t.Fatalf("an alternative to both imports was judged beside one of them: %v", err)
	}
}

// TestCheckKeepsTheQualifierOfTheImportItReplaces: a layout writing
// `T.Theme.bg` reaches the alternative through the same qualifier.
func TestCheckKeepsTheQualifierOfTheImportItReplaces(t *testing.T) {
	files := checkFiles()
	files["main.qml"] = &fstest.MapFile{Data: []byte("import tui 1.0\nimport demo 1.0\nimport demo.theme.dark 1.0 as T\n" +
		"Window { Text { text: App.status; palette.window: T.Theme.bg } }")}
	if err := tuidecl.Check(checkOptions(files)...); err != nil {
		t.Fatalf("the alternative lost the qualifier: %v", err)
	}
	files["themes/light.qml"] = &fstest.MapFile{Data: []byte(`Theme { fg: "black" }`)}
	if err := tuidecl.Check(checkOptions(files)...); err == nil || !strings.Contains(err.Error(), "T.Theme.bg") {
		t.Fatalf("a light theme without bg, reached as T.Theme.bg: %v", err)
	}
}

// TestCheckMountsAComponentAloneWithoutAWindowType: a vocabulary of its own
// (WithRegistry) may have no Window; the component is then the root.
func TestCheckMountsAComponentAloneWithoutAWindowType(t *testing.T) {
	files := fstest.MapFS{
		"main.qml":      {Data: []byte("import tui 1.0\nimport demo.ui 1.0\nText { text: \"a\" }")},
		"ui/Unused.qml": {Data: []byte(`Text { txt: "u" }`)},
	}
	reg := tuidecl.NewRegistry()
	tuidecl.Register(reg, "Text", func(b tuidecl.Build) (tui.Component, []string, error) {
		for _, p := range b.Props {
			if p.Name != "text" {
				return nil, nil, fmt.Errorf("no property %s", p.Name)
			}
		}
		return widget.NewText("x"), []string{"text"}, nil
	})
	err := tuidecl.Check(tuidecl.WithRegistry(reg), tuidecl.Layout(files, "main.qml"),
		tuidecl.Components(files, "ui", "demo.ui", "1.0"))
	if err == nil || !strings.Contains(err.Error(), "no property txt") {
		t.Fatalf("Check under a registry without Window: %v", err)
	}
}

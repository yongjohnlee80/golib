package decl_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
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
	// The layout, the light theme in place of the dark, and Farewell alone.
	if got := c.subs.Load(); got != 3 {
		t.Errorf("Check mounted %d times, want 3", got)
	}
	if c.subs.Load() != c.cancels.Load() {
		t.Errorf("%d subscriptions, %d released", c.subs.Load(), c.cancels.Load())
	}
}

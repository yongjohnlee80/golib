package decltest_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// recorder is a testing.TB that keeps what it is told instead of failing, so a
// test can assert what decltest reports. Cleanup and the rest go to the real t.
type recorder struct {
	*testing.T
	mu     sync.Mutex
	errors []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.errors...)
}

var files = fstest.MapFS{
	"main.qml": {Data: []byte("import tui 1.0\nimport demo 1.0\nimport demo.theme.dark 1.0\n" +
		"Window {\n Shortcut { sequence: \"Ctrl+G\"; onActivated: App.greet() }\n" +
		" Text { text: App.greeting; palette.window: Theme.bg; palette.windowText: Theme.fg }\n}")},
	"themes/dark.qml":  {Data: []byte(`Theme { bg: "black"; fg: "white" }`)},
	"themes/light.qml": {Data: []byte(`Theme { bg: "white"; fg: "black" }`)},
}

func options(fsys fstest.MapFS, greet func() error) []tuidecl.ProgramOption {
	return []tuidecl.ProgramOption{
		tuidecl.Layout(fsys, "main.qml"),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.greeting": "waiting"}),
		tuidecl.Commands(map[string]func() error{"App.greet": greet}),
		tuidecl.Themes(fsys, "themes", "demo.theme", "1.0"),
	}
}

func TestCheckPassesASoundProgram(t *testing.T) {
	r := &recorder{T: t}
	decltest.Check(r, options(files, nil)...)
	if got := r.got(); len(got) > 0 {
		t.Fatalf("Check failed a sound program: %q", got)
	}
}

// TestCheckReportsEachProblemAsItsOwnError: one failure per broken document,
// so the test output is the list of what to fix.
func TestCheckReportsEachProblemAsItsOwnError(t *testing.T) {
	broken := fstest.MapFS{}
	for k, v := range files {
		broken[k] = v
	}
	broken["themes/light.qml"] = &fstest.MapFile{Data: []byte(`Theme { bg: "white" }`)}
	broken["themes/blue.qml"] = &fstest.MapFile{Data: []byte(`Theme { bg: "blue"`)}
	r := &recorder{T: t}
	decltest.Check(r, options(broken, nil)...)
	got := r.got()
	if len(got) != 2 {
		t.Fatalf("Check reported %d errors, want 2 — one per broken theme:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(strings.Join(got, "\n"), "themes/blue.qml") ||
		!strings.Contains(strings.Join(got, "\n"), "demo.theme.light") {
		t.Errorf("each broken theme should be named:\n%s", strings.Join(got, "\n"))
	}
}

// TestCheckReportsASingleErrorAsItIs: a configuration refused before any
// document is judged is one error, not a list.
func TestCheckReportsASingleErrorAsItIs(t *testing.T) {
	r := &recorder{T: t}
	decltest.Check(r)
	if got := r.got(); len(got) != 1 || !strings.Contains(got[0], "no layout") {
		t.Fatalf("Check with no layout reported %q", got)
	}
}

// TestRunDrivesTheScreen: the program runs, a key reaches its shortcut, the
// handler moves a source, and the screen shows it.
func TestRunDrivesTheScreen(t *testing.T) {
	var s *decltest.Screen
	s = decltest.Run(t, 40, 5, options(files, func() error { return s.Program.Set("App.greeting", "hello") })...)
	s.WaitForText(t, "waiting")
	s.Keys(t, decltest.Ctrl('g'))
	s.WaitForText(t, "hello")
}

// TestRunFailsTheTestOnAHandlerError: a handler's error has no caller, so the
// only place it can surface in a test is the test itself.
func TestRunFailsTheTestOnAHandlerError(t *testing.T) {
	r := &recorder{T: t}
	s := decltest.Run(r, 40, 5, options(files, func() error { return fmt.Errorf("greeting failed") })...)
	s.WaitForText(t, "waiting")
	s.Keys(t, decltest.Ctrl('g'))
	for deadline := time.Now().Add(decltest.WaitTimeout); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		for _, e := range r.got() {
			if strings.Contains(e, "greeting failed") {
				return
			}
		}
	}
	t.Fatalf("the handler's error never failed the test: %q", r.got())
}

// TestRunLetsTheOptionsKeepHandlerErrors: an ErrorSink in the options wins.
func TestRunLetsTheOptionsKeepHandlerErrors(t *testing.T) {
	r := &recorder{T: t}
	kept := make(chan error, 1)
	opts := append(options(files, func() error { return fmt.Errorf("expected") }),
		tuidecl.ErrorSink(func(err error) { kept <- err }))
	s := decltest.Run(r, 40, 5, opts...)
	s.WaitForText(t, "waiting")
	s.Keys(t, decltest.Ctrl('g'))
	select {
	case err := <-kept:
		if !strings.Contains(err.Error(), "expected") {
			t.Fatalf("kept %v", err)
		}
	case <-time.After(decltest.WaitTimeout):
		t.Fatalf("the options' own sink never received the error; the test got %q", r.got())
	}
	if got := r.got(); len(got) > 0 {
		t.Fatalf("the test failed on an error its own sink took: %q", got)
	}
}

func TestTypeIsOneKeyPerRune(t *testing.T) {
	evs := decltest.Type("hé")
	if len(evs) != 2 {
		t.Fatalf("Type(\"hé\") is %d events", len(evs))
	}
	if decltest.Alt('f').Mods == decltest.Ctrl('f').Mods {
		t.Fatal("Alt and Ctrl are the same modifier")
	}
}

// TestQuitClosesWhenTheProgramStops: App.quit ends the program, and the test
// can wait for it.
func TestQuitClosesWhenTheProgramStops(t *testing.T) {
	var s *decltest.Screen
	s = decltest.Run(t, 40, 5, options(files, func() error { s.Program.Quit(); return nil })...)
	s.WaitForText(t, "waiting")
	s.Keys(t, decltest.Ctrl('g'))
	select {
	case <-s.Quit():
	case <-time.After(decltest.WaitTimeout):
		t.Fatal("the program did not stop")
	}
}

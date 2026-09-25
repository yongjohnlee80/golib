package decl_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// program_test.go holds NewProgram to the one-call promise: a QML screen,
// state, commands, themes and components, running on a real App — and what it
// does with errors, which is never to drop them.

var programFiles = fstest.MapFS{
	"main.qml": {Data: []byte("import tui 1.0\nimport demo 1.0\nimport demo.theme.dark 1.0\n" +
		"Flex {\n direction: Tui.Vertical\n" +
		" Text { id: status; text: App.status; palette.window: Theme.bg; palette.windowText: Theme.fg }\n" +
		" Button { id: go; label: \"go\"; onClicked: App.go() }\n" +
		" Greeting { }\n}")},
	"themes/dark.qml":  {Data: []byte(`Theme { bg: "black"; fg: "white" }`)},
	"themes/light.qml": {Data: []byte(`Theme { bg: "white"; fg: "black"`)}, // broken: never read
	"ui/Greeting.qml":  {Data: []byte(`Text { text: "hello from a component" }`)},
}

func startProgram(t *testing.T, extra ...tuidecl.ProgramOption) (*tuidecl.Program, *tui.TestBackend, chan error) {
	t.Helper()
	be := tui.NewTestBackend(60, 8)
	var p *tuidecl.Program
	opts := append([]tuidecl.ProgramOption{
		tuidecl.Layout(programFiles, "main.qml"),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.status": "ready"}),
		tuidecl.Commands(map[string]func() error{"App.go": func() error { return p.Set("App.status", "went") }}),
		tuidecl.Themes(programFiles, "themes", "demo.theme", "1.0"),
		tuidecl.Components(programFiles, "ui", "demo.ui", "1.0"),
		tuidecl.AppOptions(tui.WithBackend(be), tui.WithMinFrameInterval(0)),
	}, extra...)
	var err error
	p, err = tuidecl.NewProgram(opts...)
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(cancel)
	return p, be, done
}

func screenHas(t *testing.T, be *tui.TestBackend, sub string) {
	t.Helper()
	for range 300 {
		if strings.Contains(be.String(), sub) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%q never appeared:\n%s", sub, be.String())
}

func TestAProgramRunsAQMLScreenInOneCall(t *testing.T) {
	// The component module is imported through a use of its type, so the layout
	// needs its import too.
	withImport := fstest.MapFS{}
	for k, v := range programFiles {
		withImport[k] = v
	}
	withImport["main.qml"] = &fstest.MapFile{Data: []byte(strings.Replace(string(programFiles["main.qml"].Data),
		"import demo 1.0\n", "import demo 1.0\nimport demo.ui 1.0\n", 1))}
	p, be, _ := startProgram(t, tuidecl.Layout(withImport, "main.qml"))
	screenHas(t, be, "ready")
	screenHas(t, be, "hello from a component")

	// A command, by its button: it sets a source, and the binding follows.
	btn, ok := tuidecl.FindAs[*widget.Button](p, "go")
	if !ok {
		t.Fatal("FindAs found no Button with id go")
	}
	p.Post(func() { btn.Activate(tui.OriginProgrammatic) })
	screenHas(t, be, "went")

	// Post + Set from another goroutine, as a worker would.
	go p.Post(func() { _ = p.Set("App.status", "from a worker") })
	screenHas(t, be, "from a worker")
}

// TestAProgramReadsOnlyTheImportedTheme: the broken light theme is offered and
// never read, so it breaks nothing.
func TestAProgramReadsOnlyTheImportedTheme(t *testing.T) {
	withImport := fstest.MapFS{}
	for k, v := range programFiles {
		withImport[k] = v
	}
	withImport["main.qml"] = &fstest.MapFile{Data: []byte(strings.Replace(
		strings.Replace(string(programFiles["main.qml"].Data), " Greeting { }\n", "", 1),
		"import demo 1.0\n", "import demo 1.0\n", 1))}
	_, be, _ := startProgram(t, tuidecl.Layout(withImport, "main.qml"))
	screenHas(t, be, "ready")
}

// TestAProgramNeverDropsAHandlerError: with no sink of its own, Run returns
// every handler error when it ends.
func TestAProgramNeverDropsAHandlerError(t *testing.T) {
	boom := errors.New("the save failed")
	src := []byte("import demo 1.0\nButton { id: go; label: \"go\"; onClicked: App.fail() }")
	p, err := tuidecl.NewProgram(
		tuidecl.LayoutSource("fail.qml", src),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Commands(map[string]func() error{"App.fail": func() error { return boom }}),
		tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(20, 3)), tui.WithMinFrameInterval(0)),
	)
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Run(context.Background()) }()
	btn, _ := tuidecl.FindAs[*widget.Button](p, "go")
	p.Post(func() { btn.Activate(tui.OriginProgrammatic); p.Quit() })
	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Fatalf("Run returned %v, want the handler's error in it", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Quit did not end Run")
	}
}

func TestAProgramRefusesWhatItCannotRun(t *testing.T) {
	for name, opts := range map[string][]tuidecl.ProgramOption{
		"no layout":    {tuidecl.Singleton("demo", "1.0", "App")},
		"missing file": {tuidecl.Layout(programFiles, "nope.qml")},
		"a bad value": {tuidecl.LayoutSource("x.qml", []byte("Text { }")),
			tuidecl.Sources(map[string]any{"App.x": []int{1}})},
		"a bad document": {tuidecl.LayoutSource("bad.qml", []byte("Text {"))},
	} {
		if _, err := tuidecl.NewProgram(opts...); err == nil {
			t.Errorf("%s: NewProgram succeeded", name)
		} else if name == "a bad document" && !strings.Contains(err.Error(), "bad.qml:1:") {
			t.Errorf("a bad document: %v, want it placed in bad.qml", err)
		}
	}
}

func TestCommandsRefuseArgumentsTheyDoNotTake(t *testing.T) {
	src := []byte("import demo 1.0\nButton { id: go; label: \"go\"; onClicked: App.noArgs(\"x\") }")
	var sunk []error
	p, err := tuidecl.NewProgram(
		tuidecl.LayoutSource("args.qml", src),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Commands(map[string]func() error{"App.noArgs": func() error { return nil }}),
		tuidecl.ErrorSink(func(err error) { sunk = append(sunk, err) }),
		tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(20, 3)), tui.WithMinFrameInterval(0)),
	)
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Run(context.Background()) }()
	btn, _ := tuidecl.FindAs[*widget.Button](p, "go")
	p.Post(func() { btn.Activate(tui.OriginProgrammatic); p.Quit() })
	<-done
	if len(sunk) != 1 || !strings.Contains(sunk[0].Error(), "takes no arguments") {
		t.Fatalf("sunk %v, want the argument refused", sunk)
	}
}

func TestValueHoldsOnlyWhatADocumentCan(t *testing.T) {
	for v, want := range map[any]string{"s": "s", true: "true", 7: "7", 2.5: "2.5"} {
		got, err := tuidecl.Value(v)
		if err != nil || got.Raw != want {
			t.Errorf("Value(%v) = %+v, %v", v, got, err)
		}
	}
	if _, err := tuidecl.Value(struct{}{}); err == nil {
		t.Error("a struct was accepted as a value")
	}
	if v, _ := tuidecl.Value(qml.SpecValue{Kind: qml.SpecValueBool, Raw: "true"}); v.Kind != qml.SpecValueBool {
		t.Error("a SpecValue was not passed through")
	}
}

// countingProvider delivers "Clock.now" and counts how often it was cancelled.
// burst makes it deliver from its own goroutine as fast as it can until then.
type countingProvider struct {
	burst    bool
	cancels  atomic.Int32
	stopping chan struct{}
}

func (c *countingProvider) Subscribe(fn func(decl.Update)) ([]string, func() error, error) {
	v := qml.SpecValue{Kind: qml.SpecValueString, Raw: "t"}
	fn(decl.Update{Version: 1, Values: map[string]qml.SpecValue{"Clock.now": v}})
	c.stopping = make(chan struct{})
	if c.burst {
		go func(stop chan struct{}) {
			for ver := uint64(2); ; ver++ {
				select {
				case <-stop:
					return
				default:
					fn(decl.Update{Version: ver, Values: map[string]qml.SpecValue{"Clock.now": v}})
				}
			}
		}(c.stopping)
	}
	var once sync.Once
	return []string{"Clock.now"}, func() error {
		once.Do(func() { c.cancels.Add(1); close(c.stopping) })
		return nil
	}, nil
}

// TestAFailedProgramReleasesItsProviders: a parse failure and a mount failure
// both come after the provider subscribed, and both must end it.
func TestAFailedProgramReleasesItsProviders(t *testing.T) {
	for name, src := range map[string]string{
		"parse": "Text {",
		"mount": "Text { nosuch: 1 }",
	} {
		c := &countingProvider{}
		_, err := tuidecl.NewProgram(tuidecl.LayoutSource(name+".qml", []byte(src)), tuidecl.Providers(c))
		if err == nil {
			t.Fatalf("%s: NewProgram succeeded", name)
		}
		if got := c.cancels.Load(); got != 1 {
			t.Errorf("%s failure: the provider was cancelled %d times, want 1", name, got)
		}
	}
}

// TestAProgramIsBuiltSafelyUnderAProvidersTicks: a provider delivering from
// its own goroutine the whole time the Program is constructed. Run with -race;
// the Program also still starts and stops.
func TestAProgramIsBuiltSafelyUnderAProvidersTicks(t *testing.T) {
	for range 5 {
		c := &countingProvider{burst: true}
		p, err := tuidecl.NewProgram(
			tuidecl.LayoutSource("tick.qml", []byte("Text { text: Clock.now }")),
			tuidecl.Providers(c),
			tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(10, 1)), tui.WithMinFrameInterval(0)),
		)
		if err != nil {
			t.Fatalf("NewProgram: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_ = p.Run(ctx)
		cancel()
		if c.cancels.Load() != 1 {
			t.Fatalf("Run's teardown cancelled the provider %d times, want 1", c.cancels.Load())
		}
	}
}

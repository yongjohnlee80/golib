package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// layout is the screen, in QML. The Go below never builds a widget: it
// publishes what the document may reach — the App singleton — and reacts when
// the document says something happened.
//
//go:embed editor.qml
var layout []byte

// themes are OFFERED, not declared: each is importable, and only the one the
// layout's import line names is ever parsed. A new theme is a file here and a
// row in this table.
//
//go:embed themes
var themeFiles embed.FS

var themes = map[string]string{
	"editor.theme.mono":  "themes/mono.qml",
	"editor.theme.retro": "themes/retro.qml",
}

// Host is the program behind editor.qml.
//
// The split mirrors the one the Go editor draws between golib's widgets and its
// own policy. The document owns STRUCTURE — what is on screen and what each
// control triggers. The host owns BEHAVIOUR — what "Save" does to a file, what
// the status line should say.
type Host struct {
	tree    *decl.Tree
	adapter *tuidecl.Adapter
	editor  *widget.Editor

	path  string
	dirty bool
	quit  func()

	clock *clock
}

// Options are what New needs from the program around it.
type Options struct {
	// Path is the file to open, or "" for an unnamed buffer.
	Path string
	// Schedule puts work on the goroutine that owns the UI — tui.App.Update.
	// The clock ticks from its own goroutine and must not touch the tree there.
	Schedule func(func())
	// Sink receives errors from handlers, which have no caller to return to.
	Sink func(error)
	// Quit ends the program.
	Quit func()
	// Now is the clock's source of time; nil means time.Now. A test fixes it.
	Now func() time.Time
	// Tick is how often the clock advances; zero means a second.
	Tick time.Duration
	// Layout replaces editor.qml; nil means the embedded one. A test uses it to
	// run the same screen under the other theme's import line.
	Layout []byte
}

func str(s string) qml.SpecValue { return qml.SpecValue{Kind: qml.SpecValueString, Raw: s} }

// New mounts editor.qml and returns the host and the component to run.
func New(opt Options) (*Host, tui.Component, error) {
	h := &Host{path: opt.Path, quit: opt.Quit}

	h.adapter = tuidecl.New(tuidecl.StdRegistry(),
		append(tuidecl.StdProperties(), tuidecl.WithErrorSink(opt.Sink))...)
	h.tree = decl.New(h.adapter,
		decl.WithScheduler(opt.Schedule),
		decl.WithProviderErrorSink(opt.Sink))

	// The `editor` module exports ONE singleton, App. Everything the document
	// can reach of this program is under that name — and nothing else of it is
	// reachable at all.
	if err := h.tree.DeclareModule(decl.Module{
		Name: "editor", Version: "1.0", Exports: []string{"App"},
	}); err != nil {
		return nil, nil, err
	}

	for module, file := range themes {
		if err := h.tree.OfferModule(module, "1.0", themeLoader(file)); err != nil {
			return nil, nil, err
		}
	}

	// State the document reads. Each is a SOURCE, so changing it repaints
	// exactly the bindings that read it.
	for name, v := range map[string]string{
		"App.mode":   widget.ModeNormal.String(),
		"App.status": displayPath(opt.Path),
		"App.keyset": "vim",
		// The quit dialog's question. A source, so the dialog says when there
		// is something to lose without the host reaching into it.
		"App.quitQuestion": quitQuestion(false),
	} {
		if err := h.tree.Inject(name, decl.SourceValue(str(v))); err != nil {
			return nil, nil, err
		}
	}

	// The clock is a PROVIDER: it owns App.clock and says when it moves. Its
	// ticks come from its own goroutine and reach the tree through Schedule.
	h.clock = newClock(opt.Now, opt.Tick)
	if err := h.tree.Subscribe(h.clock); err != nil {
		return nil, nil, err
	}

	// Commands the document can invoke.
	for name, fn := range map[string]func() error{
		"App.newFile":    h.newFile,
		"App.openFile":   h.openFile,
		"App.saveFile":   h.saveFile,
		"App.quit":       func() error { h.quit(); return nil },
		"App.useVim":     func() error { return h.useKeyset("vim", "switched keymap to Vim (modal)") },
		"App.useNano":    func() error { return h.useKeyset("nano", "switched keymap to Nano (modeless)") },
		"App.syncStatus": h.syncStatus,
		"App.markDirty":  h.markDirty,
	} {
		fn := fn
		if err := h.tree.Inject(name, decl.Handle(func([]qml.SpecValue) error { return fn() })); err != nil {
			return nil, nil, err
		}
	}

	src := layout
	if opt.Layout != nil {
		src = opt.Layout
	}
	spec, err := qml.QML{}.Parse(src)
	if err != nil {
		return nil, nil, fmt.Errorf("editor.qml: %w", err)
	}
	if err := h.tree.Mount(spec); err != nil {
		return nil, nil, fmt.Errorf("editor.qml: %w", err)
	}

	// The one widget the host reaches into: the editor, by the id the
	// document gave it.
	id, ok := h.tree.NodeByID("editor")
	if !ok {
		return nil, nil, errors.New("editor.qml declares no node with id: editor")
	}
	comp, _ := h.adapter.Component(id)
	if h.editor, ok = tuidecl.EditorOf(comp); !ok {
		return nil, nil, errors.New("editor.qml: id editor is not an Editor")
	}
	if err := h.load(opt.Path); err != nil {
		return nil, nil, err
	}

	root, _ := h.adapter.Component(h.tree.Root())
	return h, root, nil
}

// Close stops the clock and releases the tree.
func (h *Host) Close() error { return h.tree.Destroy() }

// ---------------------------------------------------------------- commands

func (h *Host) newFile() error {
	h.editor.SetValue("")
	h.path = ""
	if err := h.setDirty(false); err != nil {
		return err
	}
	return h.message("new buffer")
}

// openFile is not wired to a dialog yet. It says so, rather than doing nothing.
func (h *Host) openFile() error { return h.message("File → Open: not implemented yet") }

func (h *Host) saveFile() error {
	if h.path == "" {
		return h.message("no file name — nothing written")
	}
	if err := os.WriteFile(h.path, []byte(h.editor.Value()), 0o644); err != nil {
		return h.message("write failed: " + err.Error())
	}
	if err := h.setDirty(false); err != nil {
		return err
	}
	return h.message(fmt.Sprintf("%q written", h.path))
}

func (h *Host) useKeyset(ks, msg string) error {
	if _, err := h.tree.SetSources(map[string]qml.SpecValue{
		"App.keyset": str(ks),
		"App.status": str(msg),
	}); err != nil {
		return err
	}
	// A keyset switch can change the mode — Nano has no Normal mode — so the
	// status line is brought up to date with it.
	return h.syncStatus()
}

func (h *Host) syncStatus() error {
	_, err := h.tree.SetSource("App.mode", str(h.editor.Mode().String()))
	return err
}

func (h *Host) markDirty() error { return h.setDirty(true) }

// setDirty records whether the buffer has unsaved changes, and keeps the quit
// dialog's question saying so. Only a CHANGE is published: markDirty runs on
// every keystroke, and republishing an unchanged question would reevaluate its
// binding for nothing.
func (h *Host) setDirty(v bool) error {
	if h.dirty == v {
		return nil
	}
	h.dirty = v
	_, err := h.tree.SetSource("App.quitQuestion", str(quitQuestion(v)))
	return err
}

// quitQuestion is what the quit dialog asks.
func quitQuestion(dirty bool) string {
	if dirty {
		return "Are you sure to quit?\nUnsaved changes will be lost."
	}
	return "Are you sure to quit?"
}

func (h *Host) message(s string) error {
	_, err := h.tree.SetSource("App.status", str(s))
	return err
}

// load reads the file, if there is one.
//
// A path that does NOT exist is not an error: it opens empty and Save creates
// it, as vim does. A path that exists but cannot be read IS an error, because
// silently showing an empty buffer for a file that is there invites
// overwriting it.
func (h *Host) load(path string) error {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	h.editor.SetValue(string(b))
	return nil
}

func displayPath(p string) string {
	if p == "" {
		return "[No Name]"
	}
	return filepath.Base(p)
}

// ---------------------------------------------------------------- clock

// clock is a decl.Provider for App.clock.
//
// It delivers the current time synchronously when subscribed — the contract
// that closes the gap between reading a value and listening for changes — and
// then once per tick from its own goroutine, each delivery carrying a strictly
// increasing version so a late one cannot overwrite a newer one.
type clock struct {
	now  func() time.Time
	tick time.Duration

	mu      sync.Mutex
	version uint64
	stop    chan struct{}
}

func newClock(now func() time.Time, tick time.Duration) *clock {
	if now == nil {
		now = time.Now
	}
	if tick <= 0 {
		tick = time.Second
	}
	return &clock{now: now, tick: tick}
}

func (c *clock) value() map[string]qml.SpecValue {
	return map[string]qml.SpecValue{"App.clock": str(c.now().Format("15:04:05"))}
}

func (c *clock) next() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
	return c.version
}

// Subscribe implements decl.Provider.
func (c *clock) Subscribe(fn func(decl.Update)) ([]string, func() error, error) {
	fn(decl.Update{Version: c.next(), Values: c.value()})

	c.stop = make(chan struct{})
	t := time.NewTicker(c.tick)
	go func(stop chan struct{}) {
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				fn(decl.Update{Version: c.next(), Values: c.value()})
			}
		}
	}(c.stop)

	var once sync.Once
	cancel := func() error {
		once.Do(func() { close(c.stop) })
		return nil
	}
	return []string{"App.clock"}, cancel, nil
}

// themeLoader reads a theme file when its module is imported, and not before.
func themeLoader(file string) decl.ModuleLoader {
	return func() (decl.ModuleContents, error) {
		src, err := themeFiles.ReadFile(file)
		if err != nil {
			return decl.ModuleContents{}, err
		}
		return decl.ValueModule(src)()
	}
}

package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Host is the program behind editor.qml.
//
// The split mirrors the one the Go editor draws between golib's widgets and its
// own policy. The document owns STRUCTURE — what is on screen and what each
// control triggers. The host owns BEHAVIOUR — what "Save" does to a file, what
// the status line should say.
//
// The host is spread over one file per concern:
//
//	app.go       the Host, and New, which assembles it
//	modules.go   the QML the program ships, and the modules that offer it
//	state.go     the App singleton's state: what the document reads
//	commands.go  the App singleton's commands: what the document invokes
//	files.go     reading and writing the buffer's file
//	clock.go     the provider behind App.clock
type Host struct {
	p      *tuidecl.Program
	editor *widget.Editor

	path  string
	dirty bool
	// quitAfterSave is `wq` on a buffer with no name: the Save dialog asks
	// for one, and the quit waits for the file to be written. Cancelling the
	// dialog or a failed write clears it.
	quitAfterSave bool
}

// Options are what New needs from the program around it.
type Options struct {
	// Path is the file to open, or "" for an unnamed buffer.
	Path string
	// Sink receives errors from handlers, which have no caller to return to.
	// Nil keeps them, and Run returns them when it ends.
	Sink func(error)
	// Now is the clock's source of time; nil means time.Now. A test fixes it.
	Now func() time.Time
	// Tick is how often the clock advances; zero means a second.
	Tick time.Duration
	// Layout replaces editor.qml; nil means the embedded one. A test uses it to
	// run the same screen under the other theme's import line.
	Layout []byte
	// App are options for the tui.App: the backend, above all.
	App []tui.AppOption
	// Dev is a directory holding editor.qml, themes/ and dialogs/ — this
	// example's own, say. Set, the program reads its QML from there instead
	// of the copy built into the binary, and follows the files as they are
	// edited: a saved change is on screen a moment later.
	Dev string
}

// New mounts editor.qml. Nothing runs until Run.
//
// The whole program is one tuidecl.NewProgram over Host.options, then the host
// attached to it.
func New(opt Options) (*Host, error) {
	h := newHost(opt)
	p, err := tuidecl.NewProgram(h.options(opt)...)
	if err != nil {
		return nil, err
	}
	if err := h.attach(p, opt.Path); err != nil {
		// The program is built — its clock already ticking — and will never
		// run; release it rather than leave the provider subscribed.
		return nil, errors.Join(err, p.Tree().Destroy())
	}
	return h, nil
}

// newHost is the host before its program exists: options needs it, to hand
// the document its commands.
func newHost(opt Options) *Host { return &Host{path: opt.Path} }

// attach binds the host to the program built from its options — by New, or
// by a test running the same options through decltest.Run — finds the one
// widget it reaches into, and loads the file.
func (h *Host) attach(p *tuidecl.Program, path string) error {
	h.p = p
	var ok bool
	if h.editor, ok = tuidecl.FindAs[*widget.Editor](p, "editor"); !ok {
		return errors.New("editor.qml declares no Editor with id: editor")
	}
	return h.load(path)
}

// options are everything the program is: the modules the document may import,
// the state it reads, the commands it invokes, the clock, the layout. ONE
// function, and everything builds from it: New, the test that checks every
// QML file (decltest.Check), and every test that runs the editor
// (decltest.Run). A program assembled twice is two programs.
func (h *Host) options(opt Options) []tuidecl.ProgramOption {
	var opts []tuidecl.ProgramOption
	if opt.Dev != "" {
		files := os.DirFS(opt.Dev)
		opts = append(h.modulesFrom(files, files),
			tuidecl.Layout(files, "editor.qml"),
			// A refused edit is reported where the user is looking; the
			// screen stays as it was until the next good save.
			tuidecl.HotReload(tuidecl.OnReloadError(func(err error) { _ = h.message(err.Error()) })))
	} else {
		src := opt.Layout
		if src == nil {
			src = layout
		}
		opts = append(h.modules(), tuidecl.LayoutSource("editor.qml", src))
	}
	opts = append(opts,
		tuidecl.Sources(h.state(opt.Path)),
		tuidecl.Handlers(h.commands()),
		tuidecl.Providers(newClock(opt.Now, opt.Tick)),
		tuidecl.AppOptions(opt.App...),
	)
	if opt.Sink != nil {
		opts = append(opts, tuidecl.ErrorSink(opt.Sink))
	}
	return opts
}

// Run runs the editor until it quits or ctx ends, and releases it.
func (h *Host) Run(ctx context.Context) error { return h.p.Run(ctx) }

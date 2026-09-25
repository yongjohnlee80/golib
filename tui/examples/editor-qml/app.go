package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
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

// New mounts editor.qml and returns the host and the component to run.
//
// Everything the document may reach is registered BEFORE the mount — modules,
// state, the clock, commands — because the engine checks a document against a
// fixed set of names, and refuses one that names anything missing.
func New(opt Options) (*Host, tui.Component, error) {
	h := &Host{path: opt.Path, quit: opt.Quit}
	h.adapter = tuidecl.New(tuidecl.StdRegistry(),
		append(tuidecl.StdProperties(), tuidecl.WithErrorSink(opt.Sink))...)
	h.tree = decl.New(h.adapter,
		decl.WithScheduler(opt.Schedule),
		decl.WithProviderErrorSink(opt.Sink))

	for _, step := range []func() error{
		h.declareModules,
		func() error { return h.injectState(opt.Path) },
		func() error { return h.startClock(opt.Now, opt.Tick) },
		h.injectCommands,
		func() error { return h.mount(opt.Layout) },
		func() error { return h.load(opt.Path) },
	} {
		if err := step(); err != nil {
			return nil, nil, err
		}
	}
	root, _ := h.adapter.Component(h.tree.Root())
	return h, root, nil
}

// Close stops the clock and releases the tree.
func (h *Host) Close() error { return h.tree.Destroy() }

// mount parses the layout, mounts it, and finds the one widget the host
// reaches into: the editor, by the id the document gave it.
func (h *Host) mount(src []byte) error {
	if src == nil {
		src = layout
	}
	spec, err := qml.QML{File: "editor.qml"}.Parse(src)
	if err != nil {
		return err
	}
	if err := h.tree.Mount(spec); err != nil {
		return err
	}
	id, ok := h.tree.NodeByID("editor")
	if !ok {
		return errors.New("editor.qml declares no node with id: editor")
	}
	comp, _ := h.adapter.Component(id)
	if h.editor, ok = tuidecl.EditorOf(comp); !ok {
		return fmt.Errorf("editor.qml: id editor is not an Editor")
	}
	return nil
}

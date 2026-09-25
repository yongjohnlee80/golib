package decl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// PROGRAM — a golib/tui application whose screen is a QML document, in one
// call.
//
//	p, err := tuidecl.NewProgram(
//	    tuidecl.Layout(files, "editor.qml"),
//	    tuidecl.Singleton("editor", "1.0", "App"),
//	    tuidecl.Sources(map[string]any{"App.mode": "NORMAL", "App.count": 0}),
//	    tuidecl.Commands(map[string]func() error{"App.save": save}),
//	    tuidecl.Themes(files, "themes", "editor.theme", "1.0"),
//	    tuidecl.Components(files, "dialogs", "editor.dialogs", "1.0"),
//	    tuidecl.Types(gauge),
//	)
//	if err != nil { return err }
//	return p.Run(ctx)
//
// Everything a host otherwise wires by hand, in the order it has to be wired:
// an adapter over the standard vocabulary and any types of your own, a tree
// whose scheduler posts to the App's loop, every module declared or offered,
// state and commands injected, the layout parsed under its file name and
// mounted, the App built around the root — and, when Run returns, the tree
// torn down.
//
// It is sugar over [New], [decl.New] and [tui.NewApp], not a second way to
// build: [Program.Tree], [Program.Adapter] and [Program.App] hand each back for
// anything the options do not cover.

// Program is a mounted QML screen and the App that runs it.
type Program struct {
	tree    *decl.Tree
	adapter *Adapter
	app     *tui.App
	root    tui.Component
	file    string

	mu      sync.Mutex
	cancel  context.CancelFunc
	pending []func()
	handled []error
}

// ProgramOption configures a Program.
type ProgramOption func(*programConfig)

type programConfig struct {
	layout     []byte
	layoutFile string
	errs       []error

	registry    *Registry
	adapterOpts []Option
	treeOpts    []decl.Option
	appOpts     []tui.AppOption
	sink        func(error)

	modules   []decl.Module
	offers    []offered
	sources   map[string]any
	handlers  map[string]decl.HandlerFunc
	providers []decl.Provider
}

type offered struct {
	name, version string
	load          decl.ModuleLoader
}

// Layout reads the document from a file system: an embed.FS, os.DirFS, any
// fs.FS. Diagnostics name the file: "editor.qml:12:5".
func Layout(fsys fs.FS, file string) ProgramOption {
	return func(c *programConfig) {
		src, err := fs.ReadFile(fsys, file)
		if err != nil {
			c.errs = append(c.errs, fmt.Errorf("layout: %w", err))
		}
		c.layout, c.layoutFile = src, file
	}
}

// LayoutSource is Layout for a document already in memory; name is what its
// diagnostics are placed in.
func LayoutSource(name string, src []byte) ProgramOption {
	return func(c *programConfig) { c.layout, c.layoutFile = src, name }
}

// Singleton declares a module the document always imports, and the singletons
// it brings into scope: `import editor 1.0` giving `App`.
func Singleton(module, version string, exports ...string) ProgramOption {
	return func(c *programConfig) {
		c.modules = append(c.modules, decl.Module{Name: module, Version: version, Exports: exports})
	}
}

// Sources publishes state the document reads, by the name it writes:
// "App.mode". Values are Go strings, bools, integers or floats; a binding that
// reads one follows [Program.Set].
func Sources(values map[string]any) ProgramOption {
	return func(c *programConfig) {
		if c.sources == nil {
			c.sources = map[string]any{}
		}
		for k, v := range values {
			c.sources[k] = v
		}
	}
}

// Commands publishes handlers that take no arguments: `App.save()`. A document
// passing one an argument is refused when the handler runs, by name.
func Commands(cmds map[string]func() error) ProgramOption {
	return func(c *programConfig) {
		for name, fn := range cmds {
			name, fn := name, fn
			c.handler(name, func(args []qml.SpecValue) error {
				if len(args) > 0 {
					return fmt.Errorf("%s takes no arguments, and was given %d", name, len(args))
				}
				return fn()
			})
		}
	}
}

// Handlers publishes handlers that take arguments, as the engine passes them:
// `App.openFile(selectedFile)`. [Arg] reads one.
func Handlers(hs map[string]decl.HandlerFunc) ProgramOption {
	return func(c *programConfig) {
		for name, fn := range hs {
			c.handler(name, fn)
		}
	}
}

func (c *programConfig) handler(name string, fn decl.HandlerFunc) {
	if c.handlers == nil {
		c.handlers = map[string]decl.HandlerFunc{}
	}
	c.handlers[name] = fn
}

// Providers subscribes values that change on their own clock — a clock, a
// watcher, a socket. They deliver from their own goroutines; the Program's
// scheduler brings each delivery onto the UI loop.
func Providers(ps ...decl.Provider) ProgramOption {
	return func(c *programConfig) { c.providers = append(c.providers, ps...) }
}

// Themes offers every `.qml` file in dir as a theme module named
// prefix.<file>: themes/retro.qml as `editor.theme.retro`. Only the one the
// document imports is read.
func Themes(fsys fs.FS, dir, prefix, version string) ProgramOption {
	return func(c *programConfig) {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			c.errs = append(c.errs, fmt.Errorf("themes: %w", err))
			return
		}
		for _, e := range entries {
			if e.IsDir() || path.Ext(e.Name()) != ".qml" {
				continue
			}
			file := path.Join(dir, e.Name())
			c.offers = append(c.offers, offered{
				name:    prefix + "." + strings.TrimSuffix(e.Name(), ".qml"),
				version: version,
				load: func() (decl.ModuleContents, error) {
					src, err := fs.ReadFile(fsys, file)
					if err != nil {
						return decl.ModuleContents{}, err
					}
					return decl.ValueModule(src)()
				},
			})
		}
	}
}

// Components offers the `.qml` files in dir as one module of component types:
// dialogs/QuitDialog.qml as the type QuitDialog, after `import <module>`.
func Components(fsys fs.FS, dir, module, version string) ProgramOption {
	return func(c *programConfig) {
		c.offers = append(c.offers, offered{name: module, version: version, load: decl.ComponentFiles(fsys, dir)})
	}
}

// Offer offers a module with a loader of your own.
func Offer(module, version string, load decl.ModuleLoader) ProgramOption {
	return func(c *programConfig) {
		c.offers = append(c.offers, offered{name: module, version: version, load: load})
	}
}

// Types adds your own widget types to the vocabulary. See [Type].
func Types(types ...Type) ProgramOption {
	return func(c *programConfig) { c.adapterOpts = append(c.adapterOpts, WithTypes(types...)) }
}

// Files sets the filesystem every FileDialog lists — any fs.FS, remote
// included. Unset, the local disk.
func Files(src widget.FileSource) ProgramOption {
	return func(c *programConfig) { c.adapterOpts = append(c.adapterOpts, WithFileSource(src)) }
}

// ErrorSink sets where a handler's error goes. Unset, the Program keeps them,
// and Run returns them with its own error — never dropped.
func ErrorSink(fn func(error)) ProgramOption {
	return func(c *programConfig) { c.sink = fn }
}

// AppOptions are passed to tui.NewApp: a backend, a theme, a trace.
func AppOptions(opts ...tui.AppOption) ProgramOption {
	return func(c *programConfig) { c.appOpts = append(c.appOpts, opts...) }
}

// AdapterOptions and TreeOptions pass options straight through, for what the
// options above do not cover.
func AdapterOptions(opts ...Option) ProgramOption {
	return func(c *programConfig) { c.adapterOpts = append(c.adapterOpts, opts...) }
}

// TreeOptions passes options to decl.New.
func TreeOptions(opts ...decl.Option) ProgramOption {
	return func(c *programConfig) { c.treeOpts = append(c.treeOpts, opts...) }
}

// WithRegistry replaces the standard vocabulary's registry — for a program
// that builds its own from scratch. Its properties then come from
// AdapterOptions.
func WithRegistry(r *Registry) ProgramOption {
	return func(c *programConfig) { c.registry = r }
}

// NewProgram builds and mounts the program. Nothing runs until [Program.Run].
func NewProgram(opts ...ProgramOption) (*Program, error) {
	var c programConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if c.layout == nil {
		return nil, errors.New("tui/decl.NewProgram: no layout; give one with Layout or LayoutSource")
	}
	p := &Program{file: c.layoutFile}

	sink := c.sink
	if sink == nil {
		sink = p.keep
	}
	reg, props := c.registry, []Option(nil)
	if reg == nil {
		reg, props = StdRegistry(), StdProperties()
	}
	p.adapter = New(reg, append(append(props, c.adapterOpts...), WithErrorSink(sink))...)
	p.tree = decl.New(p.adapter, append([]decl.Option{
		decl.WithScheduler(p.schedule),
		decl.WithProviderErrorSink(sink),
	}, c.treeOpts...)...)

	if err := p.register(c); err != nil {
		return nil, err
	}
	spec, err := qml.QML{File: c.layoutFile}.Parse(c.layout)
	if err != nil {
		return nil, err
	}
	if err := p.tree.Mount(spec); err != nil {
		return nil, err
	}
	p.root, _ = p.adapter.Component(p.tree.Root())
	p.app = tui.NewApp(p.root, c.appOpts...)
	// Work scheduled before the App existed — a provider ticking during the
	// mount — goes to it now, in the order it arrived.
	p.mu.Lock()
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	for _, fn := range pending {
		p.app.Update(fn)
	}
	return p, nil
}

// register is everything the document may reach, before it is mounted.
func (p *Program) register(c programConfig) error {
	for _, m := range c.modules {
		if err := p.tree.DeclareModule(m); err != nil {
			return err
		}
	}
	for _, o := range c.offers {
		if err := p.tree.OfferModule(o.name, o.version, o.load); err != nil {
			return err
		}
	}
	for name, v := range c.sources {
		sv, err := Value(v)
		if err != nil {
			return fmt.Errorf("source %s: %w", name, err)
		}
		if err := p.tree.Inject(name, decl.SourceValue(sv)); err != nil {
			return err
		}
	}
	for name, fn := range c.handlers {
		if err := p.tree.Inject(name, decl.Handle(fn)); err != nil {
			return err
		}
	}
	for _, pr := range c.providers {
		if err := p.tree.Subscribe(pr); err != nil {
			return err
		}
	}
	return nil
}

// schedule is the tree's scheduler: the App's loop, once there is an App.
func (p *Program) schedule(fn func()) {
	p.mu.Lock()
	app := p.app
	if app == nil {
		p.pending = append(p.pending, fn)
	}
	p.mu.Unlock()
	if app != nil {
		app.Update(fn)
	}
}

func (p *Program) keep(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handled = append(p.handled, err)
}

// Run runs the App until ctx is cancelled or [Program.Quit] is called, then
// tears the tree down. It returns the App's error, the teardown's, and every
// handler error the default sink kept.
func (p *Program) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()
	defer cancel()
	runErr := p.app.Run(ctx)
	destroyErr := p.tree.Destroy()
	p.mu.Lock()
	handled := p.handled
	p.mu.Unlock()
	return errors.Join(append([]error{runErr, destroyErr}, handled...)...)
}

// Quit ends Run. Safe from a handler and from any goroutine.
func (p *Program) Quit() {
	p.mu.Lock()
	cancel := p.cancel
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Post runs fn on the UI loop. Safe from any goroutine: it is how work done
// elsewhere changes what the screen shows.
func (p *Program) Post(fn func()) { p.app.Update(fn) }

// Set moves one source. ON THE UI LOOP — from a handler, or inside Post.
func (p *Program) Set(name string, v any) error {
	sv, err := Value(v)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	_, err = p.tree.SetSource(name, sv)
	return err
}

// SetMany moves several sources as one change: every binding sees all of them
// at once. On the UI loop.
func (p *Program) SetMany(values map[string]any) error {
	svs := make(map[string]qml.SpecValue, len(values))
	for name, v := range values {
		sv, err := Value(v)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		svs[name] = sv
	}
	_, err := p.tree.SetSources(svs)
	return err
}

// Find is the widget the document declared with an id. Look it up again after
// a reload: a rebuilt node is a new widget.
func (p *Program) Find(id string) (tui.Component, bool) {
	node, ok := p.tree.NodeByID(id)
	if !ok {
		return nil, false
	}
	return p.adapter.Component(node)
}

// FindAs is Find for a widget of a known Go type.
func FindAs[W tui.Component](p *Program, id string) (W, bool) {
	var zero W
	c, ok := p.Find(id)
	if !ok {
		return zero, false
	}
	w, ok := c.(W)
	return w, ok
}

// Call runs a method on a node the document declared, by its id — what a
// handler does with `saveDialog.open()`, from Go. On the UI loop.
func (p *Program) Call(id, method string, args ...any) error {
	node, ok := p.tree.NodeByID(id)
	if !ok {
		return fmt.Errorf("%s declares no node with id %q", p.file, id)
	}
	svs := make([]qml.SpecValue, 0, len(args))
	for _, a := range args {
		sv, err := Value(a)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", id, method, err)
		}
		svs = append(svs, sv)
	}
	return p.adapter.Invoke(node, method, svs)
}

// Reload replaces the document with src, patching what changed. On the UI
// loop. A refused reload leaves the screen as it was.
func (p *Program) Reload(src []byte) (decl.Result, error) {
	spec, err := qml.QML{File: p.file}.Parse(src)
	if err != nil {
		return decl.Result{}, err
	}
	return p.tree.Reconcile(spec)
}

// Tree, Adapter, App and Root hand back what the Program built, for anything
// its methods do not cover.
func (p *Program) Tree() *decl.Tree    { return p.tree }
func (p *Program) Adapter() *Adapter   { return p.adapter }
func (p *Program) App() *tui.App       { return p.app }
func (p *Program) Root() tui.Component { return p.root }

// ---------------------------------------------------------------- values

// Value is a Go value as the engine holds it: a string, bool, integer or
// float. Anything else is refused, rather than written as its %v.
func Value(v any) (qml.SpecValue, error) {
	switch x := v.(type) {
	case qml.SpecValue:
		return x, nil
	case string:
		return qml.SpecValue{Kind: qml.SpecValueString, Raw: x}, nil
	case bool:
		raw := "false"
		if x {
			raw = "true"
		}
		return qml.SpecValue{Kind: qml.SpecValueBool, Raw: raw}, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: fmt.Sprint(x)}, nil
	case float32, float64:
		return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: fmt.Sprint(x)}, nil
	}
	return qml.SpecValue{}, fmt.Errorf("a %T is not a value a document can hold: want a string, bool or number", v)
}

// Arg reads a handler's argument i as a string — a path, a name — or says why
// it cannot.
func Arg(args []qml.SpecValue, i int) (string, error) {
	if i >= len(args) {
		return "", fmt.Errorf("want at least %d arguments, got %d", i+1, len(args))
	}
	return args[i].Raw, nil
}

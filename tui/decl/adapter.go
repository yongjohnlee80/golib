package decl

import (
	"fmt"
	"github.com/yongjohnlee80/golib/parse/qml"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// HostFuncs maps handler names to functions in the host program.
//
// It is a convenience for [InjectHosts]: a host that already keeps its effects
// in a table can hand the table over rather than injecting one name at a time.
type HostFuncs map[string]func() error

// InjectHosts registers each entry as a handler on tr, before Mount.
//
// This replaces the adapter's old ResolveHandler table. The difference is not
// cosmetic: a handler injected here is TYPED, so the engine can refuse it where
// a value belongs and tell the author which of the two they wrote — which a map
// consulted by name could not do, because by the time it was consulted the only
// thing left of `save()` was "save".
func InjectHosts(tr *decl.Tree, hosts HostFuncs) error {
	names := make([]string, 0, len(hosts))
	for n := range hosts {
		names = append(names, n)
	}
	// Deterministic, so the FIRST failure of a bad table is always the same one
	// and a test asserting it is not ordered by map iteration.
	sort.Strings(names)
	for _, n := range names {
		fn := hosts[n]
		if fn == nil {
			return fmt.Errorf("host function %q is nil", n)
		}
		if err := tr.Inject(n, decl.Handle(func([]qml.SpecValue) error { return fn() })); err != nil {
			return err
		}
	}
	return nil
}

// Adapter satisfies decl.Adapter for golib/tui.
//
// It is not safe for concurrent use, and it does not need to be: every call
// reaches it from the engine, which runs wherever the App's loop goroutine is.
// Getting calls onto that goroutine is the caller's job, the same as for any
// other component state.
type Adapter struct {
	reg *Registry

	// nodes is the adapter's own record of what it built. It is keyed by the
	// engine's NodeID, which is never reused, so an entry can never be confused
	// with a later node that happens to occupy the same position.
	//
	// The schema TYPE is stored beside the component rather than inferred from
	// it later: two schema types may legitimately build the same Go type with
	// different property tables, and asking the component what it is would
	// answer the wrong question.
	nodes map[decl.NodeID]built

	setters map[string]map[string]Setter
	// ctorProps names the properties a type accepts ONLY at construction. It is
	// DECLARED rather than inferred, because what a builder consumed tells you
	// only about the schemas it has seen: a Split mounted without an
	// orientation consumed nothing, and nothing in that record says the widget
	// has no SetOrientation.
	ctorProps map[string]map[string]bool
	methods   map[string]map[string]Method
	signals   map[string]map[string][]string
	files     widget.FileSource
	overlay   *widget.OverlayHost
	destroyed map[string]func(tui.Component)
	sink      func(error)

	// pal is the palette tree, and restylers each type's way of wearing an
	// effective palette. See propagate.go.
	pal       map[decl.NodeID]*palNode
	restylers map[string]restyler
	// kids are each node's children, in order, as construction and every
	// restructure left them.
	kids map[decl.NodeID][]decl.NodeID
	// enums are the enumerations the types declared, published as constants
	// under their scopes.
	enums []Enum
}

// Option configures an [Adapter].
type Option func(*Adapter)

// WithErrorSink installs where a handler error goes when it surfaces from a
// toolkit callback that cannot return one.
//
// It is REQUIRED for any schema that binds a handler. Without it, mounting such
// a schema fails rather than proceeding: a toolkit callback is shaped func()
// with nowhere to put an error, so an adapter with no sink would make a handler
// that always fails indistinguishable from one that works.
//
// An earlier version documented that absence as "the error is dropped", three
// lines below a comment calling exactly that the one handling this design will
// not defend. Documenting a violation does not make it a decision; the rule is
// now enforced where it can be, and a schema with no handlers still needs no
// sink.
func WithErrorSink(fn func(error)) Option {
	return func(a *Adapter) { a.sink = fn }
}

// WithSetters registers the runtime property setters for a type — everything a
// builder does not consume at construction.
func WithSetters(typeName string, setters map[string]Setter) Option {
	return func(a *Adapter) {
		if a.setters[typeName] == nil {
			a.setters[typeName] = map[string]Setter{}
		}
		for name, fn := range setters {
			a.setters[typeName][name] = fn
		}
	}
}

// WithMethods registers the methods a handler can call on a node of a type by
// its id.
func WithMethods(typeName string, methods map[string]Method) Option {
	return func(a *Adapter) {
		if a.methods[typeName] == nil {
			a.methods[typeName] = map[string]Method{}
		}
		for name, fn := range methods {
			a.methods[typeName][name] = fn
		}
	}
}

// MethodsOf implements [decl.Methods].
func (a *Adapter) MethodsOf(typeName string) []string {
	names := make([]string, 0, len(a.methods[typeName]))
	for n := range a.methods[typeName] {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Invoke implements [decl.Methods].
func (a *Adapter) Invoke(node decl.NodeID, method string, args []qml.SpecValue) error {
	b, ok := a.nodes[node]
	if !ok {
		return fmt.Errorf("%s: node %d was not built by this adapter", method, node)
	}
	fn, ok := a.methods[b.typ][method]
	if !ok {
		return fmt.Errorf("a %s has no method %s", b.typ, method)
	}
	if err := fn(b.comp, args); err != nil {
		return fmt.Errorf("%s.%s: %w", b.typ, method, err)
	}
	return nil
}

var _ decl.Methods = (*Adapter)(nil)

// WithConstructorProps declares the properties a type accepts ONLY at
// construction — the ones a builder takes as arguments and offers no setter
// for.
//
// It exists so the adapter can tell a constructor-only property apart from one
// it does not have at all. Both are un-appliable, and the right response to
// each is opposite: change a constructor-only property and the node is rebuilt;
// name a property that does not exist and nothing should be touched, because
// the rebuilt node would refuse it too.
func WithConstructorProps(typeName string, props ...string) Option {
	return func(a *Adapter) {
		if a.ctorProps[typeName] == nil {
			a.ctorProps[typeName] = map[string]bool{}
		}
		for _, p := range props {
			a.ctorProps[typeName][p] = true
		}
	}
}

// New returns an Adapter driving reg, which must not be nil.
func New(reg *Registry, opts ...Option) *Adapter {
	if reg == nil {
		panic("tui/decl.New: registry is nil")
	}
	a := &Adapter{
		reg:       reg,
		nodes:     map[decl.NodeID]built{},
		setters:   map[string]map[string]Setter{},
		ctorProps: map[string]map[string]bool{},
		methods:   map[string]map[string]Method{},
		signals:   map[string]map[string][]string{},
		destroyed: map[string]func(tui.Component){},
		pal:       map[decl.NodeID]*palNode{},
		restylers: map[string]restyler{},
		kids:      map[decl.NodeID][]decl.NodeID{},
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Component returns the widget built for a node, and false when there is none.
//
// This is the adapter's own schemaID-to-widget record in its simplest form: the
// engine holds identity, this holds what was built for it. A reconciling
// consumer needs exactly this mapping, and it lives here rather than being
// retrofitted onto the widgets, because a widget cannot be made to carry a
// schema's notion of identity after the fact.
func (a *Adapter) Component(id decl.NodeID) (tui.Component, bool) {
	b, ok := a.nodes[id]
	if !ok {
		return nil, false
	}
	return b.comp, true
}

// built is one constructed node: the widget and the schema type that made it.
type built struct {
	comp tui.Component
	typ  string
	// attached are the attached properties this node carried, kept for its
	// parent's construction.
	attached map[string]qml.SpecValue
	// nominee is the first `focus: true` component in this node's subtree.
	nominee tui.Component
}

// Create implements decl.Adapter.
func (a *Adapter) Create(c decl.Construction) ([]string, error) {
	build, ok := a.reg.builders[c.Type]
	if !ok {
		return nil, fmt.Errorf("no widget registered for type %q (declared at %s)", c.Type, c.Pos)
	}

	children := make([]tui.Component, 0, len(c.Children))
	childAttached := make([]map[string]qml.SpecValue, 0, len(c.Children))
	var childNominee tui.Component
	for _, id := range c.Children {
		child, ok := a.nodes[id]
		if !ok {
			// The engine builds children first, so this cannot happen from a
			// well-formed mount. It is reported rather than assumed away
			// because the alternative is a nil child reaching a constructor
			// that panics on one.
			return nil, fmt.Errorf("child node %d was not built before its parent %d", id, c.Node)
		}
		children = append(children, child.comp)
		childAttached = append(childAttached, child.attached)
		if childNominee == nil {
			childNominee = child.nominee
		}
	}
	own, attached := a.splitProps(c.Props)
	own, focus, err := takeFocus(own)
	if err != nil {
		return nil, err
	}
	// Palette roles are the adapter's, on every type: they propagate to the
	// children whether or not this type wears them. See propagate.go.
	ownPalette, own, paletteNames, err := takePalette(own)
	if err != nil {
		return nil, err
	}

	// A bound emitter with nowhere to send its error is refused at the point
	// the wiring would happen, rather than becoming silence at the first
	// failure. A schema that binds nothing needs no sink and is unaffected.
	if len(c.Emitters) > 0 && a.sink == nil {
		signals := make([]string, 0, len(c.Emitters))
		for name := range c.Emitters {
			signals = append(signals, name)
		}
		sort.Strings(signals)
		return nil, fmt.Errorf(
			"%q binds %v but this adapter has no error sink, so a failing handler "+
				"would be silent: pass WithErrorSink (declared at %s)",
			c.Type, signals, c.Pos)
	}

	asked := map[string]bool{}
	comp, consumed, err := build(Build{
		Type:          c.Type,
		Pos:           c.Pos,
		Props:         own,
		Children:      children,
		ChildAttached: childAttached,
		SelfAttached:  attached,
		FocusNominee:  childNominee,
		Emitters:      c.Emitters,
		Files:         a.files,
		sink:          a.sink,
		Overlay:       a.overlay,
		asked:         asked,
	})
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, fmt.Errorf("the builder for %q returned no component", c.Type)
	}
	// Qt refuses a handler for a signal the type does not have. A builder
	// wires every signal its widget raises at construction, so one it never
	// asked for is not a signal of this type — and a handler bound to it would
	// never run.
	if unknown := unaskedSignals(c.Emitters, asked); len(unknown) > 0 {
		return nil, fmt.Errorf("%s has no signal %s: on%s is bound to nothing (declared at %s)",
			c.Type, strings.Join(unknown, ", "), capitalize(unknown[0]), c.Pos)
	}
	nominee := childNominee
	if focus {
		nominee = comp
	}
	a.nodes[c.Node] = built{comp: comp, typ: c.Type, attached: attached, nominee: nominee}
	a.kids[c.Node] = append([]decl.NodeID(nil), c.Children...)
	a.paletteBuilt(c.Node, ownPalette, c.Children)
	consumed = append(consumed, paletteNames...)
	if focus {
		consumed = append(consumed, "focus")
	}
	// The attached properties are reported consumed: they were read — by the
	// parent, at its construction — and there is no setter on THIS widget for
	// the engine to apply them through.
	for name := range attached {
		consumed = append(consumed, name)
	}
	return consumed, nil
}

// Apply implements decl.Adapter.
func (a *Adapter) Apply(app decl.Application) error {
	b, ok := a.nodes[app.Node]
	if !ok {
		return fmt.Errorf("node %d has no component", app.Node)
	}
	if isPaletteProp(app.Prop) {
		return a.setRole(app.Node, app.Prop, app.Value)
	}
	set, ok := a.setters[b.typ][app.Prop]
	if !ok {
		return fmt.Errorf("type %q has no runtime property %q (declared at %s)",
			b.typ, app.Prop, app.Value.Pos)
	}
	return set(b.comp, app.Value)
}

// Destroy implements decl.Adapter.
//
// It forgets the component and nothing more. Unmounting belongs to whoever
// mounted the root into a running App: this package builds a tree, it does not
// own the App's lifecycle, and tearing down a component the App still holds
// would be reaching past its own boundary.
func (a *Adapter) Destroy(id decl.NodeID) error {
	b, ok := a.nodes[id]
	if pn := a.pal[id]; pn != nil && pn.hasParent {
		a.kids[pn.parent] = removeKid(a.kids[pn.parent], id)
	}
	a.paletteRelease(id)
	delete(a.pal, id)
	delete(a.kids, id)
	delete(a.nodes, id)
	if hook := a.destroyed[b.typ]; ok && hook != nil {
		hook(b.comp)
	}
	return nil
}

// WithDestroyHook sets what runs when a node of a type is destroyed — by a
// reload that drops it, or by the tree's teardown — with the widget it built.
func WithDestroyHook(typeName string, fn func(tui.Component)) Option {
	return func(a *Adapter) { a.destroyed[typeName] = fn }
}

// TypeNames implements [decl.Vocabulary]: a component named like a type this
// adapter builds is refused, not allowed to replace it.
func (a *Adapter) TypeNames() []string {
	names := make([]string, 0, len(a.reg.builders))
	for n := range a.reg.builders {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

var _ decl.Vocabulary = (*Adapter)(nil)

// WithSignalParams names the parameters of a type's signals.
func WithSignalParams(typeName string, signals map[string][]string) Option {
	return func(a *Adapter) {
		if a.signals[typeName] == nil {
			a.signals[typeName] = map[string][]string{}
		}
		for sig, params := range signals {
			a.signals[typeName][sig] = params
		}
	}
}

// SignalParams implements [decl.SignalParameters].
func (a *Adapter) SignalParams(typeName, signal string) []string {
	return a.signals[typeName][signal]
}

var _ decl.SignalParameters = (*Adapter)(nil)

// withEnums publishes a type's enumerations. A scope that is already a
// singleton of the module — Tui, Dialog, another type's — panics, as a second
// registration of one name does: which constant a document meant would depend
// on registration order.
func withEnums(enums ...Enum) Option {
	return func(a *Adapter) {
		for _, e := range enums {
			for _, taken := range a.exports() {
				if taken == e.Scope {
					panic("tui/decl: enum scope " + e.Scope + " is already a singleton of the tui module")
				}
			}
			a.enums = append(a.enums, e)
		}
	}
}

// exports are the tui module's singletons: Tui, each flag set's, each enum's.
func (a *Adapter) exports() []string {
	out := tuiExports()
	for _, e := range a.enums {
		out = append(out, e.Scope)
	}
	return out
}

// WithOverlay is the overlay a Dialog or FileDialog opens on when no Window
// gave it one — a Go program's own OverlayHost, the layer its Go modals use.
// Qt opens a Popup in the overlay of the window it is shown in, whether that
// window was written in QML or built in C++; this says which window that is
// when it is Go. A Window's dialogs still open on the Window's own host.
//
// When a dialog closes, the keyboard goes back to whatever had it when the
// dialog opened: a dialog is a focus-trapping modal, and the runtime restores
// the focus a trap took.
func WithOverlay(host *widget.OverlayHost) Option {
	return func(a *Adapter) { a.overlay = host }
}

// WithFileSource sets the filesystem the file dialogs list: any fs.FS, a
// remote one included, with the root its paths are written under. Unset, they
// list the local disk.
func WithFileSource(src widget.FileSource) Option {
	return func(a *Adapter) { a.files = src }
}

// unaskedSignals are the bound signals a builder never asked for, sorted.
func unaskedSignals(bound map[string]func(args ...qml.SpecValue) error, asked map[string]bool) []string {
	var out []string
	for name := range bound {
		if !asked[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

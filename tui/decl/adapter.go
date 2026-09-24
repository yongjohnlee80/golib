package decl

import (
	"fmt"
	"sort"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/tui"
)

// HostFuncs maps handler names from a schema to functions in the host program.
//
// The schema says `onClicked: saveDocument`; this is what turns that string
// into something callable. It is the host's table because the names refer to
// the host's own behaviour, which neither the engine nor this package can see.
type HostFuncs map[string]func() error

// Adapter satisfies decl.Adapter for golib/tui.
//
// It is not safe for concurrent use, and it does not need to be: every call
// reaches it from the engine, which runs wherever the App's loop goroutine is.
// Getting calls onto that goroutine is the caller's job, the same as for any
// other component state.
type Adapter struct {
	reg   *Registry
	hosts HostFuncs

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
	sink      func(error)
}

// Option configures an [Adapter].
type Option func(*Adapter)

// WithHostFuncs supplies the handler-name table.
func WithHostFuncs(h HostFuncs) Option {
	return func(a *Adapter) { a.hosts = h }
}

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
}

// ResolveHandler implements decl.Adapter.
func (a *Adapter) ResolveHandler(_ decl.NodeID, signal, name string, pos parse.Position) (func() error, error) {
	fn, ok := a.hosts[name]
	if !ok {
		return nil, fmt.Errorf("no host function named %q for signal %q (declared at %s)", name, signal, pos)
	}
	return fn, nil
}

// Create implements decl.Adapter.
func (a *Adapter) Create(c decl.Construction) ([]string, error) {
	build, ok := a.reg.builders[c.Type]
	if !ok {
		return nil, fmt.Errorf("no widget registered for type %q (declared at %s)", c.Type, c.Pos)
	}

	children := make([]tui.Component, 0, len(c.Children))
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

	comp, consumed, err := build(Build{
		Type:     c.Type,
		Pos:      c.Pos,
		Props:    c.Props,
		Children: children,
		Emitters: c.Emitters,
		sink:     a.sink,
	})
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, fmt.Errorf("the builder for %q returned no component", c.Type)
	}
	a.nodes[c.Node] = built{comp: comp, typ: c.Type}
	return consumed, nil
}

// Apply implements decl.Adapter.
func (a *Adapter) Apply(app decl.Application) error {
	b, ok := a.nodes[app.Node]
	if !ok {
		return fmt.Errorf("node %d has no component", app.Node)
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
	delete(a.nodes, id)
	return nil
}

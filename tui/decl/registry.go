package decl

import (
	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Build is everything a builder is handed to make one widget.
//
// It is decl.Construction translated into terms an adapter can use: the
// children arrive as live tui.Components rather than IDs, and the props keep
// their parsed positions so a type error can name the line.
type Build struct {
	// Type is the schema type name being built.
	Type string
	// Pos is where the schema declared this node.
	Pos parse.Position
	// Props are the declared properties in document order.
	Props []qml.SpecProp
	// Children are this node's children, already built, in declaration order.
	Children []tui.Component
	// ChildAttached holds each child's ATTACHED properties, aligned with
	// Children: `Dock.edge` written on child i is ChildAttached[i]["Dock.edge"].
	// Read it through [Build.Attached].
	ChildAttached []map[string]qml.SpecValue
	// SelfAttached holds the attached properties THIS node carries. They are
	// the parent's to act on, but a node may need to read one to face the right
	// way — a MenuBar docked at the bottom opens its dropdowns upwards.
	SelfAttached map[string]qml.SpecValue
	// FocusNominee is the first component in this node's subtree, in document
	// order, whose declaration says `focus: true` — or nil. A container that
	// owns a focus scope hands it to the runtime as where the keyboard starts.
	FocusNominee tui.Component
	// Emitters is one function per distinct signal declared on this node.
	// Calling one runs that signal's handlers under the engine's rules. A
	// builder wires these into the widget — usually at construction, since some
	// widgets accept a callback no other way — and must never invent its own
	// path from a widget event to a handler.
	Emitters map[string]func(args ...qml.SpecValue) error
	// Files is the filesystem file widgets list: the adapter's, set with
	// [WithFileSource]; the local disk when unset.
	Files widget.FileSource

	// sink is where [Build.Emitter] sends a handler error. It belongs to the
	// Adapter that made this Build rather than to the package, because two
	// adapters in one process must not share one error destination — and a
	// package-level hook would be exactly the hidden global state this library
	// refuses.
	sink func(error)
	// overlay is the adapter's WithOverlay: where a dialog outside any Window
	// opens.
	overlay *widget.OverlayHost
}

// Emitter returns the emitter for a signal, or a harmless no-op when the schema
// bound nothing to it. A builder wiring `WithOnActivate(b.Emitter("clicked"))`
// therefore does not have to nil-check first, and a widget whose signal the
// schema ignored simply does nothing when fired.
func (b Build) Emitter(signal string) func() {
	fn := b.Emitters[signal]
	if fn == nil {
		return func() {}
	}
	sink := b.sink
	return func() {
		// A toolkit callback is shaped func() with nowhere to put an error, so
		// the emission's error goes to the adapter's sink. Dropping it is the
		// one handling this design will not defend: a handler that failed
		// would otherwise look exactly like one that did nothing.
		if err := fn(); err != nil && sink != nil {
			sink(err)
		}
	}
}

// EmitterWith is Emitter for a signal with parameters: `accepted(selectedFile)`
// is raised as emit(path). The values are passed in the order the type
// declared its parameters.
func (b Build) EmitterWith(signal string) func(args ...qml.SpecValue) {
	fn := b.Emitters[signal]
	if fn == nil {
		return func(...qml.SpecValue) {}
	}
	sink := b.sink
	return func(args ...qml.SpecValue) {
		if err := fn(args...); err != nil && sink != nil {
			sink(err)
		}
	}
}

// Builder constructs one widget from a Build and reports which properties it
// consumed during construction.
//
// Reporting the consumed set is not bookkeeping: the engine applies only the
// rest, because a constructor-consumed property may have no setter or a
// non-idempotent one. A builder that consumes nothing returns a nil or empty
// slice and lets the engine apply everything through Apply.
type Builder func(Build) (tui.Component, []string, error)

// Registry maps schema type names to builders. It is the toolkit-specific half
// of the seam: a different toolkit ships a different registry against the same
// schema.
//
// A Registry is not safe for concurrent modification; register everything
// before the first Mount and it is only read afterwards.
type Registry struct {
	builders map[string]Builder
	// attached are the attaching schemas — `Dock` and its members.
	attached map[string]attachingSchema
	// honours says which containers read which attaching schemas.
	honours map[string]map[string]bool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{builders: map[string]Builder{}}
}

// Register adds a builder for a type name. Registering the same name twice is a
// programming mistake — two builders for one type means one silently wins — so
// it panics at the call that made it, the way a duplicate route would.
func Register(r *Registry, typeName string, b Builder) {
	if b == nil {
		panic("tui/decl.Register: nil builder for " + typeName)
	}
	if _, dup := r.builders[typeName]; dup {
		panic("tui/decl.Register: " + typeName + " is already registered")
	}
	r.builders[typeName] = b
}

// Setter applies one property to a widget of a known type. It is the runtime
// half: everything a builder did not consume at construction flows through
// here.
type Setter func(c tui.Component, v qml.SpecValue) error

var _ decl.Adapter = (*Adapter)(nil)

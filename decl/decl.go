package decl

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/parse"
)

// NodeID identifies one instantiated node for the lifetime of a [Tree].
//
// IDs are drawn from a monotonic counter and are NEVER REUSED. That is what
// makes a late callback safe: something that captured a node before it was
// destroyed cannot collide with a node created afterwards, so the engine can
// answer "that node is gone" instead of answering about a different node.
type NodeID uint64

// NoNode is the zero NodeID and never identifies a real node.
const NoNode NodeID = 0

// Provenance records where a property value came from. It exists for
// diagnostics: when a value is wrong, the first question is who set it.
type Provenance uint8

const (
	// FromSchema is a literal written in the schema file.
	FromSchema Provenance = iota
	// FromHost is a value the host program set through [Tree.SetProp].
	FromHost
	// FromBinding is a value derived from a schema binding — a Ref or a Call —
	// and applied because a source changed.
	FromBinding
)

// String renders the provenance for diagnostics.
func (p Provenance) String() string {
	switch p {
	case FromSchema:
		return "schema"
	case FromHost:
		return "host"
	case FromBinding:
		return "binding"
	default:
		return "unknown"
	}
}

// Application is the engine's output: node N's property P now has this value.
//
// It carries the VALUE, not an instruction to repaint. The adapter looks the
// property up in its own registry and invokes the setter; what that costs in
// layout or redraw is the widget's decision, and the engine has no opinion.
//
// Value keeps the position it was written at, so an adapter that rejects a
// value can report where it came from without the engine having to remember.
type Application struct {
	Node   NodeID
	Prop   string
	Value  parse.SpecValue
	Origin Provenance
}

// Construction is everything an adapter needs to build one node, handed over in
// a single immutable value.
//
// It exists because real constructors are not uniform. Some widgets take
// required arguments that have no setter at all — an orientation, a pair of
// children — so a seam that created a node first and configured it afterwards
// could not build them in any order. Everything a constructor might need is
// therefore present before Create is called.
type Construction struct {
	// Node is the identity the engine has already assigned.
	Node NodeID
	// Type is the schema type name to build.
	Type string
	// Pos is where the schema declared this node.
	Pos parse.Position

	// Props are the declared properties in DOCUMENT ORDER. An adapter may
	// consume any of them at construction and must say which, by returning
	// their names from Create.
	Props []parse.SpecProp

	// Children are this node's children, ALREADY BUILT, in declaration order.
	// A constructor that requires its children has them here.
	Children []NodeID

	// Emitters is one function per DISTINCT signal the schema bound on this
	// node, keyed by signal name.
	//
	// One per signal, not one per handler: a node with three handlers on the
	// same signal has ONE entry, and calling it runs all three under the
	// engine's rules. Wiring per handler would make a single widget event run
	// the list once per wire.
	//
	// The adapter wires these into the widget — usually at construction, since
	// that is the only chance some widgets give. Calling an emitter runs the
	// schema's handlers in order, with the cycle, depth and error rules
	// applied; the adapter must never call a resolved handler itself, because
	// doing so bypasses every one of those rules.
	//
	// An emitter returns the error that stopped the emission, if any. Toolkit
	// callbacks are usually shaped func() with nowhere to put an error, so it
	// is the ADAPTER's job to route it somewhere a person will see. Dropping it
	// silently is the one handling this design will not defend.
	Emitters map[string]func() error
}

// Adapter is the toolkit seam. Everything in its signatures is either a
// standard type, a parse type, or a decl type — never a widget, a surface or a
// window — which is what allows a second toolkit to implement it without the
// engine changing at all.
//
// An implementation reports an unknown type, an unknown property or an
// unresolvable handler as an error. It must not panic on schema content: a
// schema is input, and input is not a programming mistake.
// Handlers are NOT part of this interface. They used to be: the adapter turned a
// handler NAME into a function, which quietly made the adapter a second name
// scope beside the engine's own, and made `onClicked: save` indistinguishable
// from `onClicked: save()` because only the name survived the lookup. A host now
// hands its effects to the tree with [Tree.Inject], and the engine resolves them
// through the same registry every other name goes through.
type Adapter interface {
	// Create builds the node described by c and returns the names of the
	// properties it CONSUMED during construction.
	//
	// Reporting what was consumed is not bookkeeping. Some properties have no
	// setter at all, and some setters are not idempotent — one assigns and
	// invalidates unconditionally — so re-applying a constructor-consumed value
	// is either impossible or a second, visible effect. The engine applies only
	// what Create did not claim.
	Create(c Construction) (consumed []string, err error)

	// Apply sets one property after construction. The adapter owns the setter
	// and every consequence of calling it.
	Apply(app Application) error

	// Destroy releases a node. The engine calls it children-before-parents.
	Destroy(node NodeID) error
}

// Sentinel errors. Callers branch on these with errors.Is; the detail lives in
// the [SchemaError] that wraps them.
var (
	// ErrPhase reports an operation attempted at a moment the engine refuses
	// it — mounting from inside a handler, for example.
	ErrPhase = errors.New("decl: operation is not legal in this phase")

	// ErrSignalCycle reports a signal re-entering itself while still running.
	ErrSignalCycle = errors.New("decl: signal cycle")

	// ErrEmitDepth reports an emission chain longer than the configured cap.
	ErrEmitDepth = errors.New("decl: emission nested too deeply")

	// ErrNoSuchNode reports a NodeID that names nothing in this tree.
	ErrNoSuchNode = errors.New("decl: no such node")

	// ErrAdapter reports that the adapter refused something the engine asked
	// for. The adapter's own error is wrapped and reachable with errors.As.
	ErrAdapter = errors.New("decl: adapter refused")
)

// SchemaError is the engine's error type. It names what was being done, which
// node it concerned, and where in the schema that node came from, because an
// error about a declarative file is close to useless without the line.
//
// It is a VALUE with value receivers, so the zero value is a usable error and
// Error can never be reached through a nil pointer. Recover it with errors.As
// and SPELL THE TARGET AS A VALUE — a pointer target never matches:
//
//	var se decl.SchemaError
//	if errors.As(err, &se) { … }   // yes
type SchemaError struct {
	// Op is the operation that failed, in the words of this API: "mount",
	// "create", "apply", "bind", "emit", "set" or "destroy".
	Op string
	// Node is the node concerned, or NoNode when the failure is not about one.
	Node NodeID
	// Detail names the property, signal or type involved. Optional.
	Detail string
	// Pos is where the schema said it. Zero when there is no schema position.
	Pos parse.Position
	// Err is the sentinel or adapter error underneath.
	Err error
}

// Error implements error.
func (e SchemaError) Error() string {
	s := "decl: " + e.Op
	if e.Detail != "" {
		s += " " + e.Detail
	}
	if e.Node != NoNode {
		s += fmt.Sprintf(" (node %d)", e.Node)
	}
	if e.Pos.Line != 0 {
		s += " at " + e.Pos.String()
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

// Unwrap exposes the underlying error to errors.Is and errors.As.
func (e SchemaError) Unwrap() error { return e.Err }

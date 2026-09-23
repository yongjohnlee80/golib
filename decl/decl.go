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
)

// String renders the provenance for diagnostics.
func (p Provenance) String() string {
	switch p {
	case FromSchema:
		return "schema"
	case FromHost:
		return "host"
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

// Adapter is the toolkit seam. Everything in its signatures is either a
// standard type, a parse type, or a decl type — never a widget, a surface or a
// window — which is what allows a second toolkit to implement it without the
// engine changing at all.
//
// An implementation reports an unknown type, an unknown property or an
// unresolvable handler as an error. It must not panic on schema content: a
// schema is input, and input is not a programming mistake.
type Adapter interface {
	// Create instantiates the named type. The engine has already assigned the
	// node its ID and will use that ID in every later call about it.
	Create(node NodeID, typeName string, pos parse.Position) error

	// Apply sets one property. The adapter owns the setter and every
	// consequence of calling it.
	Apply(app Application) error

	// Attach makes child a child of parent, in the order Attach is called.
	Attach(parent, child NodeID) error

	// ResolveHandler turns a handler NAME from the schema into a function.
	// Resolution belongs to the adapter because the names refer to the host
	// program, which the engine cannot see.
	ResolveHandler(node NodeID, signal, name string, pos parse.Position) (func() error, error)

	// Destroy releases a node. The engine calls it in reverse mount order so a
	// child is always released before its parent.
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
	// "apply", "emit", "bind", "set".
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

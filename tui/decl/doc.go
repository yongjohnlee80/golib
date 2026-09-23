// Package decl adapts golib/decl onto golib/tui: it turns a parsed schema into
// real widgets and satisfies decl.Adapter.
//
// The engine owns identity, ordering and the signal contract and knows nothing
// about widgets. This package owns the other half — which type name builds which
// widget, which property calls which setter, and how a widget's event reaches
// the engine's emitter — and it is where every golib/tui import lives.
//
// # The registry is an explicit table
//
// There is no name-to-constructor magic, and there cannot be: several widgets
// are generic (NewList[T], NewSelect[T]) and cannot be instantiated from a
// string without reflection, which the library forbids in core paths. So a host
// program registers each type it wants a schema to reach, by hand:
//
//	reg := tuidecl.NewRegistry()
//	tuidecl.Register(reg, "Button", buildButton)   // a builder func
//
// A type a schema names but nobody registered is a positioned error, not a
// panic: a schema is input.
//
// # Why a builder rather than a constructor plus setters
//
// Real constructors are not uniform, and the differences are load-bearing. A
// Split takes its orientation and BOTH children as required arguments and offers
// no way to add them later. A Button takes its activation callback only as a
// construction option. A Text takes its string either way. A registry that
// created a bare widget and configured it afterwards could build none of the
// first two.
//
// So a builder receives everything at once — the declared properties, the
// already-built children, and one emitter per signal — and returns the widget
// plus the property names it consumed. The engine applies only what the builder
// did not take, because re-applying a consumed property is either impossible
// (there is no setter) or a second visible effect (the setter is not
// idempotent).
package decl

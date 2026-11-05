package mermaid

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// kind is a diagram kind this package draws: its layout, and what of it is drawn.
type kind struct {
	lay func(ctx context.Context, d pm.Diagram, m Measurer) (*Grid, error)
	// drawn is ErrUnsupported for a diagram of the kind this package doesn't draw (a flowchart with
	// subgraphs, until frames land); nil: every diagram of the kind is drawn.
	drawn func(d pm.Diagram) error
}

// kinds are the kinds drawn, each registered by its own file's init: no list of kinds is kept in
// one place, so a new kind edits no shared file.
var kinds = map[pm.Kind]kind{}

// register adds a kind; one registered twice panics, so a clash fails every test run.
func register(k pm.Kind, lay func(context.Context, pm.Diagram, Measurer) (*Grid, error), drawn func(pm.Diagram) error) {
	if _, dup := kinds[k]; dup {
		panic(fmt.Sprintf("tui/mermaid: kind %v registered twice", k))
	}
	kinds[k] = kind{lay: lay, drawn: drawn}
}

// layAs is a kind's typed layout as the registry holds it: a diagram of another type is
// ErrUnsupported.
func layAs[D pm.Diagram](lay func(context.Context, D, Measurer) (*Grid, error)) func(context.Context, pm.Diagram, Measurer) (*Grid, error) {
	return func(ctx context.Context, d pm.Diagram, m Measurer) (*Grid, error) {
		td, ok := d.(D)
		if !ok {
			return nil, ErrUnsupported
		}
		return lay(ctx, td, m)
	}
}

// Kinds reports the diagram kinds this package draws, in Kind order; a host shows any other its own
// way.
func Kinds() []pm.Kind { return slices.Sorted(maps.Keys(kinds)) }

// Supported reports whether k is one of Kinds.
func Supported(k pm.Kind) bool { _, ok := kinds[k]; return ok }

// Drawn is nil when d is drawn in cells: its kind is drawn, and so is this diagram of it (some
// kinds draw only part of their grammar). ErrUnsupported otherwise, for the host's own rendering.
func Drawn(d pm.Diagram) error {
	if d == nil {
		return ErrUnsupported // a nil model draws nothing, as an unknown kind
	}
	if v := reflect.ValueOf(d); v.Kind() == reflect.Pointer && v.IsNil() {
		return ErrUnsupported // a typed nil (a nil *MindmapDiagram) too: its layout would read it
	}
	k, ok := kinds[d.Kind()]
	if !ok {
		return ErrUnsupported
	}
	if k.drawn != nil {
		return k.drawn(d)
	}
	return nil
}

// Lay lays d out for painting: ErrUnsupported for a diagram not Drawn.
func Lay(ctx context.Context, d pm.Diagram, m Measurer) (*Grid, error) {
	if err := Drawn(d); err != nil {
		return nil, err
	}
	return kinds[d.Kind()].lay(ctx, d, m)
}

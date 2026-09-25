package tui

import (
	"fmt"
	"iter"
	"slices"

	"github.com/yongjohnlee80/golib/errs"
)

// MultiChild is the shared sibling-list machinery every multi-child
// Container embeds: the
// ordered children slice, plus the framework mirror — mutations mount,
// unmount, or move through Context immediately when mounted and defer to
// Init otherwise. It is the Go-embedding analogue of Flutter's
// ContainerRenderObjectMixin: mechanics shared, policy (layout, per-child
// weight/edge/align meaning) left to the embedding container. The zero
// value is usable.
//
// Init, Move, and Children promote verbatim through embedding. Add and
// Remove are EXPECTED to be shadowed by the embedding container: only it
// knows how to validate and record per-child metadata (a weight, an
// edge, an alignment) alongside the child. A container with no metadata
// uses the promoted methods as-is. The label prefixes panic messages so
// they name the concrete container.
type MultiChild struct {
	Visibility
	items []Component
	label string
	ctx   *Context // non-nil exactly while mounted
}

// SetVisible shows or hides the container and everything in it (visibility.go).
func (m *MultiChild) SetVisible(v bool) {
	if m.Show(v) && m.ctx != nil {
		m.ctx.RequestLayout()
	}
}

// Label sets the container name used in panic messages (constructors
// only; a zero label falls back to "Container").
func (m *MultiChild) Label(name string) { m.label = name }

func (m *MultiChild) name() string {
	if m.label == "" {
		return "Container"
	}
	return m.label
}

// Add appends children, mounting each immediately if the container is
// mounted; otherwise mounting is deferred to Init.
func (m *MultiChild) Add(children ...Component) {
	for _, c := range children {
		if c == nil {
			panic(fmt.Sprintf("tui: %s.Add: nil child", m.name()))
		}
		m.items = append(m.items, c)
		if m.ctx != nil {
			m.ctx.Mount(c)
		}
	}
}

// Remove unmounts child (cascade) and forgets it. A child this container
// does not own is a silent no-op.
func (m *MultiChild) Remove(child Component) {
	m.remove(child) // shadowable seam: containers pair it with Add
}

// remove is the metadata-free core; embedding containers shadow Remove
// (and call this) when they keep per-child records to clean up.
func (m *MultiChild) remove(child Component) {
	i := m.indexOf(child)
	if i < 0 {
		return
	}
	m.items = slices.Delete(m.items, i, i+1)
	if m.ctx != nil {
		m.ctx.Unmount(child)
	}
}

// Move relocates child to index to in document order, preserving its mount
// — no unmount/Init cycle. An unmounted container reorders items only; the
// framework mirror happens at Init. A child this container does not own is
// a silent no-op, mirroring Remove.
func (m *MultiChild) Move(child Component, to int) {
	i := m.indexOf(child)
	if i < 0 || i == to {
		return
	}
	if to < 0 || to >= len(m.items) {
		panic(errs.Fatal{
			Op:   fmt.Sprintf("tui: %s.Move", m.name()),
			Rule: fmt.Sprintf("index %d out of range [0,%d)", to, len(m.items)),
		})
	}
	m.items = slices.Insert(slices.Delete(m.items, i, i+1), to, child)
	if m.ctx != nil {
		m.ctx.Move(child, to)
	}
}

// Context is the container's mount context, nil while unmounted — as
// widget.Base's is, so any golib component can be asked for one.
func (m *MultiChild) Context() *Context { return m.ctx }

// Children enumerates in document order == focus order == paint order.
func (m *MultiChild) Children() iter.Seq[Component] {
	return func(yield func(Component) bool) {
		for _, c := range m.items {
			if !yield(c) {
				return
			}
		}
	}
}

// Init mounts the deferred children. Re-entrant across remounts; promoted
// through embedding so containers without their own Init work.
func (m *MultiChild) Init(ctx *Context) {
	m.ctx = ctx
	ctx.OnUnmount(func() { m.ctx = nil })
	for _, c := range m.items {
		ctx.Mount(c)
	}
}

// Items exposes the children slice for the container's own Layout/Render —
// read-only by convention: all mutation goes through Add/Remove/Move so
// the framework mirror stays in sync.
//
// Deprecated: Items returns the live backing slice, so "read-only" rests on
// convention rather than on the type — a caller can reorder or overwrite it
// and bypass the Mount/Unmount/Move mirror entirely. Use [MultiChild.Len] and
// [MultiChild.All], which cannot alias. Items remains for source
// compatibility and may be removed only in a deliberate breaking release.
func (m *MultiChild) Items() []Component { return m.items }

// Len reports the number of children this container owns, whether mounting is
// active or deferred. It is the aliasing-free replacement for len(Items()).
func (m *MultiChild) Len() int { return len(m.items) }

// All iterates the children in document order — which is also focus tab order
// and painter's z-order — yielding each child with its index.
//
// It is the aliasing-free replacement for ranging over Items: the sequence
// hands out no slice, so a consumer cannot reorder or overwrite the backing
// storage and desynchronise the framework mirror. Index and child together
// cover the callers that need position (see Flex), which a plain
// iter.Seq[Component] could not.
//
// Stopping early (break, or a false yield) is safe and simply ends iteration.
// Like [MultiChild.Items], All is a read view for the container's own
// Layout/Render; mutating the tree while iterating is not supported.
func (m *MultiChild) All() iter.Seq2[int, Component] {
	return func(yield func(int, Component) bool) {
		for i, c := range m.items {
			if !yield(i, c) {
				return
			}
		}
	}
}

// Ctx returns the mount context (nil before mount), for the container's
// LayoutChild/PlaceChild calls.
func (m *MultiChild) Ctx() *Context { return m.ctx }

func (m *MultiChild) indexOf(child Component) int {
	for i, c := range m.items {
		if c == child {
			return i
		}
	}
	return -1
}

package tui

import (
	"fmt"
	"iter"
	"slices"
)

// MultiChild is the shared sibling-list machinery every multi-child
// Container embeds (ADR-0004 §2.7; reorder semantics ADR-0011): the
// ordered item slice, plus the framework mirror — mutations mount,
// unmount, or move through Context immediately when mounted and defer to
// Init otherwise. It is the Go-embedding analogue of Flutter's
// ContainerRenderObjectMixin: mechanics shared, policy (layout, per-item
// weight/edge/align meaning) left to the embedding container.
//
// I is the container's per-item record (flexItem, stackItem, dockItem);
// compOf extracts its Component. Embedding promotes Init, Remove, Move,
// and Children verbatim — Container implementers shadow only Add, whose
// variadic Component signature cannot promote (it must wrap Components
// in I first).
//
// Zero value is NOT usable: construct via the embedding container's
// constructor (NewFlex/NewStack/NewDock), which sets compOf and label
// (label prefixes panic messages so they name the concrete container).
type MultiChild[I any] struct {
	items  []I
	compOf func(I) Component
	label  string
	ctx    *Context // non-nil exactly while mounted
}

// Add appends children, mounting each immediately if the container is
// mounted; otherwise mounting is deferred to Init. Containers shadow this
// to adapt Component arguments into their item records (checking
// weight/edge/align on the way).
func (m *MultiChild[I]) Add(children ...I) {
	for _, it := range children {
		c := m.compOf(it)
		if c == nil {
			panic(fmt.Sprintf("tui: %s.Add: nil child", m.label))
		}
		m.items = append(m.items, it)
		if m.ctx != nil {
			m.ctx.Mount(c)
		}
	}
}

// Remove unmounts child (cascade, ADR-0004 §2.4) and forgets it. A child
// this container does not own is a silent no-op.
func (m *MultiChild[I]) Remove(child Component) {
	if i := m.indexOf(child); i >= 0 {
		m.items = slices.Delete(m.items, i, i+1)
		if m.ctx != nil {
			m.ctx.Unmount(child)
		}
	}
}

// Move relocates child to index to in document order (ADR-0011 §2.2),
// preserving its mount — no unmount/Init cycle. An unmounted container
// reorders items only; the framework mirror happens at Init. A child this
// container does not own is a silent no-op, mirroring Remove.
func (m *MultiChild[I]) Move(child Component, to int) {
	i := m.indexOf(child)
	if i < 0 || i == to {
		return
	}
	if to < 0 || to >= len(m.items) {
		panic(fmt.Sprintf("tui: %s.Move: index %d out of range [0,%d)", m.label, to, len(m.items)))
	}
	it := m.items[i]
	m.items = slices.Insert(slices.Delete(m.items, i, i+1), to, it)
	if m.ctx != nil {
		m.ctx.Move(child, to)
	}
}

// Children enumerates in document order == focus order == paint order.
func (m *MultiChild[I]) Children() iter.Seq[Component] {
	return func(yield func(Component) bool) {
		for _, it := range m.items {
			if !yield(m.compOf(it)) {
				return
			}
		}
	}
}

// Init mounts the deferred children. Re-entrant across remounts; promoted
// through embedding so containers without their own Init work.
func (m *MultiChild[I]) Init(ctx *Context) {
	m.ctx = ctx
	ctx.OnUnmount(func() { m.ctx = nil })
	for _, it := range m.items {
		ctx.Mount(m.compOf(it))
	}
}

// Items exposes the item slice for the container's own Layout/Render —
// read-only by convention: all mutation goes through Add/Remove/Move so
// the framework mirror stays in sync.
func (m *MultiChild[I]) Items() []I { return m.items }

// Ctx returns the mount context (nil before mount), for the container's
// LayoutChild/PlaceChild calls.
func (m *MultiChild[I]) Ctx() *Context { return m.ctx }

func (m *MultiChild[I]) indexOf(child Component) int {
	for i, it := range m.items {
		if m.compOf(it) == child {
			return i
		}
	}
	return -1
}

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
	if m.Show(v) {
		relayoutMounted(m.ctx)
	}
}

// relayoutMounted asks for a layout pass through a container's mount context, while it is mounted
// (ctx non-nil); before that, its Init and the first frame lay it out anyway. A function, not a
// MultiChild method, so a base method calling it is no self-call an embedder could shadow.
func relayoutMounted(ctx *Context) {
	if ctx != nil {
		ctx.RequestLayout()
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

// Len reports the number of children this container owns, whether mounting is
// active or deferred.
func (m *MultiChild) Len() int { return len(m.items) }

// All iterates the children in document order — which is also focus tab order
// and painter's z-order — yielding each child with its index.
//
// The sequence hands out no slice, so a consumer cannot reorder or overwrite the backing
// storage and desynchronise the framework mirror. Index and child together
// cover the callers that need position (see Flex), which a plain
// iter.Seq[Component] could not.
//
// Stopping early (break, or a false yield) is safe and simply ends iteration.
// All is a read view for the container's own
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

// childMeta is a container's per-child record (a weight, an edge, a layer), keyed by the child and
// made on first use. A child with no entry reads as M's zero value; the container's Remove deletes
// the entry with the child.
type childMeta[M any] map[Component]M

func (t *childMeta[M]) set(child Component, m M) {
	if *t == nil {
		*t = make(childMeta[M])
	}
	(*t)[child] = m
}

// geometry is a container that paints nothing and consumes no event: its children do both on the
// sub-Surfaces and routes the framework gives them.
type geometry struct{}

// Render paints nothing: a geometry container is pure layout.
func (geometry) Render(Surface) {}

// HandleEvent consumes nothing; events bubble through.
func (geometry) HandleEvent(Event) bool { return false }

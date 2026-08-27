package tui

import (
	"fmt"
	"iter"
	"slices"
)

// Align positions a Stack child inside the stack's area when no explicit
// offset is given (ADR-0004 §2.7.4).
type Align uint8

const (
	AlignTopLeft Align = iota
	AlignTop
	AlignTopRight
	AlignLeft
	AlignCenter
	AlignRight
	AlignBottomLeft
	AlignBottom
	AlignBottomRight
)

// stackItem is one Stack layer: aligned, or at an explicit offset.
type stackItem struct {
	comp      Component
	align     Align
	hasOffset bool
	x, y      int
}

// Stack is z-ordered layering for floating windows, modals, dropdown
// popups, toasts (ADR-0004 §2.7.4). Children are laid out in order with
// LOOSE constraints of the stack's full area and positioned by alignment or
// explicit offset; LATER children paint on top; hit-testing visits them in
// reverse (ADR-0004 §2.5.2), so the topmost layer wins the mouse. A modal
// layer = a Stack child implementing FocusScope (ADR-0004 §2.6.3) that
// consumes all mouse events on its backdrop.
type Stack struct {
	items []stackItem
	ctx   *Context // non-nil exactly while mounted
}

var _ Container = (*Stack)(nil)

// NewStack builds an empty Stack.
func NewStack() *Stack { return &Stack{} }

// Add appends top-left-aligned layers (Container contract): later children
// paint on top.
func (s *Stack) Add(children ...Component) {
	for _, c := range children {
		s.push(stackItem{comp: c, align: AlignTopLeft})
	}
}

// AddAligned appends a layer positioned by align within the stack's area.
func (s *Stack) AddAligned(child Component, align Align) {
	if align > AlignBottomRight {
		panic("tui: Stack.AddAligned: invalid alignment")
	}
	s.push(stackItem{comp: child, align: align})
}

// AddAt appends a layer at an explicit stack-relative offset.
func (s *Stack) AddAt(child Component, x, y int) {
	s.push(stackItem{comp: child, hasOffset: true, x: x, y: y})
}

func (s *Stack) push(it stackItem) {
	if it.comp == nil {
		panic("tui: Stack: nil child")
	}
	s.items = append(s.items, it)
	if s.ctx != nil {
		s.ctx.Mount(it.comp)
	}
}

// Move relocates child to index to (Container contract), preserving its
// mount — no unmount/Init cycle. In a Stack, document order is Z-order:
// moving toward the end raises the layer (and its hit-test priority).
// An unmounted Stack reorders items only; the framework mirror happens
// at Init.
func (s *Stack) Move(child Component, to int) {
	for i, it := range s.items {
		if it.comp == child {
			if to < 0 || to >= len(s.items) {
				panic(fmt.Sprintf("tui: Stack.Move: index %d out of range [0,%d)", to, len(s.items)))
			}
			if i == to {
				return
			}
			s.items = slices.Insert(slices.Delete(s.items, i, i+1), to, it)
			if s.ctx != nil {
				s.ctx.Move(child, to)
			}
			return
		}
	}
}

// Remove unmounts child (cascade) and forgets it.
func (s *Stack) Remove(child Component) {
	for i, it := range s.items {
		if it.comp == child {
			s.items = append(s.items[:i], s.items[i+1:]...)
			if s.ctx != nil {
				s.ctx.Unmount(child)
			}
			return
		}
	}
}

// Children enumerates in document order == focus order == paint order
// (bottom layer first).
func (s *Stack) Children() iter.Seq[Component] {
	return func(yield func(Component) bool) {
		for _, it := range s.items {
			if !yield(it.comp) {
				return
			}
		}
	}
}

// Init mounts the deferred children. Re-entrant across remounts.
func (s *Stack) Init(ctx *Context) {
	s.ctx = ctx
	ctx.OnUnmount(func() { s.ctx = nil })
	for _, it := range s.items {
		ctx.Mount(it.comp)
	}
}

// Layout implements ADR-0004 §2.7.4: every layer gets loose constraints of
// the stack's full area, then alignment or explicit offset positions it.
func (s *Stack) Layout(c Constraints) Size {
	w, h := c.MaxW, c.MaxH
	maxW, maxH := 0, 0
	for _, it := range s.items {
		sz := s.ctx.LayoutChild(it.comp, Loose(Size{W: w, H: h}))
		var x, y int
		if it.hasOffset {
			x, y = it.x, it.y
		} else if w != Unbounded && h != Unbounded {
			x, y = alignPos(it.align, Size{W: w, H: h}, sz)
		}
		s.ctx.PlaceChild(it.comp, Rect{X: x, Y: y, W: sz.W, H: sz.H})
		maxW = max(maxW, x+sz.W)
		maxH = max(maxH, y+sz.H)
	}
	if w == Unbounded {
		w = maxW
	}
	if h == Unbounded {
		h = maxH
	}
	return c.Constrain(Size{W: w, H: h})
}

// alignPos resolves an alignment inside avail for a child of size sz.
func alignPos(a Align, avail, sz Size) (x, y int) {
	switch a {
	case AlignTop, AlignCenter, AlignBottom:
		x = (avail.W - sz.W) / 2
	case AlignTopRight, AlignRight, AlignBottomRight:
		x = avail.W - sz.W
	}
	switch a {
	case AlignLeft, AlignCenter, AlignRight:
		y = (avail.H - sz.H) / 2
	case AlignBottomLeft, AlignBottom, AlignBottomRight:
		y = avail.H - sz.H
	}
	return x, y
}

// Render paints nothing: a Stack is pure geometry.
func (s *Stack) Render(Surface) {}

// HandleEvent consumes nothing; events bubble through.
func (s *Stack) HandleEvent(Event) bool { return false }

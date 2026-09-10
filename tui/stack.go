package tui

// Align positions a Stack child inside the stack's area when no explicit
// offset is given.
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

// stackLayer is a layer's placement metadata: aligned within the stack's
// area, or at an explicit offset. Lives in a side table keyed by the
// child; the zero value (AlignTopLeft, no offset) is the default layer.
type stackLayer struct {
	align     Align
	hasOffset bool
	x, y      int
}

// Stack provides z-ordered layered layout for floating windows, modals,
// dropdown popups, toasts, and notification banners.
//
// # Layering, Sizing & Hit-Testing Rules
//
// Stack positions children atop one another within its bounding area:
//
//	┌────────────────────────────────────────────────────────┐
//	│ Stack Layer Z-Order                                    │
//	│                                                        │
//	│  Top Layer:    Toast Notifications / Dropdown Popups   │
//	│                ▲                                       │
//	│  Middle Layer: Modal Dialog Window (Centered)          │
//	│                ▲                                       │
//	│  Bottom Layer: Primary Application Workspace (Flex/Dock│
//	└────────────────────────────────────────────────────────┘
//
// Each layer is sized, placed, painted and hit-tested by these rules:
//
//  1. Sizing: All children are laid out with loose constraints up to the stack's
//     full area (0 <= W <= MaxW, 0 <= H <= MaxH).
//  2. Positioning: Children are positioned either via [Align] (such as [AlignCenter],
//     [AlignTopRight], [AlignBottom]) or at an explicit (x, y) offset via [Stack.AddAt].
//  3. Painting & Z-Order: Children paint bottom-to-top in document order (later children
//     paint over earlier children).
//  4. Hit-Testing: Mouse event dispatch traverses children in reverse document order
//     (top-to-bottom), ensuring the topmost layer wins pointer clicks. A modal layer
//     that consumes all mouse events on its backdrop acts as an input blocker for
//     underlying layers.
type Stack struct {
	MultiChild // order (== z-order), mount mirror, Move/Children/Init
	layers     map[Component]stackLayer
}

var _ Container = (*Stack)(nil)

// NewStack builds an empty Stack.
func NewStack() *Stack {
	s := &Stack{}
	s.Label("Stack")
	return s
}

// Add appends top-left-aligned layers (Container contract): later children
// paint on top.
func (s *Stack) Add(children ...Component) {
	s.MultiChild.Add(children...)
}

// AddAligned appends a layer positioned by align within the stack's area.
func (s *Stack) AddAligned(child Component, align Align) {
	if align > AlignBottomRight {
		panic("tui: Stack.AddAligned: invalid alignment")
	}
	s.MultiChild.Add(child)
	s.setLayer(child, stackLayer{align: align})
}

// AddAt appends a layer at an explicit stack-relative offset.
func (s *Stack) AddAt(child Component, x, y int) {
	s.MultiChild.Add(child)
	s.setLayer(child, stackLayer{hasOffset: true, x: x, y: y})
}

// Remove unmounts child (cascade) and forgets it, layer entry included.
func (s *Stack) Remove(child Component) {
	delete(s.layers, child)
	s.MultiChild.remove(child)
}

func (s *Stack) setLayer(child Component, l stackLayer) {
	if s.layers == nil {
		s.layers = make(map[Component]stackLayer)
	}
	s.layers[child] = l
}

func (s *Stack) Layout(c Constraints) Size {
	w, h := c.MaxW, c.MaxH
	maxW, maxH := 0, 0
	for _, it := range s.Items() {
		sz := s.Ctx().LayoutChild(it, Loose(Size{W: w, H: h}))
		l := s.layers[it] // zero value = AlignTopLeft
		var x, y int
		if l.hasOffset {
			x, y = l.x, l.y
		} else if w != Unbounded && h != Unbounded {
			x, y = alignPos(l.align, Size{W: w, H: h}, sz)
		}
		s.Ctx().PlaceChild(it, Rect{X: x, Y: y, W: sz.W, H: sz.H})
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

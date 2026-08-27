package tui

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
	MultiChild[stackItem] // order (== z-order), mount mirror, Remove/Move/Children/Init
}

var _ Container = (*Stack)(nil)

// NewStack builds an empty Stack.
func NewStack() *Stack {
	s := &Stack{}
	s.compOf = func(it stackItem) Component { return it.comp }
	s.label = "Stack"
	return s
}

// Add appends top-left-aligned layers (Container contract): later children
// paint on top.
func (s *Stack) Add(children ...Component) {
	for _, c := range children {
		s.MultiChild.Add(stackItem{comp: c, align: AlignTopLeft})
	}
}

// AddAligned appends a layer positioned by align within the stack's area.
func (s *Stack) AddAligned(child Component, align Align) {
	if align > AlignBottomRight {
		panic("tui: Stack.AddAligned: invalid alignment")
	}
	s.MultiChild.Add(stackItem{comp: child, align: align})
}

// AddAt appends a layer at an explicit stack-relative offset.
func (s *Stack) AddAt(child Component, x, y int) {
	s.MultiChild.Add(stackItem{comp: child, hasOffset: true, x: x, y: y})
}

func (s *Stack) Layout(c Constraints) Size {
	w, h := c.MaxW, c.MaxH
	maxW, maxH := 0, 0
	for _, it := range s.Items() {
		sz := s.Ctx().LayoutChild(it.comp, Loose(Size{W: w, H: h}))
		var x, y int
		if it.hasOffset {
			x, y = it.x, it.y
		} else if w != Unbounded && h != Unbounded {
			x, y = alignPos(it.align, Size{W: w, H: h}, sz)
		}
		s.Ctx().PlaceChild(it.comp, Rect{X: x, Y: y, W: sz.W, H: sz.H})
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

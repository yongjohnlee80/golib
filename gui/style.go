package gui

import (
	"reflect"

	"github.com/yongjohnlee80/golib/tui"
)

// A Painter draws one component natively: it returns the View, and the scope the view draws for
// (the node alone beneath its children, or its whole subtree as one control). ok=false leaves
// the component to its cells this frame. A painter reads the component's public state only, and
// never changes it. It keeps tui's geometry: text and anything the user points at or selects
// stay at the cells tui gave them, and it draws no caret.
type Painter func(c tui.Component) (v View, scope tui.NativeScope, ok bool)

// Style chooses a Painter for each component: by its exact Go type first, else by its
// accessible role. A component with neither keeps its cells.
type Style struct {
	byType map[reflect.Type]Painter
	byRole map[tui.AccessibleRole]Painter
}

// CellStyle draws nothing natively: every component keeps its cells.
var CellStyle = &Style{}

// NewStyle is an empty style, for a consumer's own painter set.
func NewStyle() *Style { return &Style{} }

// Clone is a copy of s that can be changed without changing s.
func (s *Style) Clone() *Style {
	c := &Style{byType: map[reflect.Type]Painter{}, byRole: map[tui.AccessibleRole]Painter{}}
	for k, v := range s.byType {
		c.byType[k] = v
	}
	for k, v := range s.byRole {
		c.byRole[k] = v
	}
	return c
}

// ForRole paints every component reporting role r with p; a nil p removes it. It returns s.
func (s *Style) ForRole(r tui.AccessibleRole, p Painter) *Style {
	if s.byRole == nil {
		s.byRole = map[tui.AccessibleRole]Painter{}
	}
	if p == nil {
		delete(s.byRole, r)
	} else {
		s.byRole[r] = p
	}
	return s
}

// ForType paints every component of exact type T with p, whatever its role; a nil p removes it.
// It returns s.
func ForType[T tui.Component](s *Style, p func(T) (View, tui.NativeScope, bool)) *Style {
	if s.byType == nil {
		s.byType = map[reflect.Type]Painter{}
	}
	t := reflect.TypeFor[T]()
	if p == nil {
		delete(s.byType, t)
	} else {
		s.byType[t] = func(c tui.Component) (View, tui.NativeScope, bool) { return p(c.(T)) }
	}
	return s
}

// painterFor is the painter for c, or nil.
func (s *Style) painterFor(c tui.Component) Painter {
	if s == nil {
		return nil
	}
	if p, ok := s.byType[reflect.TypeOf(c)]; ok {
		return p
	}
	if len(s.byRole) == 0 {
		return nil
	}
	return s.byRole[tui.RoleOf(c)]
}

// WithStyle sets the native style: NativeStyle() by default, CellStyle for none.
func WithStyle(s *Style) Option { return func(c *config) { c.style = s } }

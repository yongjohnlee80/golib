package gui

import (
	"reflect"

	"github.com/yongjohnlee80/golib/tui"
)

// Painter Dispatch Hierarchy:
//
//	          +------------------------+
//	          |  Component to Render   |
//	          +------------------------+
//	                      |
//	                      v
//	          [ Exact Go Type Match? ]
//	            /                 \
//	          Yes                  No
//	          /                     \
//	         v                       v
//	+----------------+      [ Role Match in Style? ]
//	| Execute Type   |        /                  \
//	| Painter        |      Yes                   No
//	+----------------+      /                      \
//	                       v                        v
//	              +----------------+        +---------------+
//	              | Execute Role   |        | Render Cell   |
//	              | Painter        |        | Grid Fallback |
//	              +----------------+        +---------------+
//
// A Painter renders a component natively using vector operations. It returns the [View]
// to execute, the visual [tui.NativeScope] (whether the view applies only to the individual
// node beneath children or the entire composite subtree), and whether native rendering was accepted (ok).
//
// If ok is false, the engine leaves the component to its default cell grid rendering for the frame.
// Painters must only read public widget state and never alter component state.
type Painter func(c tui.Component) (v View, scope tui.NativeScope, ok bool)

// Style configures the registry of vector painters for UI components. Painters can be registered
// by exact concrete Go type (highest priority) or by accessible role (general fallback).
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

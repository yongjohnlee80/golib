package tui

import "math"

// Size represents a two-dimensional extent in terminal cells.
type Size struct{ W, H int }

// Point is a position in terminal cells. Its frame is contextual in the same
// way Rect's is: a pointer position delivered to a component is local to that
// component, and may be negative or past its edge when a captured gesture has
// travelled outside it.
type Point struct{ X, Y int }

// Rect is a positioned rectangle in terminal cell coordinates.
//
// Coordinate Frame Semantics:
// The meaning of (X, Y) is strictly contextual:
//   - During Layout: (X, Y) is parent-relative (offset from the parent container's top-left corner).
//   - During Render: Surface coordinates are local (0,0 is the component's own origin).
//   - In Node.absRect: (X, Y) is terminal screen-absolute (used for mouse hit-testing and hardware cursor positioning).
type Rect struct{ X, Y, W, H int }

// Empty reports whether r covers no cells (width or height <= 0).
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Intersect returns the intersection rectangle of r and o.
// An empty intersection is returned as a Rect with W=0 and H=0 positioned
// at the clamped coordinate boundary.
func (r Rect) Intersect(o Rect) Rect {
	x1 := max(r.X, o.X)
	y1 := max(r.Y, o.Y)
	x2 := min(r.X+r.W, o.X+o.W)
	y2 := min(r.Y+r.H, o.Y+o.H)
	return Rect{X: x1, Y: y1, W: max(x2-x1, 0), H: max(y2-y1, 0)}
}

// Contains reports whether the terminal cell at (x, y) lies inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Constraints bound the allowable dimensions a child component may choose in Layout:
// MinW <= W <= MaxW and MinH <= H <= MaxH.
//
// Invariants:
// For both axes, 0 <= Min <= Max. Passing invalid constraints is a programming error.
type Constraints struct{ MinW, MaxW, MinH, MaxH int }

// Unbounded represents an axis with no maximum upper bound (e.g. scrollable viewports
// offering infinite extent along their scrolling axis).
//
// A child component given an unbounded constraint along an axis must return its
// intrinsic preferred content size along that axis — never Unbounded itself.
const Unbounded = math.MaxInt

// Tight returns constraints that force the child's chosen size: Min == Max == s.
func Tight(s Size) Constraints {
	return Constraints{MinW: s.W, MaxW: s.W, MinH: s.H, MaxH: s.H}
}

// Loose returns constraints that let the child size to content up to s:
// Min = 0 and Max = s.
func Loose(s Size) Constraints {
	return Constraints{MinW: 0, MaxW: s.W, MinH: 0, MaxH: s.H}
}

// Constrain clamps s into c, guaranteeing that MinW <= W <= MaxW and MinH <= H <= MaxH.
// The framework applies Constrain to every component Layout result to prevent geometry corruption.
func (c Constraints) Constrain(s Size) Size {
	return Size{
		W: min(max(s.W, c.MinW), c.MaxW),
		H: min(max(s.H, c.MinH), c.MaxH),
	}
}

// IsTight reports whether c forces a single fixed Size on both axes.
func (c Constraints) IsTight() bool {
	return c.MinW == c.MaxW && c.MinH == c.MaxH
}

// ConstraintViolation records an incident where a component's Layout implementation
// returned a Size that violated the Constraints passed to it.
//
// The framework automatically clamps the returned Size and records the violation.
// In tests, TestBackend retains violations for assertion via ConstraintViolations()
// and FailOnViolations(). In production, violations are reported through the configured logger.
type ConstraintViolation struct {
	Node NodeID      // ID of the offending component node
	Type string      // dynamic Go type of the component
	Got  Size        // the out-of-bounds Size returned by Layout
	C    Constraints // the Constraints passed into Layout
}

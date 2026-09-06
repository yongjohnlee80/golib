package tui

import "math"

// Size is a width/height pair in terminal cells.
type Size struct{ W, H int }

// Rect is a positioned rectangle in terminal cells. Coordinate meaning is
// contextual: parent-relative in layout, surface-local in rendering.
type Rect struct{ X, Y, W, H int }

// Empty reports whether r covers no cells.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Intersect returns the intersection of r and o. An empty intersection is
// returned as a Rect with W and H of 0 (position at the clamped corner).
func (r Rect) Intersect(o Rect) Rect {
	x1 := max(r.X, o.X)
	y1 := max(r.Y, o.Y)
	x2 := min(r.X+r.W, o.X+o.W)
	y2 := min(r.Y+r.H, o.Y+o.H)
	return Rect{X: x1, Y: y1, W: max(x2-x1, 0), H: max(y2-y1, 0)}
}

// Contains reports whether the cell at (x, y) lies inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Constraints bound the Size a child may return from Layout: MinW <= W <=
// MaxW, MinH <= H <= MaxH. Invariants: 0 <= Min <= Max per axis.
type Constraints struct{ MinW, MaxW, MinH, MaxH int }

// Unbounded marks an axis with no maximum (scrollable viewports pass it to
// their content, only on the scroll axis). A child asked for an unbounded
// axis must return its intrinsic extent, never Unbounded.
const Unbounded = math.MaxInt

// Tight returns constraints that force the child's answer: Min == Max == s.
func Tight(s Size) Constraints {
	return Constraints{MinW: s.W, MaxW: s.W, MinH: s.H, MaxH: s.H}
}

// Loose returns constraints that let the child size to content up to s:
// Min = 0, Max = s.
func Loose(s Size) Constraints {
	return Constraints{MinW: 0, MaxW: s.W, MinH: 0, MaxH: s.H}
}

// Constrain clamps s into c. The framework applies it to every Layout
// return so a misbehaving widget cannot corrupt sibling geometry; the clamp
// is recorded as a ConstraintViolation.
func (c Constraints) Constrain(s Size) Size {
	return Size{
		W: min(max(s.W, c.MinW), c.MaxW),
		H: min(max(s.H, c.MinH), c.MaxH),
	}
}

// IsTight reports whether c forces a single Size on both axes.
func (c Constraints) IsTight() bool {
	return c.MinW == c.MaxW && c.MinH == c.MaxH
}

// ConstraintViolation records one clamped Layout return: a component returned
// a Size outside its Constraints and the
// framework clamped it. Kept per run, bounded. Production apps observe them
// via WithLogger; TestBackend retains them for assertion
// (ConstraintViolations / FailOnViolations).
type ConstraintViolation struct {
	Node NodeID
	Type string      // dynamic component type, for the failure message
	Got  Size        // what Layout returned
	C    Constraints // what it was given
}

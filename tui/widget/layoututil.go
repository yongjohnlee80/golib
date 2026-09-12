package widget

import "github.com/yongjohnlee80/golib/tui"

// Layout constraint calculation and frame sizing utilities shared across package widget.
//
// # Architectural Model: Constraint Transformation
//
// During the layout phase, container and control widgets evaluate [tui.Constraints] to compute
// cell dimensions. Widgets frequently need to resolve greedy expansion against unbounded axes or
// deduct border and padding frames before passing constraints down to child components:
//
//	Incoming Constraint Axis (e.g. MaxW)
//	           │
//	           ├── Is Max == tui.Unbounded?
//	           │   ├── YES: boundedMax -> return Min (intrinsic size)
//	           │   └── NO:  boundedMax -> return Max (greedy fill)
//	           │
//	           └── SubFrame(Max, frameSize)
//	               ├── Max == Unbounded -> return Unbounded
//	               └── Max bounded      -> return max(Max - frameSize, 0)
//
// # Architectural Invariants
//
//  1. Unbounded Axis Safety: When a constraint axis is [tui.Unbounded], greedy widgets must never
//     return Unbounded as their concrete layout dimension; [boundedMax] resolves to the intrinsic
//     minimum extent (`minV`), guaranteeing bounded geometry.
//  2. Frame Underflow Clamp: Subtracting border and padding frames via [subFrame] clamps at zero,
//     preventing negative width or height constraints from reaching child components when the
//     allocated container is smaller than its chrome.
//  3. Unbounded Preservation: [subFrame] preserves [tui.Unbounded] when `maxV == tui.Unbounded`,
//     allowing child widgets to calculate their unconstrained natural bounds.
//
// # Concurrency Model
//
//   - Stateless Functions: All helpers in layoututil.go are pure, stateless functions safe for
//     concurrent invocation across any goroutine.

// boundedMax resolves a constraint axis for greedy widgets: the max when
// bounded, else the min (a greedy widget asked for its intrinsic extent on
// an unbounded axis must not answer Unbounded).
func boundedMax(maxV, minV int) int {
	if maxV == tui.Unbounded {
		return minV
	}
	return maxV
}

// subFrame subtracts a frame size from a constraint max, preserving
// Unbounded.
func subFrame(maxV, frame int) int {
	if maxV == tui.Unbounded {
		return tui.Unbounded
	}
	return max(maxV-frame, 0)
}

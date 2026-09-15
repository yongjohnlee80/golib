package widget

import (
	"reflect"

	"github.com/yongjohnlee80/golib/tui"
)

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

// nilLike reports whether v is nil OR a TYPED nil — an interface holding a nil
// pointer, a nil func, or another nil-capable zero value.
//
// A plain v == nil catches only the first. A nil *myAction and an
// AnchorPolicyFunc(nil) both satisfy their interface with a live type
// descriptor, so they pass an == nil check and then panic on call — somewhere
// else entirely, in a layout pass or a consumer's executor, with nothing naming
// what supplied them. The runtime already defines a typed nil as ABSENT at its
// own action and resolver seams; this is the same rule for the seams this
// package owns, in ONE place, because three slightly different local rules is
// how two of them come to disagree.
//
// The kind switch matters as much as the reflection: IsNil panics on kinds that
// cannot be nil, so asking it about a struct value would turn a validity check
// into the crash it exists to prevent.
func nilLike(v any) bool {
	if v == nil {
		return true
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return rv.IsNil()
	}
	return false
}

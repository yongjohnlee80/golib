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
//	           │   ├── YES: kit.BoundedMax -> return Min (intrinsic size)
//	           │   └── NO:  kit.BoundedMax -> return Max (greedy fill)
//	           │
//	           └── SubFrame(Max, frameSize)
//	               ├── Max == Unbounded -> return Unbounded
//	               └── Max bounded      -> return max(Max - frameSize, 0)
//
// # Architectural Invariants
//
//  1. Unbounded Axis Safety: When a constraint axis is [tui.Unbounded], greedy widgets must never
//     return Unbounded as their concrete layout dimension; [kit.BoundedMax] resolves to the intrinsic
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

// clampPct is a percentage kept within 0 to 100, the one rule every percent option applies.
func clampPct(p int) int { return min(max(p, 0), 100) }

// pctOf is pct percent of extent cells, at least one: a share that rounds to nothing still shows.
func pctOf(extent, pct int) int { return max(extent*pct/100, 1) }

// subFrame subtracts a frame size from a constraint max, preserving
// Unbounded.
func subFrame(maxV, frame int) int {
	if maxV == tui.Unbounded {
		return tui.Unbounded
	}
	return max(maxV-frame, 0)
}

// actionValue is a in the form a handler matches: for a pointer to an action value, the value it
// points at, else a unchanged. Every widget handler switches on actionValue(inv.Action), so T and
// *T are one action wherever they arrive, as WindowCore always took them. A nil pointer, or a
// pointer whose element is no Action (one with pointer-receiver methods), stays as given.
func actionValue(a tui.Action) tui.Action {
	rv := reflect.ValueOf(a)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return a
	}
	if v, ok := rv.Elem().Interface().(tui.Action); ok {
		return v
	}
	return a
}

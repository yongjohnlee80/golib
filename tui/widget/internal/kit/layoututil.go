package kit

import "github.com/yongjohnlee80/golib/tui"

// BoundedMax resolves a constraint axis for greedy widgets: the max when
// bounded, else the min (a greedy widget asked for its intrinsic extent on
// an unbounded axis must not answer Unbounded).
func BoundedMax(maxV, minV int) int {
	if maxV == tui.Unbounded {
		return minV
	}
	return maxV
}

// FillW and FillH are the extent a greedy widget takes on each axis: all it is offered, or, offered
// an unbounded axis, its minimum, at least one cell.
func FillW(c tui.Constraints) int { return BoundedMax(c.MaxW, max(c.MinW, 1)) }

func FillH(c tui.Constraints) int { return BoundedMax(c.MaxH, max(c.MinH, 1)) }

// NilLike is the runtime's rule for an absent value (tui.NilLike): nil, or a typed nil. A nil
// *myAction and an AnchorPolicyFunc(nil) both satisfy their interface with a live type descriptor,
// so they pass an == nil check and then panic on call, in a layout pass or a consumer's executor,
// with nothing naming what supplied them. The seams this package owns refuse them by that one rule.
func NilLike(v any) bool { return tui.NilLike(v) }

package tui

import (
	"slices"
	"testing"
)

// TestRectIntersect covers the clipping primitive and how clips compose.
func TestRectIntersect(t *testing.T) {
	tests := []struct {
		name string
		a, b Rect
		want Rect
	}{
		{"overlap", Rect{0, 0, 10, 10}, Rect{5, 5, 10, 10}, Rect{5, 5, 5, 5}},
		{"contained", Rect{0, 0, 10, 10}, Rect{2, 3, 4, 5}, Rect{2, 3, 4, 5}},
		{"disjoint is empty", Rect{0, 0, 5, 5}, Rect{10, 10, 5, 5}, Rect{10, 10, 0, 0}},
		{"touching edges is empty", Rect{0, 0, 5, 5}, Rect{5, 0, 5, 5}, Rect{5, 0, 0, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Intersect(tt.b)
			if got != tt.want {
				t.Errorf("Intersect = %+v, want %+v", got, tt.want)
			}
			if rev := tt.b.Intersect(tt.a); rev.W != got.W || rev.H != got.H {
				t.Errorf("Intersect not symmetric in extent: %+v vs %+v", got, rev)
			}
		})
	}
}

func TestRectEmptyAndContains(t *testing.T) {
	if !(Rect{0, 0, 0, 5}).Empty() || !(Rect{0, 0, 5, -1}).Empty() {
		t.Error("zero/negative extents must be Empty")
	}
	r := Rect{2, 3, 4, 2}
	for _, tc := range []struct {
		x, y int
		want bool
	}{
		{2, 3, true}, {5, 4, true}, {6, 3, false}, {2, 5, false}, {1, 3, false},
	} {
		if got := r.Contains(tc.x, tc.y); got != tc.want {
			t.Errorf("Contains(%d, %d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

// TestConstraints covers the constraint modes: Tight forces the answer, Loose
// admits [0, max], Constrain clamps a misbehaving Size into the box.
func TestConstraints(t *testing.T) {
	tight := Tight(Size{W: 8, H: 3})
	if !tight.IsTight() {
		t.Error("Tight(...) must be IsTight")
	}
	if got := tight.Constrain(Size{W: 100, H: 0}); got != (Size{W: 8, H: 3}) {
		t.Errorf("tight Constrain = %+v, want forced size", got)
	}

	loose := Loose(Size{W: 8, H: 3})
	if loose.IsTight() {
		t.Error("Loose(...) must not be IsTight")
	}
	tests := []struct {
		in, want Size
	}{
		{Size{W: 4, H: 2}, Size{W: 4, H: 2}},   // inside: unchanged
		{Size{W: 100, H: 2}, Size{W: 8, H: 2}}, // clamp W to max
		{Size{W: -1, H: 2}, Size{W: 0, H: 2}},  // clamp W to min
		{Size{W: 4, H: 100}, Size{W: 4, H: 3}}, // clamp H to max
	}
	for _, tt := range tests {
		if got := loose.Constrain(tt.in); got != tt.want {
			t.Errorf("Constrain(%+v) = %+v, want %+v", tt.in, got, tt.want)
		}
	}

	// Unbounded max: an intrinsic extent passes through unclamped.
	scroll := Constraints{MinW: 0, MaxW: 20, MinH: 0, MaxH: Unbounded}
	if got := scroll.Constrain(Size{W: 10, H: 100000}); got != (Size{W: 10, H: 100000}) {
		t.Errorf("unbounded axis clamped: %+v", got)
	}
}

// TestRectAndPointFrames: Translate moves a rectangle or a point, RelativeTo states it in another
// rectangle's frame (the inverse of Translate by that rectangle's origin), and Origin is the corner.
func TestRectAndPointFrames(t *testing.T) {
	r := Rect{X: 5, Y: 7, W: 3, H: 2}
	if got := r.Translate(2, -3); got != (Rect{X: 7, Y: 4, W: 3, H: 2}) {
		t.Errorf("Translate = %+v", got)
	}
	frame := Rect{X: 4, Y: 5, W: 20, H: 20}
	if got := r.RelativeTo(frame); got != (Rect{X: 1, Y: 2, W: 3, H: 2}) {
		t.Errorf("RelativeTo = %+v", got)
	}
	if got := r.RelativeTo(frame).Translate(frame.X, frame.Y); got != r {
		t.Errorf("RelativeTo then Translate by the frame's origin = %+v, want %+v back", got, r)
	}
	if got := r.Origin(); got != (Point{X: 5, Y: 7}) {
		t.Errorf("Origin = %+v", got)
	}
	p := Point{X: 10, Y: 1}
	if got := p.RelativeTo(frame); got != (Point{X: 6, Y: -4}) {
		t.Errorf("Point.RelativeTo = %+v, want it outside the frame, negative", got)
	}
	if got := p.Translate(-10, 2); got != (Point{X: 0, Y: 3}) {
		t.Errorf("Point.Translate = %+v", got)
	}
}

// TestAlignOffset: each alignment's corner for content inside an area, and an overhang when the
// content is the larger.
func TestAlignOffset(t *testing.T) {
	avail, sz := Size{W: 10, H: 6}, Size{W: 4, H: 2}
	for a, want := range map[Align][2]int{
		AlignTopLeft: {0, 0}, AlignTop: {3, 0}, AlignTopRight: {6, 0},
		AlignLeft: {0, 2}, AlignCenter: {3, 2}, AlignRight: {6, 2},
		AlignBottomLeft: {0, 4}, AlignBottom: {3, 4}, AlignBottomRight: {6, 4},
	} {
		if x, y := a.Offset(avail, sz); [2]int{x, y} != want {
			t.Errorf("%v.Offset = (%d, %d), want %v", a, x, y, want)
		}
	}
	if x, _ := AlignRight.Offset(Size{W: 4, H: 1}, Size{W: 6, H: 1}); x != -2 {
		t.Errorf("an overhang flush right is at %d, want -2", x)
	}
}

// TestRectWithin: grown to the minimum, shrunk to the area, slid inside; the left and top edges win
// when the area is the smaller.
func TestRectWithin(t *testing.T) {
	area := Rect{X: 2, Y: 1, W: 10, H: 5}
	for _, c := range []struct {
		r    Rect
		min  int
		want Rect
	}{
		{Rect{X: 9, Y: 4, W: 5, H: 3}, 0, Rect{X: 7, Y: 3, W: 5, H: 3}},   // slid back inside
		{Rect{X: 0, Y: 0, W: 20, H: 9}, 0, Rect{X: 2, Y: 1, W: 10, H: 5}}, // shrunk to the area
		{Rect{X: 5, Y: 2, W: 0, H: 1}, 3, Rect{X: 5, Y: 2, W: 3, H: 3}},   // grown to the minimum
		{Rect{X: -4, Y: -4, W: 2, H: 2}, 1, Rect{X: 2, Y: 1, W: 2, H: 2}}, // left and top
	} {
		if got := c.r.Within(area, c.min); got != c.want {
			t.Errorf("%+v.Within(area, %d) = %+v, want %+v", c.r, c.min, got, c.want)
		}
	}
}

// largestRemainder gives each positive weight its floor share, and the cells left over to the
// largest fractional remainders, ties to the lowest index; a zero weight gets nothing, and the
// shares always add up to the total.
func TestLargestRemainderSplitsWithoutAGap(t *testing.T) {
	for _, c := range []struct {
		total   int
		weights []int
		want    []int
	}{
		{10, []int{1, 1, 1}, []int{4, 3, 3}},       // equal remainders: the lowest index first
		{10, []int{1, 0, 2}, []int{3, 0, 7}},       // 10/3 and 20/3: 2/3 beats 1/3
		{7, []int{3, 3, 1}, []int{3, 3, 1}},        // exact shares, nothing left over
		{5, []int{2, 2, 2, 2}, []int{2, 1, 1, 1}},  // one cell left, four equal remainders
		{0, []int{1, 2}, []int{0, 0}},              // nothing to split
		{9, []int{0, 0}, []int{0, 0}},              // no weight: nothing given
		{11, []int{5, 0, 1, 3}, []int{6, 0, 1, 4}}, // 55/9, 11/9, 33/9: 6 r1, 1 r2, 3 r6
	} {
		got := largestRemainder(c.total, c.weights)
		if !slices.Equal(got, c.want) {
			t.Errorf("largestRemainder(%d, %v) = %v, want %v", c.total, c.weights, got, c.want)
		}
	}
}

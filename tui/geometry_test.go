package tui

import "testing"

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

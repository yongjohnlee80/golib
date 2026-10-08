package layout

import "testing"

func TestInversions(t *testing.T) {
	for _, c := range []struct {
		xs   []int
		n    int
		want int
	}{
		{nil, 0, 0},
		{[]int{0, 1, 2}, 3, 0},
		{[]int{2, 1, 0}, 3, 3},
		{[]int{1, 0, 1, 0}, 2, 3},
	} {
		if got := inversions(c.xs, c.n); got != c.want {
			t.Errorf("inversions(%v) = %d, want %d", c.xs, got, c.want)
		}
	}
}

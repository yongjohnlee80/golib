package dao

import (
	"math"
	"testing"
)

// TestAHugeLimitBindsAsTheLargest: a LIMIT or OFFSET past math.MaxInt64 binds as math.MaxInt64, not
// as the negative a plain conversion wraps it to.
func TestAHugeLimitBindsAsTheLargest(t *testing.T) {
	for _, n := range []uint64{math.MaxUint64, math.MaxInt64 + 1} {
		if got := clampInt64(n); got != math.MaxInt64 {
			t.Errorf("%d binds as %d", n, got)
		}
	}
	if got := clampInt64(10); got != 10 {
		t.Errorf("10 binds as %d", got)
	}
}

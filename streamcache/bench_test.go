package streamcache

import (
	"strings"
	"testing"
)

// F4 was MEASURED, so its fix is measured too rather than asserted.
//
// Acquire and AppendTo were O(k²) in the number of segments a span covers,
// because each one restarted a linear scan per segment. Lector measured 64/128/
// 256 KiB at 64-byte segments: Acquire .404/1.514/6.029 ms, Append .361/1.557/
// 6.250 ms — a 4x cost for a 2x span, which is the quadratic signature.
//
// Run these three and look at the SHAPE, not the absolute figures: doubling the
// span should roughly double the time. If it quadruples, the quadratic is back.
func benchSpan(b *testing.B, size int) {
	src := strings.Repeat("x", size)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := New(strings.NewReader(src), WithSegmentSize(64))
		v, err := c.Acquire(0, int64(size))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := v.AppendTo(make([]byte, 0, size)); err != nil {
			b.Fatal(err)
		}
		v.Close()
	}
}

func BenchmarkSpan64KiB(b *testing.B)  { benchSpan(b, 64<<10) }
func BenchmarkSpan128KiB(b *testing.B) { benchSpan(b, 128<<10) }
func BenchmarkSpan256KiB(b *testing.B) { benchSpan(b, 256<<10) }

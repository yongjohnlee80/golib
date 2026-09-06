package streamcache

import (
	"strings"
	"testing"
)

// Benchmarks are SPLIT so that filling and allocation cannot mask the operation
// under test (lector r6). The first version built a whole Cache inside the
// timed loop, so Acquire and AppendTo were measured through the noise of
// reading 256 KiB — which is how a quadratic in reclamation hid behind a
// linear-looking total.

func filled(b *testing.B, size, seg int) *Cache {
	b.Helper()
	c := New(strings.NewReader(strings.Repeat("x", size)), WithSegmentSize(seg))
	if _, err := c.Ensure(0, size); err != nil {
		b.Fatal(err)
	}
	return c
}

// Acquire alone, over a pre-filled cache. Doubling the span should roughly
// double the time; quadrupling means a per-segment search has come back.
func benchAcquire(b *testing.B, size int) {
	c := filled(b, size, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, err := c.Acquire(0, int64(size))
		if err != nil {
			b.Fatal(err)
		}
		v.Close()
	}
}

func BenchmarkAcquire64KiB(b *testing.B)  { benchAcquire(b, 64<<10) }
func BenchmarkAcquire128KiB(b *testing.B) { benchAcquire(b, 128<<10) }
func BenchmarkAcquire256KiB(b *testing.B) { benchAcquire(b, 256<<10) }

// AppendTo alone, over a view acquired outside the timed loop.
func benchAppend(b *testing.B, size int) {
	c := filled(b, size, 64)
	v, err := c.Acquire(0, int64(size))
	if err != nil {
		b.Fatal(err)
	}
	defer v.Close()
	dst := make([]byte, 0, size)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := v.AppendTo(dst[:0]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAppend64KiB(b *testing.B)  { benchAppend(b, 64<<10) }
func BenchmarkAppend128KiB(b *testing.B) { benchAppend(b, 128<<10) }
func BenchmarkAppend256KiB(b *testing.B) { benchAppend(b, 256<<10) }

// CLOSE SCALING — the measurement that catches the defect this benchmark file
// exists because of.
//
// Reclamation used to rebuild the whole directory on every final Close, so its
// cost grew with the number of RETAINED segments rather than with the number
// FREED: ~4x per doubling (512 segments ~0.19 ms, 4096 ~12.5 ms), all under
// c.mu. A cursor should make this roughly linear in the closes.
//
// Read the SHAPE across the four sizes, not the absolute figures. Run BOTH
// orders: closing oldest-first lets every close truncate the directory head,
// which is the EASY case. Closing newest-first strands every freed entry behind
// a still-held one, so it is the case the amortised compaction has to carry —
// and a benchmark that only ran the easy order would report a shape the
// implementation does not have.
func benchClose(b *testing.B, segments int) { benchCloseOrder(b, segments, false) }

func benchCloseOrder(b *testing.B, segments int, newestFirst bool) {
	const seg = 64
	size := segments * seg
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		c := filled(b, size, seg)
		// One view per segment, each independently held, so no single Close can
		// reclaim more than its own segment — the shape that made the old scan
		// quadratic.
		views := make([]*View, 0, segments)
		for k := 0; k < segments; k++ {
			v, err := c.Acquire(int64(k*seg), int64(k*seg)+1)
			if err != nil {
				b.Fatal(err)
			}
			views = append(views, v)
		}
		c.Release(int64(size)) // ask for everything; every segment is held
		b.StartTimer()

		if newestFirst {
			for k := len(views) - 1; k >= 0; k-- { // the timed part
				views[k].Close()
			}
		} else {
			for _, v := range views { // the timed part: reclamation on close
				v.Close()
			}
		}
	}
}

func BenchmarkClose512Segments(b *testing.B)  { benchClose(b, 512) }
func BenchmarkClose1024Segments(b *testing.B) { benchClose(b, 1024) }
func BenchmarkClose2048Segments(b *testing.B) { benchClose(b, 2048) }
func BenchmarkClose4096Segments(b *testing.B) { benchClose(b, 4096) }

func BenchmarkCloseNewestFirst512Segments(b *testing.B)  { benchCloseOrder(b, 512, true) }
func BenchmarkCloseNewestFirst1024Segments(b *testing.B) { benchCloseOrder(b, 1024, true) }
func BenchmarkCloseNewestFirst2048Segments(b *testing.B) { benchCloseOrder(b, 2048, true) }
func BenchmarkCloseNewestFirst4096Segments(b *testing.B) { benchCloseOrder(b, 4096, true) }

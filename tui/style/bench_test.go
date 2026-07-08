package style

import "testing"

// Benchmarks co-located per golib convention (ADR-0006 §5 criterion 8).
// BenchmarkStyleSet must report 0 allocs/op with extras == nil; the
// resolver-side BenchmarkResolve lives in package tui with the resolver
// (§2.6).

var (
	sinkStyle Style
	sinkBool  bool
)

// BenchmarkStyleSet: one setter (criterion 8).
func BenchmarkStyleSet(b *testing.B) {
	s := New()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkStyle = s.Bold(true)
	}
}

// BenchmarkStyleSetterChain: a representative fluent configuration chain.
func BenchmarkStyleSetterChain(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkStyle = New().
			Foreground(TokenPrimary).
			Background(TokenSurface).
			Bold(true).
			Padding(1, 2).
			Border(BorderRounded).
			Align(AlignCenter)
	}
}

// BenchmarkStyleIsSet: the is-set check — one uint64 AND on the props
// bitfield (§2.1), read on the render hot path.
func BenchmarkStyleIsSet(b *testing.B) {
	s := New().Bold(true)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, sinkBool = s.GetBold()
	}
}

// TestSetterZeroAllocs pins criterion 8's alloc contract in a regular test
// so it fails fast without running benchmarks: with extras == nil, a setter
// and an is-set check perform zero heap allocations.
func TestSetterZeroAllocs(t *testing.T) {
	s := New()
	if n := testing.AllocsPerRun(1000, func() { sinkStyle = s.Bold(true) }); n != 0 {
		t.Errorf("one setter allocates %.1f times/op, want 0", n)
	}
	if n := testing.AllocsPerRun(1000, func() { _, sinkBool = s.GetBold() }); n != 0 {
		t.Errorf("is-set check allocates %.1f times/op, want 0", n)
	}
	if n := testing.AllocsPerRun(1000, func() { sinkStyle = s.Foreground(ANSI(3)) }); n != 0 {
		t.Errorf("color setter allocates %.1f times/op, want 0", n)
	}
}

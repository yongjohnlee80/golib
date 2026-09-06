package tui

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// newTestSurface builds a root surface over a fresh, diff-synced buffer.
func newTestSurface(w, h int, policy WidthPolicy) (*bufSurface, *buffer) {
	buf := newBuffer(w, h)
	buf.diff() // sync away the initial invalidate
	theme := style.DefaultTheme()
	ctx := newRenderContext(&theme, fullCapabilities(), policy)
	return newRootSurface(buf, ctx), buf
}

func cellAt(b *buffer, x, y int) Cell { return b.curr[y*b.w+x] }

// TestSurfaceClipping: writes outside the clip are silently dropped;
// in-clip writes land translated.
func TestSurfaceClipping(t *testing.T) {
	root, buf := newTestSurface(10, 5, WidthPolicyDefault)
	sub := root.Sub(Rect{X: 2, Y: 1, W: 4, H: 2})

	tests := []struct {
		name   string
		x, y   int
		lands  bool
		ax, ay int // expected buffer position when lands
	}{
		{"origin translates to the sub rect corner", 0, 0, true, 2, 1},
		{"interior translates", 3, 1, true, 5, 2},
		{"negative x dropped", -1, 0, false, 0, 0},
		{"negative y dropped", 0, -1, false, 0, 0},
		{"x past the sub width dropped", 4, 0, false, 0, 0},
		{"y past the sub height dropped", 0, 2, false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.resize(10, 5) // clean slate
			sub.SetCell(tt.x, tt.y, "x", style.New())
			found := -1
			for i, c := range buf.curr {
				if c.Content == "x" {
					found = i
				}
			}
			if !tt.lands {
				if found != -1 {
					t.Fatalf("out-of-clip write landed at index %d", found)
				}
				return
			}
			if want := tt.ay*10 + tt.ax; found != want {
				t.Fatalf("write landed at index %d, want %d (%d, %d)", found, want, tt.ax, tt.ay)
			}
		})
	}
}

// TestSubOfSubComposes: nested Sub offsets accumulate and clips intersect;
// the style context flows unchanged.
func TestSubOfSubComposes(t *testing.T) {
	root, buf := newTestSurface(10, 6, WidthPolicyDefault)
	a := root.Sub(Rect{X: 2, Y: 1, W: 6, H: 4})
	b := a.Sub(Rect{X: 1, Y: 1, W: 10, H: 2}) // wider than a: clip must intersect

	if got, want := b.Size(), (Size{W: 10, H: 2}); got != want {
		t.Fatalf("Size() = %+v, want nominal %+v", got, want)
	}

	b.SetCell(0, 0, "x", style.New())
	if got := cellAt(buf, 3, 2); got.Content != "x" {
		t.Fatalf("nested Sub write landed wrong: (3,2) = %+v", got)
	}

	// b's clip is a ∩ b: a ends at buffer x=8, so local x=5 (buffer x=8) is out.
	b.SetCell(5, 0, "y", style.New())
	for i, c := range buf.curr {
		if c.Content == "y" {
			t.Fatalf("write outside the composed clip landed at index %d", i)
		}
	}

	if b.Theme() != root.Theme() || b.Caps() != root.Caps() {
		t.Fatal("style context did not flow to the child unchanged")
	}
}

// TestSurfaceWideAtClipEdge: W3 against the clip — a width-2 cluster whose
// continuation would leave the clip is dropped whole.
func TestSurfaceWideAtClipEdge(t *testing.T) {
	root, buf := newTestSurface(10, 3, WidthPolicyDefault)
	sub := root.Sub(Rect{X: 1, Y: 0, W: 4, H: 1})

	sub.SetCell(3, 0, "世", style.New()) // continuation would be local x=4: out of clip
	for i, c := range buf.curr {
		if c.Content == "世" {
			t.Fatalf("half-clipped wide write landed at index %d", i)
		}
	}

	sub.SetCell(2, 0, "世", style.New()) // fits: buffer (3,0)+(4,0)
	if got := cellAt(buf, 3, 0); got.Width != 2 {
		t.Fatalf("in-clip wide write missing: (3,0) = %+v", got)
	}
}

// TestSurfaceSetCellFirstClusterOnly: content with more than one cluster
// writes only the first.
func TestSurfaceSetCellFirstClusterOnly(t *testing.T) {
	root, buf := newTestSurface(4, 1, WidthPolicyDefault)
	root.SetCell(0, 0, "ab", style.New())
	if got := cellAt(buf, 0, 0); got.Content != "a" {
		t.Fatalf("cell (0,0) = %+v, want first cluster %q", got, "a")
	}
	if got := cellAt(buf, 1, 0); got != blankCell {
		t.Fatalf("cell (1,0) = %+v, want untouched blank", got)
	}
	root.SetCell(2, 0, "", style.New()) // empty content: no write
	if got := cellAt(buf, 2, 0); got != blankCell {
		t.Fatalf("empty-content SetCell wrote %+v", got)
	}
}

// TestFillTrailingOddColumn: Fill with a width-2 cluster fills in steps of
// two and paints the trailing odd column with a SPACE cell in the fill's
// style — never left untouched.
func TestFillTrailingOddColumn(t *testing.T) {
	root, buf := newTestSurface(7, 2, WidthPolicyDefault)
	st := style.New().Bold(true)
	root.Fill(Rect{X: 0, Y: 0, W: 5, H: 1}, "世", st)

	wantAttrs := CellAttrs{Mask: AttrBold}
	for _, x := range []int{0, 2} {
		if got := cellAt(buf, x, 0); got.Content != "世" || got.Width != 2 || got.Attrs != wantAttrs {
			t.Fatalf("fill head (%d,0) = %+v", x, got)
		}
	}
	// Trailing odd column: styled space, not untouched, not half a cluster.
	if got := cellAt(buf, 4, 0); got.Content != " " || got.Width != 1 || got.Attrs != wantAttrs {
		t.Fatalf("trailing odd column (4,0) = %+v, want styled space", got)
	}
	// Outside the fill rect: untouched.
	if got := cellAt(buf, 5, 0); got != blankCell {
		t.Fatalf("cell (5,0) = %+v, want untouched", got)
	}
	assertNoOrphans(t, buf)
}

// TestFillClippedAndNarrow: Fill clips to the surface and covers every cell
// with a width-1 cluster.
func TestFillClippedAndNarrow(t *testing.T) {
	root, buf := newTestSurface(6, 4, WidthPolicyDefault)
	sub := root.Sub(Rect{X: 1, Y: 1, W: 3, H: 2})
	sub.Fill(Rect{X: -5, Y: -5, W: 100, H: 100}, ".", style.New())

	for y := 0; y < 4; y++ {
		for x := 0; x < 6; x++ {
			in := x >= 1 && x < 4 && y >= 1 && y < 3
			c := cellAt(buf, x, y)
			if in && c.Content != "." {
				t.Fatalf("cell (%d,%d) = %+v, want filled", x, y, c)
			}
			if !in && c != blankCell {
				t.Fatalf("cell (%d,%d) = %+v, want untouched", x, y, c)
			}
		}
	}
}

// TestWidthPolicyDivergence: with WidthPolicyAmbiguousWide on the Surface,
// Surface.StringWidth measures ambiguous clusters at 2 columns while the
// package-level StringWidth stays pinned to WidthPolicyDefault at 1 —
// proving the policy flows through the Surface context and the package
// default stays fixed.
func TestWidthPolicyDivergence(t *testing.T) {
	const ambiguous = "±" // East Asian Ambiguous (UAX #11)

	wideSurf, buf := newTestSurface(4, 1, WidthPolicyAmbiguousWide)
	defSurf, _ := newTestSurface(4, 1, WidthPolicyDefault)

	if got := wideSurf.StringWidth(ambiguous); got != 2 {
		t.Errorf("Surface(AmbiguousWide).StringWidth(%q) = %d, want 2", ambiguous, got)
	}
	if got := defSurf.StringWidth(ambiguous); got != 1 {
		t.Errorf("Surface(Default).StringWidth(%q) = %d, want 1", ambiguous, got)
	}
	if got := StringWidth(ambiguous); got != 1 {
		t.Errorf("package StringWidth(%q) = %d, want 1 (pinned to WidthPolicyDefault)", ambiguous, got)
	}
	if got := StringWidthPolicy(ambiguous, WidthPolicyAmbiguousWide); got != 2 {
		t.Errorf("StringWidthPolicy(%q, AmbiguousWide) = %d, want 2", ambiguous, got)
	}
	if got := StringWidthPolicy(ambiguous, WidthPolicyDefault); got != 1 {
		t.Errorf("StringWidthPolicy(%q, Default) = %d, want 1", ambiguous, got)
	}

	// SetCell caches the width measured under the Surface's policy
	// The ambiguous cluster becomes a wide pair.
	wideSurf.SetCell(0, 0, ambiguous, style.New())
	if got := cellAt(buf, 0, 0); got.Width != 2 {
		t.Errorf("SetCell under AmbiguousWide cached width %d, want 2", got.Width)
	}
	assertNoOrphans(t, buf)
}

// TestGraphemesSegmentationOnly: Graphemes is segmentation-only and
// policy-independent.
func TestGraphemesSegmentationOnly(t *testing.T) {
	var got []string
	for c := range Graphemes("a世🇦🇺") {
		got = append(got, c)
	}
	want := []string{"a", "世", "🇦🇺"}
	if len(got) != len(want) {
		t.Fatalf("Graphemes yielded %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cluster %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSurfaceZeroWidthClusterOccupiesOneCell documents the chosen reading
// for a standalone zero-width cluster (a lone combining mark): it occupies
// the cell it was written to (width clamps to 1) rather than corrupting the
// grid with a 0-width head.
func TestSurfaceZeroWidthClusterOccupiesOneCell(t *testing.T) {
	root, buf := newTestSurface(2, 1, WidthPolicyDefault)
	root.SetCell(0, 0, "́", style.New()) // lone COMBINING ACUTE ACCENT
	if got := cellAt(buf, 0, 0); got.Width != 1 {
		t.Fatalf("zero-width cluster cached width %d, want 1", got.Width)
	}
	assertNoOrphans(t, buf)
}

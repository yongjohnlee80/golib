package tui

import "testing"

// cellAt builds a plain width-1 test cell.
func narrow(content string) Cell { return Cell{Content: content, Width: 1} }

// wide builds a width-2 head test cell.
func wide(content string) Cell { return Cell{Content: content, Width: 2} }

// styledCell builds a cell with a marker attribute so W1's style rules are
// observable.
func styled(content string, width uint8, mask AttrMask) Cell {
	return Cell{Content: content, Width: width, Attrs: CellAttrs{Mask: mask}}
}

// assertNoOrphans scans the buffer's curr grid for wide-cell invariant
// violations.
func assertNoOrphans(t *testing.T, b *buffer) {
	t.Helper()
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			c := b.curr[y*b.w+x]
			if c.Continuation() && (x == 0 || b.curr[y*b.w+x-1].Width != 2) {
				t.Fatalf("orphaned continuation at (%d, %d)", x, y)
			}
			if c.Width == 2 && (x+1 >= b.w || !b.curr[y*b.w+x+1].Continuation()) {
				t.Fatalf("width-2 head without continuation at (%d, %d)", x, y)
			}
		}
	}
}

// TestSetCellWideInvariants covers the two WIDE-CELL invariants: no orphan
// halves (both halves clear on any overwrite), and no wide write into the last
// column.
func TestSetCellWideInvariants(t *testing.T) {
	const headMask = AttrBold
	tests := []struct {
		name   string
		writes []struct {
			x, y int
			c    Cell
		}
		want map[int]Cell // x → expected cell in row 0; unlisted stay blank
	}{
		{
			name: "wide write creates head plus continuation",
			writes: []struct {
				x, y int
				c    Cell
			}{{1, 0, styled("世", 2, headMask)}},
			want: map[int]Cell{
				1: styled("世", 2, headMask),
				2: {Content: "", Width: 0, Attrs: CellAttrs{Mask: headMask}},
			},
		},
		{
			name: "W1: overwriting the head frees the continuation as a space in the head's style",
			writes: []struct {
				x, y int
				c    Cell
			}{
				{1, 0, styled("世", 2, headMask)},
				{1, 0, narrow("a")},
			},
			want: map[int]Cell{
				1: narrow("a"),
				2: styled(" ", 1, headMask), // freed sibling keeps the old head's style
			},
		},
		{
			name: "W1: overwriting the continuation frees the head as a space in the head's style",
			writes: []struct {
				x, y int
				c    Cell
			}{
				{1, 0, styled("世", 2, headMask)},
				{2, 0, narrow("b")},
			},
			want: map[int]Cell{
				1: styled(" ", 1, headMask),
				2: narrow("b"),
			},
		},
		{
			name: "W3: wide write into the last column is dropped whole",
			writes: []struct {
				x, y int
				c    Cell
			}{{3, 0, wide("世")}},
			want: map[int]Cell{}, // untouched
		},
		{
			name: "W1: wide write overlapping the continuation of another pair",
			writes: []struct {
				x, y int
				c    Cell
			}{
				{0, 0, styled("世", 2, headMask)}, // occupies 0,1
				{1, 0, wide("界")},                // occupies 1,2; frees head at 0
			},
			want: map[int]Cell{
				0: styled(" ", 1, headMask),
				1: wide("界"),
				2: {Content: "", Width: 0},
			},
		},
		{
			name: "W1: wide write overlapping the head of another pair",
			writes: []struct {
				x, y int
				c    Cell
			}{
				{1, 0, styled("世", 2, headMask)}, // occupies 1,2
				{0, 0, wide("界")},                // occupies 0,1; frees continuation at 2
			},
			want: map[int]Cell{
				0: wide("界"),
				1: {Content: "", Width: 0},
				2: styled(" ", 1, headMask),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBuffer(4, 1)
			for _, w := range tt.writes {
				b.setCell(w.x, w.y, w.c)
				assertNoOrphans(t, b) // W1/W3 hold after EVERY operation
			}
			for x := 0; x < 4; x++ {
				want, ok := tt.want[x]
				if !ok {
					want = blankCell
				}
				if got := b.curr[x]; got != want {
					t.Errorf("cell (%d, 0) = %+v, want %+v", x, got, want)
				}
			}
		})
	}
}

// TestSetCellOutOfRange: out-of-range writes are dropped silently
// (clipping is a rendering fact, not an error).
func TestSetCellOutOfRange(t *testing.T) {
	b := newBuffer(2, 2)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {2, 0}, {0, 2}} {
		b.setCell(p[0], p[1], narrow("x"))
	}
	for i, c := range b.curr {
		if c != blankCell {
			t.Errorf("cell %d mutated by out-of-range write: %+v", i, c)
		}
	}
}

// TestDiffMinimal checks the golden-diff contract: scripted buffer mutations
// produce the exact expected []CellUpdate sets. SGR/CUP byte economy is
// term-emitter scope and not tested here.
func TestDiffMinimal(t *testing.T) {
	b := newBuffer(4, 2)
	b.diff() // sync away the initial full invalidate

	t.Run("no change yields an empty diff", func(t *testing.T) {
		if d := b.diff(); len(d) != 0 {
			t.Fatalf("diff = %+v, want empty", d)
		}
	})

	t.Run("single cell change yields exactly one update", func(t *testing.T) {
		b.setCell(2, 1, narrow("a"))
		d := b.diff()
		want := []CellUpdate{{X: 2, Y: 1, Cell: narrow("a")}}
		if len(d) != 1 || d[0] != want[0] {
			t.Fatalf("diff = %+v, want %+v", d, want)
		}
	})

	t.Run("wide write yields one head update covering both columns", func(t *testing.T) {
		b.setCell(0, 0, wide("世"))
		d := b.diff()
		want := CellUpdate{X: 0, Y: 0, Cell: wide("世")}
		if len(d) != 1 || d[0] != want {
			t.Fatalf("diff = %+v, want [%+v]", d, want)
		}
	})

	t.Run("diff updates last: repeated diff is empty", func(t *testing.T) {
		if d := b.diff(); len(d) != 0 {
			t.Fatalf("second diff = %+v, want empty", d)
		}
	})

	t.Run("overwriting a wide pair with narrow cells yields both updates", func(t *testing.T) {
		b.setCell(0, 0, narrow("a")) // W1 frees (1,0) as a space
		b.setCell(1, 0, narrow("b"))
		d := b.diff()
		want := []CellUpdate{
			{X: 0, Y: 0, Cell: narrow("a")},
			{X: 1, Y: 0, Cell: narrow("b")},
		}
		if len(d) != 2 || d[0] != want[0] || d[1] != want[1] {
			t.Fatalf("diff = %+v, want %+v", d, want)
		}
	})

	t.Run("diff is ordered row-major", func(t *testing.T) {
		b.setCell(3, 1, narrow("z"))
		b.setCell(0, 0, narrow("q"))
		d := b.diff()
		if len(d) != 2 || d[0].Y != 0 || d[1].Y != 1 {
			t.Fatalf("diff not row-major: %+v", d)
		}
	})
}

// TestDiffResizeFullInvalidate: resize reallocates and invalidates last
// entirely — the post-resize diff equals the full grid, never a diff across
// sizes.
func TestDiffResizeFullInvalidate(t *testing.T) {
	b := newBuffer(3, 2)
	b.setCell(1, 1, narrow("x"))
	b.diff() // sync

	b.resize(4, 3)
	d := b.diff()
	if len(d) != 4*3 {
		t.Fatalf("post-resize diff has %d updates, want full grid %d", len(d), 4*3)
	}
	for _, u := range d {
		if u.Cell != blankCell {
			t.Fatalf("post-resize cell (%d, %d) = %+v, want blank", u.X, u.Y, u.Cell)
		}
	}
	if d2 := b.diff(); len(d2) != 0 {
		t.Fatalf("diff after full-invalidate sync = %+v, want empty", d2)
	}
}

// TestDiffFlushRoundTrip drives the produced diff into a TestBackend and
// asserts the applied grid — the Flush contract consuming the diff.
func TestDiffFlushRoundTrip(t *testing.T) {
	b := newBuffer(4, 1)
	be := NewTestBackend(4, 1)

	b.setCell(0, 0, narrow("a"))
	b.setCell(1, 0, wide("世"))
	if err := be.Flush(b.diff()); err != nil {
		t.Fatal(err)
	}
	if got, want := be.String(), "a世 "; got != want {
		t.Fatalf("grid = %q, want %q", got, want)
	}
	if be.Flushes() != 1 {
		t.Fatalf("flushes = %d, want 1", be.Flushes())
	}

	// Steady state: nothing changed, nothing to flush.
	if d := b.diff(); len(d) != 0 {
		t.Fatalf("steady-state diff = %+v, want empty", d)
	}
}

// TestDiffSteadyStateAllocs: the diff path reuses its scratch slice — the
// steady-state frame allocates nothing.
func TestDiffSteadyStateAllocs(t *testing.T) {
	b := newBuffer(20, 10)
	b.diff() // sync; also grows scratch to full-grid capacity
	toggle := false
	allocs := testing.AllocsPerRun(100, func() {
		toggle = !toggle
		content := "x"
		if toggle {
			content = "y"
		}
		b.setCell(3, 3, narrow(content)) // always differs from last
		if d := b.diff(); len(d) != 1 {
			t.Fatalf("diff = %d updates, want 1", len(d))
		}
	})
	if allocs != 0 {
		t.Errorf("steady-state diff allocates %.1f/op, want 0", allocs)
	}
}

// BenchmarkDiffFullRepaint: full-repaint diff of a 200×60 grid.
func BenchmarkDiffFullRepaint(b *testing.B) {
	buf := newBuffer(200, 60)
	buf.diff() // sync + grow scratch
	b.ReportAllocs()
	toggle := false
	for b.Loop() {
		toggle = !toggle
		content := "x"
		if toggle {
			content = "y"
		}
		c := narrow(content)
		for y := 0; y < 60; y++ {
			for x := 0; x < 200; x++ {
				buf.setCell(x, y, c)
			}
		}
		if d := buf.diff(); len(d) != 200*60 {
			b.Fatalf("diff = %d updates, want full grid", len(d))
		}
	}
}

// BenchmarkDiffSteadyState: a small dirty region in a large clean grid
// — the steady-state frame path allocates zero bytes
// amortized (reused scratch).
func BenchmarkDiffSteadyState(b *testing.B) {
	buf := newBuffer(200, 60)
	buf.diff()
	b.ReportAllocs()
	toggle := false
	for b.Loop() {
		toggle = !toggle
		content := "x"
		if toggle {
			content = "y"
		}
		c := narrow(content)
		for x := 0; x < 10; x++ {
			buf.setCell(x, 30, c)
		}
		if d := buf.diff(); len(d) != 10 {
			b.Fatalf("diff = %d updates, want 10", len(d))
		}
	}
}

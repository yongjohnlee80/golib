package tui

// Layout tests: Flex largest-remainder determinism, Dock, Stack golden
// frames, constraint-violation clamping, relayout economy, the cursor rule
// (ADR-0004 §5.5–§5.8).

import (
	"math/rand"
	"testing"
)

// childRects reads the placed parent-relative rects of comps on the loop.
func childRects(h *harness, comps ...Component) []Rect {
	out := make([]Rect, len(comps))
	h.onLoop(func() {
		for i, c := range comps {
			if n := h.app.byComp[c]; n != nil {
				out[i] = n.rect
			}
		}
	})
	return out
}

// TestFlexLargestRemainder: ADR-0004 §5.5 — table-driven Flex distribution:
// R=10 over (1,1,1) → 4,3,3; R=7 over (2,3) → 3,4; ties broken by index;
// Σ assigned == R.
func TestFlexLargestRemainder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		total   int
		weights []int
		want    []int
	}{
		{"R=10 over (1,1,1)", 10, []int{1, 1, 1}, []int{4, 3, 3}},
		{"R=7 over (2,3)", 7, []int{2, 3}, []int{3, 4}},
		{"R=5 over (1,1,1,1)", 5, []int{1, 1, 1, 1}, []int{2, 1, 1, 1}},
		{"R=9 over (1,2)", 9, []int{1, 2}, []int{3, 6}},
		{"R=8 over (3,3,2)", 8, []int{3, 3, 2}, []int{3, 3, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flex := NewFlex(Horizontal)
			comps := make([]Component, len(tc.weights))
			for i, w := range tc.weights {
				p := &probe{name: "w"}
				comps[i] = p
				flex.AddWeighted(p, w)
			}
			h := startApp(t, flex, tc.total, 2)
			rects := childRects(h, comps...)
			sum := 0
			x := 0
			for i, r := range rects {
				if r.W != tc.want[i] {
					t.Errorf("child %d width = %d, want %d (rects %+v)", i, r.W, tc.want[i], rects)
				}
				if r.X != x {
					t.Errorf("child %d x = %d, want %d (gap-free packing)", i, r.X, x)
				}
				x += r.W
				sum += r.W
			}
			if sum != tc.total {
				t.Errorf("Σ assigned = %d, want R = %d", sum, tc.total)
			}
			FailOnViolations(t, h.tb)
		})
	}
}

// TestFlexDistributionSumFuzz: ADR-0004 §5.5 — Σ assigned == R proven for a
// fuzz range of (R, weights).
func TestFlexDistributionSumFuzz(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1)) // deterministic
	for trial := 0; trial < 25; trial++ {
		total := 1 + rng.Intn(120)
		n := 1 + rng.Intn(6)
		weights := make([]int, n)
		for i := range weights {
			weights[i] = 1 + rng.Intn(9)
		}
		flex := NewFlex(Horizontal)
		comps := make([]Component, n)
		for i, w := range weights {
			p := &probe{name: "w"}
			comps[i] = p
			flex.AddWeighted(p, w)
		}
		h := startApp(t, flex, total, 1)
		rects := childRects(h, comps...)
		sum := 0
		for _, r := range rects {
			sum += r.W
		}
		if sum != total {
			t.Fatalf("trial %d (R=%d, weights=%v): Σ assigned = %d, want %d",
				trial, total, weights, sum, total)
		}
		h.wait()
	}
}

// TestFlexGoldenFramesDeterministic: byte-identical TestBackend frames
// across repeated runs (ADR-0004 §5.5).
func TestFlexGoldenFramesDeterministic(t *testing.T) {
	t.Parallel()
	render := func() string {
		a := &probe{name: "a", fill: "a"}
		b := &probe{name: "b", fill: "b"}
		c := &probe{name: "c", fill: "c"}
		flex := NewFlex(Horizontal)
		flex.AddWeighted(a, 1)
		flex.AddWeighted(b, 1)
		flex.AddWeighted(c, 1)
		h := startApp(t, flex, 10, 2)
		s := h.tb.String()
		h.wait()
		return s
	}
	want := "aaaabbbccc\naaaabbbccc" // 10 over (1,1,1) → 4,3,3
	for run := 0; run < 5; run++ {
		if got := render(); got != want {
			t.Fatalf("run %d frame:\n%q\nwant\n%q", run, got, want)
		}
	}
}

// TestFlexFixedThenWeighted: fixed children are measured first (loose main,
// tight cross); weighted children split what remains.
func TestFlexFixedThenWeighted(t *testing.T) {
	t.Parallel()
	fixed := &probe{name: "fixed", pref: Size{W: 3, H: 9}, fill: "f"}
	grow := &probe{name: "grow", fill: "g"}
	flex := NewFlex(Horizontal)
	flex.Add(fixed)
	flex.AddWeighted(grow, 1)
	h := startApp(t, flex, 10, 2)
	if got, want := h.tb.String(), "fffggggggg\nfffggggggg"; got != want {
		t.Fatalf("frame:\n%q\nwant\n%q", got, want)
	}
	FailOnViolations(t, h.tb)
}

// TestDockLayout: ADR-0004 §2.7.3 — pinned edges consume in declaration
// order; center fills the rest.
func TestDockLayout(t *testing.T) {
	t.Parallel()
	top := &probe{name: "top", pref: Size{W: 99, H: 1}, fill: "t"}
	left := &probe{name: "left", pref: Size{W: 3, H: 99}, fill: "l"}
	center := &probe{name: "center", fill: "c"}
	dock := NewDock()
	dock.Pin(DockTop, top)
	dock.Pin(DockLeft, left)
	dock.Add(center)
	h := startApp(t, dock, 10, 4)
	want := "tttttttttt\nlllccccccc\nlllccccccc\nlllccccccc"
	if got := h.tb.String(); got != want {
		t.Fatalf("frame:\n%q\nwant\n%q", got, want)
	}
}

// TestStackZOrderPaint: ADR-0004 §2.7.4 — later children paint on top.
func TestStackZOrderPaint(t *testing.T) {
	t.Parallel()
	base := &probe{name: "base", pref: Size{W: 10, H: 3}, fill: "a"}
	float := &probe{name: "float", pref: Size{W: 3, H: 1}, fill: "b"}
	stack := NewStack()
	stack.Add(base)
	stack.AddAt(float, 2, 1)
	h := startApp(t, stack, 10, 3)
	want := "aaaaaaaaaa\naabbbaaaaa\naaaaaaaaaa"
	if got := h.tb.String(); got != want {
		t.Fatalf("frame:\n%q\nwant\n%q", got, want)
	}
}

// TestStackAlignment: AlignCenter positions a sized layer centrally.
func TestStackAlignment(t *testing.T) {
	t.Parallel()
	base := &probe{name: "base", pref: Size{W: 10, H: 3}, fill: "."}
	pop := &probe{name: "pop", pref: Size{W: 4, H: 1}, fill: "p"}
	stack := NewStack()
	stack.Add(base)
	stack.AddAligned(pop, AlignCenter)
	h := startApp(t, stack, 10, 3)
	want := "..........\n...pppp...\n.........."
	if got := h.tb.String(); got != want {
		t.Fatalf("frame:\n%q\nwant\n%q", got, want)
	}
}

// TestConstraintViolationClamped: ADR-0004 §5.6 — a child returning a Size
// outside its Constraints is clamped, siblings are unaffected, and the
// violation is recorded on the TestBackend with node/type/sizes.
func TestConstraintViolationClamped(t *testing.T) {
	t.Parallel()
	liar := &probe{name: "liar", fill: "x"}
	liar.onLayout = func(*probe, Constraints) Size { return Size{W: 999, H: 999} }
	sibling := &probe{name: "sib", fill: "s"}
	flex := NewFlex(Horizontal)
	flex.AddWeighted(liar, 1)
	flex.AddWeighted(sibling, 1)
	h := startApp(t, flex, 10, 1)

	if got, want := h.tb.String(), "xxxxxsssss"; got != want {
		t.Fatalf("frame:\n%q\nwant\n%q (sibling geometry must be intact)", got, want)
	}
	violations := h.tb.ConstraintViolations()
	if len(violations) == 0 {
		t.Fatal("no ConstraintViolation recorded for the out-of-constraints return")
	}
	v := violations[0]
	var liarID NodeID
	h.onLoop(func() { liarID = h.app.byComp[liar].id })
	if v.Node != liarID || v.Got != (Size{W: 999, H: 999}) {
		t.Fatalf("violation = %+v, want node %d with Got 999x999", v, liarID)
	}
	if v.Type == "" {
		t.Fatal("violation missing the component type")
	}
}

// TestRelayoutEconomy: ADR-0004 §5.7 — MarkDirty alone repaints with ZERO
// Layout calls; RequestLayout triggers exactly one full pass; ResizeEvent
// produces one pass plus a full repaint.
func TestRelayoutEconomy(t *testing.T) {
	t.Parallel()
	pane := &probe{name: "pane", fill: "p"}
	flex := NewFlex(Horizontal)
	flex.AddWeighted(pane, 1)
	h := startApp(t, flex, 6, 2)
	h.sync()

	layouts0 := pane.layouts.Load()
	renders0 := pane.renders.Load()
	flushes0 := h.tb.Flushes()

	// MarkDirty: render dirt only — a frame with no Layout calls.
	h.onLoop(func() { pane.ctx.MarkDirty() })
	waitFor(t, "repaint", func() bool { return h.tb.Flushes() == flushes0+1 })
	if got := pane.layouts.Load(); got != layouts0 {
		t.Errorf("MarkDirty ran %d Layout calls, want 0", got-layouts0)
	}
	if got := pane.renders.Load(); got != renders0+1 {
		t.Errorf("MarkDirty renders = %d, want exactly 1 more", got-renders0)
	}

	// RequestLayout: exactly one full pass.
	h.onLoop(func() { pane.ctx.RequestLayout() })
	waitFor(t, "relayout frame", func() bool { return h.tb.Flushes() == flushes0+2 })
	if got := pane.layouts.Load(); got != layouts0+1 {
		t.Errorf("RequestLayout ran %d Layout calls, want exactly 1", got-layouts0)
	}

	// Resize: one pass + repaint.
	h.tb.InjectResize(8, 2)
	waitFor(t, "resize frame", func() bool { return h.tb.Flushes() == flushes0+3 })
	if got := pane.layouts.Load(); got != layouts0+2 {
		t.Errorf("resize ran %d further Layout calls, want exactly 1", got-layouts0-1)
	}
}

// TestCursorRule: ADR-0004 §5.8 — focusing a CursorReporter that reports
// (x,y,true) parks the backend cursor at the absolute translation and shows
// it; focusing a non-reporter hides it.
func TestCursorRule(t *testing.T) {
	t.Parallel()
	pad := &probe{name: "pad", pref: Size{W: 3, H: 9}, fill: "."}
	input := &cursorProbe{}
	input.name = "input"
	input.accepts.Store(true)
	input.cx, input.cy = 2, 1
	input.report.Store(true)
	other := newFocusProbe("other", Size{W: 2, H: 1})
	flex := NewFlex(Horizontal)
	flex.Add(pad)
	flex.AddWeighted(input, 1)
	flex.Add(other)
	h := startApp(t, flex, 10, 3)

	h.onLoop(func() { input.ctx.RequestFocus() })
	waitFor(t, "cursor parked", func() bool {
		x, y, visible := h.tb.CursorPos()
		return visible && x == 3+2 && y == 1
	})

	h.onLoop(func() { other.ctx.RequestFocus() })
	waitFor(t, "cursor hidden on non-reporter", func() bool {
		_, _, visible := h.tb.CursorPos()
		return !visible
	})
}

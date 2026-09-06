package style

import "testing"

// TestColorConstructors: each constructor produces a Color whose read
// accessors report exactly its own kind.
func TestColorConstructors(t *testing.T) {
	// Default / zero value.
	if !Default().IsDefault() {
		t.Error("Default().IsDefault() = false")
	}
	if Default() != (Color{}) {
		t.Error("Default() is not the zero Color")
	}

	// ANSI.
	c := ANSI(9)
	if n, ok := c.ANSIIndex(); !ok || n != 9 {
		t.Errorf("ANSI(9).ANSIIndex() = (%d, %v), want (9, true)", n, ok)
	}
	if c.IsDefault() {
		t.Error("ANSI(9).IsDefault() = true")
	}
	if _, ok := c.ANSI256Index(); ok {
		t.Error("ANSI(9) reports as ANSI-256")
	}

	// ANSI256.
	c = ANSI256(214)
	if n, ok := c.ANSI256Index(); !ok || n != 214 {
		t.Errorf("ANSI256(214).ANSI256Index() = (%d, %v), want (214, true)", n, ok)
	}
	if _, ok := c.ANSIIndex(); ok {
		t.Error("ANSI256(214) reports as ANSI-16")
	}

	// RGB.
	c = RGB(12, 34, 56)
	if r, g, b, ok := c.RGBValues(); !ok || r != 12 || g != 34 || b != 56 {
		t.Errorf("RGB(12,34,56).RGBValues() = (%d, %d, %d, %v)", r, g, b, ok)
	}

	// Adaptive.
	c = Adaptive(ANSI(15), RGB(1, 2, 3))
	light, dark, ok := c.AdaptivePair()
	if !ok {
		t.Fatal("Adaptive(...).AdaptivePair() ok = false")
	}
	if light != ANSI(15) {
		t.Errorf("adaptive light leaf = %v, want ANSI(15)", light)
	}
	if dark != RGB(1, 2, 3) {
		t.Errorf("adaptive dark leaf = %v, want RGB(1,2,3)", dark)
	}
	if _, ok := c.Token(); ok {
		t.Error("adaptive color reports as token")
	}

	// Comparability: same construction, same value.
	if ANSI(3) != ANSI(3) || RGB(1, 2, 3) != RGB(1, 2, 3) {
		t.Error("identically-constructed Colors are not ==")
	}
	if ANSI(3) == ANSI256(3) {
		t.Error("ANSI(3) == ANSI256(3): kinds must differ")
	}
}

// TestColorConstructorRangePanics: out-of-range palette indices fail loud at
// construction, per golib convention.
func TestColorConstructorRangePanics(t *testing.T) {
	mustPanic(t, "ANSI(-1)", func() { ANSI(-1) })
	mustPanic(t, "ANSI(16)", func() { ANSI(16) })
	mustPanic(t, "ANSI256(-1)", func() { ANSI256(-1) })
	mustPanic(t, "ANSI256(256)", func() { ANSI256(256) })
}

// TestAdaptiveRejectsTokenAndAdaptiveLeaves covers the construction-side half
// of the no-nested-adaptive rule: Adaptive handed a token- or adaptive-kind
// Color panics at construction. The type system already blocks Token
// arguments (Token is not a Color); a token-kind Color is only reachable by
// reading one back out of a Style — exercised here.
// (The other half — the downsampling chain and the DarkBackground pick —
// lives with the resolver in package tui.)
func TestAdaptiveRejectsTokenAndAdaptiveLeaves(t *testing.T) {
	tokenColor, _ := New().Foreground(TokenPrimary).GetForeground()
	if _, ok := tokenColor.Token(); !ok {
		t.Fatal("test setup: expected a token-kind Color")
	}
	mustPanic(t, "Adaptive(token, _)", func() { Adaptive(tokenColor, ANSI(0)) })
	mustPanic(t, "Adaptive(_, token)", func() { Adaptive(ANSI(0), tokenColor) })
	mustPanic(t, "Adaptive(adaptive, _)", func() { Adaptive(Adaptive(ANSI(0), ANSI(15)), ANSI(1)) })
	mustPanic(t, "Adaptive(_, adaptive)", func() { Adaptive(ANSI(1), Adaptive(ANSI(0), ANSI(15))) })
}

// TestColorAccessorsExclusive: exactly one read accessor reports ok for any
// Color — the resolver's dispatch relies on it.
func TestColorAccessorsExclusive(t *testing.T) {
	tokenColor, _ := New().Foreground(TokenBorder).GetForeground()
	colors := []struct {
		name string
		c    Color
	}{
		{"default", Default()},
		{"ansi", ANSI(4)},
		{"ansi256", ANSI256(100)},
		{"rgb", RGB(9, 9, 9)},
		{"adaptive", Adaptive(ANSI(7), ANSI(0))},
		{"token", tokenColor},
	}
	for _, tc := range colors {
		n := 0
		if tc.c.IsDefault() {
			n++
		}
		if _, ok := tc.c.ANSIIndex(); ok {
			n++
		}
		if _, ok := tc.c.ANSI256Index(); ok {
			n++
		}
		if _, _, _, ok := tc.c.RGBValues(); ok {
			n++
		}
		if _, _, ok := tc.c.AdaptivePair(); ok {
			n++
		}
		if _, ok := tc.c.Token(); ok {
			n++
		}
		if n != 1 {
			t.Errorf("%s: %d accessors report ok, want exactly 1", tc.name, n)
		}
	}
}

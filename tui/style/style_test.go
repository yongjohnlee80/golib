package style

import "testing"

// mustPanic asserts fn panics.
func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic, got none", name)
		}
	}()
	fn()
}

// setters is a table of every fluent setter, used by the comparability and
// value-semantics sweeps.
var setters = []struct {
	name string
	fn   func(Style) Style
}{
	{"Foreground", func(s Style) Style { return s.Foreground(ANSI(1)) }},
	{"Background", func(s Style) Style { return s.Background(ANSI(2)) }},
	{"Bold", func(s Style) Style { return s.Bold(true) }},
	{"Italic", func(s Style) Style { return s.Italic(true) }},
	{"Underline", func(s Style) Style { return s.Underline(true) }},
	{"Strikethrough", func(s Style) Style { return s.Strikethrough(true) }},
	{"Reverse", func(s Style) Style { return s.Reverse(true) }},
	{"Blink", func(s Style) Style { return s.Blink(true) }},
	{"Faint", func(s Style) Style { return s.Faint(true) }},
	{"Padding", func(s Style) Style { return s.Padding(1) }},
	{"Margin", func(s Style) Style { return s.Margin(1) }},
	{"Width", func(s Style) Style { return s.Width(10) }},
	{"Height", func(s Style) Style { return s.Height(5) }},
	{"MaxWidth", func(s Style) Style { return s.MaxWidth(20) }},
	{"MaxHeight", func(s Style) Style { return s.MaxHeight(8) }},
	{"Align", func(s Style) Style { return s.Align(AlignCenter, AlignMiddle) }},
	{"Border", func(s Style) Style { return s.Border(BorderRounded) }},
	{"BorderForeground", func(s Style) Style { return s.BorderForeground(ANSI(3)) }},
	{"BorderTopForeground", func(s Style) Style { return s.BorderTopForeground(ANSI(4)) }},
	{"BorderRightForeground", func(s Style) Style { return s.BorderRightForeground(ANSI(4)) }},
	{"BorderBottomForeground", func(s Style) Style { return s.BorderBottomForeground(ANSI(4)) }},
	{"BorderLeftForeground", func(s Style) Style { return s.BorderLeftForeground(ANSI(4)) }},
}

// TestStyleComparable covers ADR-0006 §5 acceptance criterion 2: Style is
// comparable — usable as a map key, == for identically-built chains, and
// unequal after any single setter.
func TestStyleComparable(t *testing.T) {
	build := func() Style {
		return New().Foreground(TokenPrimary).Background(ANSI(0)).
			Bold(true).Padding(1, 2).Border(BorderRounded).Align(AlignCenter)
	}
	a, b := build(), build()
	if a != b {
		t.Fatal("identically-built chains are not ==")
	}

	// Map-key usage.
	m := map[Style]int{a: 1}
	if got := m[b]; got != 1 {
		t.Fatalf("map lookup via identically-built key = %d, want 1", got)
	}

	// == after copy (assignment is the copy operation).
	c := a
	if c != a {
		t.Fatal("copy by assignment is not == to the original")
	}

	// Inequality after any single setter (applied to an empty base — every
	// setter flips at least its props bit, so the result must differ).
	base := New()
	for _, tc := range setters {
		if tc.fn(base) == base {
			t.Errorf("%s: style unchanged (==) after setter", tc.name)
		}
	}

	// And on a populated chain: changing one property's value breaks ==.
	if build().Foreground(ANSI(7)) == build() {
		t.Error("changing one property left the styles ==")
	}
}

// TestValueSemantics covers acceptance criterion 3: mutate-after-copy
// independence — a derived style leaves the original untouched (props bit
// and value both).
func TestValueSemantics(t *testing.T) {
	a := New()
	b := a.Bold(true)
	if v, ok := a.GetBold(); v || ok {
		t.Fatalf("a.GetBold() = (%v, %v) after b := a.Bold(true); want (false, false)", v, ok)
	}
	if v, ok := b.GetBold(); !v || !ok {
		t.Fatalf("b.GetBold() = (%v, %v), want (true, true)", v, ok)
	}
	if a != New() {
		t.Fatal("a changed after deriving b")
	}

	// A longer fluent chain: every derivation is independent of its base.
	base := New().Padding(1).Foreground(ANSI(1))
	derived := base.Padding(3, 4).Bold(true).Foreground(ANSI(2))
	if top, right, _, _ := base.GetPadding(); top != 1 || right != 1 {
		t.Fatalf("base padding changed: top=%d right=%d, want 1 1", top, right)
	}
	if c, _ := base.GetForeground(); c != ANSI(1) {
		t.Fatal("base foreground changed by derived chain")
	}
	if _, ok := base.GetBold(); ok {
		t.Fatal("base gained bold from derived chain")
	}
	if top, right, _, _ := derived.GetPadding(); top != 3 || right != 4 {
		t.Fatalf("derived padding = %d %d, want 3 4", top, right)
	}
}

// TestSetnessDistinctFromZero covers acceptance criterion 5:
// New().Bold(false) reports (false, true); New() reports (false, false).
func TestSetnessDistinctFromZero(t *testing.T) {
	if v, ok := New().GetBold(); v || ok {
		t.Fatalf("New().GetBold() = (%v, %v), want (false, false)", v, ok)
	}
	if v, ok := New().Bold(false).GetBold(); v || !ok {
		t.Fatalf("New().Bold(false).GetBold() = (%v, %v), want (false, true)", v, ok)
	}
	// The explicit set is also visible to == (props bit differs).
	if New().Bold(false) == New() {
		t.Fatal("Bold(false) compares == to an untouched style")
	}
}

// TestUnsetRestoresZeroValue: Unset* clears the props bit AND zeroes the
// field, so unset-then-compare equals never-set (supports criterion 2's ==
// semantics).
func TestUnsetRestoresZeroValue(t *testing.T) {
	tests := []struct {
		name string
		st   Style
	}{
		{"Foreground", New().Foreground(ANSI(1)).UnsetForeground()},
		{"Background", New().Background(ANSI(1)).UnsetBackground()},
		{"Bold", New().Bold(true).UnsetBold()},
		{"Italic", New().Italic(true).UnsetItalic()},
		{"Underline", New().Underline(true).UnsetUnderline()},
		{"Strikethrough", New().Strikethrough(true).UnsetStrikethrough()},
		{"Reverse", New().Reverse(true).UnsetReverse()},
		{"Blink", New().Blink(true).UnsetBlink()},
		{"Faint", New().Faint(true).UnsetFaint()},
		{"Padding", New().Padding(1, 2, 3, 4).UnsetPadding()},
		{"Margin", New().Margin(2).UnsetMargin()},
		{"Width", New().Width(10).UnsetWidth()},
		{"Height", New().Height(5).UnsetHeight()},
		{"MaxWidth", New().MaxWidth(9).UnsetMaxWidth()},
		{"MaxHeight", New().MaxHeight(9).UnsetMaxHeight()},
		{"Align", New().Align(AlignRight, AlignBottom).UnsetAlign()},
		{"Border", New().Border(BorderThick).UnsetBorder()},
		{"BorderForeground", New().BorderForeground(ANSI(5)).UnsetBorderForeground()},
		{"BorderTopForeground", New().BorderTopForeground(ANSI(5)).UnsetBorderTopForeground()},
		{"BorderRightForeground", New().BorderRightForeground(ANSI(5)).UnsetBorderRightForeground()},
		{"BorderBottomForeground", New().BorderBottomForeground(ANSI(5)).UnsetBorderBottomForeground()},
		{"BorderLeftForeground", New().BorderLeftForeground(ANSI(5)).UnsetBorderLeftForeground()},
	}
	for _, tc := range tests {
		if tc.st != New() {
			t.Errorf("%s: set-then-unset != New()", tc.name)
		}
	}
}

// TestCSSShorthand: variadic Padding/Margin expansion — 1 arg = all,
// 2 = v/h, 3 = t/h/b, 4 = t/r/b/l; 0 or >4 panic (§2.3).
func TestCSSShorthand(t *testing.T) {
	tests := []struct {
		name                     string
		sides                    []int
		top, right, bottom, left int
	}{
		{"one", []int{2}, 2, 2, 2, 2},
		{"two", []int{1, 3}, 1, 3, 1, 3},
		{"three", []int{1, 2, 3}, 1, 2, 3, 2},
		{"four", []int{1, 2, 3, 4}, 1, 2, 3, 4},
	}
	for _, tc := range tests {
		st := New().Padding(tc.sides...)
		top, right, bottom, left := st.GetPadding()
		if top != tc.top || right != tc.right || bottom != tc.bottom || left != tc.left {
			t.Errorf("Padding(%v) = %d %d %d %d, want %d %d %d %d",
				tc.sides, top, right, bottom, left, tc.top, tc.right, tc.bottom, tc.left)
		}
		st = New().Margin(tc.sides...)
		top, right, bottom, left = st.GetMargin()
		if top != tc.top || right != tc.right || bottom != tc.bottom || left != tc.left {
			t.Errorf("Margin(%v) = %d %d %d %d, want %d %d %d %d",
				tc.sides, top, right, bottom, left, tc.top, tc.right, tc.bottom, tc.left)
		}
	}
	mustPanic(t, "Padding()", func() { New().Padding() })
	mustPanic(t, "Padding(5 args)", func() { New().Padding(1, 2, 3, 4, 5) })
	mustPanic(t, "Margin()", func() { New().Margin() })
	mustPanic(t, "Margin(5 args)", func() { New().Margin(1, 2, 3, 4, 5) })
}

// TestBorderEdgesShorthand: Border's variadic edge switches follow the CSS
// rule; no args = all edges.
func TestBorderEdgesShorthand(t *testing.T) {
	tests := []struct {
		name                     string
		edges                    []bool
		top, right, bottom, left bool
	}{
		{"none=all", nil, true, true, true, true},
		{"one", []bool{false}, false, false, false, false},
		{"two", []bool{true, false}, true, false, true, false},
		{"three", []bool{true, false, true}, true, false, true, false},
		{"four", []bool{true, false, false, true}, true, false, false, true},
	}
	for _, tc := range tests {
		st := New().Border(BorderNormal, tc.edges...)
		top, right, bottom, left := st.GetBorderEdges()
		if top != tc.top || right != tc.right || bottom != tc.bottom || left != tc.left {
			t.Errorf("%s: Border edges = %v %v %v %v, want %v %v %v %v",
				tc.name, top, right, bottom, left, tc.top, tc.right, tc.bottom, tc.left)
		}
		if b, ok := st.GetBorder(); !ok || b != BorderNormal {
			t.Errorf("%s: GetBorder() = (%v, %v)", tc.name, b, ok)
		}
	}
	mustPanic(t, "Border(5 edges)", func() {
		New().Border(BorderNormal, true, true, true, true, true)
	})
}

// TestAlign: horizontal always set, vertical only when supplied; invalid
// values panic.
func TestAlign(t *testing.T) {
	st := New().Align(AlignRight)
	if h, ok := st.GetAlignHorizontal(); !ok || h != AlignRight {
		t.Fatalf("GetAlignHorizontal() = (%v, %v), want (AlignRight, true)", h, ok)
	}
	if _, ok := st.GetAlignVertical(); ok {
		t.Fatal("vertical alignment set without a vertical argument")
	}
	st = New().Align(AlignCenter, AlignBottom)
	if v, ok := st.GetAlignVertical(); !ok || v != AlignBottom {
		t.Fatalf("GetAlignVertical() = (%v, %v), want (AlignBottom, true)", v, ok)
	}
	mustPanic(t, "Align(vertical as h)", func() { New().Align(AlignMiddle) })
	mustPanic(t, "Align(horizontal as v)", func() { New().Align(AlignLeft, AlignRight) })
	mustPanic(t, "Align(two v args)", func() { New().Align(AlignLeft, AlignTop, AlignBottom) })
}

// TestColorSpecFlattening: setters accept Color or Token; a Token flattens
// into the internal Color representation at set time (§2.4) — Style never
// stores an interface.
func TestColorSpecFlattening(t *testing.T) {
	st := New().Foreground(TokenError)
	c, ok := st.GetForeground()
	if !ok {
		t.Fatal("foreground not set")
	}
	tok, isTok := c.Token()
	if !isTok || tok != TokenError {
		t.Fatalf("flattened foreground Token() = (%v, %v), want (TokenError, true)", tok, isTok)
	}

	st = New().Background(Adaptive(ANSI(15), ANSI(0)))
	c, _ = st.GetBackground()
	light, dark, isAdaptive := c.AdaptivePair()
	if !isAdaptive || light != ANSI(15) || dark != ANSI(0) {
		t.Fatalf("adaptive background pair = (%v, %v, %v)", light, dark, isAdaptive)
	}

	// Token-carrying styles built through identical chains stay comparable.
	if New().Foreground(TokenPrimary) != New().Foreground(TokenPrimary) {
		t.Fatal("token-flattened styles are not ==")
	}
}

// TestBitfieldIsSetSemantics: the props bitfield tracks set-ness per
// property independently of values (§2.1).
func TestBitfieldIsSetSemantics(t *testing.T) {
	st := New().Bold(true).Italic(false).Width(7)
	for _, tc := range []struct {
		name string
		k    propKey
		want bool
	}{
		{"bold", propBold, true},
		{"italic", propItalic, true}, // explicitly set to false — still set
		{"underline", propUnderline, false},
		{"width", propWidth, true},
		{"height", propHeight, false},
	} {
		if got := st.isSet(tc.k); got != tc.want {
			t.Errorf("isSet(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Unset clears exactly the targeted bit.
	st = st.UnsetBold()
	if st.isSet(propBold) {
		t.Error("propBold still set after UnsetBold")
	}
	if !st.isSet(propItalic) || !st.isSet(propWidth) {
		t.Error("UnsetBold disturbed unrelated props bits")
	}
}

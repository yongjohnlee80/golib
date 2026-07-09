package style

import "testing"

// TestInheritOnlyUnsetProps covers ADR-0006 §5 acceptance criterion 4 (part
// 1): Inherit copies from other ONLY the properties not already set on the
// receiver.
func TestInheritOnlyUnsetProps(t *testing.T) {
	parent := New().Foreground(ANSI(1)).Background(ANSI(2)).Bold(true).Italic(true)
	child := New().Foreground(ANSI(9)).Bold(false)

	got := child.Inherit(parent)

	// Already set on the child: kept.
	if c, _ := got.GetForeground(); c != ANSI(9) {
		t.Errorf("foreground = %v, want the child's ANSI(9)", c)
	}
	if v, ok := got.GetBold(); v || !ok {
		t.Errorf("bold = (%v, %v), want the child's explicit (false, true)", v, ok)
	}
	// Unset on the child: inherited.
	if c, ok := got.GetBackground(); !ok || c != ANSI(2) {
		t.Errorf("background = (%v, %v), want inherited (ANSI(2), true)", c, ok)
	}
	if v, ok := got.GetItalic(); !v || !ok {
		t.Errorf("italic = (%v, %v), want inherited (true, true)", v, ok)
	}
	// Value semantics: the receiver itself is untouched.
	if _, ok := child.GetBackground(); ok {
		t.Error("Inherit mutated its receiver")
	}
}

// TestInheritNeverCopiesMarginsPadding covers criterion 4 (part 2): margins
// and padding are NEVER inherited — they are placement, not appearance (the
// lipgloss rule, kept verbatim).
func TestInheritNeverCopiesMarginsPadding(t *testing.T) {
	parent := New().Padding(1, 2, 3, 4).Margin(5, 6, 7, 8).Bold(true)
	child := New().Inherit(parent)

	if top, right, bottom, left := child.GetPadding(); top != 0 || right != 0 || bottom != 0 || left != 0 {
		t.Errorf("padding inherited: %d %d %d %d, want all 0", top, right, bottom, left)
	}
	if top, right, bottom, left := child.GetMargin(); top != 0 || right != 0 || bottom != 0 || left != 0 {
		t.Errorf("margin inherited: %d %d %d %d, want all 0", top, right, bottom, left)
	}
	for _, k := range []propKey{
		propPaddingTop, propPaddingRight, propPaddingBottom, propPaddingLeft,
		propMarginTop, propMarginRight, propMarginBottom, propMarginLeft,
	} {
		if child.isSet(k) {
			t.Errorf("padding/margin props bit %#x set after Inherit", uint64(k))
		}
	}
	// Everything else still flows.
	if v, ok := child.GetBold(); !v || !ok {
		t.Error("bold not inherited alongside the skipped padding/margin")
	}
}

// TestUnsetThenInheritReadmits covers criterion 4 (part 3): Unset* then
// Inherit re-admits a property.
func TestUnsetThenInheritReadmits(t *testing.T) {
	parent := New().Bold(true).Foreground(ANSI(3))

	child := New().Bold(false).Foreground(ANSI(9))
	// While set, Inherit must not override…
	if v, _ := child.Inherit(parent).GetBold(); v {
		t.Fatal("Inherit overrode an explicitly set bold")
	}
	// …after Unset, the property is inheritable again.
	child = child.UnsetBold().UnsetForeground()
	got := child.Inherit(parent)
	if v, ok := got.GetBold(); !v || !ok {
		t.Errorf("bold = (%v, %v) after unset+inherit, want (true, true)", v, ok)
	}
	if c, ok := got.GetForeground(); !ok || c != ANSI(3) {
		t.Errorf("foreground = (%v, %v) after unset+inherit, want (ANSI(3), true)", c, ok)
	}
}

// TestInheritFullSurface sweeps every inheritable property through an
// empty receiver: child.Inherit(parent) must equal parent minus
// padding/margin and extras.
func TestInheritFullSurface(t *testing.T) {
	parent := New().
		Foreground(ANSI(1)).Background(ANSI(2)).
		Bold(true).Italic(true).Underline(true).Strikethrough(true).
		Reverse(true).Blink(true).Faint(true).
		Width(10).Height(5).MaxWidth(20).MaxHeight(9).
		Align(AlignCenter, AlignMiddle).
		Border(BorderDouble, true, false).
		BorderForeground(ANSI(6)).
		Padding(1).Margin(2)

	got := New().Inherit(parent)
	want := parent.UnsetPadding().UnsetMargin()
	if got != want {
		t.Fatalf("Inherit(everything) = %+v,\nwant parent minus padding/margin = %+v", got, want)
	}
}

// TestInheritDoesNotCopyExtras: extras are the escape hatch, not a style
// property — Inherit leaves them alone.
func TestInheritDoesNotCopyExtras(t *testing.T) {
	k := ExtKey{Pkg: "acme/tuix", Name: "underline-color"}
	parent := New().Bold(true).Ext(k, 7)
	got := New().Inherit(parent)
	if _, ok := got.GetExt(k); ok {
		t.Error("Inherit copied extras")
	}
	if got.extras != nil {
		t.Error("Inherit attached an extras pointer")
	}
}

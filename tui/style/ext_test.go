package style

import "testing"

var (
	extA = ExtKey{Pkg: "acme/tuix", Name: "underline-color"}
	extB = ExtKey{Pkg: "acme/tuix", Name: "hyperlink"}
)

// TestExtCopyOnWrite covers ADR-0006 §5 acceptance criterion 9 (copy-on-write
// half): Ext never mutates the receiver's map — the extras map is cloned per
// call, so prior copies are untouched.
func TestExtCopyOnWrite(t *testing.T) {
	a := New().Ext(extA, 1)
	b := a.Ext(extA, 2).Ext(extB, "x")

	if v, ok := a.GetExt(extA); !ok || v != 1 {
		t.Errorf("a's extA = (%v, %v) after deriving b, want (1, true)", v, ok)
	}
	if _, ok := a.GetExt(extB); ok {
		t.Error("a gained extB from derived style")
	}
	if v, ok := b.GetExt(extA); !ok || v != 2 {
		t.Errorf("b's extA = (%v, %v), want (2, true)", v, ok)
	}
	if a.extras == b.extras {
		t.Error("derived style shares the receiver's extras map (no clone)")
	}

	// Zero-overhead common case: no Ext, no pointer.
	if New().Bold(true).extras != nil {
		t.Error("extras pointer allocated without Ext")
	}
}

// TestExtNeverAffectsCore covers criterion 9 (extras-never-consulted half,
// as visible from this package): a style with Ext(...) set is identical to
// the same style without it on every core property — same props bits, same
// values — so the tui resolver, which reads only the core surface, renders
// both identically. (The resolver-side render comparison itself lives in
// package tui, §2.6.)
func TestExtNeverAffectsCore(t *testing.T) {
	plain := New().Foreground(ANSI(1)).Bold(true).Padding(1, 2).Border(BorderRounded)
	extended := plain.Ext(extA, 42)

	if plain.props != extended.props {
		t.Error("Ext changed the props bitfield")
	}
	stripped := extended
	stripped.extras = nil
	if stripped != plain {
		t.Error("Ext changed a core field beyond the extras pointer")
	}
}

// TestExtrasPointerComparability covers the documented §2.1 semantics: the
// extras pointer compares by identity, so copies share it and stay ==, while
// two styles with equal-but-distinct extras maps compare unequal — Style
// remains comparable (criterion 2) with extras present.
func TestExtrasPointerComparability(t *testing.T) {
	a := New().Ext(extA, 1)
	b := a // plain copy: shares the pointer
	if a != b {
		t.Error("copy of an extras-carrying style is not ==")
	}
	m := map[Style]int{a: 7}
	if m[b] != 7 {
		t.Error("extras-carrying style unusable as a map key")
	}

	c := New().Ext(extA, 1) // equal contents, distinct map
	if a == c {
		t.Error("styles with distinct extras maps compare == (identity semantics violated)")
	}
}

// TestApplyBatchesExtClones covers §2.7 (rev 1, Q5): Apply is the batching
// path — every option receives the SAME working copy, so N Ext calls inside
// one Apply share a single clone instead of Ext-chaining's O(N²).
func TestApplyBatchesExtClones(t *testing.T) {
	base := New().Ext(extA, 1)

	var p1, p2 *extraProps
	got := base.Apply(
		func(s *Style) { *s = s.Ext(extB, "x"); p1 = s.extras },
		func(s *Style) { *s = s.Ext(extA, 2); p2 = s.extras },
	)

	if p1 == nil || p1 != p2 {
		t.Fatal("second Ext inside Apply re-cloned the extras map (want one clone per Apply)")
	}
	if p1 == base.extras {
		t.Fatal("Apply mutated the receiver's extras map in place")
	}
	// The receiver is untouched (value semantics through Apply).
	if v, _ := base.GetExt(extA); v != 1 {
		t.Errorf("base extA = %v after Apply, want 1", v)
	}
	if _, ok := base.GetExt(extB); ok {
		t.Error("base gained extB through Apply")
	}
	// The result carries both extensions.
	if v, _ := got.GetExt(extA); v != 2 {
		t.Errorf("result extA = %v, want 2", v)
	}
	if v, _ := got.GetExt(extB); v != "x" {
		t.Errorf("result extB = %v, want \"x\"", v)
	}
	// The returned style is at rest: later Ext calls are copy-on-write again.
	later := got.Ext(extA, 3)
	if v, _ := got.GetExt(extA); v != 2 {
		t.Errorf("post-Apply style mutated by a later Ext: extA = %v, want 2", v)
	}
	if later.extras == got.extras {
		t.Error("post-Apply Ext mutated in place (extraMode leaked)")
	}
}

// TestApplyComposesCoreSetters: Apply is the general StyleOption seam, not
// just an extras path — options can use the whole fluent surface, and the
// result equals the equivalent direct chain (comparable, criterion 2).
func TestApplyComposesCoreSetters(t *testing.T) {
	bolden := func(s *Style) { *s = s.Bold(true) }
	pad := func(s *Style) { *s = s.Padding(1, 2) }

	got := New().Foreground(ANSI(1)).Apply(bolden, pad, nil) // nil options skipped
	want := New().Foreground(ANSI(1)).Bold(true).Padding(1, 2)
	if got != want {
		t.Fatalf("Apply result differs from the equivalent chain:\ngot  %+v\nwant %+v", got, want)
	}
	// extraMode is reset: the result is == to a style built without Apply.
	if got.extraMode != extCOW {
		t.Fatal("Apply leaked its working-copy extraMode")
	}
}

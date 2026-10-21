package style

import "testing"

// A Style's attributes are one AttrMask: Attrs is what it sets on, and WithAttrs sets those on
// again, for every mask a Style can carry (all but AttrInvisible).
func TestAStylesAttrsAreTheSharedMask(t *testing.T) {
	setters := map[AttrMask]func(Style, bool) Style{
		AttrBold: Style.Bold, AttrFaint: Style.Faint, AttrItalic: Style.Italic, AttrUnderline: Style.Underline,
		AttrBlink: Style.Blink, AttrReverse: Style.Reverse, AttrStrikethrough: Style.Strikethrough,
	}
	for bit, set := range setters {
		if got := set(New(), true).Attrs(); got != bit {
			t.Errorf("a style set %b reports Attrs %b", bit, got)
		}
		if got := set(New(), false).Attrs(); got != 0 {
			t.Errorf("a style set %b false reports Attrs %b", bit, got)
		}
	}
	carried := AttrBold | AttrFaint | AttrItalic | AttrUnderline | AttrBlink | AttrReverse | AttrStrikethrough
	for m := AttrMask(0); m < AttrInvisible<<1; m++ {
		if got := New().WithAttrs(m).Attrs(); got != m&carried {
			t.Errorf("WithAttrs(%b).Attrs() = %b, want %b", m, got, m&carried)
		}
	}
	if got := New().Bold(true).Italic(true).UnsetBold().Attrs(); got != AttrItalic {
		t.Errorf("an unset attribute stays in Attrs: %b", got)
	}
}

// AttrNames names every bit once, in bit order, so the web client is handed all of them.
func TestAttrNamesCoverEveryBit(t *testing.T) {
	var seen AttrMask
	for i, a := range AttrNames {
		if a.Bit != 1<<i || seen&a.Bit != 0 || a.Name == "" {
			t.Errorf("AttrNames[%d] = %+v", i, a)
		}
		seen |= a.Bit
	}
	if seen != AttrInvisible<<1-1 {
		t.Errorf("AttrNames covers %b, want every bit up to AttrInvisible", seen)
	}
}

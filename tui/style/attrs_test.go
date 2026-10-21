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
	for i, a := range AttrNames() {
		if a.Bit != 1<<i || seen&a.Bit != 0 || a.Name == "" {
			t.Errorf("AttrNames[%d] = %+v", i, a)
		}
		seen |= a.Bit
	}
	if seen != AttrInvisible<<1-1 {
		t.Errorf("AttrNames covers %b, want every bit up to AttrInvisible", seen)
	}
}

// AttrNames is a copy: a caller's change to it reaches neither a later call nor the web client.
func TestAttrNamesIsACopy(t *testing.T) {
	names := AttrNames()
	names[0].Bit, names[0].Name = AttrReverse, "changed"
	if again := AttrNames(); again[0].Bit != AttrBold || again[0].Name != "bold" {
		t.Errorf("a caller's change reached the catalog: %+v", again[0])
	}
}

// Each attribute getter reports its own value and whether it is set: unset is (false, false), set
// true (true, true), set false (false, true).
func TestEachAttrGetterReportsItsValueAndSetness(t *testing.T) {
	for name, c := range map[string]struct {
		set func(Style, bool) Style
		get func(Style) (bool, bool)
	}{
		"bold": {Style.Bold, Style.GetBold}, "faint": {Style.Faint, Style.GetFaint},
		"italic": {Style.Italic, Style.GetItalic}, "underline": {Style.Underline, Style.GetUnderline},
		"blink": {Style.Blink, Style.GetBlink}, "reverse": {Style.Reverse, Style.GetReverse},
		"strikethrough": {Style.Strikethrough, Style.GetStrikethrough},
	} {
		if v, ok := c.get(New()); v || ok {
			t.Errorf("%s unset: (%v, %v), want (false, false)", name, v, ok)
		}
		if v, ok := c.get(c.set(New(), true)); !v || !ok {
			t.Errorf("%s set true: (%v, %v), want (true, true)", name, v, ok)
		}
		if v, ok := c.get(c.set(New(), false)); v || !ok {
			t.Errorf("%s set false: (%v, %v), want (false, true)", name, v, ok)
		}
		if v, _ := c.get(New().WithAttrs(AttrInvisible<<1 - 1)); !v {
			t.Errorf("%s: WithAttrs of every bit leaves it off", name)
		}
	}
}

// The bit helpers derive a prop's bit from its place in the propKey list: the attribute props in
// AttrMask's order, the border edges in the edge bits' order. A reordered const block fails here
// rather than silently mapping the wrong bit.
func TestPropBitsFollowTheirOrder(t *testing.T) {
	for k, want := range map[propKey]AttrMask{
		propBold: AttrBold, propFaint: AttrFaint, propItalic: AttrItalic, propUnderline: AttrUnderline,
		propBlink: AttrBlink, propReverse: AttrReverse, propStrikethrough: AttrStrikethrough,
		propForeground: 0, propPaddingTop: 0,
	} {
		if got := attrBit(k); got != want {
			t.Errorf("attrBit(%#x) = %b, want %b", uint64(k), got, want)
		}
	}
	for k, want := range map[propKey]uint8{
		propBorderTop: edgeTop, propBorderRight: edgeRight, propBorderBottom: edgeBottom, propBorderLeft: edgeLeft,
		propBorderStyle: 0, propBorderTopForeground: 0,
	} {
		if got := edgeBit(k); got != want {
			t.Errorf("edgeBit(%#x) = %b, want %b", uint64(k), got, want)
		}
	}
	for i, k := range []propKey{propBorderTopForeground, propBorderRightForeground, propBorderBottomForeground, propBorderLeftForeground} {
		if got := edgeFgIndex(k); got != i {
			t.Errorf("edgeFgIndex(%#x) = %d, want %d", uint64(k), got, i)
		}
	}
}

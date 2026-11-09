package widget

import (
	"slices"
	"testing"
)

// A separator with nothing to separate is not shown: first or last among the rows shown, or right
// after another, as when the rows of a group all hide.
func TestASeparatorWithNothingToSeparateIsNotShown(t *testing.T) {
	row := func(id string, visible bool) MenuItemModel {
		r := NewCommand(ItemID(id), id, nil)
		r.Visible = visible
		return r
	}
	cases := []struct {
		name  string
		items []MenuItemModel
		want  []int
	}{
		{"between rows", []MenuItemModel{row("a", true), NewSeparator("s"), row("b", true)}, []int{0, 1, 2}},
		{"a group hidden: trailing", []MenuItemModel{row("a", true), NewSeparator("s"), row("b", false), row("c", false)}, []int{0}},
		{"leading", []MenuItemModel{NewSeparator("s"), row("a", true)}, []int{1}},
		{"two in a row", []MenuItemModel{row("a", true), NewSeparator("s1"), row("b", false), NewSeparator("s2"), row("c", true)}, []int{0, 1, 4}},
		{"only separators", []MenuItemModel{NewSeparator("s1"), NewSeparator("s2")}, []int{}},
	}
	for _, c := range cases {
		if got := visibleRows(c.items); !slices.Equal(got, c.want) {
			t.Errorf("%s: shown %v, want %v", c.name, got, c.want)
		}
	}
}

// A row that cannot be chosen reads as off at a glance: muted and faint, not muted alone, which sits
// too close to the text on a warm palette.
func TestADisabledMenuRowIsMutedAndFaint(t *testing.T) {
	if faint, set := DefaultMenuStyle().Disabled().GetFaint(); !set || !faint {
		t.Error("a disabled row is not faint")
	}
}

package widget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// menu_toggled_internal_test.go: the ONE check rule, and its report — the same
// for a native owner of the rows as for a declarative layer over the menu.

func toggledMenu(t *testing.T) (*Menu, *[]string) {
	t.Helper()
	var got []string
	m := NewMenu(WithMenuOnToggled(func(id ItemID, checked, byUser bool) {
		got = append(got, fmt.Sprintf("%s=%v/%v", id, checked, byUser))
	}))
	a := NewRadio("a", "A", "g", nil)
	a.Checked = true
	if err := m.SetModel([]MenuItemModel{
		NewSubmenu("view", "View", []MenuItemModel{NewCheck("wrap", "Wrap", nil), a, NewRadio("b", "B", "g", nil)}),
	}); err != nil {
		t.Fatal(err)
	}
	return m, &got
}

// A user's toggle: a check flips; a radio checks and clears its group — each
// change reported, the cleared rows first, and Checked answers the new state.
func TestAUsersToggleIsReportedByTheOneRule(t *testing.T) {
	m, got := toggledMenu(t)
	m.activate("wrap", tui.ActionInvocation{})
	m.activate("b", tui.ActionInvocation{})
	m.activate("b", tui.ActionInvocation{}) // already checked: no change, no report
	if g := strings.Join(*got, " "); g != "wrap=true/true a=false/true b=true/true" {
		t.Errorf("reported %q", g)
	}
	for id, want := range map[ItemID]bool{"wrap": true, "a": false, "b": true} {
		if c, ok := m.Checked(id); !ok || c != want {
			t.Errorf("Checked(%s) = %v,%v, want %v", id, c, ok, want)
		}
	}
}

// SetChecked follows the same rule and is reported the same way, not by the user.
func TestSetCheckedIsReportedByTheSameRule(t *testing.T) {
	m, got := toggledMenu(t)
	m.SetChecked("b", true)
	m.SetChecked("wrap", false) // unchanged: nothing to report
	if g := strings.Join(*got, " "); g != "a=false/false b=true/false" {
		t.Errorf("reported %q", g)
	}
}

package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A CheckBox is a check box to a native style, though it holds a Button.
func TestCheckBoxRoleAndChecked(t *testing.T) {
	n := &checkBoxNode{checked: true}
	if tui.RoleOf(n) != tui.RoleCheckBox {
		t.Fatalf("role %v; want RoleCheckBox", tui.RoleOf(n))
	}
	if !n.Checked() {
		t.Fatal("Checked() is false on a checked box")
	}
}

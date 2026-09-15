package widget_test

// ROW WIDTH IS WHAT THE ROW PAINTS, NOT WHAT ITS TEXT MEASURES.
//
// paintRow lays a row out as: one pad, an optional mark, the label, an optional
// accelerator, an optional submenu arrow two cells from the right edge, and one
// pad. rowWidth has to ask for all of that, and it asked for the content and
// NEITHER PAD. The widest row of a level sets the popup's width, so that row
// then painted its last cell onto the frame.
//
// Two different symptoms, which is why it survived: a check, radio or plain row
// keeps its whole label and destroys the right border, while a submenu row also
// loses its last character, because the arrow is placed at a fixed offset from
// the right edge and lands on top of the label. A test that only looked at
// label text would see three of the four cases as fine.
//
// MenuItem — the standalone row component — already sized itself as
// measure(label)+2. This is the model-driven painter agreeing with it.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// popupLines returns the rendered lines that contain want, so an assertion can
// look at the row's own line rather than the whole screen.
func popupLines(grid, want string) []string {
	var out []string
	for line := range strings.SplitSeq(grid, "\n") {
		if strings.Contains(line, want) {
			out = append(out, line)
		}
	}
	return out
}

// openOnly builds a level holding exactly one row and opens it, so that row is
// necessarily the widest and no neighbour lends it slack.
func openOnly(t *testing.T, row widget.MenuItemModel) string {
	t.Helper()
	menu := widget.NewMenu()
	host := widget.NewOverlayHost(menu)
	h := startApp(t, host, 60, 12)
	defer h.stop()

	h.onLoop(func() {
		if err := menu.SetModel([]widget.MenuItemModel{
			widget.NewSubmenu("top", "T", []widget.MenuItemModel{row}),
		}); err != nil {
			t.Fatalf("SetModel: %v", err)
		}
	})
	h.settle()
	h.onLoop(func() {
		if err := menu.Open("top"); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()
	return h.grid()
}

// TestTheWidestRowOfALevelFitsInsideItsFrame.
//
// Both halves of the contract, for every kind: the label survives, and so does
// the border beside it. Counting the frame glyphs on the row's own line is what
// makes the second half real — asserting merely that the line CONTAINS "│"
// passes on the broken output, because the left border is still there.
func TestTheWidestRowOfALevelFitsInsideItsFrame(t *testing.T) {
	for _, tc := range []struct {
		name  string
		row   widget.MenuItemModel
		label string
	}{
		{"a submenu row", widget.NewSubmenu("only", "Keymaps", []widget.MenuItemModel{
			widget.NewCommand("leaf", "Leaf", nil),
		}), "Keymaps"},
		{"a check row", widget.NewCheck("only", "Checkable", nil), "Checkable"},
		{"a radio row", widget.NewRadio("only", "Radioed", "g", nil), "Radioed"},
		{"a plain row", widget.NewCommand("only", "Plain", nil), "Plain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grid := openOnly(t, tc.row)

			if !strings.Contains(grid, tc.label) {
				t.Errorf("the label %q is not intact: the row was sized to its content "+
					"and then painted with a marker it had no room for\n%s", tc.label, grid)
			}
			lines := popupLines(grid, tc.label)
			if len(lines) == 0 {
				t.Fatalf("the row never rendered:\n%s", grid)
			}
			for _, line := range lines {
				if n := strings.Count(line, "│"); n != 2 {
					t.Errorf("the row's line carries %d frame glyphs, want 2 (left and "+
						"right); the row overflowed onto the border\n%s", n, grid)
				}
			}
		})
	}
}

// TestAnAcceleratorStillSitsInsideTheFrame.
//
// The accelerator was already counted, so this is the case that must NOT change
// — a control against a fix that simply widened every row until the symptoms
// went away.
func TestAnAcceleratorStillSitsInsideTheFrame(t *testing.T) {
	row := widget.NewCommand("only", "Save", nil)
	row.Accel = "Ctrl+S"
	grid := openOnly(t, row)

	if !strings.Contains(grid, "Save") || !strings.Contains(grid, "Ctrl+S") {
		t.Fatalf("label and accelerator are not both on screen:\n%s", grid)
	}
	for _, line := range popupLines(grid, "Ctrl+S") {
		if n := strings.Count(line, "│"); n != 2 {
			t.Errorf("the accelerator row's line carries %d frame glyphs, want 2\n%s",
				n, grid)
		}
	}
}

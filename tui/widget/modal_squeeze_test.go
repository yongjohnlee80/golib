package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// modal_squeeze_test.go: a card taller than its host gives up its blank rows
// before its message — the message is what the dialog is for.

func squeezeFixture(t *testing.T, h int) (*harness, []string) {
	t.Helper()
	yes := widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept))
	no := widget.NewButton("No", widget.WithRole(widget.ButtonRoleReject))
	md := widget.NewModal(widget.NewText("Leave now?"), widget.WithModalTitle("quit?"),
		widget.WithModalRule(true), widget.WithButtons(yes, no))
	host := widget.NewOverlayHost(widget.NewText(""))
	hh := startApp(t, host, 40, h)
	hh.onLoop(func() {
		if err := md.Open(host); err != nil {
			t.Fatal(err)
		}
	})
	hh.settle()
	return hh, strings.Split(hh.grid(), "\n")
}

// On a host too short for the card, the message and the buttons are shown.
func TestASqueezedCardKeepsItsMessageAndButtons(t *testing.T) {
	for _, h := range []int{8, 7, 6, 5} {
		hh, _ := squeezeFixture(t, h)
		scr := hh.grid()
		if !strings.Contains(scr, "Leave now?") || !strings.Contains(scr, "Yes") || !strings.Contains(scr, "No") {
			t.Errorf("h=%d: the message or a button was squeezed out:\n%s", h, scr)
		}
		hh.stop()
	}
}

// With room, the card is laid out as it always was: a padding row under the
// border, the message, a blank row, the rule, a blank row, the buttons.
func TestACardWithRoomKeepsItsAir(t *testing.T) {
	hh, rows := squeezeFixture(t, 20)
	defer hh.stop()
	top := -1
	for i, r := range rows {
		if strings.Contains(r, "quit?") {
			top = i
			break
		}
	}
	if top < 0 || top+6 >= len(rows) {
		t.Fatalf("no card found:\n%s", hh.grid())
	}
	want := []string{"", "Leave now?", "", "├", "", "Yes"}
	for k, w := range want {
		r := rows[top+1+k]
		inner := strings.Trim(strings.TrimSpace(r), "│")
		switch {
		case w == "" && strings.TrimSpace(inner) != "":
			t.Errorf("row %d under the title should be blank, is %q", k+1, r)
		case w != "" && !strings.Contains(r, w):
			t.Errorf("row %d under the title should hold %q, is %q", k+1, w, r)
		}
	}
}

// A squeezed card with a help line keeps its message, its buttons and its help
// line, drawn on the row the layout gave it.
func TestASqueezedCardKeepsItsHelpLine(t *testing.T) {
	// Down to 5 rows: there, room is left for one of the rule and the help line
	// beside the message and the button, and the help line — information —
	// outlasts the rule, which is decoration.
	for _, h := range []int{9, 8, 7, 6, 5} {
		yes := widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept))
		md := widget.NewModal(widget.NewText("Leave now?"), widget.WithModalTitle("quit?"),
			widget.WithModalRule(true), widget.WithButtons(yes), widget.WithModalFooter("Esc closes"))
		host := widget.NewOverlayHost(widget.NewText(""))
		hh := startApp(t, host, 40, h)
		hh.onLoop(func() {
			if err := md.Open(host); err != nil {
				t.Fatal(err)
			}
		})
		hh.settle()
		scr := hh.grid()
		for _, want := range []string{"Leave now?", "Yes", "Esc closes"} {
			if !strings.Contains(scr, want) {
				t.Errorf("h=%d: %q was squeezed out:\n%s", h, want, scr)
			}
		}
		// On the row the layout gave it: with the card squeezed, the bottom
		// padding is gone, so the help line sits right above the bottom border.
		if h < 9 {
			rows := strings.Split(scr, "\n")
			foot, bottom := -1, -1
			for i, r := range rows {
				if strings.Contains(r, "Esc closes") {
					foot = i
				}
				if strings.Contains(r, "└") {
					bottom = i
				}
			}
			if foot < 0 || foot != bottom-1 {
				t.Errorf("h=%d: the help line is on row %d, the bottom border on %d; want it right above:\n%s", h, foot, bottom, scr)
			}
		}
		hh.stop()
	}
}

// At the very least — the border, one line of message, one row of buttons —
// the rule and the help line give way too: decoration and help before the
// question and its answers.
func TestACardAtItsSmallestKeepsTheQuestionAndItsAnswers(t *testing.T) {
	for _, footer := range []string{"", "Esc closes"} {
		yes := widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept))
		opts := []widget.ModalOption{widget.WithModalRule(true), widget.WithButtons(yes)}
		if footer != "" {
			opts = append(opts, widget.WithModalFooter(footer))
		}
		md := widget.NewModal(widget.NewText("Leave now?"), opts...)
		host := widget.NewOverlayHost(widget.NewText(""))
		hh := startApp(t, host, 40, 4)
		hh.onLoop(func() {
			if err := md.Open(host); err != nil {
				t.Fatal(err)
			}
		})
		hh.settle()
		scr := hh.grid()
		if !strings.Contains(scr, "Leave now?") || !strings.Contains(scr, "Yes") {
			t.Errorf("footer=%q, 4 rows: the message or the button was squeezed out:\n%s", footer, scr)
		}
		hh.stop()
	}
}

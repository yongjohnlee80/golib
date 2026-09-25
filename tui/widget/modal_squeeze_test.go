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

// A body that FILLS what it is offered — a list — needs only a row. A roomy
// dialog around one keeps its rule and help line; squeezed, it keeps a row of
// the list and its button.
func TestACardAroundAFillingBodySqueezesOnlyWhenItMust(t *testing.T) {
	for _, c := range []struct {
		h        int
		wantRule bool
	}{{16, true}, {5, false}} {
		list := widget.NewList(widget.WithItems([]string{"alpha", "beta", "gamma"}, func(s string) string { return s }))
		yes := widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept))
		md := widget.NewModal(list, widget.WithModalRule(true), widget.WithButtons(yes),
			widget.WithModalFooter("Esc closes"))
		host := widget.NewOverlayHost(widget.NewText(""))
		hh := startApp(t, host, 40, c.h)
		hh.onLoop(func() {
			if err := md.Open(host); err != nil {
				t.Fatal(err)
			}
		})
		hh.settle()
		scr := hh.grid()
		if !strings.Contains(scr, "alpha") || !strings.Contains(scr, "Yes") {
			t.Errorf("h=%d: the list or the button was squeezed out:\n%s", c.h, scr)
		}
		if got := strings.Contains(scr, "├") && strings.Contains(scr, "Esc closes"); got != c.wantRule {
			t.Errorf("h=%d: rule and help line shown=%v, want %v:\n%s", c.h, got, c.wantRule, scr)
		}
		hh.stop()
	}
}

// A message longer than the room is content, not a filler: squeezed, it keeps
// as many of its lines as the card can hold once the decoration has yielded.
func TestASqueezedCardKeepsAsMuchOfALongMessageAsFits(t *testing.T) {
	yes := widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept))
	md := widget.NewModal(widget.NewText("one\ntwo\nthree\nfour\nfive\nsix", widget.WithWrapMode(widget.Wrap)),
		widget.WithModalRule(true), widget.WithButtons(yes))
	host := widget.NewOverlayHost(widget.NewText(""))
	hh := startApp(t, host, 40, 8)
	defer hh.stop()
	hh.onLoop(func() {
		if err := md.Open(host); err != nil {
			t.Fatal(err)
		}
	})
	hh.settle()
	scr := hh.grid()
	for _, want := range []string{"one", "two", "three", "four", "five", "Yes"} {
		if !strings.Contains(scr, want) {
			t.Errorf("8 rows: %q is not shown — the border and a button leave five rows for the message:\n%s", want, scr)
		}
	}
}

func openSmallCard(t *testing.T, title, footer string) *harness {
	t.Helper()
	yes := widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept))
	no := widget.NewButton("No", widget.WithRole(widget.ButtonRoleReject))
	opts := []widget.ModalOption{widget.WithModalTitle(title), widget.WithButtons(yes, no)}
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
	return hh
}

// buttonRow returns the card's button row and where its border is.
func buttonRow(t *testing.T, hh *harness) (row string, left, right int) {
	t.Helper()
	for _, r := range strings.Split(hh.grid(), "\n") {
		if strings.Contains(r, "Yes") {
			return r, strings.Index(r, "│"), strings.LastIndex(r, "│")
		}
	}
	t.Fatalf("no button row:\n%s", hh.grid())
	return "", -1, -1
}

// Neither a title nor a help line wider than the card sets the width its
// buttons are centred in: they stay inside the card's border.
func TestASqueezedCardCentresItsButtonsInsideItself(t *testing.T) {
	long := strings.Repeat("a long line ", 8)
	for _, c := range []struct{ name, title, footer string }{
		{"title", long, ""},
		{"help line", "quit?", long},
	} {
		hh := openSmallCard(t, c.title, c.footer)
		r, left, right := buttonRow(t, hh)
		if yes, no := strings.Index(r, "Yes"), strings.Index(r, "No"); left < 0 || right <= left || yes < left || no > right {
			t.Errorf("%s: the buttons are outside the card: %q\n%s", c.name, r, hh.grid())
		}
		hh.stop()
	}
}

// A help line the squeeze took away no longer widens the card.
func TestASqueezedOutHelpLineDoesNotWidenTheCard(t *testing.T) {
	plain := openSmallCard(t, "quit?", "")
	_, pl, pr := buttonRow(t, plain)
	plain.stop()
	helped := openSmallCard(t, "quit?", "Esc closes; Enter answers yes")
	if strings.Contains(helped.grid(), "Esc closes") {
		t.Fatalf("the help line was expected to be squeezed out at 4 rows:\n%s", helped.grid())
	}
	_, hl, hr := buttonRow(t, helped)
	if hr-hl != pr-pl {
		t.Errorf("the card is %d wide with its help line squeezed out, %d without one:\n%s", hr-hl, pr-pl, helped.grid())
	}
	helped.stop()
}

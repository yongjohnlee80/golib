package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// modal_prompt_test.go holds a dialog shaped as a prompt — a field, no
// buttons, a help line — to what makes it one: it is as wide as it is told,
// the help sits under a rule, the keyboard lands in the field, and the field
// paints its whole box.

func TestAModalWidthSetsTheCardWidthAndAFieldFillsIt(t *testing.T) {
	field := widget.NewTextInput(widget.WithTextInputStyles(widget.TextInputStyles{
		Text: style.New().Background(blue),
	}))
	m := widget.NewModal(field, widget.WithModalWidth(30), widget.WithModalTitle("Command"))
	h, host, _ := modalFixture(t, m, 80, 12)
	defer h.stop()
	openOn(t, h, m, host)

	y := h.rowWith("┌ Command ")
	row := []rune(h.row(y))
	left := strings.IndexRune(string(row), '┌')
	left = len([]rune(string(row)[:left]))
	right := left
	for right < len(row) && row[right] != '┐' {
		right++
	}
	if w := right - left + 1; w != 30 {
		t.Fatalf("the card is %d wide, want 30:\n%s", w, h.grid())
	}
	fw := 0
	for _, c := range h.tb.Snapshot()[y+2] {
		if isBlue(c) {
			fw++
		}
	}
	if fw != 30-4 {
		t.Errorf("the field is %d wide, want the card's inside, %d", fw, 30-4)
	}
}

func TestAModalWidthNeverExceedsTheHost(t *testing.T) {
	m := widget.NewModal(widget.NewTextInput(), widget.WithModalWidth(500))
	h, host, _ := modalFixture(t, m, 40, 10)
	defer h.stop()
	openOn(t, h, m, host)
	if !strings.Contains(h.row(h.rowWith("┌")), "┐") {
		t.Fatalf("a card wider than the host lost its frame:\n%s", h.grid())
	}
}

func TestTheHelpLineOfAButtonlessDialogSitsUnderARule(t *testing.T) {
	m := widget.NewModal(widget.NewText("body"),
		widget.WithModalRule(true), widget.WithModalFooter("the help"))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	body, help := h.rowWith("body"), h.rowWith("the help")
	if help != body+4 || !strings.Contains(h.row(body+2), "├") {
		t.Fatalf("want body, blank, rule, blank, help:\n%s", h.grid())
	}
}

func TestAButtonlessDialogPutsTheKeyboardInItsField(t *testing.T) {
	field := widget.NewTextInput()
	m := widget.NewModal(field, widget.WithModalFooter("help"))
	h, host, _ := modalFixture(t, m, 40, 10)
	defer h.stop()
	openOn(t, h, m, host)

	var inField, onModal bool
	h.onLoop(func() {
		inField = field.Context().Focused()
		onModal = m.AcceptsFocus()
	})
	if !inField {
		t.Error("focus did not land on the dialog's field")
	}
	if onModal {
		t.Error("the Modal node offers itself as a tab stop beside a field that takes focus")
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'x', Text: "x"})
	h.settle()
	if field.Value() != "x" {
		t.Errorf("typing reached %q, want the field to hold \"x\"", field.Value())
	}
}

func TestAnEmptyFieldPaintsItsWholeBox(t *testing.T) {
	field := widget.NewTextInput(widget.WithTextInputStyles(widget.TextInputStyles{
		Text: style.New().Background(blue),
	}))
	h := startApp(t, field, 20, 1)
	defer h.stop()
	h.settle()
	for x, c := range h.tb.Snapshot()[0] {
		if !isBlue(c) {
			t.Fatalf("cell %d: bg %+v, want the field's", x, c.Attrs.BG)
		}
	}
}

var blue = style.RGB(0, 0, 170)

func isBlue(c tui.Cell) bool {
	return c.Attrs.BG.R == 0 && c.Attrs.BG.G == 0 && c.Attrs.BG.B == 170 && c.Attrs.BG.Kind != 0
}

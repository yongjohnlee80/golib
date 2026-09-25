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

// cardWidth is the width of the card whose top border holds title.
func cardWidth(t *testing.T, h *harness, title string) int {
	t.Helper()
	y := h.rowWith(title)
	row := []rune(h.row(y))
	left := 0
	for left < len(row) && row[left] != '┌' {
		left++
	}
	right := left
	for right < len(row) && row[right] != '┐' {
		right++
	}
	return right - left + 1
}

// TestAModalWidthNeverClipsTheHelpLine: a width is a width, not a clip — the
// help line still fits, as it does on a card sized to its content.
func TestAModalWidthNeverClipsTheHelpLine(t *testing.T) {
	help := "Enter runs the command, Esc closes"
	m := widget.NewModal(widget.NewTextInput(), widget.WithModalWidth(20),
		widget.WithModalTitle("Command"), widget.WithModalFooter(help))
	h, host, _ := modalFixture(t, m, 80, 12)
	defer h.stop()
	openOn(t, h, m, host)
	if w := cardWidth(t, h, "┌ Command "); w != len(help)+4 {
		t.Fatalf("the card is %d wide, want the help line's %d:\n%s", w, len(help)+4, h.grid())
	}
	h.wantContains(help)
}

// TestTheBodysControlIsFoundWhereverItSits: inside a Box, inside a Split, and
// — with no control at all — the Modal node is the stop.
func TestTheBodysControlIsFoundWhereverItSits(t *testing.T) {
	for _, c := range []struct {
		name string
		body func(field *widget.TextInput) tui.Component
	}{
		{"in a box", func(f *widget.TextInput) tui.Component { return widget.NewBox(f) }},
		{"in a split", func(f *widget.TextInput) tui.Component {
			return widget.NewSplit(widget.Horizontal, widget.NewText("label"), f)
		}},
	} {
		field := widget.NewTextInput()
		m := widget.NewModal(c.body(field), widget.WithModalFooter("help"))
		h, host, _ := modalFixture(t, m, 60, 12)
		openOn(t, h, m, host)
		var inField, onModal bool
		h.onLoop(func() { inField, onModal = field.Context().Focused(), m.AcceptsFocus() })
		if !inField || onModal {
			t.Errorf("%s: focus in the field %v, the Modal a stop %v", c.name, inField, onModal)
		}
		h.stop()
	}

	m := widget.NewModal(widget.NewText("nothing to type in"), widget.WithModalFooter("help"))
	h, host, _ := modalFixture(t, m, 60, 12)
	defer h.stop()
	openOn(t, h, m, host)
	var accepts bool
	h.onLoop(func() { accepts = m.AcceptsFocus() })
	if !accepts {
		t.Error("a dialog with no control and no button left its trap without a stop")
	}
}

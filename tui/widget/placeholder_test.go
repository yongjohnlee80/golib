package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// cellAt is the cell of the harness's screen in the column where text starts on row y.
func cellAt(h *harness, y int, text string) (tui.Cell, bool) {
	row := h.row(y)
	i := strings.Index(row, text)
	if i < 0 {
		return tui.Cell{}, false
	}
	return h.tb.Snapshot()[y][len([]rune(row[:i]))], true
}

// TestAPlaceholderIsDrawnMutedNotAsText: a field's placeholder is the muted colour AND faint, so it
// cannot be taken for a value typed or prefilled; the text typed over it is neither. The colour
// alone, TokenTextMuted, is the foreground's by default, and drew the placeholder as bright as text.
func TestAPlaceholderIsDrawnMutedNotAsText(t *testing.T) {
	h, _, sh := focusedInput(t, widget.WithPlaceholder("https://example.com/repo"))
	h.settle()
	c, ok := cellAt(h, 0, "https")
	if !ok {
		t.Fatalf("no placeholder:\n%s", h.grid())
	}
	if c.Attrs.Mask&tui.AttrFaint == 0 {
		t.Fatalf("the placeholder is not faint: %+v", c.Attrs)
	}
	h.inject(typeString("typed")...)
	h.barrier(sh)
	if c, ok := cellAt(h, 0, "typed"); !ok || c.Attrs.Mask&tui.AttrFaint != 0 {
		t.Fatalf("typed text is faint (%v): %+v", ok, c.Attrs)
	}
}

// TestTheRulerIsDrawnInThePlaceholdersMutedLook: the Editor's guide is "drawn in the placeholder's
// muted look", as its doc says: faint, where the Editor's default had no placeholder look at all.
func TestTheRulerIsDrawnInThePlaceholdersMutedLook(t *testing.T) {
	ed := widget.NewEditor(widget.WithRuler(10))
	ed.SetKeyset(widget.KeysetStandard)
	ed.SetValue("short")
	h := startApp(t, ed, 40, 3)
	defer h.stop()
	h.settle()
	if c := h.tb.Snapshot()[1][9]; c.Content != "│" || c.Attrs.Mask&tui.AttrFaint == 0 {
		t.Fatalf("the ruler %q is not faint: %+v\n%s", c.Content, c.Attrs, h.grid())
	}
}

// TestASelectsPlaceholderIsMutedToo: Select's placeholder, drawn by the same muted look.
func TestASelectsPlaceholderIsMutedToo(t *testing.T) {
	h, _, _ := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta")),
		widget.WithSelectPlaceholder[string]("empty…"))
	h.wantContains("empty…")
	y := -1
	for i := range 10 {
		if strings.Contains(h.row(i), "empty…") {
			y = i
			break
		}
	}
	if c, ok := cellAt(h, y, "empty"); !ok || c.Attrs.Mask&tui.AttrFaint == 0 {
		t.Fatalf("Select's placeholder is not faint (%v): %+v", ok, c.Attrs)
	}
}

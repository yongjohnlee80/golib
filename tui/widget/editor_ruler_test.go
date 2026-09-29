package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestTheRulerMarksItsColumnPastTheText: the guide stands in column 10 (the 10th) wherever a line
// has ended — rows past the document too — and yields to a line that crosses it; 0 takes it away.
func TestTheRulerMarksItsColumnPastTheText(t *testing.T) {
	ed := widget.NewEditor(widget.WithRuler(10))
	ed.SetKeyset(widget.KeysetStandard)
	ed.SetValue("short\nthis line runs past the ruler")
	h := startApp(t, ed, 40, 4)
	defer h.stop()
	h.settle()
	at := func(row int) string {
		r := []rune(h.row(row))
		if len(r) < 10 {
			return ""
		}
		return string(r[9])
	}
	if at(0) != "│" || at(2) != "│" {
		t.Fatalf("the guide is not in column 10 past the text:\n%s", h.grid())
	}
	if at(1) == "│" {
		t.Fatalf("the guide covered a line that crosses it:\n%s", h.grid())
	}
	h.onLoop(func() { ed.SetRuler(0) })
	h.settle()
	if strings.Contains(h.grid(), "│") {
		t.Fatalf("SetRuler(0) left the guide:\n%s", h.grid())
	}
}

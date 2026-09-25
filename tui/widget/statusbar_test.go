package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestStatusBarSegments(t *testing.T) {
	bar := widget.NewStatusBar()
	h := startApp(t, bar, 40, 1)
	h.onLoop(func() {
		bar.SetLeft("LEFT")
		bar.SetCenter("CENTER")
		bar.SetRight("RIGHT", style.New().Bold(true))
	})
	h.settle()
	row := h.row(0)
	if !strings.HasPrefix(row, "LEFT") || !strings.HasSuffix(row, "RIGHT") {
		t.Fatalf("segment placement: %q", row)
	}
	if got := strings.Index(row, "CENTER"); got != 17 {
		t.Fatalf("center segment at %d, want 17: %q", got, row)
	}
	// Truncation priority (center first, right survives): narrow to 14.
	h.tb.InjectResize(14, 1)
	h.waitFor("narrow bar", func() bool { return strings.HasSuffix(h.row(0), "RIGHT") && !strings.Contains(h.row(0), "CENTER") })
}

// TestStatusBarOptions: the construction option sets the bar's look, a nil
// option is skipped, and a segment takes one style at most.
func TestStatusBarOptions(t *testing.T) {
	red := style.New().Background(style.ANSI(1))
	s := widget.NewStatusBar(nil, widget.WithBarStyle(red))
	h := startApp(t, s, 10, 1)
	h.waitFor("the bar in red", func() bool {
		var bg tui.CellColor
		h.onLoop(func() { bg = h.tb.Snapshot()[0][0].Attrs.BG })
		return bg == tui.CellColor{Kind: tui.CellColorANSI, Index: 1}
	})
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "at most 1") {
			t.Fatalf("two styles: recovered %v", r)
		}
	}()
	s.SetLeft("x", red, red)
}

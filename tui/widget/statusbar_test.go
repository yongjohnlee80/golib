package widget_test

import (
	"strings"
	"testing"

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

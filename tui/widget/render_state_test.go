package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// A determinate bar's filled columns are its full cells and the partial block after them; an
// indeterminate bar has none to report.
func TestAProgressBarsFilledColumnsAreWhatItDraws(t *testing.T) {
	p := widget.NewProgressBar()
	for _, tc := range []struct {
		f    float64
		want int
	}{{0, 0}, {0.5, 5}, {0.55, 6}, {0.501, 5}, {1, 10}} {
		p.SetProgress(tc.f)
		if got := p.FilledColumns(10); got != tc.want {
			t.Errorf("%v of 10: %d filled, want %d", tc.f, got, tc.want)
		}
	}
	p.SetIndeterminate()
	if got := p.FilledColumns(10); got != 0 {
		t.Errorf("indeterminate: %d filled, want 0", got)
	}
}

// A button's mnemonic column is where its cells show the key's cluster.
func TestAButtonsMnemonicColumnIsWhereItsKeyIsDrawn(t *testing.T) {
	b := widget.NewButton("Open", widget.WithMnemonic('p'))
	h := startAppOpts(t, newShell(b), 12, 1)
	defer h.stop()
	h.waitFor("the button drawn", func() bool { return strings.Contains(h.row(0), "Open") })
	want := strings.Index(h.row(0), "p")
	var col int
	var ok bool
	h.onLoop(func() { col, ok = b.MnemonicColumn(12) })
	if !ok || col != want {
		t.Fatalf("MnemonicColumn(12) = %d, %v; the key is drawn at %d in %q", col, ok, want, h.row(0))
	}
	h.onLoop(func() { col, ok = widget.NewButton("Open").MnemonicColumn(12) })
	if ok {
		t.Errorf("an unmounted button reported a mnemonic column %d", col)
	}
}

package widget_test

import (
	"image/color"
	"strings"
	"testing"
	"time"

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
	// a spinner bar draws one cell, a frame or a blank, at any progress: none of it a fill
	sp := widget.NewProgressBar(widget.WithSpinner([]string{"|", "/"}, time.Second))
	sp.SetProgress(0.5)
	if got := sp.FilledColumns(10); got != 0 {
		t.Errorf("a spinner at 0.5: %d filled, want 0 (Render draws one blank cell)", got)
	}
	if !sp.Spinner() || p.Spinner() {
		t.Errorf("Spinner: %v for a WithSpinner bar, %v for a plain one; want true, false", sp.Spinner(), p.Spinner())
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

// A strip's bar colour is what SetBarColor gives, asked each time; none without one, or once taken
// away.
func TestATabStripsBarColorIsWhatItWasGiven(t *testing.T) {
	s := widget.NewTabStrip()
	if _, ok := s.BarColor(); ok {
		t.Fatal("a new strip has a bar colour")
	}
	c, ok := color.NRGBA{R: 1}, false
	s.SetBarColor(func() (color.NRGBA, bool) { return c, ok })
	if _, got := s.BarColor(); got {
		t.Fatal("a colour that has none yet was reported")
	}
	ok = true
	if got, has := s.BarColor(); !has || got != c {
		t.Fatalf("BarColor = %v, %v; want %v", got, has, c)
	}
	s.SetBarColor(nil)
	if _, has := s.BarColor(); has {
		t.Fatal("a bar colour taken away was still reported")
	}
}

// A WindowButtons' controls are its buttons in their order, each with the operation it invokes.
func TestWindowButtonsControlsPairEachButtonWithItsOperation(t *testing.T) {
	w := widget.NewWindowButtons(widget.NewWindowCore(widget.WithWindowClose(), widget.WithWindowMaximize()))
	got := w.Controls()
	if len(got) != 2 || got[0].Op != widget.WindowMaximize || got[1].Op != widget.WindowClose {
		t.Fatalf("Controls %+v, want maximize then close", got)
	}
	for _, c := range got {
		want := map[widget.WindowOperation]string{widget.WindowMaximize: "+", widget.WindowClose: "x"}[c.Op]
		if c.Button == nil || c.Button.Label() != want {
			t.Errorf("%v's button is %+v, want the one labelled %q", c.Op, c.Button, want)
		}
	}
}

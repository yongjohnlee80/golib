package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// slotView is a DocumentView that records what it was shown.
type slotView struct {
	*widget.Text
	shown string
}

func (v *slotView) ShowDocument(text []byte) { v.shown = string(text) }

// A DocumentSlot is the Rendered state both editors keep. Rendered is drawn when it is chosen and
// the document can render: the view turned on, or the editor rendering itself, and not withheld.
// Show hands the view the text and swaps its visibility, once per change; a slot with no view
// changes nothing. The choice is kept while Raw is drawn.
func TestADocumentSlotDrawsWhatCanRender(t *testing.T) {
	var d widget.DocumentSlot
	d.Choose(widget.ViewRendered)
	if d.Drawn(false) != widget.ViewRaw || d.Drawn(true) != widget.ViewRendered {
		t.Errorf("no view: drawn %v without a renderer, %v with one; want Raw, Rendered", d.Drawn(false), d.Drawn(true))
	}
	if d.Show(true, func() []byte { return []byte("x") }) {
		t.Error("Show with no view reported a change")
	}
	v := &slotView{Text: widget.NewText("")}
	if old := d.PutDocument(v); old != nil || v.Visible() {
		t.Errorf("PutDocument: old %v, visible %v; want none, hidden", old, v.Visible())
	}
	if d.Drawn(false) != widget.ViewRaw || d.Active() {
		t.Error("a view not turned on renders")
	}
	if !d.SetOn(true) || d.SetOn(true) || d.Drawn(false) != widget.ViewRendered {
		t.Errorf("SetOn: drawn %v; want Rendered, and one change", d.Drawn(false))
	}
	if !d.SetEnabled(false) || d.Drawn(true) != widget.ViewRaw || d.Chosen() != widget.ViewRendered || d.Enabled() {
		t.Errorf("withheld: drawn %v, chosen %v; want Raw drawn, Rendered kept", d.Drawn(true), d.Chosen())
	}
	d.SetEnabled(true)
	if !d.Show(true, func() []byte { return []byte("doc") }) || d.Show(true, nil) || !d.Shown() || v.shown != "doc" || !v.Visible() {
		t.Errorf("Show: shown %v, text %q, visible %v; want shown once, the text, visible", d.Shown(), v.shown, v.Visible())
	}
	if !d.Show(false, nil) || d.Shown() || v.Visible() {
		t.Error("Show(false) left the view shown")
	}
}

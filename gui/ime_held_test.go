package gui

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/io/key"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"golang.org/x/image/math/fixed"
)

// fcitx5's Hangul engine, typing 하 then ㅇ: it commits 하 and starts composing ㅇ in one go.
// take holds the committed 하 while the composition runs, so held must show it, before the
// composition, which alone is the composing range.
func hangulHeldThenComposing(s *imeState) {
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "ㅎ"})
	s.compose(key.CompositionEvent{Start: 0, End: 1})
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "하"})
	s.compose(key.CompositionEvent{Start: 0, End: 1})
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "하"}) // the commit
	s.edit(key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: "ㅇ"}) // the next composition
	s.compose(key.CompositionEvent{Start: 1, End: 2})
}

func TestHeldShowsTheCommittedTextTheCompositionHolds(t *testing.T) {
	var s imeState
	hangulHeldThenComposing(&s)
	if pieces, ok := s.take(); ok {
		t.Fatalf("take delivered %q during a composition; the committed part waits with it", pieces)
	}
	held, comp, ok := s.held()
	if !ok || string(held) != "하ㅇ" || comp != (key.Range{Start: 1, End: 2}) {
		t.Fatalf("held %q comp %v ok %v, want \"하ㅇ\" with ㅇ (1..2) composing", string(held), comp, ok)
	}
	if got := s.preedit(); got != "ㅇ" {
		t.Errorf("preedit %q, want the composition alone", got)
	}
	s.compose(key.CompositionEvent{Start: -1, End: -1})
	if _, _, ok := s.held(); ok {
		t.Error("held reported text with no composition active (take delivers it then)")
	}
}

// Drawn at the caret, the held 하 shows in the text's ink and only ㅇ is underlined: before, the
// composition alone was drawn there, so 하 vanished under ㅇ until a space committed both.
func TestThePreeditDrawsTheHeldCommitBeforeTheComposition(t *testing.T) {
	b := NewBackend()
	hangulHeldThenComposing(&b.gio.ime)
	fs := b.font.Load()
	m := measure(fs.fm, fs.size, 0, image.Pt(240, 60), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	hw, err := headless.NewWindow(m.window.X, m.window.Y)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	// each syllable's advance, as drawPreedit shapes it
	s := b.gio.shaper
	s.LayoutString(text.Parameters{Font: fontOf(fs.typeface), PxPerEm: fixed.Int26_6(m.ppem * 64), MaxWidth: 1 << 20}, "하ㅇ")
	var advs []int
	for g, ok := s.NextGlyph(); ok; g, ok = s.NextGlyph() {
		advs = append(advs, g.Advance.Round())
	}
	if len(advs) != 2 {
		t.Fatalf("shaped %d glyphs for 하ㅇ, want 2", len(advs))
	}
	page := color.NRGBA{R: 0xe8, G: 0xdc, B: 0xc0, A: 0xff}
	ink := color.NRGBA{R: 0x30, G: 0x28, B: 0x20, A: 0xff}
	caret := image.Rect(10, 10, 10+m.cell.X, 10+m.cell.Y)
	ops := new(op.Ops)
	// a white window: what the preedit's box does not cover stays white, never mistaken for ink
	paint.Fill(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	b.drawPreedit(ops, &frame{caret: caret, base: m.baseline, fg: ink, bg: page, tinted: true}, m, fs)
	if err := hw.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rectangle{Max: m.window})
	if err := hw.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	isInk := func(x, y int) bool { c := img.RGBAAt(x, y); return int(c.R)+int(c.G)+int(c.B) < 3*120 }
	inkIn := func(x0, x1 int) bool {
		for x := x0; x < x1; x++ {
			for y := caret.Min.Y; y < caret.Max.Y-2; y++ {
				if isInk(x, y) {
					return true
				}
			}
		}
		return false
	}
	first, second := caret.Min.X, caret.Min.X+advs[0]
	if !inkIn(first+1, second-1) {
		t.Error("no ink where 하 is held: it is hidden")
	}
	if !inkIn(second+1, second+advs[1]-1) {
		t.Error("no ink where ㅇ is composed")
	}
	bottom := caret.Max.Y - 1
	if isInk(first+advs[0]/2, bottom) {
		t.Error("the held 하 is underlined; only the composition is")
	}
	if !isInk(second+advs[1]/2, bottom) {
		t.Error("the composition ㅇ is not underlined")
	}
}

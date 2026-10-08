package gui

import (
	"image"
	"slices"
	"strings"
	"sync"
	"testing"

	"gioui.org/io/key"
	"gioui.org/op"
	"gioui.org/unit"
)

// SetZoom scales the size the cells are measured at, and every change is a new generation: the
// next frame's metrics differ from the last's, so the App is told to lay out and paint again.
func TestSetZoomScalesTheCellsAndIsANewGeneration(t *testing.T) {
	b := NewBackend()
	before := b.font.Load()
	b.SetZoom(150)
	after := b.font.Load()
	if after.size != before.size*3/2 || after.gen != before.gen+1 {
		t.Fatalf("after SetZoom(150): size %v gen %d, from size %v gen %d", after.size, after.gen, before.size, before.gen)
	}
	at := func(fs *fontState) metrics {
		m := measure(fs.fm, fs.size, 0, image.Pt(1000, 700), unit.Metric{PxPerDp: 1, PxPerSp: 1})
		m.fontGen = fs.gen
		return m
	}
	m0, m1 := at(before), at(after)
	if d := m1.cell.Y*2 - m0.cell.Y*3; d < -2 || d > 2 {
		t.Errorf("a cell %v at 150%% of %v", m1.cell, m0.cell)
	}
	if m0.sameCells(m1) {
		t.Error("the zoomed metrics are the same cells: no ResizeEvent would be pushed")
	}
	b.SetZoom(1000)
	if got := b.font.Load().size; got != b.fontSize*3 {
		t.Errorf("SetZoom(1000) drew at %v, want 300%% of %v", got, b.fontSize)
	}
}

// A change of family alone, at the same size, is still a change of cells: the frame that first
// sees it pushes a ResizeEvent, and the renderer draws every row again with no glyph cached in the
// old face.
func TestAFamilyOnlyChangeRepaintsEveryRow(t *testing.T) {
	b := NewBackend()
	fs := b.font.Load()
	m := measure(fs.fm, fs.size, 0, image.Pt(200, 100), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	m.fontGen = fs.gen
	var g grid
	g.resize(m.grid.W, m.grid.H)
	g.apply(nil)
	b.render.frame(&g, m, cursorState{}, nil, nil, false)
	b.render.glyphs[glyphKey{content: "x"}] = glyphShape{advance: 7}

	b.SetProseFont("Go") // the size is unchanged: only the generation moves
	next := b.font.Load()
	m2 := measure(next.fm, next.size, 0, image.Pt(200, 100), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	m2.fontGen = next.gen
	if m2.cell != m.cell || m2.ppem != m.ppem {
		t.Fatalf("a prose change moved the cells: %v at %v, from %v at %v", m2.cell, m2.ppem, m.cell, m.ppem)
	}
	if m.sameCells(m2) {
		t.Fatal("a family-only change is the same cells: no ResizeEvent, no repaint")
	}
	b.render.frame(&g, m2, cursorState{}, nil, nil, false)
	if len(b.render.glyphs) != 0 && b.render.glyphs[glyphKey{content: "x"}].advance == 7 {
		t.Error("a glyph of the old generation survived")
	}
	if b.render.fonts.Prose != "Go" || b.render.fonts.Gen != next.gen {
		t.Errorf("the canvases' fonts are %+v, want prose Go at generation %d", b.render.fonts, next.gen)
	}
}

// SetFont changes the face everywhere it is read: the cells' renderer, the native views' cell
// family (CellFamily), and the fontState the Gio goroutine shapes the IME's preedit in.
func TestSetFontChangesTheFaceOfCellsViewsAndPreedit(t *testing.T) {
	b := NewBackend()
	b.SetFont("Go Mono", 18)
	fs := b.font.Load()
	if !strings.HasPrefix(fs.typeface, "Go Mono") || fs.size != 18 {
		t.Fatalf("fontState %q at %v", fs.typeface, fs.size)
	}
	if string(b.render.typeface) != fs.typeface || b.render.fonts.Mono != fs.typeface {
		t.Errorf("renderer %q, views' mono %q; want both %q", b.render.typeface, b.render.fonts.Mono, fs.typeface)
	}
	b.SetFont("", 0) // keeps both
	if again := b.font.Load(); again.typeface != fs.typeface || again.size != 18 || again.gen != fs.gen+1 {
		t.Errorf("SetFont(\"\", 0): %q at %v gen %d", again.typeface, again.size, again.gen)
	}
}

// The Gio goroutine reads one fontState a frame, for measure and the preedit, while the tui loop
// changes font and zoom: under -race, the two never share a field.
func TestFontChangesRaceNeitherFramesNorPreedit(t *testing.T) {
	b := NewBackend()
	b.gio.ime.edit(key.EditEvent{Text: "한"})
	b.gio.ime.compose(key.CompositionEvent{Start: 0, End: 1})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the Gio goroutine's frame
		defer wg.Done()
		for range 200 {
			fs := b.font.Load()
			m := measure(fs.fm, fs.size, 0, image.Pt(400, 200), unit.Metric{PxPerDp: 1, PxPerSp: 1})
			m.fontGen = fs.gen
			var ops op.Ops
			b.drawPreedit(&ops, &frame{caret: image.Rect(0, 0, m.cell.X, m.cell.Y), base: m.baseline}, m, fs)
		}
	}()
	for i := range 200 { // the tui loop
		switch i % 3 {
		case 0:
			b.SetZoom(100 + i%5*25)
		case 1:
			b.SetFont("Go Mono", float32(12+i%6))
		default:
			b.SetProseFont("Go")
		}
	}
	wg.Wait()
}

// An empty Family is the window's prose family, and CellFamily its cell family, as the canvas's
// shaper has them now.
func TestFontsResolveTheEmptyAndTheCellFamily(t *testing.T) {
	fonts := Fonts{Prose: "Noto Serif", Mono: "Go Mono, monospace"}
	if got := string(gioFont(Font{}, fonts).Typeface); got != "Noto Serif" {
		t.Errorf("Family \"\" is %q", got)
	}
	if got := string(gioFont(Font{Family: CellFamily}, fonts).Typeface); got != "Go Mono, monospace" {
		t.Errorf("CellFamily is %q", got)
	}
	if got := string(gioFont(Font{Family: "Go"}, fonts).Typeface); got != "Go" {
		t.Errorf("a named family became %q", got)
	}
	if got := string(gioFont(Font{}, Fonts{}).Typeface); got != uiFamily {
		t.Errorf("no fonts: Family \"\" is %q, want %q", got, uiFamily)
	}
}

// fc-list's lines become a sorted list, each family once, by its first name.
func TestFamilyListParsesFcList(t *testing.T) {
	got := familyList("JetBrainsMono Nerd Font,JetBrainsMono NF\nGo Mono\n\nJetBrainsMono Nerd Font,JetBrainsMono NF\nAdwaita Mono\n")
	if want := []string{"Adwaita Mono", "Go Mono", "JetBrainsMono Nerd Font"}; !slices.Equal(got, want) {
		t.Errorf("familyList = %q, want %q", got, want)
	}
	if familyList("") != nil {
		t.Error("no fontconfig: a list")
	}
}

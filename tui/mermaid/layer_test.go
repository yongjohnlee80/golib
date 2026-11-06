package mermaid

import (
	"context"
	"reflect"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// The kinds drawn are the registry's, in Kind order; a kind registered twice panics; Lay answers
// ErrUnsupported for a kind not registered, and for a model of another type than its kind's.
func TestTheKindsAreTheRegistrys(t *testing.T) {
	if got, want := Kinds(), []pm.Kind{pm.Flowchart, pm.Sequence, pm.State, pm.Class, pm.ER, pm.Journey, pm.Mindmap, pm.Timeline, pm.Requirement, pm.Gantt, pm.Pie, pm.Quadrant, pm.XYChart}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("registering a kind twice did not panic")
			}
		}()
		register(pm.Mindmap, nil, nil)
	}()
	if !Supported(pm.Mindmap) || Supported(undrawnDiagram{}.Kind()) {
		t.Error("Supported")
	}
	if _, err := Lay(context.Background(), undrawnDiagram{}, newCells(1, 1)); err != ErrUnsupported {
		t.Errorf("a kind not registered: %v", err)
	}
	if _, err := layAs(LayMindmap)(context.Background(), &pm.TimelineDiagram{}, newCells(1, 1)); err != ErrUnsupported {
		t.Errorf("a timeline handed to the mind map's layout: %v", err)
	}
}

// A label is capped by its role: at the cap it is whole, past it cut at a cluster's edge with …
// last, the whole width the cap; a wide cluster that would cross the cap drops whole; a message's
// cap is its own.
func TestALabelIsCappedByItsRole(t *testing.T) {
	m := newCells(1, 1)
	at := strings.Repeat("a", caps[RoleLabel])
	if s, w, capped := capLabel(at, RoleLabel, m); s != at || w != caps[RoleLabel] || capped {
		t.Errorf("a label at the cap: %q %d %v", s, w, capped)
	}
	over := strings.Repeat("b", caps[RoleLabel]+1)
	if s, w, capped := capLabel(over, RoleLabel, m); s != strings.Repeat("b", caps[RoleLabel]-1)+"…" || w != caps[RoleLabel] || !capped {
		t.Errorf("a label one past the cap: %q %d %v", s, w, capped)
	}
	cjk := strings.Repeat("字", 30) // 60 cells
	s, w, capped := capLabel(cjk, RoleLabel, m)
	if !capped || s != strings.Repeat("字", 23)+"…" || w != 47 {
		t.Errorf("a CJK label: %q (%d cells), want 23 clusters and … (47: a 24th would cross the cap)", s, w)
	}
	msg := strings.Repeat("c", 52)
	if _, _, capped := capLabel(msg, RoleMessage, m); capped {
		t.Error("a 52-cell message is capped; a message's cap is 56")
	}
	if _, _, capped := capLabel(msg, RoleLabel, m); !capped {
		t.Error("a 52-cell label is not capped at 48")
	}
}

// A capped label keeps its whole text where it is drawn: HitAt answers it over the label's cells,
// box or text, and nothing over an uncapped one, so only a cut label has a card.
func TestACappedLabelRevealsItsWholeText(t *testing.T) {
	long := "a timeline event whose text runs well past the cap of forty-eight cells"
	d, err := pm.Parse("timeline\n  title " + long + "\n  2020 : short\n  2021 : " + long + "\n")
	if err != nil {
		t.Fatal(err)
	}
	g, err := Lay(context.Background(), d, newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	var capped, short *Box
	for i := range g.Boxes {
		switch g.Boxes[i].Label {
		case long:
			capped = &g.Boxes[i]
		case "short":
			short = &g.Boxes[i]
		}
	}
	if capped == nil || short == nil {
		t.Fatalf("boxes %+v", g.Boxes)
	}
	if full, _, ok := g.HitAt(capped.Rect.X+3, capped.Rect.Y+1); !ok || full != long {
		t.Errorf("over the capped box: %q %v", full, ok)
	}
	if _, _, ok := g.HitAt(short.Rect.X+3, short.Rect.Y+1); ok {
		t.Error("an uncapped box has a region")
	}
	if full, r, ok := g.HitAt(2, 0); !ok || full != long || r.W != caps[RoleLabel] {
		t.Errorf("over the capped title: %q %+v %v", full, r, ok)
	}
}

// The strokes through a cell resolve to the one stroke when they agree, else light: a dashed line
// never reads as cut where it crosses a light one.
func TestStrokesResolveToOneOrLight(t *testing.T) {
	g := newGrid()
	g.run(cell{0, 0}, cell{4, 0})
	g.run(cell{2, -1}, cell{2, 1})
	for x := range 5 {
		g.addStroke(cell{x, 0}, StrokeDashed)
	}
	g.addStroke(cell{2, 0}, StrokeLight)
	g.resolveStrokes()
	if g.stroke[cell{1, 0}] != StrokeDashed || g.stroke[cell{2, 0}] != StrokeLight {
		t.Errorf("strokes %v %v", g.stroke[cell{1, 0}], g.stroke[cell{2, 0}])
	}
	if g.lineOf(cell{1, 0}, east|west) != "┄" || g.lineOf(cell{2, 0}, g.lines[cell{2, 0}]) != "┼" {
		t.Errorf("drawn %q %q", g.lineOf(cell{1, 0}, east|west), g.lineOf(cell{2, 0}, g.lines[cell{2, 0}]))
	}
}

// The legend sits under the drawing, a blank row first, its rows in the order given, swatches in
// one column, labels in the next, columns right-aligned; footnotes are one sequence per grid,
// ¹…⁹ then *10, each a legend row under its mark.
func TestTheLegendAndFootnotes(t *testing.T) {
	m := newCells(1, 1)
	g := newGrid()
	g.run(cell{0, 0}, cell{3, 0})
	g.legend(legendRow{swatch: "█", label: "apples", cols: []string{"42%", "7"}},
		legendRow{swatch: "░", label: "pears", cols: []string{"8.5%", "12"}})
	var marks []string
	for range 10 {
		marks = append(marks, g.footnote("note"))
	}
	if marks[0] != "¹" || marks[8] != "⁹" || marks[9] != "*10" {
		t.Errorf("marks %q", marks)
	}
	g, err := g.finish(m)
	if err != nil {
		t.Fatal(err)
	}
	s := newCells(g.Size.W, g.Size.H)
	g.Paint(s, 0, 0, -1, Theme{})
	rows := strings.Split(s.String(), "\n")
	if rows[1] != "" || !strings.HasPrefix(rows[2], "█   apples   42%   7") || !strings.HasPrefix(rows[3], "░   pears   8.5%  12") {
		t.Errorf("the legend:\n%s", s.String())
	}
	if !strings.HasPrefix(rows[4], "¹   note") || !strings.HasPrefix(rows[13], "*10 note") {
		t.Errorf("the footnotes:\n%s", s.String())
	}
}

// Capped labels draw as goldens in two kinds: a timeline (an event past the cap, one exactly at it,
// a CJK one) and a sequence (a message past the message cap, a note past the label cap).
func TestCappedLabelsDrawInCells(t *testing.T) {
	at := strings.Repeat("x", caps[RoleLabel])
	cases := map[string]string{
		"capped-timeline": "timeline\n  2020 : an event whose text is long enough to be cut at the cap, with its tail\n  2021 : " + at + "\n  2022 : " + strings.Repeat("字", 30) + "\n",
		"capped-sequence": "sequenceDiagram\n  participant A\n  participant B\n  A->>B: a message long enough to pass the message cap of fifty-six cells, cut\n  Note right of B: a note long enough to pass the label cap and be cut\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, got := paint(t, src)
			golden(t, name, got)
		})
	}
}

// ambiguousWide measures as an ambiguous-wide terminal does: … (East Asian Ambiguous) is two cells.
type ambiguousWide struct{}

func (ambiguousWide) StringWidth(s string) int {
	return newCells(1, 1).StringWidth(s) + strings.Count(s, "…")
}

// The … takes its own width from the cap: under an ambiguous-wide policy it is two cells, so one
// cell less of the label shows, and the width reported is the width drawn.
func TestTheEllipsisTakesItsOwnWidth(t *testing.T) {
	long := strings.Repeat("a", 60)
	s, w, capped := capLabel(long, RoleLabel, ambiguousWide{})
	if !capped || s != strings.Repeat("a", caps[RoleLabel]-2)+"…" || w != caps[RoleLabel] || (ambiguousWide{}).StringWidth(s) != w {
		t.Errorf("ambiguous-wide: %q, %d cells reported, %d measured; want %d a's and …, %d cells", s, w, (ambiguousWide{}).StringWidth(s), caps[RoleLabel]-2, caps[RoleLabel])
	}
}

// A nil model is not drawn, untyped or typed: ErrUnsupported, as for a kind not registered, never a
// panic in its layout.
func TestANilDiagramIsUnsupported(t *testing.T) {
	for name, d := range map[string]pm.Diagram{"nil": nil, "typed nil": (*pm.MindmapDiagram)(nil)} {
		if err := Drawn(d); err != ErrUnsupported {
			t.Errorf("Drawn(%s) = %v", name, err)
		}
		if _, err := Lay(context.Background(), d, newCells(1, 1)); err != ErrUnsupported {
			t.Errorf("Lay(%s) = %v", name, err)
		}
	}
}

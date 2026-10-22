package gui

import (
	"slices"
	"testing"
)

func TestMeasureClustersMatchLayout(t *testing.T) {
	sh := NewRecordingCanvas(Size{W: 100, H: 20}, Size{W: 8, H: 16}).Text()
	f := Font{Size: 16}
	for _, s := range []string{"hello world", "áb", "한국어", "👍🏽ok", "x"} {
		m := sh.Measure(s, f)
		if len(m.X) != len(m.Clusters)+1 {
			t.Fatalf("%q: %d edges for %d clusters", s, len(m.X), len(m.Clusters))
		}
		for i := 1; i < len(m.X); i++ {
			if m.X[i] < m.X[i-1] {
				t.Fatalf("%q: x goes back at %d: %v", s, i, m.X)
			}
		}
		if w := sh.Layout(s, f, 0).Width; abs32(m.X[len(m.X)-1]-w) > 0.5 {
			t.Errorf("%q: measured width %v, Layout's %v", s, m.X[len(m.X)-1], w)
		}
		if m.Ascent <= 0 {
			t.Errorf("%q: ascent %v", s, m.Ascent)
		}
	}
	// a combining mark is one cluster with its base: two clusters, not three
	if m := sh.Measure("áb", f); len(m.Clusters) != 2 || m.Clusters[1] != 3 {
		t.Errorf("a+combining+b: clusters %v", m.Clusters)
	}
}

func TestMeasureEmptyHasMetrics(t *testing.T) {
	sh := NewRecordingCanvas(Size{W: 100, H: 20}, Size{W: 8, H: 16}).Text()
	m := sh.Measure("", Font{Size: 16})
	if len(m.Clusters) != 0 || len(m.X) != 1 || m.X[0] != 0 || m.Ascent <= 0 {
		t.Fatalf("empty: %+v", m)
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Runs shaped together are one line, each run at its X: the layout reaches the last run's end, its
// first glyph is at the first run's X, the runs share the tallest ascent, an empty run takes no
// room and a line break is a space.
func TestLayoutRunsPlacesEachRunOnOneLine(t *testing.T) {
	sh := NewRecordingCanvas(Size{W: 100, H: 20}, Size{W: 8, H: 16}).Text()
	f := Font{Size: 16}
	ab, cd := sh.Layout("ab", f, 0), sh.Layout("cd", f, 0)
	l := sh.LayoutRuns([]TextRun{{Text: "ab", X: 20}, {Text: ""}, {Text: "cd", X: 100}}, f)
	if len(l.lines) != 1 {
		t.Fatalf("%d lines, want the runs on one", len(l.lines))
	}
	if l.lines[0].x != 20 {
		t.Errorf("the first glyph at %v, want the first run's 20", l.lines[0].x)
	}
	if want := 100 + cd.Width; abs32(l.Width-want) > 0.5 {
		t.Errorf("width %v, want the last run's end %v", l.Width, want)
	}
	if l.Ascent != ab.Ascent || abs32(l.Height-(l.Ascent+l.Descent)) > 0.01 {
		t.Errorf("ascent %v height %v, want the runs' ascent %v and one line's height", l.Ascent, l.Height, ab.Ascent)
	}
	if got, want := sh.LayoutRuns([]TextRun{{Text: "a\nb"}}, f).Width, sh.Layout("a b", f, 0).Width; abs32(got-want) > 0.5 {
		t.Errorf("a line break laid %v wide, want a space's %v", got, want)
	}
	if e := sh.LayoutRuns([]TextRun{{Text: ""}}, f); e.Width != 0 || len(e.lines) != 0 {
		t.Errorf("empty runs laid %+v", e)
	}
}

// A shaper wraps as a leased Measurer does: one interface, one rule. The shaper is made as the
// measurer makes its own (NewTextShaper), so the two shape in the same fonts and only the wrap is
// compared.
func TestTheShaperWrapsAsTheMeasurerDoes(t *testing.T) {
	sh := NewTextShaper(1)
	m, release := AcquireMeasurer(sh.Scale())
	defer release()
	f := Font{Size: 14}
	for _, s := range []string{"a few words to wrap across lines", "東京都の天気", "long-hyphenated-word here", ""} {
		gl, gs := sh.Wrap(s, f, 60)
		ml, ms := m.Wrap(s, f, 60)
		if !slices.Equal(gl, ml) || gs != ms {
			t.Errorf("%q: the shaper wraps %q %v, the measurer %q %v", s, gl, gs, ml, ms)
		}
	}
}

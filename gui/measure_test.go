package gui

import (
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

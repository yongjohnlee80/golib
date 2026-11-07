package widget

import (
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// metaMask hides span text over metadata clusters (tui/widget's MarkdownMetadata.HiddenClusters),
// splitting a span where the mask changes. It never strips the buffer: every piece keeps its
// source line and cluster, so the caret still maps onto it.
func metaMask(m *tuiwidget.MarkdownMetadata, spans []flow.Span, ln int) []flow.Span {
	mask, ok := m.HiddenClusters(ln)
	if !ok {
		return spans
	}
	var out []flow.Span
	for _, sp := range spans {
		if sp.Text == "" || sp.Atom != nil {
			out = append(out, sp)
			continue
		}
		start, offset, col := 0, 0, sp.Col
		hidden := sp.Hidden || (col < len(mask) && col >= 0 && mask[col])
		for c := range tui.Graphemes(sp.Text) {
			next := sp.Hidden || (col < len(mask) && col >= 0 && mask[col])
			if next != hidden {
				s := sp
				s.Text = sp.Text[start:offset]
				s.Col = sp.Col + clusters(sp.Text[:start])
				s.Hidden = hidden
				if start > 0 {
					s.Room = 0
				}
				out = append(out, s)
				start, hidden = offset, next
			}
			offset += len(c)
			col++
		}
		s := sp
		s.Text = sp.Text[start:]
		s.Col = sp.Col + clusters(sp.Text[:start])
		s.Hidden = hidden
		if start > 0 {
			s.Room = 0
		}
		out = append(out, s)
	}
	return out
}

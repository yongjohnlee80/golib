package mergeview

import (
	"github.com/yongjohnlee80/golib/diff"
	"github.com/yongjohnlee80/golib/diff/merge"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// spansOf are each hunk's lines in a pane, by the hunk's index.
func (v *MergeView) spansOf(pane int) []diff.Span {
	if pane == resultPane {
		return v.model.ResultSpans()
	}
	hunks := v.model.Hunks()
	out := make([]diff.Span, len(hunks))
	for i, h := range hunks {
		if pane == leftPane {
			out[i] = h.Left
		} else {
			out[i] = h.Right
		}
	}
	return out
}

// hunkAt is the hunk a pane's line is in, and the line's place in it: the last hunk for a line
// past the text's end, -1 when there are none.
func hunkAt(spans []diff.Span, line int) (h, k int) {
	for i, s := range spans {
		if line >= s.From && line < s.To {
			return i, line - s.From
		}
	}
	for i := len(spans) - 1; i >= 0; i-- { // past the end, or on an empty span's line
		if spans[i].From <= line {
			return i, max(line-spans[i].From, 0)
		}
	}
	return -1, 0
}

// computeLooks finds each pane's line looks. A side's lines in a conflict are a conflict's; in a
// change the side made, added where the base had none, changed otherwise. The result's lines in an
// unresolved change are a conflict's or a change's; in a resolved one, added (taken in).
func (v *MergeView) computeLooks() {
	if v.model == nil {
		v.looks = [3][]look{}
		return
	}
	hunks := v.model.Hunks()
	changes := v.model.Changes()
	state := make([]merge.Resolution, len(hunks))
	for i := range hunks {
		if c := v.model.Change(i); c >= 0 {
			state[i] = changes[c].State
		}
	}
	for pane := range v.panes {
		spans := v.spansOf(pane)
		n := 0
		if len(spans) > 0 {
			n = spans[len(spans)-1].To
		}
		looks := make([]look, n)
		for i, h := range hunks {
			l := plain
			switch {
			case h.Kind == diff.Same:
			case pane == resultPane && state[i] == merge.Unresolved && h.Kind == diff.Conflict:
				l = conflict
			case pane == resultPane && state[i] == merge.Unresolved:
				l = changed
			case pane == resultPane && state[i] != merge.Dismissed:
				l = added
			case pane == resultPane:
			case h.Kind == diff.Conflict:
				l = conflict
			case (pane == leftPane && (h.Kind == diff.LeftOnly || h.Kind == diff.Both)) ||
				(pane == rightPane && (h.Kind == diff.RightOnly || h.Kind == diff.Both)):
				l = changed
				if h.Base.Len() == 0 {
					l = added
				}
			}
			for k := spans[i].From; k < spans[i].To && k < n; k++ {
				looks[k] = l
			}
		}
		v.looks[pane] = looks
	}
}

// lineStyle is a pane's line looks, as its editor asks for them.
func (v *MergeView) lineStyle(pane int) func(line int) (style.Style, bool) {
	return func(line int) (style.Style, bool) {
		looks := v.looks[pane]
		if line < 0 || line >= len(looks) {
			return style.Style{}, false
		}
		return v.style.of(looks[line])
	}
}

// scrolled keeps the other panes with the one that scrolled: each goes to the same place in the
// same hunk, its first line where the hunk is shorter. A pane that tells later (a window's) may
// tell a top it has since left, or the top the view gave it: neither is the user's scroll, and
// following it would move the panes back. A pane that tells at once (tui's) tells the top it was
// given while the view is syncing, so its gave stays set until its next tell, which clears it: a
// tell equal to it would be no move, so none is dropped wrongly.
func (v *MergeView) scrolled(pane, top int) {
	if v.syncing || v.model == nil || top != v.panes[pane].TopLine() {
		return
	}
	if gave := v.gave[pane]; gave >= 0 {
		v.gave[pane] = -1
		if top == gave {
			return
		}
	}
	h, k := hunkAt(v.spansOf(pane), top)
	if h < 0 {
		return
	}
	v.syncing = true
	defer func() { v.syncing = false }()
	for other := range v.panes {
		if other == pane {
			continue
		}
		s := v.spansOf(other)[h]
		p := v.panes[other]
		p.SetTopLine(s.From + min(k, max(s.Len()-1, 0)))
		v.gave[other] = p.TopLine() // as clamped
	}
	v.MarkDirty() // the connectors follow
}

// glyph is one cell of a connector: what it draws, and what a click on it does.
type glyph struct {
	x, y   int
	text   string
	st     style.Style
	change int
	act    action
}

type action uint8

const (
	noAction action = iota
	takeLeft
	takeRight
	dismiss
)

// glyphs are the connectors' cells: each change's marks beside its lines in a side and in the
// result, and, while it is unresolved, its arrows and × on the side's first line.
func (v *MergeView) glyphs() []glyph {
	if v.model == nil || v.panesH <= 0 {
		return nil
	}
	var out []glyph
	spans := [3][]diff.Span{v.spansOf(leftPane), v.spansOf(resultPane), v.spansOf(rightPane)}
	tops := [3]int{v.panes[leftPane].TopLine(), v.panes[resultPane].TopLine(), v.panes[rightPane].TopLine()}
	row := func(pane, line int) (int, bool) {
		y := line - tops[pane]
		return v.panesY + y, y >= 0 && y < v.panesH
	}
	// gone is a deletion's mark: the Deleted tint's colour, where a side has no lines of the change.
	gone := style.New()
	if bg, ok := v.style.Deleted.GetBackground(); ok {
		gone = gone.Foreground(bg)
	}
	mark := func(x, pane int, s diff.Span, st style.Style, i int) {
		if s.Len() == 0 { // nothing on this side: a mark where its lines would be
			if y, ok := row(pane, s.From); ok {
				out = append(out, glyph{x: x, y: y, text: "▁", st: gone.Inherit(st), change: i})
			}
			return
		}
		for line := s.From; line < s.To; line++ {
			if y, ok := row(pane, line); ok {
				out = append(out, glyph{x: x, y: y, text: "▌", st: st, change: i})
			}
		}
	}
	lx := v.paneX[leftPane] + v.paneW[leftPane] // the left connector's first column
	rx := v.paneX[resultPane] + v.paneW[resultPane]
	for i, c := range v.model.Changes() {
		hi := v.hunkOf(i)
		l := changed
		if c.Conflict() {
			l = conflict
		}
		st := marker(l)
		if c.State != merge.Unresolved {
			st = style.New().Foreground(style.TokenTextMuted)
		}
		if i == v.cur {
			st = st.Bold(true)
		}
		left := c.Hunk.Kind != diff.RightOnly
		right := c.Hunk.Kind != diff.LeftOnly
		if left {
			mark(lx, leftPane, spans[leftPane][hi], st, i)
			mark(lx+3, resultPane, spans[resultPane][hi], st, i)
		}
		if right {
			mark(rx, resultPane, spans[resultPane][hi], st, i)
			mark(rx+3, rightPane, spans[rightPane][hi], st, i)
		}
		if c.State != merge.Unresolved {
			continue
		}
		if y, ok := row(leftPane, spans[leftPane][hi].From); ok && left {
			out = append(out, glyph{x: lx + 1, y: y, text: "»", st: st, change: i, act: takeLeft},
				glyph{x: lx + 2, y: y, text: "×", st: st, change: i, act: dismiss})
		}
		if y, ok := row(rightPane, spans[rightPane][hi].From); ok && right {
			out = append(out, glyph{x: rx + 1, y: y, text: "×", st: st, change: i, act: dismiss},
				glyph{x: rx + 2, y: y, text: "«", st: st, change: i, act: takeRight})
		}
	}
	return out
}

// hunkOf is change i's hunk index, or -1.
func (v *MergeView) hunkOf(i int) int {
	if i < 0 || v.model == nil {
		return -1
	}
	for h := range v.model.Hunks() {
		if v.model.Change(h) == i {
			return h
		}
	}
	return -1
}

// click runs the arrow or × under (x, y), if there is one.
func (v *MergeView) click(x, y int) bool {
	for _, g := range v.glyphs() {
		if g.x != x || g.y != y || g.act == noAction {
			continue
		}
		switch g.act {
		case takeLeft:
			v.Take(g.change, merge.Left)
		case takeRight:
			v.Take(g.change, merge.Right)
		case dismiss:
			v.Dismiss(g.change)
		}
		return true
	}
	return false
}

// focusedPane is the pane holding the focus, or the result pane.
func (v *MergeView) focusedPane() int {
	if ctx := v.Context(); ctx != nil {
		for i, p := range v.panes {
			if ctx.FocusWithin(p) {
				return i
			}
		}
	}
	return resultPane
}

// current is the change at the focused pane's cursor: the one last moved to when the cursor is at
// it (two changes may start on one line), else the first there, else the one last moved to.
func (v *MergeView) current() int {
	if v.model == nil {
		return -1
	}
	pane := v.focusedPane()
	line := v.panes[pane].Core().Cursor().Line
	spans := v.spansOf(pane)
	at := func(s diff.Span) bool { return (line >= s.From && line < s.To) || (s.Len() == 0 && line == s.From) }
	if h := v.hunkOf(v.cur); h >= 0 && at(spans[h]) {
		return v.cur
	}
	for h, s := range spans {
		if (line >= s.From && line < s.To) || (s.Len() == 0 && line == s.From) {
			if c := v.model.Change(h); c >= 0 {
				return c
			}
		}
	}
	return v.cur
}

// move goes to the next (delta 1) or the previous (-1) change after or before the result pane's
// cursor, or the next conflict: the result's cursor on its first line, every pane brought to it.
func (v *MergeView) move(delta int, conflictsOnly bool) {
	changes := v.model.Changes()
	if len(changes) == 0 {
		return
	}
	line := v.panes[resultPane].Core().Cursor().Line
	// on is whether the cursor is at the change last moved to: then a change starting on the same
	// line is the next (or the one before), not passed over.
	on := v.cur >= 0 && v.cur < len(changes) && changes[v.cur].Result.From == line
	pick := -1
	if delta > 0 {
		for i, c := range changes {
			if (c.Result.From > line || (on && c.Result.From == line && i > v.cur)) && (!conflictsOnly || c.Conflict()) {
				pick = i
				break
			}
		}
	} else {
		for i := len(changes) - 1; i >= 0; i-- {
			c := changes[i]
			if (c.Result.From < line || (on && c.Result.From == line && i < v.cur)) && (!conflictsOnly || c.Conflict()) {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		return
	}
	v.cur = pick
	h := v.hunkOf(pick)
	for pane, p := range v.panes {
		s := v.spansOf(pane)[h]
		p.Core().SetCursor(editor.TextPos{Line: s.From})
	}
	v.MarkDirty()
}

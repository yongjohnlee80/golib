package gui

import "gioui.org/io/key"

// imeState mirrors the small text buffer Gio's input method edits on the backend's behalf.
//
// A terminal has no text field, so the backend gives the input method an empty one. Plain typing
// lands in it as an edit and is committed at once. While a composition is active (Korean jamo
// being assembled, say), its text is the preedit, drawn at the caret and not yet sent. When no
// composition remains, everything in the buffer is committed as typed text and the buffer, and
// Gio's copy of it, are emptied again. Owned by the Gio goroutine.
type imeState struct {
	text []rune
	comp key.Range // the composing range; empty when nothing is being composed
	// commits is each edit's committed text, in arrival order, folded when a
	// later edit rewrites an earlier one's bytes (an input method revising what
	// it inserted). take() delivers them in this order.
	commits []string
}

// edit applies an edit: Range (in runes) is replaced with Text. Gio and the input method agree on
// the buffer, so a range outside it is clamped rather than trusted.
func (s *imeState) edit(e key.EditEvent) {
	start, end := s.clamp(e.Range)
	repl := []rune(e.Text)
	next := make([]rune, 0, len(s.text)-(end-start)+len(repl))
	next = append(next, s.text[:start]...)
	next = append(next, repl...)
	s.text = append(next, s.text[end:]...)
	// An empty replacement deletes bytes only: what it deleted is gone from the
	// commits too. A replacement revises the committed pieces it touched.
	s.commits = s.revise(start, end, string(repl))
}

// revise records an edit's effect on the commits: a piece the edit's range
// touched is folded — its bytes outside the range kept, the replacement in
// their place — at the earliest touched piece's position; an edit that touched
// no piece (typing at the end, an insertion in the middle of one that left all
// its bytes in place) appends. Edits are delivered in the order they arrived.
func (s *imeState) revise(start, end int, repl string) []string {
	if len(s.commits) == 0 {
		if repl == "" {
			return nil
		}
		return []string{repl}
	}
	// where each piece's runes sit in the buffer, before the edit
	var at int
	var out []string
	folded := false
	for _, c := range s.commits {
		r := []rune(c)
		pEnd := at + len(r)
		if pEnd <= start || at >= end || len(c) == 0 {
			out = append(out, c)
			at = pEnd
			continue
		}
		if !folded {
			folded = true
			head := r[:max(start-at, 0)]
			var tail []rune
			if pEnd > end {
				tail = r[end-at:]
			}
			merged := make([]rune, 0, len(head)+len([]rune(repl))+len(tail))
			merged = append(merged, head...)
			merged = append(merged, []rune(repl)...)
			merged = append(merged, tail...)
			out = append(out, string(merged))
		}
		at = pEnd
	}
	if !folded && repl != "" {
		out = append(out, repl) // the edit touched no piece: a new commit
	}
	return out
}

// compose records the composing range.
func (s *imeState) compose(e key.CompositionEvent) { s.comp = key.Range(e) }

func (s *imeState) composing() bool {
	start, end := s.clamp(s.comp)
	return s.comp.Start >= 0 && end > start
}

// preedit is the text being composed, or "".
func (s *imeState) preedit() string {
	if !s.composing() {
		return ""
	}
	start, end := s.clamp(s.comp)
	return string(s.text[start:end])
}

// take returns the text ready to commit and empties the buffer: everything, once no composition
// is active; nothing while one is (the committed part waits with it, keeping its order). Each
// committed piece is delivered on its own, in arrival order.
func (s *imeState) take() ([]string, bool) {
	if s.composing() || len(s.text) == 0 {
		return nil, false
	}
	out := s.commits
	if len(out) == 0 {
		out = []string{string(s.text)} // commits that predate the order bookkeeping
	} else {
		out = append([]string(nil), s.commits...)
	}
	s.text = s.text[:0]
	s.comp = key.Range{}
	s.commits = s.commits[:0]
	return out, true
}

// empty reports whether the buffer holds nothing, so Gio's copy should be reset to match.
func (s *imeState) empty() bool { return len(s.text) == 0 }

func (s *imeState) clamp(r key.Range) (start, end int) {
	start, end = r.Start, r.End
	if start > end {
		start, end = end, start
	}
	return min(max(start, 0), len(s.text)), min(max(end, 0), len(s.text))
}

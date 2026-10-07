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
// is active; nothing while one is (the committed part waits with it, keeping its order).
func (s *imeState) take() (string, bool) {
	if s.composing() || len(s.text) == 0 {
		return "", false
	}
	out := string(s.text)
	s.text = s.text[:0]
	s.comp = key.Range{}
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

package gui

import (
	"sort"

	"gioui.org/io/key"
)

// Input Method Editor (IME) text buffering and composition pipeline.
//
// Modern operating systems utilize Input Method Editors to allow users to enter complex
// character scripts (such as Korean Hangul, Japanese Kanji/Kana, and Chinese Hanzi) using
// standard Latin keyboards.
//
// # How IME Composition Works: The Virtual Text Buffer
//
// Unlike native GUI input fields with dedicated multi-line text models, terminal applications
// traditionally receive finalized character bytes after the OS completes composition.
// To bridge Gio's OS-level IME integration with the TUI runtime without modifying widgets,
// the GUI backend exposes a lightweight virtual text buffer:
//
//	Keystrokes: [ 'g', 'k', 's' ] (Typing "한")
//	     │
//	     ▼
//	┌─────────────────────────────────────────────────────────────┐
//	│                 OS Input Method Engine (IME)                │
//	│  - Assembles phonetic jamo into syllable block "한"         │
//	│  - Emits key.EditEvent and key.CompositionEvent             │
//	└──────────────────────────────┬──────────────────────────────┘
//	                               │
//	                               ▼
//	┌─────────────────────────────────────────────────────────────┐
//	│                  imeState (Virtual Buffer)                  │
//	│  - text:   [ '한' ]                                          │
//	│  - comp:   Range[0, 1] (Active composition/preedit)         │
//	│  - pieces: Tracks arrival vs buffer order for insertions    │
//	└──────────────────────────────┬──────────────────────────────┘
//	                               │
//	         ┌─────────────────────┴─────────────────────┐
//	         ▼                                           ▼
//	While Composing (Active Preedit):           Composition Finishes:
//	- Drawn inline at the pixel caret           - take() drains committed pieces
//	- Candidate window anchored to caret        - Dispatched to TUI loop as KeyEvent
//	- Suppressed from tui.Backend.Events        - Buffer reset to empty
//
// # Concurrency & Ownership
//
// All methods on [imeState] are invoked strictly on the Gio window event goroutine inside
// [Backend.run]. Text mutations never access the TUI component tree directly.

// imeState coordinates the text editing buffer between the OS input method and the backend.
type imeState struct {
	text []rune
	comp key.Range // the composing range; empty when nothing is being composed
	// pieces tracks committed text pieces alongside their buffer positions.
	// Preserves arrival order while tracking positional shifts caused by prior edits.
	pieces []imePiece
	// sel mirrors the active selection within the buffer (from key.SelectionEvent),
	// allowing the backend to report consistent state back to the OS.
	sel key.Range
}

// imePiece represents one committed segment of text and its offset within the virtual buffer.
type imePiece struct {
	runs   []rune
	from   int // buffer position of runs[0], maintained across later edits
	folded bool
}

// edit applies an incoming [key.EditEvent] by replacing buffer runes [Range.Start, Range.End)
// with the replacement text. Out-of-bounds ranges are safely clamped.
func (s *imeState) edit(e key.EditEvent) {
	start, end := s.clamp(e.Range)
	repl := []rune(e.Text)
	next := make([]rune, 0, len(s.text)-(end-start)+len(repl))
	next = append(next, s.text[:start]...)
	next = append(next, repl...)
	s.text = append(next, s.text[end:]...)
	s.pieces = s.revise(start, end, repl)
}

// revise maintains the pieces for an edit that replaced buffer runes [start, end)
// with repl. The invariant the delivery needs: the pieces, concatenated, are the
// buffer — so the replacement merges into the piece that OWNED the replaced
// bytes (at that piece's arrival slot), and pieces before and after keep their
// relative order with shifted spans. A replacement that owned no bytes (a plain
// insertion between pieces, or at the ends) appends as a new piece.
func (s *imeState) revise(start, end int, repl []rune) []imePiece {
	delta := len(repl) - (end - start)
	// find the owning piece: the one whose bytes the replaced range cut or
	// covered. An insertion (start == end) owns nothing: it lands between.
	owner := -1
	for i, p := range s.pieces {
		pEnd := p.from + len(p.runs)
		if pEnd > start && p.from < end {
			owner = i
			break
		}
		if p.from == start && p.from == pEnd && len(p.runs) == 0 {
			continue // an empty piece owns nothing
		}
	}
	var out []imePiece
	for i, p := range s.pieces {
		pEnd := p.from + len(p.runs)
		switch {
		case i == owner:
			// merge the replacement into this piece: its bytes outside the
			// range kept, the replacement in their place
			head := p.runs[:max(start-p.from, 0)]
			var tail []rune
			if pEnd > end {
				tail = p.runs[end-p.from:]
			}
			merged := make([]rune, 0, len(head)+len(repl)+len(tail))
			merged = append(merged, head...)
			merged = append(merged, repl...)
			merged = append(merged, tail...)
			out = append(out, imePiece{runs: merged, from: p.from, folded: true})
		case pEnd <= start || p.from >= end:
			// untouched: shift it when it sits after the edit
			if p.from >= end {
				p.from += delta
			}
			out = append(out, p)
		default:
			// the range cut this piece with no earlier owner found: keep what
			// remains of it (an owner earlier in the loop took the merge)
			var tail []rune
			if pEnd > end {
				tail = p.runs[end-p.from:]
			}
			var head []rune
			if start > p.from {
				head = p.runs[:start-p.from]
			}
			rem := make([]rune, 0, len(head)+len(tail))
			rem = append(rem, head...)
			rem = append(rem, tail...)
			if len(rem) > 0 {
				at := p.from
				if len(head) == 0 {
					at = end + delta - len(tail)
				}
				out = append(out, imePiece{runs: rem, from: at})
			}
		}
	}
	if owner < 0 && len(repl) > 0 {
		// the edit owned no piece's bytes: a new piece, slotted by position
		out = append(out, imePiece{runs: repl, from: start})
	}
	return out
}

// compose records the composing range.
func (s *imeState) compose(e key.CompositionEvent) { s.comp = key.Range(e) }

// composing reports whether there is an active non-empty IME composition range.
func (s *imeState) composing() bool {
	start, end := s.clamp(s.comp)
	return s.comp.Start >= 0 && end > start
}

// held is everything the buffer holds while a composition is active, for drawing at the caret:
// the committed text that waits for the composition to end (take delivers nothing until then)
// and the composition itself, in buffer order, with the composing range in runes. Drawing only
// the composition hid the committed part: typing 하 then ㅇ left 하 nowhere on screen until a
// space committed both. ok is false when no composition is active.
func (s *imeState) held() (text []rune, comp key.Range, ok bool) {
	if !s.composing() {
		return nil, key.Range{}, false
	}
	start, end := s.clamp(s.comp)
	return s.text, key.Range{Start: start, End: end}, true
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
// is active; nothing while one is (the committed part waits with it, keeping its order). The
// pieces are delivered in buffer order, so their sequential insertion reproduces the buffer
// exactly — an input method that inserts before or rewrites across an earlier piece's bytes
// makes arrival order diverge from the buffer, and arrival order there would deliver the wrong
// text.
func (s *imeState) take() ([]string, bool) {
	if s.composing() || len(s.text) == 0 {
		return nil, false
	}
	ordered := append([]imePiece(nil), s.pieces...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].from < ordered[j].from })
	var out []string
	for _, p := range ordered {
		if len(p.runs) > 0 {
			out = append(out, string(p.runs))
		}
	}
	if len(out) == 0 {
		out = []string{string(s.text)} // pieces that predate the order bookkeeping
	}
	s.text = s.text[:0]
	s.comp = key.Range{}
	s.pieces = s.pieces[:0]
	s.sel = key.Range{}
	return out, true
}

// selection is the input method's selection in the buffer, clamped to it.
func (s *imeState) selection() key.Range {
	start, end := s.clamp(s.sel)
	if s.sel.Start > s.sel.End {
		start, end = end, start // keep the direction the input method gave
	}
	return key.Range{Start: start, End: end}
}

// snippet is the buffer's text over r, clamped to the buffer: the answer to Gio's SnippetEvent,
// so its copy of the text the input method edits matches the input method's.
func (s *imeState) snippet(r key.Range) key.Snippet {
	start, end := s.clamp(r)
	return key.Snippet{Range: key.Range{Start: start, End: end}, Text: string(s.text[start:end])}
}

// empty reports whether the buffer holds nothing, so Gio's copy should be reset to match.
func (s *imeState) empty() bool { return len(s.text) == 0 }

// clamp returns normalized start and end rune indices clamped to the buffer length.
func (s *imeState) clamp(r key.Range) (start, end int) {
	start, end = r.Start, r.End
	if start > end {
		start, end = end, start
	}
	return min(max(start, 0), len(s.text)), min(max(end, 0), len(s.text))
}

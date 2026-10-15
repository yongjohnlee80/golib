package gui

import (
	"sort"

	"github.com/yongjohnlee80/gio/io/key"

	"github.com/yongjohnlee80/golib/tui"
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
//	- Suppressed from tui.Backend.Events        - Buffer emptied where the text ended
//
// # Coordinates
//
// The toolkit and the input method address the buffer in their own coordinates, which keep
// counting across deliveries: after 군 is delivered, the next syllable is composed at 1, not
// 0. The buffer's first rune sits at base in those coordinates. Delivering advances base past
// the delivered text and leaves the selection where the input method put it, so the toolkit's
// copy of the editor state does not move. Moving it back to 0 looked to the toolkit like the
// app moving the selection, and on macOS that discards the input method's marked text, while
// the input method, running in its own process, may still be about to send the consonant that
// begins the next syllable: 군요 came out as 군ㅛ.
//
// # Concurrency & Ownership
//
// All methods on [imeState] are invoked strictly on the Gio window event goroutine inside
// [Backend.run]. Text mutations never access the TUI component tree directly.

// imeState coordinates the text editing buffer between the OS input method and the backend.
type imeState struct {
	text []rune
	// base is where text[0] sits in the toolkit's coordinates. An empty buffer sits wherever
	// the input method's next edit or selection lands, so it follows the toolkit when its
	// coordinates start again (a focus change).
	base int
	comp key.Range // the composing range, in the toolkit's coordinates; empty when nothing is being composed
	// pieces tracks committed text pieces alongside their buffer positions.
	// Preserves arrival order while tracking positional shifts caused by prior edits.
	pieces []imePiece
	// sel mirrors the input method's selection (from a SelectionSet), in the toolkit's
	// coordinates, so the backend reports back exactly the one the input method holds.
	sel key.Range
}

// imePiece represents one committed segment of text and its offset within the virtual buffer.
type imePiece struct {
	runs   []rune
	from   int // buffer position of runs[0], maintained across later edits
	folded bool
}

// edit applies an incoming [TextEdit] by replacing buffer runes [Start, End) with the
// replacement text. Out-of-bounds ranges are safely clamped.
func (s *imeState) edit(e TextEdit) {
	if len(s.text) == 0 {
		s.base = max(min(e.Start, e.End), 0)
	}
	start, end := s.clamp(e.Start-s.base, e.End-s.base)
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

// compose records the composing range of a [Composition].
func (s *imeState) compose(e Composition) { s.comp = key.Range{Start: e.Start, End: e.End} }

// setSelection records the input method's selection of the buffer.
func (s *imeState) setSelection(e SelectionSet) {
	s.sel = key.Range{Start: e.Start, End: e.End}
	if len(s.text) == 0 {
		s.base = max(min(e.Start, e.End), 0)
	}
}

// local converts a range in the toolkit's coordinates to buffer rune indices, clamped to the
// buffer.
func (s *imeState) local(start, end int) (int, int) { return s.clamp(start-s.base, end-s.base) }

// composing reports whether there is an active non-empty IME composition range.
func (s *imeState) composing() bool {
	start, end := s.local(s.comp.Start, s.comp.End)
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
	start, end := s.local(s.comp.Start, s.comp.End)
	return s.text, key.Range{Start: start, End: end}, true
}

// preedit is the text being composed, or "".
func (s *imeState) preedit() string {
	if !s.composing() {
		return ""
	}
	start, end := s.local(s.comp.Start, s.comp.End)
	return string(s.text[start:end])
}

// take returns the text ready to commit and empties the buffer where the text ended, keeping the
// input method's selection: everything, once no composition is active; nothing while one is
// (the committed part waits with it, keeping its order). The
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
	s.base += len(s.text)
	s.text = s.text[:0]
	s.comp = key.Range{}
	s.pieces = s.pieces[:0]
	return out, true
}

// selection is the input method's selection, exactly as it set it: reporting any other one makes
// the toolkit cancel the composition.
func (s *imeState) selection() key.Range { return s.sel }

// snippet is the buffer's text over ask, clamped to the buffer: the answer to a [SnippetAsk],
// so the toolkit's copy of the text the input method edits matches the input method's.
func (s *imeState) snippet(ask SnippetAsk) SnippetReply {
	start, end := s.local(ask.Start, ask.End)
	return SnippetReply{Start: s.base + start, End: s.base + end, Text: string(s.text[start:end])}
}

// commitFrame is what the buffer commits when a frame's input ends: the committed
// pieces as key events in delivery order, and the commands that tell the toolkit
// the buffer is now empty where the delivered text ended, with the input method's
// selection unchanged. ok is false while a composition holds the buffer or
// nothing was typed. The window sends these through the adapter; the replay
// checks them against the recording.
func (s *imeState) commitFrame() (keys []tui.KeyEvent, resets []TextInputCmd, ok bool) {
	pieces, ok := s.take()
	if !ok {
		return nil, nil, false
	}
	for _, p := range pieces {
		keys = append(keys, textKeys(p)...)
	}
	sel := s.selection()
	return keys, []TextInputCmd{SnippetReply{Start: s.base, End: s.base}, SelectionTell{Start: sel.Start, End: sel.End}}, true
}

// empty reports whether the buffer holds nothing, so Gio's copy should be reset to match.
func (s *imeState) empty() bool { return len(s.text) == 0 }

// clamp returns normalized start and end rune indices clamped to the buffer length.
func (s *imeState) clamp(start, end int) (int, int) {
	if start > end {
		start, end = end, start
	}
	return min(max(start, 0), len(s.text)), min(max(end, 0), len(s.text))
}

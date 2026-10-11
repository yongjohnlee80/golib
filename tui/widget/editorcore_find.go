package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
)

// FIND AND REPLACE — editor core functions over the text, in buffer positions.
//
// A term matches whole grapheme clusters, within one line. The ACTIVE find (SetFind) is what n and
// N step through and what the editor marks: every match on a painted line takes the Alert style,
// over the host's own overlay, so the marks look like the host's Alert (an editor whose syntax
// styles leave Alert unset shows no marks; the cursor still moves to each match).

// FindOptions narrow a find. The zero value is a literal, case-sensitive, forward search that
// stops at the end.
type FindOptions struct {
	IgnoreCase bool // fold case
	Backward   bool // towards the start
	Wrap       bool // continue from the other end
}

// findState is the active find.
type findState struct {
	term string
	opts FindOptions
	// plain says the core installed a highlighter of its own so the marks paint on a core that had
	// none; clearing the find, or the host setting one, removes it.
	plain bool
	// host is the host's own overlay (SetHighlightOverlay): the marks go over it.
	host highlight.Overlay
	// cur is the current match, drawn as a selection (SetCurrentMatch); curOn says there is one
	cur   TextRange
	curOn bool
}

// plainHighlighter styles nothing: a core with no highlighter paints no overlay.
var plainHighlighter = highlight.HighlighterFunc(func(string, highlight.State) ([]highlight.Span, highlight.State) {
	return nil, 0
})

// matchesIn is every match of term (its clusters) in line, as cluster columns [start, end).
func matchesIn(line string, term []string, fold bool) [][2]int {
	if len(term) == 0 {
		return nil
	}
	hay := clusters(line)
	var out [][2]int
	for i := 0; i+len(term) <= len(hay); i++ {
		ok := true
		for k, t := range term {
			if h := hay[i+k]; h != t && !(fold && strings.EqualFold(h, t)) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, [2]int{i, i + len(term)})
			i += len(term) - 1 // matches do not overlap
		}
	}
	return out
}

// FindAll is every match of term in the text, in order. Backward and Wrap do not apply.
func (c *EditorCore) FindAll(term string, o FindOptions) []TextRange {
	t := clusters(term)
	var out []TextRange
	for ln, line := range c.buf.lines {
		for _, m := range matchesIn(line, t, o.IgnoreCase) {
			out = append(out, TextRange{From: TextPos{ln, m[0]}, To: TextPos{ln, m[1]}})
		}
	}
	return out
}

// Find is the next match of term from `from`: forward, the first starting after from (so finding
// again from a match moves on); backward, the last starting before it. With Wrap it continues from
// the other end. ok is false when there is none.
func (c *EditorCore) Find(term string, from TextPos, o FindOptions) (TextRange, bool) {
	all := c.FindAll(term, o)
	if len(all) == 0 {
		return TextRange{}, false
	}
	at := c.clampPos(from)
	after := func(r TextRange) bool {
		return r.From.Line > at.ln || (r.From.Line == at.ln && r.From.Col > at.col)
	}
	before := func(r TextRange) bool {
		return r.From.Line < at.ln || (r.From.Line == at.ln && r.From.Col < at.col)
	}
	if !o.Backward {
		for _, r := range all {
			if after(r) {
				return r, true
			}
		}
		if o.Wrap {
			return all[0], true
		}
		return TextRange{}, false
	}
	for i := len(all) - 1; i >= 0; i-- {
		if before(all[i]) {
			return all[i], true
		}
	}
	if o.Wrap {
		return all[len(all)-1], true
	}
	return TextRange{}, false
}

// ReplaceAll replaces every match of term with `with`, in one undo group, and reports how many.
func (c *EditorCore) ReplaceAll(term, with string, o FindOptions) (int, error) {
	all := c.FindAll(term, o)
	if len(all) == 0 {
		return 0, nil
	}
	err := c.Batch(func() error {
		// from the end: an earlier replacement would move every later match
		for i := len(all) - 1; i >= 0; i-- {
			if _, err := c.Replace(all[i], with); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(all), nil
}

// SetFind makes term the active find, or clears it with "": its matches are marked, and n and N
// step through them. Backward and Wrap in o are ignored: n always goes forward and N back, both
// wrapping.
func (c *EditorCore) SetFind(term string, o FindOptions) {
	c.find.term, c.find.opts = term, FindOptions{IgnoreCase: o.IgnoreCase}
	if term == "" {
		c.find.curOn = false
	}
	switch {
	case term != "" && c.hl.hl == nil:
		c.hl.setHighlighter(plainHighlighter)
		c.find.plain = true
	case term == "" && c.find.plain:
		c.hl.setHighlighter(nil)
		c.find.plain = false
	}
	c.hl.overlay = c.findOverlay()
	c.InvalidateHighlightPaint()
}

// FindTerm is the active find's term and options; "" with none.
func (c *EditorCore) FindTerm() (string, FindOptions) { return c.find.term, c.find.opts }

// FindNext moves the cursor to the active find's next match (back, with back), wrapping, and
// reports whether there was one. With no active find it does nothing and reports false.
func (c *EditorCore) FindNext(back bool) bool {
	if c.find.term == "" {
		return false
	}
	o := c.find.opts
	o.Backward, o.Wrap = back, true
	r, ok := c.Find(c.find.term, c.Cursor(), o)
	if ok {
		c.SetLine(r.From.Line, r.From.Col)
		c.SetCurrentMatch(r)
	}
	return ok
}

// SetCurrentMatch draws r as the current match, as a selection whatever the mode and the focus: the
// one a find is on, so the user sees which. An empty r clears it; SetFind("") clears it too.
func (c *EditorCore) SetCurrentMatch(r TextRange) {
	lo, hi := c.clampRange(r)
	c.find.cur = TextRange{From: TextPos{lo.ln, lo.col}, To: TextPos{hi.ln, hi.col}}
	c.find.curOn = lo != hi
	c.markDirty()
}

// CurrentMatch is the current match; ok is false with none.
func (c *EditorCore) CurrentMatch() (TextRange, bool) { return c.find.cur, c.find.curOn }

func (c *EditorCore) inCurrentMatch(ln, col int) bool {
	if !c.find.curOn {
		return false
	}
	r := c.find.cur
	lo, hi := taPos{r.From.Line, r.From.Col}, taPos{r.To.Line, r.To.Col}
	return posLE(lo, ln, col) && !posLE(hi, ln, col)
}

// findOverlay is the host's overlay with the active find's marks over it.
func (c *EditorCore) findOverlay() highlight.Overlay {
	host := c.find.host
	if c.find.term == "" {
		return host
	}
	term, fold := clusters(c.find.term), c.find.opts.IgnoreCase
	return func(line string, semantic []highlight.Span) []highlight.Span {
		spans := semantic
		if host != nil {
			spans = host(line, spans)
		}
		return markMatches(line, spans, term, fold)
	}
}

// markMatches lays an Alert span over every match in line, splitting the spans it covers.
func markMatches(line string, spans []highlight.Span, term []string, fold bool) []highlight.Span {
	found := matchesIn(line, term, fold)
	if len(found) == 0 {
		return spans
	}
	// byte offset of each cluster boundary
	cs := clusters(line)
	at := make([]int, len(cs)+1)
	for i, s := range cs {
		at[i+1] = at[i] + len(s)
	}
	styles := make([]highlight.Style, len(line))
	for _, s := range spans {
		for i := max(s.Start, 0); i < s.End && i < len(styles); i++ {
			styles[i] = s.Style
		}
	}
	for _, m := range found {
		for i := at[m[0]]; i < at[m[1]]; i++ {
			styles[i] = highlight.Alert
		}
	}
	var out []highlight.Span
	for i := 0; i < len(styles); {
		j := i
		for j < len(styles) && styles[j] == styles[i] {
			j++
		}
		if styles[i] != highlight.Normal {
			out = append(out, highlight.Span{Start: i, End: j, Style: styles[i]})
		}
		i = j
	}
	return out
}

// ReplaceSession is replace's walk through the matches, one question at a time: a widget asks the
// user at each match and answers with Answer. It walks the whole document from the top, each match
// once, so a replacement that contains the term is never asked about again. Every replacement of
// one session undoes with a single u.
type ReplaceSession struct {
	c        *EditorCore
	term     []string
	with     string
	o        FindOptions
	cur      TextRange
	ok       bool // cur is a match waiting for its answer
	grouped  bool // a replacement began the session's undo group
	replaced int
}

// StartReplace begins a replace session at the first match in the document, selected; ok is
// false when there is none.
func (c *EditorCore) StartReplace(term, with string, o FindOptions) (*ReplaceSession, bool) {
	s := &ReplaceSession{c: c, term: clusters(term), with: with, o: o}
	s.seek(taPos{}, true)
	return s, s.ok
}

// seek selects the first match at or after `at`, or ends the session.
func (s *ReplaceSession) seek(at taPos, inclusive bool) {
	s.ok = false
	for ln := at.ln; ln < len(s.c.buf.lines); ln++ {
		for _, m := range matchesIn(s.c.buf.lines[ln], s.term, s.o.IgnoreCase) {
			if ln == at.ln && (m[0] < at.col || (!inclusive && m[0] == at.col)) {
				continue
			}
			s.cur, s.ok = TextRange{From: TextPos{ln, m[0]}, To: TextPos{ln, m[1]}}, true
			s.c.Select(s.cur)
			return
		}
	}
	s.c.Unselect()
}

// Current is the match waiting for an answer; ok is false once the session has ended.
func (s *ReplaceSession) Current() (TextRange, bool) { return s.cur, s.ok }

// Replaced is how many matches were replaced.
func (s *ReplaceSession) Replaced() int { return s.replaced }

// Answer replaces the current match (yes) or skips it (no), and goes to the next. more is false
// when there are no more matches: the session has ended.
func (s *ReplaceSession) Answer(yes bool) (more bool, err error) {
	if !s.ok {
		return false, nil
	}
	next := s.c.clampPos(s.cur.To)
	if yes {
		s.c.joinGroup = s.grouped
		end, err := s.c.Replace(s.cur, s.with)
		s.c.joinGroup = false
		if err != nil {
			s.Stop()
			return false, err
		}
		s.grouped = true
		s.replaced++
		next = s.c.clampPos(end)
	}
	s.seek(next, true)
	return s.ok, nil
}

// Stop ends the session where it is: the replacements made stay.
func (s *ReplaceSession) Stop() {
	if s.ok {
		s.ok = false
		s.c.Unselect()
	}
}

// SetOnEscape gives the host Esc in Normal mode (or a modeless editor's Esc) when the editor has
// nothing of its own to cancel (no pending count or operator, no selection, no active find): fn
// reports whether it took the key (the host's own find in the page, say), and a key it did not
// take bubbles as before. Only a focused editor sees that Esc, so the host's answer is scoped to
// it. nil: none.
func (c *EditorCore) SetOnEscape(fn func() bool) { c.onEscape = fn }

// escapeFind is Esc with nothing else to cancel: the active find ends; else the host's (SetOnEscape)
// answer. It reports whether the key was taken.
func (c *EditorCore) escapeFind() bool {
	if c.find.term != "" {
		c.SetFind("", FindOptions{})
		return true
	}
	return c.onEscape != nil && c.onEscape()
}

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
	}
	return ok
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

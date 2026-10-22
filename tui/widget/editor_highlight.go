package widget

import (
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/style"
)

// SYNTAX HIGHLIGHTING — Qt's QSyntaxHighlighter, on the Editor.
//
// A highlight.Highlighter colours one line at a time, carrying a state from
// each line to the next for what spans lines. The Editor asks it only for the
// lines it paints, and remembers the answer: a line is highlighted again only
// when its text, or the state it starts in, has changed. So an edit re-colours
// from the edited line until the carried state is what it was before, and
// never past the bottom of the screen.
//
// A jump deep into a long file needs every line above the screen, for the
// state it carries down. A frame highlights at most hlFrameBudget of them,
// paints the screen from a provisional state meanwhile, and asks for the next
// frame, until the cache reaches the screen: the UI never freezes on a file's
// length, and the colours end carried from the first line.
//
// A span carries a style KIND, never a colour. SyntaxStyles says what each
// kind looks like; an unset kind paints as Normal, and Normal unset as the
// text. Painting order: the text style, the span's style over it, the
// selection over both.

// SyntaxStyles is a look per highlight.Style. A zero entry paints as Normal's,
// and a zero Normal as the text.
type SyntaxStyles [highlight.Styles]style.Style

// hlLine is what the highlighter made of one line, and what it was computed from. gen numbers it
// among its text's entries: an editor's painted styles for the line are good while gen is.
type hlLine struct {
	text     string
	in       highlight.State
	out      highlight.State
	semantic []highlight.Span
	gen      uint64
}

// hlText is a text's highlighting: the highlighter, and what it made of each line, remembered
// with what it was computed from (the walk below). A Buffer holds one; every editor showing the
// buffer walks and reads it, and paints from it in its own looks (highlightCache).
type hlText struct {
	hl    highlight.Highlighter
	lines []hlLine
	// valid is how many leading lines of the cache are VERIFIED: each entry's text is its line's,
	// and its starting state the one before it hands down. The walk starts there, not at line 0,
	// so a frame's work is what changed and what is on screen, never the file's length. A change
	// to the text pulls it back to the first line changed (textBuffer.touch); a new highlighter
	// clears it.
	valid int
	// examined counts the lines the walk has looked at, for a test to hold a frame's work to what
	// it promises.
	examined int
	states   highlight.StateStore
	gen      uint64 // the last entry's gen: store numbers every line it keeps
}

// highlightCache is an editor's highlighting: the shown buffer's text half (text), what each style
// looks like, the overlay (the host's marks and the find's), and the styles painted from the text's
// spans with them, by line. It reads the text it is given and keeps nothing of the widget, so any
// highlighted text view can hold one.
type highlightCache struct {
	text          *hlText
	syntax        SyntaxStyles
	paint         []hlPainted // by line: the styles painted from text.lines[i]
	overlay       highlight.Overlay
	paintRevision uint64
}

// hlPainted is one line's painted styles: per grapheme cluster, from the text entry numbered gen,
// under the cache's paintRevision rev.
type hlPainted struct {
	styles []highlight.Style
	gen    uint64
	rev    uint64
}

// setHighlighter replaces the highlighter: every line is highlighted afresh.
func (t *hlText) setHighlighter(h highlight.Highlighter) {
	t.setSource(highlight.Source{Highlighter: h})
}

func (t *hlText) lease(entry hlLine, retain bool) {
	if t.states == nil {
		return
	}
	if retain {
		t.states.Retain(entry.in)
		t.states.Retain(entry.out)
	} else {
		t.states.Release(entry.in)
		t.states.Release(entry.out)
	}
}

// setSource replaces the source: every line is highlighted afresh, and so painted afresh (each new
// entry has a new gen).
func (t *hlText) setSource(source highlight.Source) {
	for _, entry := range t.lines {
		t.lease(entry, false)
	}
	t.hl, t.states, t.lines, t.valid = source.Highlighter, source.States, nil, 0
}

// beginHighlightFrame starts a Render's walk from the screen's top line. When the walk has not
// reached the screen, the next frame is asked for after this one, since a render cannot mark
// itself dirty.
func (e *Editor) beginHighlightFrame() *HighlightFrame {
	f := e.core.BeginHighlight(e.cells.top)
	if f.Behind {
		if ctx := e.Context(); ctx != nil {
			ctx.Update(e.MarkDirty)
		}
	}
	return f
}

// highlighted returns the style of every cluster of line ln: a line on screen, asked for in
// order by Render.
func (e *Editor) highlighted(ln int, f *HighlightFrame) []highlight.Style { return f.Styles(ln) }

// hlFrameBudget is how many lines a frame may EXAMINE to catch up — check
// against the cache or highlight afresh — on its way to the screen. A jump
// deep into a long file needs every line above the screen, for the state it
// hands down; doing them all in one frame freezes the UI for a large file. So
// a frame does this many, paints the screen from a provisional state, and asks
// for another frame; input is handled between.
const hlFrameBudget = 2000

// hlFrame is one Render's highlighting walk.
type hlFrame struct {
	// checked is how far down the buffer this frame has confirmed the cache.
	checked int
	// provisional is set when the catch-up above the screen did not finish:
	// the screen's lines are then highlighted from state 0 into local, and
	// never into the cache, whose entries are keyed by the state they
	// started in.
	provisional bool
	local       map[int][]highlight.Style
	localOut    highlight.State
	localNext   int
	states      highlight.StateStore
	closed      bool
	collecting  bool
}

func (f *hlFrame) close() {
	if !f.closed && f.states != nil {
		f.states.Release(f.localOut)
	}
	f.closed = true
}

// verifiedFrom is where the walk starts: the verified prefix, pulled back to the first line
// changed since the last frame (takeChanged), and cut to the text's length.
func (t *hlText) verifiedFrom(text []string, takeChanged func() int) int {
	if ch := takeChanged(); ch < t.valid {
		t.valid = ch
	}
	if len(t.lines) > len(text) {
		for _, entry := range t.lines[len(text):] {
			t.lease(entry, false)
		}
		t.lines = t.lines[:len(text)]
	}
	t.valid = min(t.valid, len(t.lines))
	return t.valid
}

// confirm makes line i's cache entry right — kept, when its text and starting
// state are unchanged, or highlighted afresh — and advances the verified
// prefix over it when it is next.
func (t *hlText) confirm(text []string, i int) {
	t.examined++
	in := highlight.State(0)
	if i > 0 {
		in = t.lines[i-1].out
	}
	if !(i < len(t.lines) && t.lines[i].text == text[i] && t.lines[i].in == in) {
		t.store(i, t.highlightLine(text[i], in))
	}
	if i == t.valid {
		t.valid++
	}
}

// catchUp confirms the cache for the lines above the screen (top), examining at
// most hlFrameBudget of them, and reports whether it reached the screen.
func (t *hlText) catchUp(f *hlFrame, text []string, top int, takeChanged func() int) bool {
	from, to := t.verifiedFrom(text, takeChanged), min(top, len(text))
	for i := from; i < to; i++ {
		if i-from == hlFrameBudget {
			return false
		}
		t.confirm(text, i)
	}
	f.checked = max(to, from)
	return true
}

// store keeps entry as line i, numbered afresh: every editor's painted styles for the line it
// replaces are stale from here.
func (t *hlText) store(i int, entry hlLine) {
	t.gen++
	entry.gen = t.gen
	t.lease(entry, true)
	if i < len(t.lines) {
		t.lease(t.lines[i], false)
		t.lines[i] = entry
	} else {
		t.lines = append(t.lines, entry)
	}
}

// beginFrame starts a walk over text with the screen from line top. behind is true when the
// catch-up did not reach the screen: the frame is provisional, and the widget asks for another.
func (t *hlText) beginFrame(text []string, top int, takeChanged func() int) (f *hlFrame, behind bool) {
	f = &hlFrame{states: t.states}
	if t.hl == nil {
		return f, false
	}
	if t.states != nil && t.states.Collect(hlFrameBudget) {
		f.collecting = true
		return f, true
	}
	if !t.catchUp(f, text, top, takeChanged) {
		f.provisional = true
		return f, true
	}
	return f, false
}

// highlightLine runs the highlighter over one line: its spans, and the state it hands down.
func (t *hlText) highlightLine(text string, in highlight.State) hlLine {
	spans, out := t.hl.HighlightBlock(text, in)
	return hlLine{text: text, in: in, out: out, semantic: append([]highlight.Span(nil), spans...)}
}

// highlighted returns the style of every cluster of line ln, a line on screen from top, asked
// for in order.
func (c *highlightCache) highlighted(text []string, top, ln int, f *hlFrame) []highlight.Style {
	t := c.text
	if t.hl == nil || f.closed {
		return nil
	}
	if f.collecting {
		if ln < len(t.lines) && ln < len(text) && t.lines[ln].text == text[ln] {
			return c.painted(ln)
		}
		return nil
	}
	if f.provisional {
		if f.local == nil {
			f.local, f.localNext = map[int][]highlight.Style{}, top
		}
		for ; f.localNext <= ln && f.localNext < len(text); f.localNext++ {
			entry := t.highlightLine(text[f.localNext], f.localOut)
			if f.states != nil {
				f.states.Retain(entry.out)
				f.states.Release(f.localOut)
			}
			f.local[f.localNext], f.localOut = c.paintEntry(entry), entry.out
		}
		return f.local[ln]
	}
	for i := f.checked; i <= ln && i < len(text); i++ {
		t.confirm(text, i)
	}
	f.checked = max(f.checked, ln+1)
	if ln < len(t.lines) {
		return c.painted(ln)
	}
	return nil
}

// painted is line i's styles, painted from the text's entry: kept while the entry (gen) and the
// looks (paintRevision) are the ones they were painted from.
func (c *highlightCache) painted(i int) []highlight.Style {
	entry := c.text.lines[i]
	if i < len(c.paint) {
		if p := c.paint[i]; p.styles != nil && p.gen == entry.gen && p.rev == c.paintRevision {
			return p.styles
		}
	}
	styles := c.paintEntry(entry)
	if i >= len(c.paint) {
		c.paint = append(c.paint, make([]hlPainted, i+1-len(c.paint))...)
	}
	c.paint[i] = hlPainted{styles: styles, gen: entry.gen, rev: c.paintRevision}
	return styles
}

// paintEntry maps an entry's spans, with the overlay over them, onto the line's clusters.
func (c *highlightCache) paintEntry(entry hlLine) []highlight.Style {
	spans := entry.semantic
	if c.overlay != nil {
		spans = c.overlay(entry.text, append([]highlight.Span(nil), spans...))
	}
	// A malformed extension cannot make byte projection reach outside the line.
	valid := make([]highlight.Span, 0, len(spans))
	end := 0
	for _, s := range spans {
		if s.Start < end || s.End <= s.Start || s.Start < 0 || s.End > len(entry.text) {
			continue
		}
		if s.Start > 0 && !utf8.RuneStart(entry.text[s.Start]) {
			continue
		}
		if s.End < len(entry.text) && !utf8.RuneStart(entry.text[s.End]) {
			continue
		}
		valid = append(valid, s)
		end = s.End
	}
	spans = valid
	cs := clusters(entry.text)
	styles := make([]highlight.Style, len(cs))
	byteAt := 0
	si := 0
	for col, cl := range cs {
		for si < len(spans) && spans[si].End <= byteAt {
			si++
		}
		if si < len(spans) && spans[si].Start <= byteAt && byteAt < spans[si].End {
			styles[col] = spans[si].Style
		}
		byteAt += len(cl)
	}
	return styles
}

// syntaxStyle is the look a cluster's highlight style adds over the text: its
// own, or — unset — the Normal style's, which is also what uncovered text
// wears. Neither set, the text's own.
func (c *highlightCache) syntaxStyle(k highlight.Style) (style.Style, bool) {
	if int(k) >= len(c.syntax) {
		k = highlight.Normal
	}
	if st := c.syntax[k]; st != (style.Style{}) {
		return st, true
	}
	st := c.syntax[highlight.Normal]
	return st, st != (style.Style{})
}

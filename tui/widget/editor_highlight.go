package widget

import (
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

// hlLine is one line's highlighting, and what it was computed from.
type hlLine struct {
	text   string
	in     highlight.State
	out    highlight.State
	styles []highlight.Style // per grapheme cluster
}

// highlightCache is a text's highlighting: the highlighter, what each style looks like, and each
// line's styles remembered with what they were computed from (editor_highlight.go's walk). It
// reads the text it is given and keeps nothing of the widget, so any highlighted text view can
// hold one.
type highlightCache struct {
	hl     highlight.Highlighter
	syntax SyntaxStyles
	lines  []hlLine
	// valid is how many leading lines of the cache are VERIFIED: each entry's text is its line's,
	// and its starting state the one before it hands down. The walk starts there, not at line 0,
	// so a frame's work is what changed and what is on screen, never the file's length. A change
	// to the text pulls it back to the first line changed (textBuffer.touch); a new highlighter
	// clears it.
	valid int
	// examined counts the lines the walk has looked at, for a test to hold a frame's work to what
	// it promises.
	examined int
}

// setHighlighter replaces the highlighter: every line is highlighted afresh.
func (c *highlightCache) setHighlighter(h highlight.Highlighter) {
	c.hl, c.lines, c.valid = h, nil, 0
}

// WithHighlighter sets the Editor's highlighter; nil is none.
func WithHighlighter(h highlight.Highlighter) EditorOption {
	return func(e *Editor) { e.hlc.hl = h }
}

// WithSyntaxStyles sets what each highlight style looks like.
func WithSyntaxStyles(st SyntaxStyles) EditorOption {
	return func(e *Editor) { e.hlc.syntax = st }
}

// SetHighlighter replaces the highlighter at runtime; nil turns highlighting
// off. Every line is highlighted afresh.
func (e *Editor) SetHighlighter(h highlight.Highlighter) {
	e.hlc.setHighlighter(h)
	e.MarkDirty()
}

// WithSyntaxStyles replaces what each highlight style looks like.
func (e *Editor) WithSyntaxStyles(st SyntaxStyles) *Editor {
	e.hlc.syntax = st
	e.MarkDirty()
	return e
}

// beginHighlightFrame starts a Render's walk: the catch-up above the screen,
// and — when it has not reached the screen — the next frame, asked for after
// this one, since a render cannot mark itself dirty.
func (e *Editor) beginHighlightFrame() *hlFrame {
	f, behind := e.hlc.beginFrame(e.lines, e.top, e.takeChanged)
	if behind {
		if ctx := e.Context(); ctx != nil {
			ctx.App().Update(e.MarkDirty)
		}
	}
	return f
}

// highlighted returns the style of every cluster of line ln — a line on
// screen, asked for in order by Render.
func (e *Editor) highlighted(ln int, f *hlFrame) []highlight.Style {
	return e.hlc.highlighted(e.lines, e.top, ln, f)
}

// syntaxStyle is the look a cluster's highlight style adds over the text.
func (e *Editor) syntaxStyle(k highlight.Style) (style.Style, bool) { return e.hlc.syntaxStyle(k) }

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
}

// verifiedFrom is where the walk starts: the verified prefix, pulled back to the first line
// changed since the last frame (takeChanged), and cut to the text's length.
func (c *highlightCache) verifiedFrom(text []string, takeChanged func() int) int {
	if ch := takeChanged(); ch < c.valid {
		c.valid = ch
	}
	if len(c.lines) > len(text) {
		c.lines = c.lines[:len(text)]
	}
	c.valid = min(c.valid, len(c.lines))
	return c.valid
}

// confirm makes line i's cache entry right — kept, when its text and starting
// state are unchanged, or highlighted afresh — and advances the verified
// prefix over it when it is next.
func (c *highlightCache) confirm(text []string, i int) {
	c.examined++
	in := highlight.State(0)
	if i > 0 {
		in = c.lines[i-1].out
	}
	if !(i < len(c.lines) && c.lines[i].text == text[i] && c.lines[i].in == in) {
		c.store(i, c.highlightLine(text[i], in))
	}
	if i == c.valid {
		c.valid++
	}
}

// catchUp confirms the cache for the lines above the screen (top), examining at
// most hlFrameBudget of them, and reports whether it reached the screen.
func (c *highlightCache) catchUp(f *hlFrame, text []string, top int, takeChanged func() int) bool {
	from, to := c.verifiedFrom(text, takeChanged), min(top, len(text))
	for i := from; i < to; i++ {
		if i-from == hlFrameBudget {
			return false
		}
		c.confirm(text, i)
	}
	f.checked = max(to, from)
	return true
}

func (c *highlightCache) store(i int, entry hlLine) {
	if i < len(c.lines) {
		c.lines[i] = entry
	} else {
		c.lines = append(c.lines, entry)
	}
}

// beginFrame starts a walk over text with the screen from line top. behind is true when the
// catch-up did not reach the screen: the frame is provisional, and the widget asks for another.
func (c *highlightCache) beginFrame(text []string, top int, takeChanged func() int) (f *hlFrame, behind bool) {
	f = &hlFrame{}
	if c.hl == nil {
		return f, false
	}
	if !c.catchUp(f, text, top, takeChanged) {
		f.provisional = true
		return f, true
	}
	return f, false
}

// highlighted returns the style of every cluster of line ln, a line on screen from top, asked
// for in order.
func (c *highlightCache) highlighted(text []string, top, ln int, f *hlFrame) []highlight.Style {
	if c.hl == nil {
		return nil
	}
	if f.provisional {
		if f.local == nil {
			f.local, f.localNext = map[int][]highlight.Style{}, top
		}
		for ; f.localNext <= ln && f.localNext < len(text); f.localNext++ {
			entry := c.highlightLine(text[f.localNext], f.localOut)
			f.local[f.localNext], f.localOut = entry.styles, entry.out
		}
		return f.local[ln]
	}
	for i := f.checked; i <= ln && i < len(text); i++ {
		c.confirm(text, i)
	}
	f.checked = max(f.checked, ln+1)
	if ln < len(c.lines) {
		return c.lines[ln].styles
	}
	return nil
}

// highlightLine runs the highlighter over one line and maps its byte spans
// onto the line's clusters.
func (c *highlightCache) highlightLine(text string, in highlight.State) hlLine {
	spans, out := c.hl.HighlightBlock(text, in)
	cs := clusters(text)
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
	return hlLine{text: text, in: in, out: out, styles: styles}
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

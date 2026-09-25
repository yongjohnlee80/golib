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
// A span carries a style KIND, never a colour. SyntaxStyles says what each
// kind looks like; an unset kind paints as the text. Painting order: the text
// style, the span's style over it, the selection over both.

// SyntaxStyles is a look per highlight.Style. A zero entry paints as the text.
type SyntaxStyles [highlight.Styles]style.Style

// hlLine is one line's highlighting, and what it was computed from.
type hlLine struct {
	text   string
	in     highlight.State
	out    highlight.State
	styles []highlight.Style // per grapheme cluster
}

// WithHighlighter sets the Editor's highlighter; nil is none.
func WithHighlighter(h highlight.Highlighter) EditorOption {
	return func(e *Editor) { e.hl = h }
}

// WithSyntaxStyles sets what each highlight style looks like.
func WithSyntaxStyles(st SyntaxStyles) EditorOption {
	return func(e *Editor) { e.syntax = st }
}

// SetHighlighter replaces the highlighter at runtime; nil turns highlighting
// off. Every line is highlighted afresh.
func (e *Editor) SetHighlighter(h highlight.Highlighter) {
	e.hl = h
	e.hlCache = nil
	e.MarkDirty()
}

// WithSyntaxStyles replaces what each highlight style looks like.
func (e *Editor) WithSyntaxStyles(st SyntaxStyles) *Editor {
	e.syntax = st
	e.MarkDirty()
	return e
}

// highlighted returns the style of every cluster of line ln, highlighting
// each line from where the last call stopped down to ln. A Render calls it
// for its lines in order, so a frame walks the buffer once, down to its last
// visible line; checked is that walk's position.
func (e *Editor) highlighted(ln int, checked *int) []highlight.Style {
	if e.hl == nil {
		return nil
	}
	if len(e.hlCache) > len(e.lines) {
		e.hlCache = e.hlCache[:len(e.lines)]
	}
	for i := *checked; i <= ln && i < len(e.lines); i++ {
		in := highlight.State(0)
		if i > 0 {
			in = e.hlCache[i-1].out
		}
		if i < len(e.hlCache) && e.hlCache[i].text == e.lines[i] && e.hlCache[i].in == in {
			continue // unchanged text, unchanged starting state: unchanged colours
		}
		entry := e.highlightLine(i, in)
		if i < len(e.hlCache) {
			e.hlCache[i] = entry
		} else {
			e.hlCache = append(e.hlCache, entry)
		}
	}
	*checked = max(*checked, ln+1)
	if ln < len(e.hlCache) {
		return e.hlCache[ln].styles
	}
	return nil
}

// highlightLine runs the highlighter over one line and maps its byte spans
// onto the line's clusters.
func (e *Editor) highlightLine(ln int, in highlight.State) hlLine {
	text := e.lines[ln]
	spans, out := e.hl.HighlightBlock(text, in)
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

// syntaxStyle is the look a cluster's highlight style adds over the text.
func (e *Editor) syntaxStyle(k highlight.Style) (style.Style, bool) {
	if k == highlight.Normal || int(k) >= len(e.syntax) {
		return style.Style{}, false
	}
	st := e.syntax[k]
	return st, st != (style.Style{})
}

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
	e.hlCache, e.hlValid = nil, 0
	e.MarkDirty()
}

// WithSyntaxStyles replaces what each highlight style looks like.
func (e *Editor) WithSyntaxStyles(st SyntaxStyles) *Editor {
	e.syntax = st
	e.MarkDirty()
	return e
}

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

// hlValid is how many leading lines of the cache are VERIFIED: each entry's
// text is its line's, and its starting state the one before it hands down.
// The walk starts there, not at line 0, so a frame's work is what changed and
// what is on screen — never the file's length. A change to the text pulls it
// back to the first line changed (textBuffer.touch); a new highlighter clears
// it.
func (e *Editor) verifiedFrom() int {
	if ch := e.takeChanged(); ch < e.hlValid {
		e.hlValid = ch
	}
	if len(e.hlCache) > len(e.lines) {
		e.hlCache = e.hlCache[:len(e.lines)]
	}
	e.hlValid = min(e.hlValid, len(e.hlCache))
	return e.hlValid
}

// confirm makes line i's cache entry right — kept, when its text and starting
// state are unchanged, or highlighted afresh — and advances the verified
// prefix over it when it is next.
func (e *Editor) confirm(i int) {
	e.hlExamined++
	in := highlight.State(0)
	if i > 0 {
		in = e.hlCache[i-1].out
	}
	if !(i < len(e.hlCache) && e.hlCache[i].text == e.lines[i] && e.hlCache[i].in == in) {
		e.store(i, e.highlightLine(i, in))
	}
	if i == e.hlValid {
		e.hlValid++
	}
}

// catchUp confirms the cache for the lines above the screen, examining at
// most hlFrameBudget of them, and reports whether it reached the screen.
func (e *Editor) catchUp(f *hlFrame) bool {
	from, to := e.verifiedFrom(), min(e.top, len(e.lines))
	for i := from; i < to; i++ {
		if i-from == hlFrameBudget {
			return false
		}
		e.confirm(i)
	}
	f.checked = max(to, from)
	return true
}

func (e *Editor) store(i int, entry hlLine) {
	if i < len(e.hlCache) {
		e.hlCache[i] = entry
	} else {
		e.hlCache = append(e.hlCache, entry)
	}
}

// highlighted returns the style of every cluster of line ln — a line on
// screen, asked for in order by Render.
func (e *Editor) highlighted(ln int, f *hlFrame) []highlight.Style {
	if e.hl == nil {
		return nil
	}
	if f.provisional {
		if f.local == nil {
			f.local, f.localNext = map[int][]highlight.Style{}, e.top
		}
		for ; f.localNext <= ln && f.localNext < len(e.lines); f.localNext++ {
			entry := e.highlightLine(f.localNext, f.localOut)
			f.local[f.localNext], f.localOut = entry.styles, entry.out
		}
		return f.local[ln]
	}
	for i := f.checked; i <= ln && i < len(e.lines); i++ {
		e.confirm(i)
	}
	f.checked = max(f.checked, ln+1)
	if ln < len(e.hlCache) {
		return e.hlCache[ln].styles
	}
	return nil
}

// beginHighlightFrame starts a Render's walk: the catch-up above the screen,
// and — when it has not reached the screen — the next frame, asked for after
// this one, since a render cannot mark itself dirty.
func (e *Editor) beginHighlightFrame() *hlFrame {
	f := &hlFrame{}
	if e.hl == nil {
		return f
	}
	if !e.catchUp(f) {
		f.provisional = true
		if ctx := e.Context(); ctx != nil {
			ctx.App().Update(e.MarkDirty)
		}
	}
	return f
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

// syntaxStyle is the look a cluster's highlight style adds over the text: its
// own, or — unset — the Normal style's, which is also what uncovered text
// wears. Neither set, the text's own.
func (e *Editor) syntaxStyle(k highlight.Style) (style.Style, bool) {
	if int(k) >= len(e.syntax) {
		k = highlight.Normal
	}
	if st := e.syntax[k]; st != (style.Style{}) {
		return st, true
	}
	st := e.syntax[highlight.Normal]
	return st, st != (style.Style{})
}

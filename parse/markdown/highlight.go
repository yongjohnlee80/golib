package markdown

import (
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
)

// Highlighter colours Markdown one line at a time, for an editor and a
// preview: CommonMark with GitHub's and Obsidian's extensions.
//
// A LINE IS PARSED BY THIS PACKAGE, alone: what the parser finds in it —
// a heading, emphasis, a code span, a link, a wikilink, a tag — is what is
// coloured, so a line reads as the parser reads it. What only a whole document
// shows is carried from line to line as the state: the frontmatter at the top,
// a fenced code block, an HTML comment. What needs lines BELOW it (a setext
// heading's underline, a paragraph's lazy continuation) is not seen; a line
// is highlighted as it stands.
//
//	a heading                      Keyword
//	**strong**, *emphasis*          DataType, Attribute
//	~~strikethrough~~               Comment
//	`code`, a fenced block          VerbatimString (its fences Preprocessor)
//	[link](url), ![image](src)      String; <autolink> Import
//	[[wikilink]], ![[embed]]        Function
//	#tag                            Constant
//	a list marker, a task box, >    Operator
//	> [!note] a callout's title     Information ([!warning] and kin: Warning)
//	---, ***                        RegionMarker
//	[label]: url                    Import
//	frontmatter                     Documentation, its keys Attribute
//	<!-- comment -->                Comment
//	inline HTML, an HTML block      Preprocessor
func Highlighter() highlight.Highlighter {
	return highlight.HighlighterFunc(highlightMarkdown)
}

// States carried between lines. Zero is the first line, the only one that may
// open frontmatter; every later line carries at least stateBody.
const (
	stateStart highlight.State = iota
	stateBody
	stateFrontmatter
	stateComment
	// stateFence and above is inside a fenced code block: the fence's length
	// and character are its offset from here.
	stateFence highlight.State = 16
)

func fenceState(ch byte, n int) highlight.State {
	st := stateFence + highlight.State(n<<1)
	if ch == '~' {
		st++
	}
	return st
}

func fenceOf(st highlight.State) (byte, int) {
	v := int(st - stateFence)
	ch := byte('`')
	if v&1 == 1 {
		ch = '~'
	}
	return ch, v >> 1
}

// line is one line's styles, a byte at a time, before they are spans.
type mdLine struct {
	text   string
	styles []highlight.Style
}

func (l *mdLine) paint(start, end int, st highlight.Style) {
	start, end = max(start, 0), min(end, len(l.styles))
	for i := start; i < end; i++ {
		l.styles[i] = st
	}
}

// spans are the painted runs, in order, disjoint; Normal is left unwritten.
func (l *mdLine) spans() []highlight.Span {
	var out []highlight.Span
	for i := 0; i < len(l.styles); {
		j := i
		for j < len(l.styles) && l.styles[j] == l.styles[i] {
			j++
		}
		if l.styles[i] != highlight.Normal {
			out = append(out, highlight.Span{Start: i, End: j, Style: l.styles[i]})
		}
		i = j
	}
	return out
}

func highlightMarkdown(text string, previous highlight.State) ([]highlight.Span, highlight.State) {
	l := &mdLine{text: text, styles: make([]highlight.Style, len(text))}
	trimmed := strings.TrimSpace(text)
	switch {
	case previous == stateFrontmatter:
		if trimmed == "---" || trimmed == "..." {
			l.paint(0, len(text), highlight.RegionMarker)
			return l.spans(), stateBody
		}
		l.paint(0, len(text), highlight.Documentation)
		if k := strings.IndexByte(text, ':'); k > 0 && !strings.HasPrefix(strings.TrimLeft(text, " "), "-") {
			l.paint(0, k, highlight.Attribute)
		}
		return l.spans(), stateFrontmatter
	case previous >= stateFence:
		ch, n := fenceOf(previous)
		if fenceCloses(text, ch, n) {
			l.paint(0, len(text), highlight.Preprocessor)
			return l.spans(), stateBody
		}
		l.paint(0, len(text), highlight.VerbatimString)
		return l.spans(), previous
	case previous == stateComment:
		end := strings.Index(text, "-->")
		if end < 0 {
			l.paint(0, len(text), highlight.Comment)
			return l.spans(), stateComment
		}
		l.paint(0, end+3, highlight.Comment)
		return l.rest(text[end+3:], end+3)
	case previous == stateStart && trimmed == "---":
		l.paint(0, len(text), highlight.RegionMarker)
		return l.spans(), stateFrontmatter
	}
	if ch, n, ok := fenceOpens(text); ok {
		l.paint(0, len(text), highlight.Preprocessor)
		return l.spans(), fenceState(ch, n)
	}
	return l.rest(text, 0)
}

// rest highlights text, which starts at byte off of the line, as a line of
// the body: an HTML comment opened in it and left open is carried on.
func (l *mdLine) rest(text string, off int) ([]highlight.Span, highlight.State) {
	if open := strings.Index(text, "<!--"); open >= 0 && !strings.Contains(text[open+4:], "-->") {
		l.inline(text[:open], off)
		l.paint(off+open, off+len(text), highlight.Comment)
		return l.spans(), stateComment
	}
	l.inline(text, off)
	return l.spans(), stateBody
}

// inline parses text as a document of its own and paints what the parser
// found in it.
func (l *mdLine) inline(text string, off int) {
	if strings.TrimSpace(text) == "" {
		return
	}
	// "---" alone is a rule in the body; to the Obsidian extension, a document
	// that starts with it opens frontmatter.
	if t := strings.TrimSpace(text); t == "---" || t == "***" || t == "___" {
		l.paint(off, off+len(text), highlight.RegionMarker)
		return
	}
	doc := Parse([]byte(text), GFM(), Obsidian())
	l.walk(doc.Root, off)
}

// walk paints n, then its children over it, so the innermost construct is
// the colour a byte ends up with.
func (l *mdLine) walk(n *Node, off int) {
	start, end := off+n.Span.Start, off+n.Span.End
	switch n.Kind {
	case KindHeading:
		l.paint(start, end, highlight.Keyword)
	case KindStrong:
		l.paint(start, end, highlight.DataType)
	case KindEmph:
		l.paint(start, end, highlight.Attribute)
	case KindStrikethrough:
		l.paint(start, end, highlight.Comment)
	case KindCodeSpan, KindCodeBlock:
		l.paint(start, end, highlight.VerbatimString)
	case KindLink, KindImage:
		l.paint(start, end, highlight.String)
	case KindAutolink:
		l.paint(start, end, highlight.Import)
	case KindWikilink, KindEmbed:
		l.paint(start, end, highlight.Function)
	case KindTag:
		l.paint(start, end, highlight.Constant)
	case KindThematicBreak:
		l.paint(start, end, highlight.RegionMarker)
	case KindLinkRefDef:
		l.paint(start, end, highlight.Import)
	case KindRawHTML, KindHTMLBlock:
		l.paint(start, end, highlight.Preprocessor)
	case KindItem:
		// the marker, a task box included: from the item's start to its content
		if c := n.FirstChild; c != nil && c.Span.Start > n.Span.Start {
			l.paint(start, off+c.Span.Start, highlight.Operator)
		} else if c == nil {
			l.paint(start, end, highlight.Operator)
		}
	case KindBlockQuote:
		// the marker is the ">" and the space after it; a callout's [!type]
		// after that is its title's
		gt := strings.IndexByte(l.text[start:end], '>')
		if gt < 0 {
			break
		}
		mark := start + gt + 1
		if mark < end && l.text[mark] == ' ' {
			mark++
		}
		l.paint(start, mark, highlight.Operator)
		if n.Callout != nil {
			if c := n.FirstChild; c != nil && off+c.Span.Start > mark {
				l.paint(mark, off+c.Span.Start, calloutStyle(n))
			}
		}
	case KindCalloutTitle:
		l.paint(start, end, calloutStyle(n.Parent))
	}
	for c := n.FirstChild; c != nil; c = c.Next {
		l.walk(c, off)
	}
}

// calloutStyle is a callout's colour: Warning for the kinds Obsidian draws as
// one, Information for the rest.
func calloutStyle(quote *Node) highlight.Style {
	if quote != nil && quote.Callout != nil && warningCallout(string(quote.Callout.Type)) {
		return highlight.Warning
	}
	return highlight.Information
}

// warningCallout is a callout type Obsidian draws as a warning.
func warningCallout(t string) bool {
	switch strings.ToLower(t) {
	case "warning", "caution", "attention", "danger", "error", "bug", "failure", "fail", "missing":
		return true
	}
	return false
}

// fenceOpens reports a line that opens a fenced code block: up to three
// spaces, then three or more backticks or tildes (a backtick fence's info
// string holds no backtick).
func fenceOpens(text string) (byte, int, bool) {
	s := strings.TrimLeft(text, " ")
	if len(text)-len(s) > 3 || len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return 0, 0, false
	}
	ch := s[0]
	n := 0
	for n < len(s) && s[n] == ch {
		n++
	}
	if n < 3 || (ch == '`' && strings.IndexByte(s[n:], '`') >= 0) {
		return 0, 0, false
	}
	return ch, n, true
}

// fenceCloses reports a line that closes a fence of ch, n long: up to three
// spaces, at least n of ch, then only spaces.
func fenceCloses(text string, ch byte, n int) bool {
	s := strings.TrimLeft(text, " ")
	if len(text)-len(s) > 3 {
		return false
	}
	k := 0
	for k < len(s) && s[k] == ch {
		k++
	}
	return k >= n && strings.TrimSpace(s[k:]) == ""
}

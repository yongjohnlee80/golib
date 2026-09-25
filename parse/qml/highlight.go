package qml

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse"
)

// HIGHLIGHTING — QML, with the JavaScript of its bindings and handlers, one
// line at a time, for an editor.
//
// It is beside the parser, not through it. The parser REFUSES invalid input,
// and text being typed is invalid most of the time; a highlighter must colour
// "Text { text: " as well as a finished file. So it never refuses: a comment
// or template string left open is a State carried to the next line, a string
// left open ends at the line, and anything it does not recognise is Normal.
//
// Styles are KSyntaxHighlighting's:
//
//	import, property, signal, function…       Keyword
//	if, for, return, switch…                    ControlFlow
//	the module of an import line                Import
//	"…"  '…'  `…`                               String (escapes SpecialChar)
//	42  3.5  0x1f                               DecVal, Float, BaseN
//	// …  /* … */                               Comment (TODO, FIXME: Alert)
//	true, false, null, undefined                Constant
//	int, string, bool, real…  Rectangle         DataType
//	text:  onClicked:                           Attribute
//	App.save(                                   Function
//	+ - = < ? : …                               Operator

// States carried between lines.
const (
	stateNone highlight.State = iota
	stateBlockComment
	stateTemplate
)

// Highlighter returns the QML highlighter. It holds no state of its own, so
// one serves any number of editors.
func Highlighter() highlight.Highlighter {
	return highlight.HighlighterFunc(highlightLine)
}

var (
	keywords = set(
		// QML
		"import", "as", "property", "alias", "readonly", "signal", "default", "required",
		"component", "enum", "pragma", "on",
		// JavaScript
		"var", "let", "const", "function", "new", "delete", "typeof", "instanceof", "in", "of",
		"this", "class", "extends", "super", "yield", "async", "await", "void", "with", "debugger")
	controlFlow = set("if", "else", "for", "while", "do", "switch", "case", "break",
		"continue", "return", "try", "catch", "finally", "throw")
	constants = set("true", "false", "null", "undefined", "NaN", "Infinity")
	dataTypes = set("bool", "double", "int", "list", "real", "string", "url", "color",
		"date", "point", "rect", "size", "font", "enumeration")
	alerts = []string{"TODO", "FIXME", "XXX", "HACK"}
)

func set(words ...string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}

// line is one line being highlighted: its scanner, and the spans so far.
type line struct {
	text  string
	sc    *parse.Scanner
	spans []highlight.Span
	// importLine is set once the line's first word is `import`: what follows,
	// up to the version, is the module.
	importLine bool
	firstWord  bool
}

func (l *line) at() int { return l.sc.Pos().Offset }

func (l *line) span(start, end int, st highlight.Style) {
	if end > start {
		l.spans = append(l.spans, highlight.Span{Start: start, End: end, Style: st})
	}
}

// toEnd advances to the end of the line.
func (l *line) toEnd() {
	for !l.sc.Done() {
		l.sc.Next()
	}
}

func highlightLine(text string, previous highlight.State) ([]highlight.Span, highlight.State) {
	l := &line{text: text, sc: parse.NewScanner([]byte(text)), firstWord: true}
	state := stateNone
	switch previous {
	case stateBlockComment:
		if !l.blockComment(0) {
			return l.spans, stateBlockComment
		}
	case stateTemplate:
		if !l.quoted(0, '`') {
			return l.spans, stateTemplate
		}
	}
	for !l.sc.Done() {
		start := l.at()
		r, _ := l.sc.Next()
		switch {
		case unicode.IsSpace(r):
		case r == '/' && l.sc.Take("/"):
			l.toEnd()
			l.comment(start, l.at())
		case r == '/' && l.sc.Take("*"):
			if !l.blockComment(start) {
				state = stateBlockComment
			}
		case r == '"' || r == '\'':
			l.quoted(start, r)
		case r == '`':
			if !l.quoted(start, '`') {
				state = stateTemplate
			}
		case r >= '0' && r <= '9' || r == '.' && l.digitNext():
			l.number(start, r)
		case r == '_' || r == '$' || unicode.IsLetter(r):
			l.word(start)
		case strings.ContainsRune("+-*/%=<>!&|^~?:,;.", r):
			l.operator(start)
		default:
			// Punctuation — braces, brackets, parentheses — and anything else:
			// Normal.
		}
	}
	return l.spans, state
}

// blockComment colours from start through the closing `*/`, and reports
// whether it closed on this line.
func (l *line) blockComment(start int) bool {
	for !l.sc.Done() {
		if l.sc.Take("*/") {
			l.comment(start, l.at())
			return true
		}
		l.sc.Next()
	}
	l.comment(start, l.at())
	return false
}

// comment colours [start,end) as a comment, with TODO and its kind as Alert.
func (l *line) comment(start, end int) {
	body := l.text[start:end]
	at := 0
	for {
		i, word := -1, ""
		for _, a := range alerts {
			if j := strings.Index(body[at:], a); j >= 0 && (i < 0 || j < i) {
				i, word = j, a
			}
		}
		if i < 0 {
			l.span(start+at, end, highlight.Comment)
			return
		}
		l.span(start+at, start+at+i, highlight.Comment)
		l.span(start+at+i, start+at+i+len(word), highlight.Alert)
		at += i + len(word)
	}
}

// quoted colours a string opened at start by quote, escapes as SpecialChar,
// through its closing quote or the end of the line; it reports whether it
// closed.
func (l *line) quoted(start int, quote rune) bool {
	from := start
	for !l.sc.Done() {
		at := l.at()
		r, _ := l.sc.Next()
		switch r {
		case '\\':
			l.span(from, at, highlight.String)
			if !l.sc.Done() {
				l.sc.Next()
			}
			l.span(at, l.at(), highlight.SpecialChar)
			from = l.at()
		case quote:
			l.span(from, l.at(), highlight.String)
			return true
		}
	}
	l.span(from, l.at(), highlight.String)
	return false
}

func (l *line) digitNext() bool {
	r, ok := l.sc.Peek()
	return ok && r >= '0' && r <= '9'
}

// number colours a numeric literal whose first character was first.
func (l *line) number(start int, first rune) {
	style := highlight.DecVal
	if first == '0' {
		if r, ok := l.sc.Peek(); ok && strings.ContainsRune("xXbBoO", r) {
			l.sc.Next()
			style = highlight.BaseN
		}
	}
	if first == '.' {
		style = highlight.Float
	}
	for !l.sc.Done() {
		r, _ := l.sc.Peek()
		switch {
		case style == highlight.BaseN && (unicode.IsDigit(r) || strings.ContainsRune("abcdefABCDEF_", r)):
		case unicode.IsDigit(r) || r == '_':
		case r == '.' && style == highlight.DecVal:
			style = highlight.Float
		case (r == 'e' || r == 'E') && style != highlight.BaseN:
			style = highlight.Float
			l.sc.Next()
			if r, ok := l.sc.Peek(); ok && (r == '+' || r == '-') {
				l.sc.Next()
			}
			continue
		default:
			l.span(start, l.at(), style)
			return
		}
		l.sc.Next()
	}
	l.span(start, l.at(), style)
}

// word colours an identifier or keyword that began at start.
func (l *line) word(start int) {
	for !l.sc.Done() {
		r, _ := l.sc.Peek()
		if r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		l.sc.Next()
	}
	w := l.text[start:l.at()]
	first := l.firstWord
	l.firstWord = false
	if first && w == "import" {
		l.importLine = true
		l.span(start, l.at(), highlight.Keyword)
		l.module()
		return
	}
	next := l.nextNonSpace()
	switch {
	case controlFlow[w]:
		l.span(start, l.at(), highlight.ControlFlow)
	case keywords[w]:
		l.span(start, l.at(), highlight.Keyword)
	case constants[w]:
		l.span(start, l.at(), highlight.Constant)
	// A name before `:` is a property, even one spelled like a type —
	// `color: "red"` sets the property color.
	case next == ':' && !strings.HasPrefix(l.text[l.afterSpace():], "::"):
		l.span(start, l.at(), highlight.Attribute)
	case dataTypes[w]:
		l.span(start, l.at(), highlight.DataType)
	case next == '(':
		l.span(start, l.at(), highlight.Function)
	case isUpper(w):
		l.span(start, l.at(), highlight.DataType)
	}
}

// module colours an import line's module — dotted words, or a quoted path —
// through to its version or `as`.
func (l *line) module() {
	for !l.sc.Done() {
		start := l.at()
		r, _ := l.sc.Next()
		switch {
		case unicode.IsSpace(r):
		case r == '"' || r == '\'':
			l.quoted(start, r)
		case r == '_' || unicode.IsLetter(r) || r == '.':
			for !l.sc.Done() {
				r, _ := l.sc.Peek()
				if r != '_' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					break
				}
				l.sc.Next()
			}
			if l.text[start:l.at()] == "as" {
				l.span(start, l.at(), highlight.Keyword)
				continue
			}
			l.span(start, l.at(), highlight.Import)
		case r >= '0' && r <= '9':
			l.number(start, r)
		default:
			return
		}
	}
}

func (l *line) afterSpace() int {
	i := l.at()
	for i < len(l.text) && (l.text[i] == ' ' || l.text[i] == '\t') {
		i++
	}
	return i
}

func (l *line) nextNonSpace() rune {
	i := l.afterSpace()
	if i >= len(l.text) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.text[i:])
	return r
}

// operator colours a run of operator characters that began at start.
func (l *line) operator(start int) {
	for !l.sc.Done() {
		r, _ := l.sc.Peek()
		if !strings.ContainsRune("+-*=<>!&|^~?%", r) {
			break
		}
		l.sc.Next()
	}
	l.span(start, l.at(), highlight.Operator)
}

func isUpper(w string) bool {
	r, _ := utf8.DecodeRuneInString(w)
	return unicode.IsUpper(r)
}

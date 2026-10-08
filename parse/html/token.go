// Package html tokenizes HTML and builds a small tree from it, with the standard library alone.
//
// It is a bounded reader for documents a program wants the content of, not a browser's tree
// builder: tags, attributes, text, comments and doctypes; raw-text elements (script, style, and
// the escapable textarea and title); entities, decoded by the standard library's html package;
// void elements; and the common implied end tags (p, li, dt, dd, tr, td, th, option, optgroup,
// thead, tbody, tfoot). Misnested formatting is closed at the first end tag that names an open
// element, not repaired as a browser would.
//
// Every token and node carries its byte span in the source. Every limit (Limits) refuses with
// ErrTooLarge: nothing is dropped or cut to fit, since a dropped attribute could be the one that
// hides an element's text.
package html

import (
	"bytes"
	"errors"
	stdhtml "html"
	"strings"
)

// TokenKind is what a token is.
type TokenKind uint8

const (
	// Text is character data, entities decoded (but a script's or style's, which is raw).
	Text TokenKind = iota
	// StartTag opens an element.
	StartTag
	// EndTag closes one.
	EndTag
	// SelfClosing is a tag written <name/>: an element with no content.
	SelfClosing
	// Comment is <!-- … -->, or a bogus comment (<? … >, </ … > not naming a tag).
	Comment
	// Doctype is <!DOCTYPE …> or any other <! … > declaration.
	Doctype
)

// Attr is one attribute: its name lower-cased, its value entity-decoded.
type Attr struct{ Name, Value string }

// Token is one token of the source.
type Token struct {
	Kind  TokenKind
	Name  string // a tag's, lower-cased
	Attrs []Attr // a start or self-closing tag's, first of each name kept
	Data  string // Text, Comment and Doctype
	Span  [2]int // its bytes in the source, [start, end)
}

// Limits bound a tokenization and a parse. A zero field is its default (DefaultLimits).
type Limits struct {
	MaxSource int // bytes of source (default 32 MiB)
	MaxNodes  int // nodes in a parse's tree (default 1 << 20)
	MaxAttrs  int // attributes on one tag (default 64)
	MaxAttr   int // bytes of one attribute value, as written (default 64 KiB)
	MaxDepth  int // a parse's nesting; deeper elements are flattened (default 256)
}

// DefaultLimits are the limits a zero Limits field takes.
var DefaultLimits = Limits{MaxSource: 32 << 20, MaxNodes: 1 << 20, MaxAttrs: 64, MaxAttr: 64 << 10, MaxDepth: 256}

// ErrTooLarge is a limit passed: the source, the node count, a tag's attribute count or one
// value's length.
var ErrTooLarge = errors.New("html: over a parse limit")

func (l Limits) withDefaults() Limits {
	d := DefaultLimits
	if l.MaxSource > 0 {
		d.MaxSource = l.MaxSource
	}
	if l.MaxNodes > 0 {
		d.MaxNodes = l.MaxNodes
	}
	if l.MaxAttrs > 0 {
		d.MaxAttrs = l.MaxAttrs
	}
	if l.MaxAttr > 0 {
		d.MaxAttr = l.MaxAttr
	}
	if l.MaxDepth > 0 {
		d.MaxDepth = l.MaxDepth
	}
	return d
}

// rawText are the elements whose content is text up to their own end tag; escapable decodes
// entities in it.
var rawText = map[string]bool{"script": true, "style": true, "textarea": true, "title": true}

func escapable(name string) bool { return name == "textarea" || name == "title" }

// Tokenizer reads a source one token at a time.
type Tokenizer struct {
	src []byte
	pos int
	lim Limits
	raw string // the raw-text element just opened: its content comes next
	err error
}

// NewTokenizer reads src under l (a zero Limits is DefaultLimits). A source over MaxSource
// yields no tokens, and Err is ErrTooLarge.
func NewTokenizer(src []byte, l Limits) *Tokenizer {
	t := &Tokenizer{src: src, lim: l.withDefaults()}
	if len(src) > t.lim.MaxSource {
		t.err = ErrTooLarge
	}
	return t
}

// Err is why Next stopped early: nil at the source's end.
func (t *Tokenizer) Err() error { return t.err }

// Next is the next token; false at the end of the source or on an error (Err).
func (t *Tokenizer) Next() (Token, bool) {
	if t.err != nil || t.pos >= len(t.src) {
		return Token{}, false
	}
	if t.raw != "" {
		name := t.raw
		t.raw = ""
		if tok, ok := t.rawContent(name); ok {
			return tok, true
		}
	}
	for t.pos < len(t.src) && t.src[t.pos] == '<' {
		if t.pos+2 < len(t.src) && t.src[t.pos+1] == '/' && t.src[t.pos+2] == '>' {
			t.pos += 3 // "</>" is nothing
			continue
		}
		tok, ok := t.markup()
		if ok || t.err != nil {
			return tok, ok
		}
		break
	}
	if t.pos >= len(t.src) {
		return Token{}, false
	}
	return t.text(), true
}

// text is character data up to the next '<' that starts markup.
func (t *Tokenizer) text() Token {
	start := t.pos
	i := t.pos + 1
	for i < len(t.src) {
		if t.src[i] == '<' && startsMarkup(t.src[i:]) {
			break
		}
		i++
	}
	t.pos = i
	return Token{Kind: Text, Data: stdhtml.UnescapeString(string(t.src[start:i])), Span: [2]int{start, i}}
}

// startsMarkup reports whether b, beginning at '<', opens a tag, a comment or a declaration.
func startsMarkup(b []byte) bool {
	if len(b) < 2 {
		return false
	}
	c := b[1]
	return isLetter(c) || c == '!' || c == '?' || (c == '/' && len(b) > 2)
}

// markup reads the construct at '<'; false means it is text after all.
func (t *Tokenizer) markup() (Token, bool) {
	b := t.src[t.pos:]
	start := t.pos
	switch {
	case len(b) < 2:
		return Token{}, false
	case strings.HasPrefix(string(b[:min(len(b), 4)]), "<!--"):
		end := indexFrom(t.src, "-->", t.pos+4)
		dataEnd, next := end, end+3
		if end < 0 {
			dataEnd, next = len(t.src), len(t.src)
		}
		t.pos = next
		return Token{Kind: Comment, Data: string(t.src[start+4 : dataEnd]), Span: [2]int{start, next}}, true
	case b[1] == '!':
		return t.declaration(start, Doctype), true
	case b[1] == '?':
		return t.declaration(start, Comment), true
	case b[1] == '/':
		if len(b) > 2 && isLetter(b[2]) {
			return t.tag(start, true)
		}
		return t.declaration(start, Comment), true
	case isLetter(b[1]):
		return t.tag(start, false)
	}
	return Token{}, false
}

// declaration reads <! … > or a bogus comment up to the next '>'.
func (t *Tokenizer) declaration(start int, k TokenKind) Token {
	end := indexFrom(t.src, ">", start+2)
	dataEnd, next := end, end+1
	if end < 0 {
		dataEnd, next = len(t.src), len(t.src)
	}
	t.pos = next
	return Token{Kind: k, Data: strings.TrimSpace(string(t.src[start+2 : dataEnd])), Span: [2]int{start, next}}
}

// tag reads a start or end tag whose name starts at start+1 (start+2 for an end tag).
func (t *Tokenizer) tag(start int, end bool) (Token, bool) {
	i := start + 1
	if end {
		i++
	}
	n := i
	for n < len(t.src) && !isSpace(t.src[n]) && t.src[n] != '/' && t.src[n] != '>' {
		n++
	}
	tok := Token{Kind: StartTag, Name: strings.ToLower(string(t.src[i:n]))}
	if end {
		tok.Kind = EndTag
	}
	i = n
	for i < len(t.src) {
		c := t.src[i]
		switch {
		case c == '>':
			i++
			goto done
		case c == '/' && i+1 < len(t.src) && t.src[i+1] == '>':
			if !end {
				tok.Kind = SelfClosing
			}
			i += 2
			goto done
		case isSpace(c) || c == '/':
			i++
		default:
			var a Attr
			var ok bool
			a, i, ok = t.attr(i)
			if !ok {
				return Token{}, false
			}
			if end || hasAttr(tok.Attrs, a.Name) {
				continue
			}
			if len(tok.Attrs) == t.lim.MaxAttrs {
				t.err = ErrTooLarge
				return Token{}, false
			}
			tok.Attrs = append(tok.Attrs, a)
		}
	}
done:
	t.pos = i
	tok.Span = [2]int{start, i}
	if tok.Kind == StartTag && rawText[tok.Name] {
		t.raw = tok.Name
	}
	return tok, true
}

// attr reads one attribute at i: its name, and a value after '=' (quoted or not).
func (t *Tokenizer) attr(i int) (Attr, int, bool) {
	n := i
	for n < len(t.src) && !isSpace(t.src[n]) && t.src[n] != '/' && t.src[n] != '>' && (t.src[n] != '=' || n == i) {
		n++
	}
	a := Attr{Name: strings.ToLower(string(t.src[i:n]))}
	j := n
	for j < len(t.src) && isSpace(t.src[j]) {
		j++
	}
	if j >= len(t.src) || t.src[j] != '=' {
		return a, n, true
	}
	j++
	for j < len(t.src) && isSpace(t.src[j]) {
		j++
	}
	var vs, ve, next int
	if j < len(t.src) && (t.src[j] == '"' || t.src[j] == '\'') {
		q := t.src[j]
		vs = j + 1
		ve = vs
		for ve < len(t.src) && t.src[ve] != q {
			ve++
		}
		next = min(ve+1, len(t.src))
	} else {
		vs = j
		ve = j
		for ve < len(t.src) && !isSpace(t.src[ve]) && t.src[ve] != '>' {
			ve++
		}
		next = ve
	}
	if ve-vs > t.lim.MaxAttr {
		t.err = ErrTooLarge
		return Attr{}, next, false
	}
	a.Value = stdhtml.UnescapeString(string(t.src[vs:ve]))
	return a, next, true
}

// rawContent is a raw-text element's content: everything up to "</name" ending the element.
// false when the content is empty (the end tag follows at once).
func (t *Tokenizer) rawContent(name string) (Token, bool) {
	start := t.pos
	i := start
	for {
		j := indexFold(t.src, "</"+name, i)
		if j < 0 {
			i = len(t.src)
			break
		}
		if k := j + 2 + len(name); k >= len(t.src) || isSpace(t.src[k]) || t.src[k] == '/' || t.src[k] == '>' {
			i = j
			break
		}
		i = j + 1
	}
	if i == start {
		return Token{}, false
	}
	t.pos = i
	data := string(t.src[start:i])
	if escapable(name) {
		data = stdhtml.UnescapeString(data)
	}
	return Token{Kind: Text, Data: data, Span: [2]int{start, i}}, true
}

func hasAttr(as []Attr, name string) bool {
	for _, a := range as {
		if a.Name == name {
			return true
		}
	}
	return false
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// indexFrom is the index of sep in b at or after from, or -1.
func indexFrom(b []byte, sep string, from int) int {
	if from > len(b) {
		return -1
	}
	if i := bytes.Index(b[from:], []byte(sep)); i >= 0 {
		return from + i
	}
	return -1
}

// indexFold is indexFrom, ignoring ASCII case.
func indexFold(b []byte, sep string, from int) int {
	for i := from; i+len(sep) <= len(b); i++ {
		if b[i] == '<' && strings.EqualFold(string(b[i:i+len(sep)]), sep) {
			return i
		}
	}
	return -1
}

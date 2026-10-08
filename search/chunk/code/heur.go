package code

import (
	"strings"
)

// src is a source with its scan: navigation that sees only code, so a bracket or keyword inside a
// string or comment is never read as structure.
type src struct {
	b []byte
	m []byte
}

func newSrc(b []byte, s syntax) src { return src{b: b, m: scan(b, s)} }

func (t src) code(i int) bool { return i >= 0 && i < len(t.b) && t.m[i] == inCode }

// skip moves past whitespace and comments.
func (t src) skip(i int) int {
	for i < len(t.b) && (isSpace(t.b[i]) || t.m[i] == inComment) {
		i++
	}
	return i
}

// skipInline moves past spaces, tabs and comments, not newlines.
func (t src) skipInline(i int) int {
	for i < len(t.b) && (t.b[i] == ' ' || t.b[i] == '\t' || t.b[i] == '\r' || (t.m[i] == inComment)) {
		i++
	}
	return i
}

// word is the identifier at i (in code), and the offset past it.
func (t src) word(i int) (string, int) {
	if !t.code(i) || !isWordByte(t.b[i]) {
		return "", i
	}
	e := i
	for e < len(t.b) && t.code(e) && isWordByte(t.b[e]) {
		e++
	}
	return string(t.b[i:e]), e
}

// is reports whether the code at i is the byte c.
func (t src) is(i int, c byte) bool { return t.code(i) && t.b[i] == c }

// match is the offset past the bracket closing the one at i, counting only code brackets; the end
// of the source when it never closes.
func (t src) match(i int) int {
	depth := 0
	for k := i; k < len(t.b); k++ {
		if !t.code(k) {
			continue
		}
		switch t.b[k] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return k + 1
			}
		}
	}
	return len(t.b)
}

// findCode is the first offset in [i, end) holding one of set in code at bracket depth 0 relative
// to i (brackets opened after i are skipped whole), or -1.
func (t src) findCode(i, end int, set string) int {
	for k := i; k < end && k < len(t.b); k++ {
		if !t.code(k) {
			continue
		}
		c := t.b[k]
		if strings.IndexByte(set, c) >= 0 {
			return k
		}
		if c == '(' || c == '[' || c == '{' {
			k = t.match(k) - 1
		}
	}
	return -1
}

// statementEnd is where a statement starting at i ends: past a ";" at depth 0, or at the end of a
// line at depth 0 whose last code is not an operator that continues it onto the next line.
func (t src) statementEnd(i int) int {
	for k := i; k < len(t.b); k++ {
		if !t.code(k) {
			continue
		}
		switch c := t.b[k]; c {
		case ';':
			return k + 1
		case '(', '[', '{':
			k = t.match(k) - 1
		case '\n':
			if !continues(t.lastCodeBefore(k)) && !leads(t.nextCodeByte(k)) {
				return k
			}
		}
	}
	return len(t.b)
}

func continues(c byte) bool { return strings.IndexByte("=|&,(<:?+-*/.!%^~[{", c) >= 0 }
func leads(c byte) bool     { return strings.IndexByte("|&.?:)]}", c) >= 0 && c != 0 }

func (t src) lastCodeBefore(k int) byte {
	for j := k - 1; j >= 0; j-- {
		if t.code(j) && !isSpace(t.b[j]) {
			return t.b[j]
		}
	}
	return 0
}

func (t src) nextCodeByte(k int) byte {
	j := t.skip(k)
	if j < len(t.b) && t.code(j) {
		return t.b[j]
	}
	return 0
}

// lineCuts are the offsets in (from, to) where a line's first non-blank byte sits at the bracket
// depth of from: the statements of a block whose body starts at from, as places to cut it.
func (t src) lineCuts(from, to int) []int {
	var cuts []int
	depth := 0
	atLineStart := false
	for k := from; k < to && k < len(t.b); k++ {
		c := t.b[k]
		if c == '\n' {
			atLineStart = true
			continue
		}
		if atLineStart && !isSpace(c) {
			atLineStart = false
			if depth == 0 && (t.code(k) || t.m[k] == inComment) {
				cuts = append(cuts, k)
			}
		}
		if t.code(k) {
			switch c {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			}
		}
	}
	return cuts
}

// summary is what a later fragment of a split unit embeds besides the signature: its comments and
// the identifiers it declares and calls.
func (t src) summary(s, e int, declKeywords ...string) string {
	var comments []string
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if n != "" && !seen[n] && !isKeyword(n) {
			seen[n] = true
			names = append(names, n)
		}
	}
	for k := s; k < e && k < len(t.b); {
		switch {
		case t.m[k] == inComment:
			j := k
			for j < e && t.m[j] == inComment {
				j++
			}
			comments = append(comments, cleanComment(string(t.b[k:j])))
			k = j
		case t.code(k) && isWordByte(t.b[k]) && (k == 0 || !isWordByte(t.b[k-1])):
			w, j := t.word(k)
			n := t.skipInline(j)
			if t.is(n, '(') {
				add(w)
			}
			for _, kw := range declKeywords {
				if w == kw {
					if name, _ := t.word(t.skipInline(j)); name != "" {
						add(name)
					}
				}
			}
			k = j
		default:
			k++
		}
	}
	return joinNonEmpty(strings.Join(comments, "\n"), strings.Join(names, " "))
}

func isKeyword(w string) bool {
	switch w {
	case "if", "for", "while", "switch", "match", "return", "catch", "function", "fn", "loop", "def", "elif", "else", "with", "await", "new", "typeof", "in", "not", "and", "or":
		return true
	}
	return false
}

// cleanComment strips comment markers: //, ///, //!, #, /* */, /** */ and the leading * of a
// block comment's lines.
func cleanComment(c string) string {
	var lines []string
	for _, l := range strings.Split(c, "\n") {
		l = strings.TrimSpace(l)
		for _, p := range []string{"/**", "/*!", "/*", "*/", "///", "//!", "//", "#"} {
			l = strings.TrimPrefix(l, p)
		}
		l = strings.TrimSuffix(l, "*/")
		l = strings.TrimPrefix(strings.TrimSpace(l), "* ")
		if l = strings.TrimSpace(strings.TrimPrefix(l, "*")); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// prefix is a declaration's leading comments and attributes: the comments directly above it (no
// blank line between them and what follows) and, through attr, the attributes or decorators
// between. It returns where the declaration's span starts, the doc text, and where its code starts.
// A comment separated from the declaration by a blank line is not its doc: it stays outside the
// unit, as glue.
func (t src) prefix(i int, attr func(int) int, isDoc func(string) bool) (start int, doc string, code int) {
	start = -1
	var docs []string
	k := i
	for {
		ws := k
		newlines := 0
		for k < len(t.b) && isSpace(t.b[k]) {
			if t.b[k] == '\n' {
				newlines++
			}
			k++
		}
		if newlines >= 2 && start >= 0 {
			start, docs = -1, nil // a blank line: what came before is not this declaration's
		}
		_ = ws
		if k >= len(t.b) {
			break
		}
		if t.m[k] == inComment {
			j := k
			for j < len(t.b) && t.m[j] == inComment {
				j++
			}
			if start < 0 {
				start = k
			}
			if text := string(t.b[k:j]); isDoc(text) {
				docs = append(docs, cleanComment(text))
			}
			k = j
			continue
		}
		if attr != nil {
			if j := attr(k); j > k {
				if start < 0 {
					start = k
				}
				k = j
				continue
			}
		}
		break
	}
	if start < 0 {
		start = k
	}
	return start, strings.Join(docs, "\n"), k
}

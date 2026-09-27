package markdown

import "bytes"

// htmlTag returns the length of an open or closing tag at the start of b, or 0. Whitespace inside a
// tag may include one line ending.
//
// https://spec.commonmark.org/0.31.2/#raw-html
func htmlTag(b []byte) int {
	if len(b) < 3 || b[0] != '<' {
		return 0
	}
	if b[1] == '/' {
		j := tagName(b, 2)
		if j == 2 {
			return 0
		}
		j = skipTagSpace(b, j)
		if j < len(b) && b[j] == '>' {
			return j + 1
		}
		return 0
	}
	j := tagName(b, 1)
	if j == 1 {
		return 0
	}
	for {
		k := skipTagSpace(b, j)
		if k == j { // an attribute needs whitespace before it
			break
		}
		a := attribute(b, k)
		if a == k {
			j = k
			break
		}
		j = a
	}
	j = skipTagSpace(b, j)
	if j < len(b) && b[j] == '/' {
		j++
	}
	if j < len(b) && b[j] == '>' {
		return j + 1
	}
	return 0
}

func tagName(b []byte, i int) int {
	if i >= len(b) || !isASCIIAlpha(b[i]) {
		return i
	}
	j := i + 1
	for j < len(b) && (isASCIIAlnum(b[j]) || b[j] == '-') {
		j++
	}
	return j
}

// skipTagSpace skips spaces and tabs and at most one line ending.
func skipTagSpace(b []byte, i int) int {
	nl := false
	for i < len(b) {
		switch b[i] {
		case ' ', '\t':
		case '\n':
			if nl {
				return i
			}
			nl = true
		default:
			return i
		}
		i++
	}
	return i
}

// attribute scans one attribute (a name, and optionally '=' and a value) at i.
func attribute(b []byte, i int) int {
	if i >= len(b) || !(isASCIIAlpha(b[i]) || b[i] == '_' || b[i] == ':') {
		return i
	}
	j := i + 1
	for j < len(b) && (isASCIIAlnum(b[j]) || b[j] == '_' || b[j] == '.' || b[j] == ':' || b[j] == '-') {
		j++
	}
	k := skipTagSpace(b, j)
	if k >= len(b) || b[k] != '=' {
		return j
	}
	k = skipTagSpace(b, k+1)
	if k >= len(b) {
		return j
	}
	switch q := b[k]; q {
	case '"', '\'':
		e := bytes.IndexByte(b[k+1:], q)
		if e < 0 {
			return j
		}
		return k + 1 + e + 1
	default:
		e := k
		for e < len(b) && !(b[e] == ' ' || b[e] == '\t' || b[e] == '\n' || b[e] == '"' ||
			b[e] == '\'' || b[e] == '=' || b[e] == '<' || b[e] == '>' || b[e] == '`') {
			e++
		}
		if e == k {
			return j
		}
		return e
	}
}

// rawHTML returns the length of inline raw HTML at the start of b: a tag, a comment, a processing
// instruction, a declaration or a CDATA section.
//
// https://spec.commonmark.org/0.31.2/#raw-html
func rawHTML(b []byte) int {
	if n := htmlTag(b); n > 0 {
		return n
	}
	switch {
	case bytes.HasPrefix(b, []byte("<!-->")):
		return 5
	case bytes.HasPrefix(b, []byte("<!--->")):
		return 6
	case bytes.HasPrefix(b, []byte("<!--")):
		if e := bytes.Index(b[4:], []byte("-->")); e >= 0 {
			return 4 + e + 3
		}
	case bytes.HasPrefix(b, []byte("<?")):
		if e := bytes.Index(b[2:], []byte("?>")); e >= 0 {
			return 2 + e + 2
		}
	case bytes.HasPrefix(b, []byte("<![CDATA[")):
		if e := bytes.Index(b[9:], []byte("]]>")); e >= 0 {
			return 9 + e + 3
		}
	case len(b) > 2 && b[1] == '!' && isASCIIAlpha(b[2]):
		if e := bytes.IndexByte(b[2:], '>'); e >= 0 {
			return 2 + e + 1
		}
	}
	return 0
}

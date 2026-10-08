package code

// The heuristic extractors (TypeScript/JavaScript, Python, Rust) find declarations by reading
// keywords and brackets, which is only sound where the bytes are code: a brace in a string, a
// template literal or a comment must never open or close a unit. scan marks every byte as code,
// string or comment once, for the extractors to consult.

const (
	inCode    byte = 0
	inString  byte = 1
	inComment byte = 2
)

// syntax is what the scanner needs to know about a language's strings and comments.
type syntax struct {
	slashComments bool // // and /* */
	hashComments  bool // #
	nestedBlocks  bool // /* /* */ */ nests (Rust)
	backticks     bool // `template ${expr}` (JavaScript)
	regex         bool // /regex/ literals (JavaScript)
	triple        bool // ''' and """ strings, with r/b/u/f prefixes (Python)
	rustStrings   bool // r"..", r#".."#, b"..", and 'c' chars that are not lifetimes
	singleQuotes  bool // '...' is a string
}

func tsSyntax() syntax {
	return syntax{slashComments: true, backticks: true, regex: true, singleQuotes: true}
}

func pySyntax() syntax { return syntax{hashComments: true, triple: true, singleQuotes: true} }

func rustSyntax() syntax { return syntax{slashComments: true, nestedBlocks: true, rustStrings: true} }

// scan returns a mask the length of src: inCode, inString or inComment for every byte. Quote
// characters belong to their string and comment markers to their comment. An unterminated string
// or comment runs to the end of the source.
func scan(src []byte, s syntax) []byte {
	m := make([]byte, len(src))
	// templates is a stack of open template literals' expression depths: inside `${`, code runs
	// until the brace that closes it, and that code may hold strings and templates of its own.
	var templates []int
	depth := 0
	lastCode := byte(0) // the last non-space code byte, for telling a regex from a division
	lastWord := ""
	i := 0
	mark := func(from, to int, kind byte) {
		for k := from; k < to && k < len(src); k++ {
			m[k] = kind
		}
	}
	for i < len(src) {
		c := src[i]
		switch {
		case s.slashComments && c == '/' && i+1 < len(src) && src[i+1] == '/',
			s.hashComments && c == '#':
			e := i
			for e < len(src) && src[e] != '\n' {
				e++
			}
			mark(i, e, inComment)
			i = e
			continue
		case s.slashComments && c == '/' && i+1 < len(src) && src[i+1] == '*':
			e := blockEnd(src, i, s.nestedBlocks)
			mark(i, e, inComment)
			i = e
			continue
		case s.backticks && c == '`':
			e := templateEnd(src, i+1)
			mark(i, e, inString)
			if e < len(src) && e > 0 && src[e-1] == '{' && e >= 2 && src[e-2] == '$' {
				// stopped at "${": code resumes inside the template
				templates = append(templates, depth)
				depth++
			}
			i = e
			lastCode = '`'
			continue
		case s.backticks && c == '}' && len(templates) > 0 && depth-1 == templates[len(templates)-1]:
			// the "}" closing a template expression: the template's text resumes
			depth--
			templates = templates[:len(templates)-1]
			e := templateEnd(src, i+1)
			mark(i, e, inString)
			if e >= 2 && src[e-1] == '{' && src[e-2] == '$' {
				templates = append(templates, depth)
				depth++
			}
			i = e
			lastCode = '`'
			continue
		case s.regex && c == '/' && regexAllowed(lastCode, lastWord):
			if e, ok := regexEnd(src, i+1); ok {
				mark(i, e, inString)
				i = e
				lastCode = '/'
				continue
			}
		case s.triple && (c == '"' || c == '\'') && i+2 < len(src) && src[i+1] == c && src[i+2] == c:
			e := tripleEnd(src, i+3, c)
			mark(prefixStart(src, i), e, inString)
			i = e
			lastCode = c
			continue
		case s.rustStrings && c == 'r' && rawStart(src, i):
			e := rawEnd(src, i)
			mark(i, e, inString)
			i = e
			lastCode = '"'
			continue
		case c == '"' || (c == '\'' && s.singleQuotes):
			e := quoteEnd(src, i+1, c, s.rustStrings)
			mark(prefixStart(src, i), e, inString)
			i = e
			lastCode = c
			continue
		case s.rustStrings && c == '\'':
			if e, ok := rustChar(src, i); ok {
				mark(i, e, inString)
				i = e
				lastCode = '\''
				continue
			}
			// a lifetime: plain code
		}
		switch c {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
		}
		if !isSpace(c) {
			if isWordByte(c) {
				e := i
				for e < len(src) && isWordByte(src[e]) {
					e++
				}
				lastWord = string(src[i:e])
				lastCode = src[e-1]
				i = e
				continue
			}
			lastCode = c
			lastWord = ""
		}
		i++
	}
	return m
}

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// prefixStart extends a quote back over a string prefix (Python's r, b, f, u; Rust's b), so the
// prefix is string, not an identifier.
func prefixStart(src []byte, q int) int {
	i := q
	for i > 0 && q-i < 2 && isPrefix(src[i-1]) {
		i--
	}
	if i > 0 && isWordByte(src[i-1]) {
		return q // part of a longer identifier: not a prefix
	}
	return i
}

func isPrefix(c byte) bool {
	switch c {
	case 'r', 'R', 'b', 'B', 'u', 'U', 'f', 'F':
		return true
	}
	return false
}

// quoteEnd is the offset past the quote closing a string that started before from. Where a
// string cannot span lines (JavaScript, Python), a newline ends one that never closes, so one stray
// quote cannot swallow the file; Rust's strings may span lines.
func quoteEnd(src []byte, from int, q byte, multiline bool) int {
	for i := from; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case q:
			return i + 1
		case '\n':
			if !multiline {
				return i
			}
		}
	}
	return len(src)
}

func tripleEnd(src []byte, from int, q byte) int {
	for i := from; i < len(src); i++ {
		if src[i] == '\\' {
			i++
			continue
		}
		if i+2 < len(src) && src[i] == q && src[i+1] == q && src[i+2] == q {
			return i + 3
		}
	}
	return len(src)
}

// templateEnd is the offset past the closing backtick, or past "${" where code resumes.
func templateEnd(src []byte, from int) int {
	for i := from; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '`':
			return i + 1
		case '$':
			if i+1 < len(src) && src[i+1] == '{' {
				return i + 2
			}
		}
	}
	return len(src)
}

func blockEnd(src []byte, start int, nested bool) int {
	depth := 0
	for i := start; i+1 < len(src); i++ {
		if src[i] == '/' && src[i+1] == '*' {
			if depth == 0 || nested {
				depth++
			}
			i++
			continue
		}
		if src[i] == '*' && src[i+1] == '/' {
			depth--
			i++
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}

// regexAllowed reports whether a "/" after the previous code begins a regular expression rather
// than a division: after an operator, an opening bracket, or a keyword that takes an expression.
func regexAllowed(last byte, word string) bool {
	switch word {
	case "return", "typeof", "case", "do", "else", "in", "of", "new", "delete", "void", "throw", "yield", "await":
		return true
	}
	if word != "" {
		return false
	}
	switch last {
	case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '<', '>', '~', '^':
		return true
	}
	return false
}

// regexEnd finds the "/" closing a regex literal (outside a character class) and its flags; a
// newline first means it was not a regex.
func regexEnd(src []byte, from int) (int, bool) {
	class := false
	for i := from; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '[':
			class = true
		case ']':
			class = false
		case '\n':
			return 0, false
		case '/':
			if !class {
				e := i + 1
				for e < len(src) && isWordByte(src[e]) {
					e++
				}
				return e, true
			}
		}
	}
	return 0, false
}

// rawStart reports a Rust raw string at i: r"…", r#"…"#, br"…".
func rawStart(src []byte, i int) bool {
	if i > 0 && isWordByte(src[i-1]) && !(src[i-1] == 'b' && (i < 2 || !isWordByte(src[i-2]))) {
		return false
	}
	j := i + 1
	for j < len(src) && src[j] == '#' {
		j++
	}
	return j < len(src) && src[j] == '"'
}

func rawEnd(src []byte, i int) int {
	j := i + 1
	hashes := 0
	for j < len(src) && src[j] == '#' {
		hashes++
		j++
	}
	for k := j + 1; k < len(src); k++ {
		if src[k] != '"' {
			continue
		}
		n := 0
		for n < hashes && k+1+n < len(src) && src[k+1+n] == '#' {
			n++
		}
		if n == hashes {
			return k + 1 + n
		}
	}
	return len(src)
}

// rustChar reads a char literal at i ('a', '\n', '\u{1F600}', '"'); a quote that is not one is a
// lifetime ('a, 'static) and is code.
func rustChar(src []byte, i int) (int, bool) {
	if i+2 < len(src) && src[i+1] == '\\' {
		for k := i + 2; k < len(src) && k < i+12; k++ {
			if src[k] == '\'' {
				return k + 1, true
			}
		}
		return 0, false
	}
	// one character (one UTF-8 sequence) then a closing quote
	k := i + 1
	if k < len(src) && src[k] >= 0x80 {
		for k+1 < len(src) && src[k+1]&0xC0 == 0x80 {
			k++
		}
	}
	if k+1 < len(src) && src[k+1] == '\'' {
		return k + 2, true
	}
	return 0, false
}

package yaml

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// mark is a place in the source: its offset, and the line and column (in characters, from 0)
// the productions depend on.
type mark struct{ off, line, col int }

type tokenKind uint8

const (
	tStreamStart tokenKind = iota + 1
	tStreamEnd
	tVersionDirective
	tTagDirective
	tReservedDirective
	tDocumentStart
	tDocumentEnd
	tBlockSequenceStart
	tBlockMappingStart
	tBlockEnd
	tFlowSequenceStart
	tFlowSequenceEnd
	tFlowMappingStart
	tFlowMappingEnd
	tBlockEntry
	tFlowEntry
	tKey
	tValue
	tAlias
	tAnchor
	tTag
	tScalar
)

type token struct {
	kind       tokenKind
	start, end mark
	value      []byte // a scalar's content, an anchor or alias name, a tag's suffix, a directive's name
	style      Style
	handle     []byte   // a tag's or a %TAG directive's handle; nil for a verbatim tag
	suffix     []byte   // a tag's suffix
	prefix     []byte   // a %TAG directive's prefix
	params     []string // a directive's parameters, as written
}

// simpleKey is a place where an implicit key may have started: a key is known only when a ':'
// arrives, on the same line and within 1024 characters (spec 7.4, 8.2).
type simpleKey struct {
	possible, required bool
	tokenNumber        int
	mark               mark
	tabbed             bool // started on a line whose indentation held a tab
}

// scanner turns characters into tokens, tracking what the productions depend on: the block
// indentation stack, the flow level, and pending simple keys.
type scanner struct {
	src    []byte
	i      int
	line   int
	col    int
	tokens []token
	head   int
	parsed int // tokens handed to the parser so far

	started, ended   bool
	indent           int
	indents          []int
	flowLevel        int
	simpleKeyAllowed bool
	simpleKeys       []simpleKey

	// tabIndent is set when the current token's line began with whitespace holding a tab, in block
	// context; tabSpaces is how many spaces came before the first tab.
	tabIndent bool
	tabSpaces int
	// adjacent is set after a JSON-like node (a quoted scalar or a flow collection) in flow
	// context, where a ':' directly after it is a value indicator (spec 7.4).
	adjacent bool
	// flowIndent is the block indentation the outermost flow collection's lines must exceed.
	flowIndent int
	// flowKinds holds, per flow level, whether it is a mapping: a flow mapping's implicit keys may
	// span lines (ns-flow-map-implicit-entry), a flow sequence's single pairs may not.
	flowKinds []bool
	// lineSpaces is how many spaces begin the current token's line, when the token is the first on
	// it: indentation counts spaces only.
	lineSpaces  int
	atLineStart bool
	// afterIndicator: the previous token was a block indicator ('-', '?' or ':' in block context),
	// so whitespace holding a tab after it cannot lead into a nested block collection.
	afterIndicator bool
}

func (s *scanner) at(k int) byte {
	if s.i+k < len(s.src) {
		return s.src[s.i+k]
	}
	return 0
}

func (s *scanner) eof(k int) bool { return s.i+k >= len(s.src) }

func isBreak(c byte) bool { return c == '\n' || c == '\r' }
func isBlank(c byte) bool { return c == ' ' || c == '\t' }
func isFlowIndicator(c byte) bool {
	return c == ',' || c == '[' || c == ']' || c == '{' || c == '}'
}

func (s *scanner) blankz(k int) bool {
	return s.eof(k) || isBlank(s.at(k)) || isBreak(s.at(k))
}

func (s *scanner) breakz(k int) bool { return s.eof(k) || isBreak(s.at(k)) }

func (s *scanner) mark() mark { return mark{s.i, s.line, s.col} }

// advance consumes one character that is not a line break.
func (s *scanner) advance() {
	s.i += charLen(s.src[s.i:])
	s.col++
}

// skipBreak consumes one line break: LF, CR or CRLF.
func (s *scanner) skipBreak() {
	if s.at(0) == '\r' && s.at(1) == '\n' {
		s.i += 2
	} else {
		s.i++
	}
	s.line++
	s.col = 0
}

// errorf reports a lexical error found at m: the text is wrong where it stands.
//
// NOTE: an error raised with the cursor at the end of the input is not thereby Incomplete. A
// complete escape naming no character, "\uD800", fails there too, and no text appended repairs it.
// The sites that fail because the characters ran out say so with incompletef.
func (s *scanner) errorf(m mark, msg string) error {
	return &Error{Pos: position(s.src, lineStarts(s.src), m.off), Msg: msg}
}

// incompletef reports a lexical error found at m because the input ended inside a construct that
// more text could finish: a quoted scalar not closed, an escape or a tag cut short, a key waiting
// for its ':'.
func (s *scanner) incompletef(m mark, msg string) error {
	return &Error{Pos: position(s.src, lineStarts(s.src), m.off), Msg: msg, Incomplete: true}
}

// lexf reports a lexical error at m, Incomplete exactly when ranOut: the condition the site checked
// was the end of the input rather than a wrong character.
func (s *scanner) lexf(ranOut bool, m mark, msg string) error {
	if ranOut {
		return s.incompletef(m, msg)
	}
	return s.errorf(m, msg)
}

// token returns the next token, scanning as far ahead as a pending simple key requires.
func (s *scanner) token() (*token, error) {
	for {
		need := s.head == len(s.tokens)
		if !need {
			if err := s.staleSimpleKeys(); err != nil {
				return nil, err
			}
			for _, k := range s.simpleKeys {
				if k.possible && k.tokenNumber == s.parsed {
					need = true
					break
				}
			}
		}
		if !need || s.ended && s.head < len(s.tokens) {
			break
		}
		if err := s.fetch(); err != nil {
			return nil, err
		}
	}
	return &s.tokens[s.head], nil
}

// consume hands the current token to the parser.
func (s *scanner) consume() {
	s.head++
	s.parsed++
	if s.head > 64 && s.head*2 > len(s.tokens) {
		n := copy(s.tokens, s.tokens[s.head:])
		s.tokens = s.tokens[:n]
		s.head = 0
	}
}

func (s *scanner) push(t token) { s.tokens = append(s.tokens, t) }

// insert puts t at the given token number, ahead of tokens already queued.
func (s *scanner) insert(number int, t token) {
	at := s.head + number - s.parsed
	s.tokens = append(s.tokens, token{})
	copy(s.tokens[at+1:], s.tokens[at:])
	s.tokens[at] = t
}

func (s *scanner) fetch() error {
	if !s.started {
		s.started = true
		s.indent = -1
		s.simpleKeyAllowed = true
		s.simpleKeys = append(s.simpleKeys, simpleKey{})
		m := s.mark()
		s.push(token{kind: tStreamStart, start: m, end: m})
		return nil
	}
	if err := s.scanToNextToken(); err != nil {
		return err
	}
	if err := s.staleSimpleKeys(); err != nil {
		return err
	}
	s.unrollIndent(s.col)
	if s.eof(0) {
		return s.fetchStreamEnd()
	}
	c := s.at(0)
	if s.flowLevel > 0 && s.atLineStart && s.lineSpaces <= s.flowIndent {
		return s.errorf(s.mark(), "a flow collection's lines must be indented past the block around it")
	}
	if s.tabIndent {
		if err := s.checkTabIndent(c); err != nil {
			return err
		}
	}
	if s.col == 0 && c == '%' {
		return s.fetchDirective()
	}
	if s.col == 0 && s.documentIndicator() {
		return s.fetchDocumentIndicator(c == '-')
	}
	adjacent := s.adjacent
	s.adjacent = false
	switch c {
	case '[':
		return s.fetchFlowCollectionStart(tFlowSequenceStart)
	case '{':
		return s.fetchFlowCollectionStart(tFlowMappingStart)
	case ']':
		return s.fetchFlowCollectionEnd(tFlowSequenceEnd)
	case '}':
		return s.fetchFlowCollectionEnd(tFlowMappingEnd)
	case ',':
		return s.fetchFlowEntry()
	case '-':
		if s.blankz(1) {
			return s.fetchBlockEntry()
		}
	case '?':
		if s.blankz(1) || (s.flowLevel > 0 && isFlowIndicator(s.at(1))) {
			return s.fetchKey()
		}
	case ':':
		if s.blankz(1) || (s.flowLevel > 0 && (isFlowIndicator(s.at(1)) || adjacent)) {
			return s.fetchValue()
		}
	case '*':
		return s.fetchAnchor(tAlias)
	case '&':
		return s.fetchAnchor(tAnchor)
	case '!':
		return s.fetchTag()
	case '|', '>':
		if s.flowLevel == 0 {
			return s.fetchBlockScalar(c == '|')
		}
	case '\'', '"':
		return s.fetchFlowScalar(c == '\'')
	}
	if s.plainStart() {
		return s.fetchPlainScalar()
	}
	return s.errorf(s.mark(), "found a character that cannot start any token")
}

// checkTabIndent: a tab may separate tokens, but in block context it is never indentation. Content
// after a tab at a line's start is allowed only where indentation does not decide structure: a
// flow node (not a key, a block indicator or a block scalar), past the block's indentation.
func (s *scanner) checkTabIndent(c byte) error {
	switch c {
	case '-', '?', ':':
		if s.blankz(1) {
			return s.errorf(s.mark(), "a tab cannot indent a block indicator")
		}
	case '|', '>':
		if s.atLineStart {
			return s.errorf(s.mark(), "a tab cannot indent a block scalar")
		}
	}
	if s.tabSpaces <= s.indent {
		return s.errorf(s.mark(), "a tab cannot be indentation")
	}
	return nil
}

// restOfLineBlank reports whether only spaces and tabs remain on the line.
func (s *scanner) restOfLineBlank() bool {
	j := s.i
	for j < len(s.src) && isBlank(s.src[j]) {
		j++
	}
	return j >= len(s.src) || isBreak(s.src[j])
}

// plainStart is ns-plain-first: a character that is not an indicator, or '-', '?' or ':' followed
// by a character a plain scalar may hold.
func (s *scanner) plainStart() bool {
	c := s.at(0)
	switch c {
	case '-', '?', ':':
		return !s.blankz(1) && !(s.flowLevel > 0 && isFlowIndicator(s.at(1)))
	case ',', '[', ']', '{', '}', '#', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
		return false
	case ' ', '\t', '\n', '\r':
		return false
	}
	return true
}

func (s *scanner) documentIndicator() bool {
	c := s.at(0)
	return (c == '-' || c == '.') && s.at(1) == c && s.at(2) == c && s.blankz(3)
}

// scanToNextToken skips whitespace, comments and line breaks. A line break in block context allows
// a simple key again.
func (s *scanner) scanToNextToken() error {
	lineStart := s.col == 0
	afterIndicator := s.afterIndicator
	s.afterIndicator = false
	s.tabIndent, s.tabSpaces = false, 0
	sawTab := false
	spaces := 0
	if lineStart {
		spaces = -1 // not yet counted
	}
	for {
		if s.col == 0 && s.at(0) == 0xEF && s.at(1) == 0xBB && s.at(2) == 0xBF {
			s.i += 3 // a byte order mark before a document
		}
		for s.at(0) == ' ' || s.at(0) == '\t' {
			if s.at(0) == '\t' {
				if lineStart && !sawTab {
					spaces = s.col
				}
				sawTab = true
			}
			s.advance()
		}
		if lineStart && !sawTab {
			spaces = s.col
		}
		if s.at(0) == '#' {
			if s.i > 0 && !isBlank(s.src[s.i-1]) && !isBreak(s.src[s.i-1]) {
				return s.errorf(s.mark(), "a comment must be separated from what precedes it by whitespace")
			}
			for !s.breakz(0) {
				s.advance()
			}
		}
		if !s.eof(0) && isBreak(s.at(0)) {
			s.skipBreak()
			if s.flowLevel == 0 {
				s.simpleKeyAllowed = true
			}
			lineStart, sawTab, spaces, afterIndicator = true, false, -1, false
			continue
		}
		break
	}
	s.atLineStart = lineStart
	s.lineSpaces = spaces
	if sawTab && s.flowLevel == 0 && !s.eof(0) && (lineStart || afterIndicator) {
		s.tabIndent, s.tabSpaces = true, spaces
		if !lineStart {
			s.tabSpaces = s.indent + 1 // mid-line: only what may follow matters, not how far in
		}
	}
	return nil
}

func (s *scanner) staleSimpleKeys() error {
	for i := range s.simpleKeys {
		k := &s.simpleKeys[i]
		inFlowMapping := i > 0 && i <= len(s.flowKinds) && s.flowKinds[i-1]
		if k.possible && !inFlowMapping && (k.mark.line < s.line || k.mark.off+1024 < s.i) {
			if k.required {
				return s.errorf(k.mark, "could not find the expected ':' of an implicit key")
			}
			k.possible = false
		}
	}
	return nil
}

func (s *scanner) saveSimpleKey() error {
	required := s.flowLevel == 0 && s.indent == s.col
	if s.simpleKeyAllowed {
		if err := s.removeSimpleKey(); err != nil {
			return err
		}
		s.simpleKeys[len(s.simpleKeys)-1] = simpleKey{possible: true, required: required,
			tokenNumber: s.parsed + len(s.tokens) - s.head, mark: s.mark(), tabbed: s.tabIndent}
	}
	return nil
}

func (s *scanner) removeSimpleKey() error {
	k := &s.simpleKeys[len(s.simpleKeys)-1]
	if k.possible && k.required {
		// At the end of the input the key's ':' may still come; anywhere else a token arrived
		// where it should have been.
		return s.lexf(s.eof(0), k.mark, "could not find the expected ':' of an implicit key")
	}
	k.possible = false
	return nil
}

// rollIndent opens a block collection at col, inserting its start token at number (or appending it
// when number is -1).
func (s *scanner) rollIndent(col, number int, kind tokenKind, m mark) {
	if s.flowLevel > 0 || s.indent >= col {
		return
	}
	s.indents = append(s.indents, s.indent)
	s.indent = col
	t := token{kind: kind, start: m, end: m}
	if number < 0 {
		s.push(t)
	} else {
		s.insert(number, t)
	}
}

// unrollIndent closes the block collections indented past col.
func (s *scanner) unrollIndent(col int) {
	if s.flowLevel > 0 {
		return
	}
	for s.indent > col {
		m := s.mark()
		s.push(token{kind: tBlockEnd, start: m, end: m})
		s.indent = s.indents[len(s.indents)-1]
		s.indents = s.indents[:len(s.indents)-1]
	}
}

func (s *scanner) fetchStreamEnd() error {
	if s.col != 0 {
		s.col = 0
		s.line++
	}
	s.unrollIndent(-1)
	if err := s.removeSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = false
	s.ended = true
	m := s.mark()
	s.push(token{kind: tStreamEnd, start: m, end: m})
	return nil
}

func (s *scanner) simpleToken(kind tokenKind) {
	start := s.mark()
	s.advance()
	s.push(token{kind: kind, start: start, end: s.mark()})
}

func (s *scanner) fetchDocumentIndicator(start bool) error {
	s.unrollIndent(-1)
	if err := s.removeSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = false
	m := s.mark()
	s.advance()
	s.advance()
	s.advance()
	kind := tDocumentStart
	if !start {
		kind = tDocumentEnd
		// only a comment may follow "..." on its line
		j := s.i
		for j < len(s.src) && isBlank(s.src[j]) {
			j++
		}
		if j < len(s.src) && !isBreak(s.src[j]) && s.src[j] != '#' {
			return s.errorf(s.mark(), "only a comment may follow a document end marker")
		}
	}
	s.push(token{kind: kind, start: m, end: s.mark()})
	return nil
}

func (s *scanner) fetchFlowCollectionStart(kind tokenKind) error {
	if err := s.saveSimpleKey(); err != nil {
		return err
	}
	if s.flowLevel == 0 {
		s.flowIndent = s.indent
	}
	s.simpleKeys = append(s.simpleKeys, simpleKey{})
	s.flowKinds = append(s.flowKinds, kind == tFlowMappingStart)
	s.flowLevel++
	s.simpleKeyAllowed = true
	s.simpleToken(kind)
	return nil
}

func (s *scanner) fetchFlowCollectionEnd(kind tokenKind) error {
	if err := s.removeSimpleKey(); err != nil {
		return err
	}
	if s.flowLevel > 0 {
		s.flowLevel--
		s.simpleKeys = s.simpleKeys[:len(s.simpleKeys)-1]
		s.flowKinds = s.flowKinds[:len(s.flowKinds)-1]
	}
	s.simpleKeyAllowed = false
	s.simpleToken(kind)
	s.adjacent = s.flowLevel > 0
	return s.afterFlowNode()
}

// afterFlowNode checks what may follow a flow collection or a quoted scalar on its line: space, a
// flow indicator, ':', a comment (after space) or the line's end.
func (s *scanner) afterFlowNode() error {
	c := s.at(0)
	if s.eof(0) || isBlank(c) || isBreak(c) || c == ':' || (s.flowLevel > 0 && isFlowIndicator(c)) {
		return nil
	}
	if c == '#' {
		return s.errorf(s.mark(), "a comment must be separated from what precedes it by whitespace")
	}
	if s.flowLevel == 0 {
		return s.errorf(s.mark(), "unexpected content after a flow node")
	}
	return nil
}

func (s *scanner) fetchFlowEntry() error {
	if err := s.removeSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = true
	s.simpleToken(tFlowEntry)
	return nil
}

func (s *scanner) fetchBlockEntry() error {
	if s.flowLevel == 0 {
		if !s.simpleKeyAllowed {
			return s.errorf(s.mark(), "a block sequence entry is not allowed here")
		}
		s.rollIndent(s.col, -1, tBlockSequenceStart, s.mark())
	} else {
		return s.errorf(s.mark(), "a block sequence entry is not allowed in a flow collection")
	}
	if err := s.removeSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = true
	s.simpleToken(tBlockEntry)
	s.afterIndicator = true
	return nil
}

func (s *scanner) fetchKey() error {
	if s.flowLevel == 0 {
		if !s.simpleKeyAllowed {
			return s.errorf(s.mark(), "a mapping key is not allowed here")
		}
		s.rollIndent(s.col, -1, tBlockMappingStart, s.mark())
	}
	if err := s.removeSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = s.flowLevel == 0
	s.simpleToken(tKey)
	s.afterIndicator = s.flowLevel == 0
	return nil
}

func (s *scanner) fetchValue() error {
	k := &s.simpleKeys[len(s.simpleKeys)-1]
	if k.possible {
		if k.tabbed {
			return s.errorf(k.mark, "a tab cannot indent an implicit key")
		}
		s.insert(k.tokenNumber, token{kind: tKey, start: k.mark, end: k.mark})
		s.rollIndent(k.mark.col, k.tokenNumber, tBlockMappingStart, k.mark)
		k.possible = false
		s.simpleKeyAllowed = false
	} else {
		if s.flowLevel == 0 {
			if !s.simpleKeyAllowed {
				// A ':' that ends the input may still be the start of a plain scalar's ":x"; the
				// character after it decides, and it has not arrived.
				return s.lexf(s.eof(1), s.mark(), "a mapping value is not allowed here")
			}
			s.rollIndent(s.col, -1, tBlockMappingStart, s.mark())
		}
		s.simpleKeyAllowed = s.flowLevel == 0
	}
	s.simpleToken(tValue)
	s.afterIndicator = s.flowLevel == 0
	return nil
}

// fetchAnchor scans &name or *name. An anchor's name is any run of characters but whitespace and
// flow indicators (spec 6.9.2), so it may hold ':'.
func (s *scanner) fetchAnchor(kind tokenKind) error {
	if err := s.saveSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = false
	start := s.mark()
	s.advance()
	from := s.i
	for !s.blankz(0) && !isFlowIndicator(s.at(0)) {
		s.advance()
	}
	if s.i == from {
		return s.lexf(s.eof(0), start, "an anchor or alias needs a name")
	}
	s.push(token{kind: kind, start: start, end: s.mark(), value: s.src[from:s.i:s.i]})
	return nil
}

func isWordChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-'
}

// isURIChar is ns-uri-char, bar '%' (escapes are read apart).
func isURIChar(c byte) bool {
	if isWordChar(c) {
		return true
	}
	return bytes.IndexByte([]byte("#;/?:@&=+$,_.!~*'()[]"), c) >= 0
}

// fetchTag scans a tag: verbatim !<uri>, a shorthand !handle!suffix or !suffix, or the non-specific
// "!" alone.
func (s *scanner) fetchTag() error {
	if err := s.saveSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = false
	start := s.mark()
	var handle, suffix []byte
	if s.at(1) == '<' {
		s.advance()
		s.advance()
		var err error
		if suffix, err = s.scanURI(false); err != nil {
			return err
		}
		if s.at(0) != '>' || len(suffix) == 0 {
			return s.lexf(s.eof(0), s.mark(), "a verbatim tag must end with '>'")
		}
		s.advance()
	} else {
		// a handle is "!", "!!" or "!word!"
		j := 1
		for isWordChar(s.at(j)) {
			j++
		}
		if s.at(j) == '!' {
			handle = s.src[s.i : s.i+j+1]
			for k := 0; k <= j; k++ {
				s.advance()
			}
		} else {
			handle = []byte("!")
			s.advance()
		}
		var err error
		if suffix, err = s.scanURI(true); err != nil {
			return err
		}
		if len(suffix) == 0 {
			if string(handle) != "!" {
				return s.lexf(s.eof(0), start, "a tag handle needs a suffix")
			}
			handle, suffix = nil, []byte("!") // the non-specific tag
		}
	}
	if !s.blankz(0) && !(s.flowLevel > 0 && isFlowIndicator(s.at(0))) {
		return s.errorf(s.mark(), "a tag must be followed by whitespace")
	}
	s.push(token{kind: tTag, start: start, end: s.mark(), handle: handle, suffix: suffix})
	return nil
}

// scanURI reads URI characters, decoding %-escapes. In a shorthand (tagChars) '!' and the flow
// indicators end it.
func (s *scanner) scanURI(tagChars bool) ([]byte, error) {
	var out []byte
	for {
		c := s.at(0)
		if c == '%' {
			if !isHexDigit(s.at(1)) || !isHexDigit(s.at(2)) {
				ranOut := s.eof(1) || (isHexDigit(s.at(1)) && s.eof(2))
				return nil, s.lexf(ranOut, s.mark(), "a URI escape needs two hex digits")
			}
			v, _ := strconv.ParseUint(string(s.src[s.i+1:s.i+3]), 16, 8)
			out = append(out, byte(v))
			s.advance()
			s.advance()
			s.advance()
			continue
		}
		if s.eof(0) || !isURIChar(c) || (tagChars && (c == '!' || isFlowIndicator(c))) {
			break
		}
		out = append(out, c)
		s.advance()
	}
	return out, nil
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func (s *scanner) fetchDirective() error {
	s.unrollIndent(-1)
	if err := s.removeSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = false
	start := s.mark()
	s.advance()
	from := s.i
	for !s.blankz(0) {
		s.advance()
	}
	name := s.src[from:s.i]
	var params []string
	for {
		for isBlank(s.at(0)) {
			s.advance()
		}
		if s.blankz(0) || s.at(0) == '#' {
			break
		}
		p := s.i
		for !s.blankz(0) {
			s.advance()
		}
		params = append(params, string(s.src[p:s.i]))
	}
	end := s.mark()
	if s.at(0) == '#' {
		if !isBlank(s.src[s.i-1]) {
			return s.errorf(s.mark(), "a comment must be separated from what precedes it by whitespace")
		}
		for !s.breakz(0) {
			s.advance()
		}
	}
	t := token{kind: tReservedDirective, start: start, end: end, value: name, params: params}
	switch string(name) {
	case "YAML":
		if len(params) != 1 || !validVersion(params[0]) {
			ranOut := s.eof(0) && (len(params) == 0 || (len(params) == 1 && versionPrefix(params[0])))
			return s.lexf(ranOut, start, "a %YAML directive takes one version, major.minor")
		}
		t.kind = tVersionDirective
	case "TAG":
		if len(params) != 2 || !validHandle(params[0]) || !validTagPrefix(params[1]) {
			ranOut := s.eof(0) && (len(params) == 0 ||
				(len(params) == 1 && handlePrefix(params[0])) ||
				(len(params) == 2 && validHandle(params[0]) && tagPrefixPrefix(params[1])))
			return s.lexf(ranOut, start, "a %TAG directive takes a handle and a prefix")
		}
		t.kind = tTagDirective
		t.handle = []byte(params[0])
		t.prefix = decodeURI([]byte(params[1]))
	}
	s.push(t)
	return nil
}

func validVersion(v string) bool {
	dot := bytes.IndexByte([]byte(v), '.')
	if dot <= 0 || dot == len(v)-1 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if i != dot && (v[i] < '0' || v[i] > '9') {
			return false
		}
	}
	return true
}

// yamlMajor begins every %YAML version this parser reads. The parser refuses any other, and
// versionPrefix completes only toward it, so the two cannot disagree.
const yamlMajor = "1."

// versionPrefix reports whether more text could make v a version this parser reads. It is asked
// only of a version that is not already valid, so the one way left is that v is still part of "1.":
// "", "1" or "1." itself. "2" or "12" can never become one, whatever follows.
func versionPrefix(v string) bool { return strings.HasPrefix(yamlMajor, v) }

// handlePrefix reports whether more text could make h a valid tag handle.
func handlePrefix(h string) bool {
	if validHandle(h) {
		return true
	}
	if h == "" || h[0] != '!' {
		return false
	}
	for i := 1; i < len(h); i++ {
		if !isWordChar(h[i]) {
			return false
		}
	}
	return true
}

// tagPrefixPrefix reports whether more text could make p a valid tag prefix: it already is one, or
// it ends in a %-escape cut short.
func tagPrefixPrefix(p string) bool {
	return validTagPrefix(p) || validTagPrefix(p+"0") || validTagPrefix(p+"00")
}

func validHandle(h string) bool {
	if h == "!" || h == "!!" {
		return true
	}
	if len(h) < 3 || h[0] != '!' || h[len(h)-1] != '!' {
		return false
	}
	for i := 1; i < len(h)-1; i++ {
		if !isWordChar(h[i]) {
			return false
		}
	}
	return true
}

func validTagPrefix(p string) bool {
	if p == "" {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '%' {
			if i+2 >= len(p) || !isHexDigit(p[i+1]) || !isHexDigit(p[i+2]) {
				return false
			}
			i += 2
			continue
		}
		if !isURIChar(c) || (i == 0 && c != '!' && isFlowIndicator(c)) {
			return false
		}
	}
	return true
}

func decodeURI(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); i++ {
		if b[i] == '%' && i+2 < len(b) {
			v, _ := strconv.ParseUint(string(b[i+1:i+3]), 16, 8)
			out = append(out, byte(v))
			i += 2
			continue
		}
		out = append(out, b[i])
	}
	return out
}

// fold joins the lines a flow or plain scalar crossed (spec 6.5): a single line break becomes a
// space, and n+1 breaks become n newlines.
type folder struct {
	out            []byte
	whitespace     []byte
	leadingBreak   bool
	trailingBreaks int
	leadingBlanks  bool
}

func (f *folder) flush() {
	if f.leadingBlanks {
		if f.leadingBreak && f.trailingBreaks == 0 {
			f.out = append(f.out, ' ')
		}
		for ; f.trailingBreaks > 0; f.trailingBreaks-- {
			f.out = append(f.out, '\n')
		}
		f.leadingBreak, f.leadingBlanks = false, false
	} else {
		f.out = append(f.out, f.whitespace...)
	}
	f.whitespace = f.whitespace[:0]
}

func (f *folder) brk() {
	if !f.leadingBlanks {
		f.whitespace = f.whitespace[:0]
		f.leadingBreak, f.leadingBlanks = true, true
	} else {
		f.trailingBreaks++
	}
}

// fetchPlainScalar scans a plain scalar (spec 7.3.3), which may run over several lines.
func (s *scanner) fetchPlainScalar() error {
	if err := s.saveSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = false
	start := s.mark()
	end := start
	var f folder
	indent := s.indent + 1
	for {
		if s.col == 0 && s.documentIndicator() {
			break
		}
		if s.at(0) == '#' {
			break
		}
		for !s.blankz(0) {
			c := s.at(0)
			if c == ':' && (s.blankz(1) || (s.flowLevel > 0 && isFlowIndicator(s.at(1)))) {
				break
			}
			if s.flowLevel > 0 && isFlowIndicator(c) {
				break
			}
			f.flush()
			n := charLen(s.src[s.i:])
			f.out = append(f.out, s.src[s.i:s.i+n]...)
			s.advance()
			end = s.mark()
		}
		if !isBlank(s.at(0)) && !isBreak(s.at(0)) || s.eof(0) {
			break
		}
		for isBlank(s.at(0)) || (!s.eof(0) && isBreak(s.at(0))) {
			if isBlank(s.at(0)) {
				if f.leadingBlanks && s.col < indent && s.at(0) == '\t' && !s.restOfLineBlank() {
					// a line of only whitespace is not a continuation, so its tab indents nothing
					return s.errorf(s.mark(), "a tab cannot be indentation")
				}
				if !f.leadingBlanks {
					f.whitespace = append(f.whitespace, s.at(0))
				}
				s.advance()
			} else {
				f.brk()
				s.skipBreak()
			}
		}
		if s.flowLevel == 0 && s.col < indent {
			break
		}
	}
	s.push(token{kind: tScalar, start: start, end: end, value: f.out, style: StylePlain})
	if f.leadingBlanks {
		s.simpleKeyAllowed = true
	}
	// the cursor may have moved past the scalar's trailing whitespace and lines
	return nil
}

// fetchFlowScalar scans a single- or double-quoted scalar (spec 7.3.1, 7.3.2).
func (s *scanner) fetchFlowScalar(single bool) error {
	if err := s.saveSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = false
	start := s.mark()
	quote := s.at(0)
	s.advance()
	var f folder
	for {
		if s.col == 0 && s.documentIndicator() {
			return s.errorf(s.mark(), "a document marker cannot appear inside a quoted scalar")
		}
		if s.eof(0) {
			return s.incompletef(start, "a quoted scalar is not closed")
		}
		for !s.blankz(0) {
			c := s.at(0)
			if single && c == '\'' && s.at(1) == '\'' {
				f.flush()
				f.out = append(f.out, '\'')
				s.advance()
				s.advance()
				continue
			}
			if c == quote {
				break
			}
			if !single && c == '\\' && isBreak(s.at(1)) {
				f.flush()
				s.advance()
				s.skipBreak()
				f.leadingBlanks = true // an escaped line break: the next line joins with nothing
				f.leadingBreak = false
				break
			}
			if !single && c == '\\' {
				f.flush()
				if err := s.escape(&f.out); err != nil {
					return err
				}
				continue
			}
			f.flush()
			n := charLen(s.src[s.i:])
			f.out = append(f.out, s.src[s.i:s.i+n]...)
			s.advance()
		}
		if s.at(0) == quote {
			break
		}
		for isBlank(s.at(0)) || (!s.eof(0) && isBreak(s.at(0))) {
			if isBlank(s.at(0)) {
				if !f.leadingBlanks {
					f.whitespace = append(f.whitespace, s.at(0))
				}
				s.advance()
			} else {
				f.brk()
				s.skipBreak()
				if err := s.continuationIndent(); err != nil {
					return err
				}
			}
		}
		if s.eof(0) {
			return s.incompletef(start, "a quoted scalar is not closed")
		}
	}
	f.flush()
	s.advance() // the closing quote
	style := StyleDoubleQuoted
	if single {
		style = StyleSingleQuoted
	}
	s.push(token{kind: tScalar, start: start, end: s.mark(), value: f.out, style: style})
	s.adjacent = s.flowLevel > 0
	return s.afterFlowNode()
}

// continuationIndent checks a quoted scalar's next line in block context: its leading spaces must
// pass the block indentation (a tab after them is separation).
func (s *scanner) continuationIndent() error {
	if s.flowLevel > 0 {
		return nil
	}
	j, spaces := s.i, 0
	for j < len(s.src) && s.src[j] == ' ' {
		j++
		spaces++
	}
	for j < len(s.src) && isBlank(s.src[j]) {
		j++
	}
	if j >= len(s.src) || isBreak(s.src[j]) {
		return nil // a blank line
	}
	if spaces <= s.indent {
		return s.errorf(mark{j, s.line, j - s.i}, "a quoted scalar's lines must be indented past the block around it")
	}
	return nil
}

// escape decodes one double-quoted escape sequence (spec 5.7) onto out.
func (s *scanner) escape(out *[]byte) error {
	start := s.mark()
	s.advance() // the backslash
	c := s.at(0)
	simple := map[byte]string{'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n", 'v': "\v",
		'f': "\f", 'r': "\r", 'e': "\x1b", ' ': " ", '"': "\"", '/': "/", '\\': "\\",
		'N': "\u0085", '_': " ", 'L': " ", 'P': " "}
	if v, ok := simple[c]; ok {
		*out = append(*out, v...)
		s.advance()
		return nil
	}
	width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
	if width == 0 {
		// A backslash that ends the input has not been given its escape yet.
		return s.lexf(s.eof(0), start, "an unknown escape sequence")
	}
	s.advance()
	var v rune
	for k := 0; k < width; k++ {
		d := s.at(0)
		if !isHexDigit(d) {
			return s.lexf(s.eof(0), start, "an escape needs "+itoa(width)+" hex digits")
		}
		n, _ := strconv.ParseUint(string(d), 16, 8)
		v = v<<4 | rune(n)
		s.advance()
	}
	if (v >= 0xD800 && v <= 0xDFFF) || v > utf8.MaxRune {
		return s.errorf(start, "an escape names no character")
	}
	*out = utf8.AppendRune(*out, v)
	return nil
}

// fetchBlockScalar scans a literal or folded block scalar (spec 8.1): its header, then its content
// lines at the indentation the header gives or the first non-empty line sets.
func (s *scanner) fetchBlockScalar(literal bool) error {
	if err := s.removeSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = true
	start := s.mark()
	s.advance()
	chomp, increment := 0, 0
	for k := 0; k < 2; k++ {
		switch c := s.at(0); {
		case (c == '+' || c == '-') && chomp == 0:
			chomp = map[byte]int{'+': 1, '-': -1}[c]
			s.advance()
		case c >= '1' && c <= '9' && increment == 0:
			increment = int(c - '0')
			s.advance()
		case c == '0':
			return s.errorf(s.mark(), "a block scalar's indentation indicator is 1 to 9")
		}
	}
	for isBlank(s.at(0)) {
		s.advance()
	}
	if s.at(0) == '#' {
		if !isBlank(s.src[s.i-1]) {
			return s.errorf(s.mark(), "a comment must be separated from what precedes it by whitespace")
		}
		for !s.breakz(0) {
			s.advance()
		}
	}
	if !s.breakz(0) {
		return s.errorf(s.mark(), "a block scalar's header must end its line")
	}
	end := s.mark()
	if !s.eof(0) {
		s.skipBreak()
	}
	indent := -1
	if increment > 0 {
		indent = max(s.indent, -1) + increment
		if s.indent < 0 {
			indent = increment - 1
		}
	}
	var out []byte
	trailing := 0
	var err error
	if indent, trailing, err = s.blockBreaks(indent, &end); err != nil {
		return err
	}
	leadingBreak := false
	leadingBlank := false
	for s.col == indent && !s.eof(0) && !(s.col == 0 && s.documentIndicator()) {
		trailingBlank := isBlank(s.at(0))
		if !literal && leadingBreak && !leadingBlank && !trailingBlank {
			if trailing == 0 {
				out = append(out, ' ')
			}
		} else if leadingBreak {
			out = append(out, '\n')
		}
		for ; trailing > 0; trailing-- {
			out = append(out, '\n')
		}
		leadingBlank = isBlank(s.at(0))
		for !s.breakz(0) {
			n := charLen(s.src[s.i:])
			out = append(out, s.src[s.i:s.i+n]...)
			s.advance()
		}
		end = s.mark()
		if s.eof(0) {
			leadingBreak = true // the end of the input ends the last line
			break
		}
		s.skipBreak()
		end = s.mark()
		leadingBreak = true
		if indent, trailing, err = s.blockBreaks(indent, &end); err != nil {
			return err
		}
	}
	if chomp != -1 && leadingBreak {
		out = append(out, '\n')
	}
	if chomp == 1 {
		for ; trailing > 0; trailing-- {
			out = append(out, '\n')
		}
	}
	style := StyleFolded
	if literal {
		style = StyleLiteral
	}
	s.push(token{kind: tScalar, start: start, end: end, value: out, style: style})
	return nil
}

// blockBreaks consumes the empty lines before a block scalar's next content line, and, when the
// indentation is not yet known, sets it from that line. A leading empty line may not hold more
// spaces than the line that sets the indentation.
func (s *scanner) blockBreaks(indent int, end *mark) (int, int, error) {
	breaks := 0
	maxEmpty := 0
	for {
		for (indent < 0 || s.col < indent) && s.at(0) == ' ' {
			s.advance()
		}
		if indent < 0 && s.breakz(0) && !s.eof(0) {
			maxEmpty = max(maxEmpty, s.col)
		}
		if s.at(0) == '\t' && ((indent >= 0 && s.col < indent) || (indent < 0 && s.col < s.indent+1)) {
			// where indentation is expected, a tab is neither indentation nor an empty line
			return 0, 0, s.errorf(s.mark(), "a tab cannot be a block scalar's indentation")
		}
		if s.eof(0) {
			if s.col > 0 && (indent < 0 || s.col <= indent) {
				breaks++ // a last line of spaces, ended by the end of the input
			}
			break
		}
		if !isBreak(s.at(0)) {
			break
		}
		s.skipBreak()
		*end = s.mark()
		breaks++
	}
	if indent < 0 {
		indent = s.col
		minIndent := s.indent + 1
		if s.eof(0) || isBreak(s.at(0)) || s.col < minIndent {
			indent = max(minIndent, maxEmpty)
			if s.eof(0) {
				indent = max(minIndent, 1, maxEmpty)
			}
		} else if maxEmpty > indent {
			return 0, 0, s.errorf(s.mark(), "a leading empty line holds more spaces than the block scalar's first line")
		}
		if indent < 0 {
			indent = 0
		}
	}
	return indent, breaks, nil
}

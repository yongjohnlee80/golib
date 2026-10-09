package lexical

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"github.com/yongjohnlee80/golib/parse/internal/states"
)

// Quote describes one literal opener/terminator for the shared scanner.
type Quote struct {
	Open, Close  string
	Escape       byte
	Multiline    bool
	Continuation bool
	Style        highlight.Style
	Template     bool
	Doubling     bool
}

type comment struct {
	open, close string
	nested      bool
}
type block struct{ Kind, Prefix, Close string }
type heredoc struct {
	Word string
	Tabs bool
}
type context struct {
	Mode             string
	Close            string
	Open             string
	Escape           byte
	Style            highlight.Style
	Multiline        bool
	Continuation     bool
	Nested           bool
	Doubling         bool
	Depth            int
	Templates        []int
	Stack            []block
	Here             []heredoc
	Last             string
	Scalar           int
	Unit             string
	ExpressionParens []bool
	Markup           []markup
}

type markup struct {
	Depth           int
	Mode, Resume    string
	Closing         bool
	ExpressionDepth int
	Quote           byte
	Name            bool
}

// Token is structural source text outside comments and literal bodies.
type Token struct {
	Text       string
	Start, End int
	Style      highlight.Style
}

// TokenRule recognizes a language-specific atomic token before ordinary quotes.
type TokenRule func(line string, at int) (end int, style highlight.Style, matched bool)

// LiteralRule recognizes a language's variable literal delimiter.
type LiteralRule func(line string, at int) (Quote, bool)

type configuration struct {
	words              map[string]highlight.Style
	comments           []string
	commentBoundary    func(string, int) bool
	blocks             []comment
	quotes             []Quote
	literals           []LiteralRule
	dynamicComments    []LiteralRule
	tokens             []TokenRule
	regexp             bool
	here               bool
	dollar             bool
	unit               string
	rules              rules
	markup             bool
	expressionWords    map[string]bool
	expressionControls map[string]bool
}

// Option configures a language's lexical declarations.
type Option func(*configuration)

// Words classifies complete identifiers, never prefixes of an identifier.
func Words(style highlight.Style, words string) Option {
	return func(c *configuration) {
		for _, w := range strings.Fields(words) {
			c.words[w] = style
		}
	}
}

// LineComments chooses comment markers and their optional lexical boundary.
func LineComments(boundary func(string, int) bool, markers ...string) Option {
	return func(c *configuration) { c.comments = append(c.comments, markers...); c.commentBoundary = boundary }
}

// BlockComment declares a delimited comment, optionally nesting.
func BlockComment(open, close string, nested bool) Option {
	return func(c *configuration) { c.blocks = append(c.blocks, comment{open, close, nested}) }
}

// Quotes declares literal forms in precedence order, longest openers first.
func Quotes(forms ...Quote) Option {
	return func(c *configuration) { c.quotes = append(c.quotes, forms...) }
}

// Literals adds language-owned variable delimiter recognition.
func Literals(rules ...LiteralRule) Option {
	return func(c *configuration) { c.literals = append(c.literals, rules...) }
}

// DynamicComments adds language-owned variable comment delimiters.
func DynamicComments(rules ...LiteralRule) Option {
	return func(c *configuration) { c.dynamicComments = append(c.dynamicComments, rules...) }
}

// Tokens adds language-specific token recognition before ordinary quotes.
func Tokens(rules ...TokenRule) Option {
	return func(c *configuration) { c.tokens = append(c.tokens, rules...) }
}

// RegularExpressions enables lexical expression-position regexp recognition.
func RegularExpressions() Option { return func(c *configuration) { c.regexp = true } }

// HereDocuments enables the shared POSIX/Bash heredoc delimiter mechanics.
func HereDocuments() Option { return func(c *configuration) { c.here = true } }

// DollarIdentifiers admits JavaScript-style dollar identifiers.
func DollarIdentifiers() Option { return func(c *configuration) { c.dollar = true } }

// Expressions declares operand/control-word context for regexp disambiguation.
func Expressions(operands, controls string) Option {
	return func(c *configuration) {
		c.expressionWords = wordSet(operands)
		c.expressionControls = wordSet(controls)
	}
}

// MarkupExpressions enables embedded tag/text/expression mechanics for JSX providers.
func MarkupExpressions() Option { return func(c *configuration) { c.markup = true } }

// Session is one document's tolerant source scanner and indentation policy.
type Session struct {
	config configuration
	store  *states.Store[context]
}

// New builds document-owned behavior from language-owned declarations.
func New(opts ...Option) *Session {
	c := configuration{words: map[string]highlight.Style{}}
	for _, opt := range opts {
		opt(&c)
	}
	s := &Session{config: c}
	s.store = states.New(contextKey)
	return s
}

// Source pairs this instance's highlighting, indentation and state leases.
func (s *Session) Source() highlight.Source {
	return highlight.Source{Highlighter: s, Indenter: s, States: s}
}

// Retain implements highlight.StateStore.
func (s *Session) Retain(id highlight.State) { s.store.Retain(id) }

// Release implements highlight.StateStore.
func (s *Session) Release(id highlight.State) { s.store.Release(id) }

// Collect implements highlight.StateStore.
func (s *Session) Collect(budget int) bool { return s.store.Collect(budget) }

// CollectNodes counts collection visits for composite sources.
func (s *Session) CollectNodes(budget int) (int, bool) { return s.store.CollectNodes(budget) }

// RetainedStates measures currently retained tuples, independent of historical edits.
func (s *Session) RetainedStates() int { return s.store.Len() }

func clone(c context) context {
	c.Stack = append([]block(nil), c.Stack...)
	c.Templates = append([]int(nil), c.Templates...)
	c.Here = append([]heredoc(nil), c.Here...)
	c.ExpressionParens = append([]bool(nil), c.ExpressionParens...)
	c.Markup = append([]markup(nil), c.Markup...)
	return c
}

func normalize(c context) context {
	if len(c.Stack) == 0 {
		c.Stack = nil
	}
	if len(c.Templates) == 0 {
		c.Templates = nil
	}
	if len(c.Here) == 0 {
		c.Here = nil
	}
	if len(c.ExpressionParens) == 0 {
		c.ExpressionParens = nil
	}
	if len(c.Markup) == 0 {
		c.Markup = nil
	}
	return c
}

// HighlightBlock carries immutable lexical/structural context between lines.
func (s *Session) HighlightBlock(line string, previous highlight.State) ([]highlight.Span, highlight.State) {
	c, ok := s.store.Get(previous)
	if !ok {
		return nil, 0
	}
	if s.config.rules.mapping && c.Scalar > 0 {
		if strings.TrimSpace(line) == "" || len(indent.Leading(line)) >= c.Scalar {
			spans := []highlight.Span{}
			if line != "" {
				spans = append(spans, highlight.Span{Start: 0, End: len(line), Style: highlight.String})
			}
			return spans, previous
		}
		c.Scalar = 0
	}
	spans, tokens, next := s.scan(line, clone(c))
	if s.config.rules.mapping {
		spans = mappingKeys(line, spans, tokens)
	}
	next, _ = s.structure(line, tokens, c, next)
	next = normalize(next)
	if next.Mode == "" && len(next.Stack) == 0 && len(next.Here) == 0 && len(next.Templates) == 0 && next.Last == "" && next.Scalar == 0 && next.Unit == "" && len(next.ExpressionParens) == 0 && len(next.Markup) == 0 {
		return spans, 0
	}
	return spans, s.store.Intern(next)
}

func endMode(c *context) {
	c.Mode, c.Close, c.Open = "", "", ""
	c.Escape, c.Style, c.Depth = 0, 0, 0
	c.Multiline, c.Continuation, c.Nested, c.Doubling = false, false, false, false
}

func (s *Session) scan(text string, c context) (spans []highlight.Span, tokens []Token, next context) {
	paint := func(start, end int, style highlight.Style) {
		if end > start && style != highlight.Normal {
			spans = append(spans, highlight.Span{Start: start, End: end, Style: style})
		}
	}
	commentPaint := func(start, end int) {
		for start < end {
			at, word := -1, ""
			for _, alert := range []string{"TODO", "FIXME", "XXX", "HACK"} {
				if n := strings.Index(text[start:end], alert); n >= 0 && (at < 0 || n < at) {
					at, word = n, alert
				}
			}
			if at < 0 {
				paint(start, end, highlight.Comment)
				break
			}
			paint(start, start+at, highlight.Comment)
			paint(start+at, start+at+len(word), highlight.Alert)
			start += at + len(word)
		}
	}
	if len(c.Here) > 0 {
		body := text
		if c.Here[0].Tabs {
			body = strings.TrimLeft(body, "\t")
		}
		paint(0, len(text), highlight.VerbatimString)
		if body == c.Here[0].Word {
			c.Here = c.Here[1:]
		}
		return spans, nil, c
	}
	i := 0
	consume := func(start int) {
		from := start
		for i < len(text) {
			if c.Mode == "comment" {
				if c.Nested && c.Open != "" && strings.HasPrefix(text[i:], c.Open) {
					c.Depth++
					i += len(c.Open)
					continue
				}
				if strings.HasPrefix(text[i:], c.Close) {
					i += len(c.Close)
					c.Depth--
					if c.Depth == 0 {
						commentPaint(from, i)
						endMode(&c)
						return
					}
					continue
				}
				i++
				continue
			}
			if c.Close == "`" && s.config.regexp && strings.HasPrefix(text[i:], "${") {
				paint(from, i, c.Style)
				paint(i, i+2, highlight.Operator)
				i += 2
				endMode(&c)
				c.Templates = append(c.Templates, 0)
				c.Last = "operand"
				return
			}
			if c.Escape != 0 && text[i] == c.Escape {
				paint(from, i, c.Style)
				at := i
				i++
				if i < len(text) {
					_, n := utf8.DecodeRuneInString(text[i:])
					i += n
				}
				paint(at, i, highlight.SpecialChar)
				from = i
				if i == len(text) && at == len(text)-1 && c.Continuation {
					return
				}
				continue
			}
			if strings.HasPrefix(text[i:], c.Close) {
				if c.Doubling && strings.HasPrefix(text[i+len(c.Close):], c.Close) {
					i += 2 * len(c.Close)
					continue
				}
				i += len(c.Close)
				paint(from, i, c.Style)
				endMode(&c)
				c.Last = "value"
				return
			}
			_, n := utf8.DecodeRuneInString(text[i:])
			i += n
		}
		if c.Mode == "comment" {
			commentPaint(from, i)
		} else {
			paint(from, i, c.Style)
			if !c.Multiline {
				endMode(&c)
			}
		}
	}
	if c.Mode != "" {
		consume(0)
	}
	for i < len(text) {
		if c.Mode != "" {
			break
		}
		if len(c.Markup) > 0 && c.Markup[len(c.Markup)-1].Mode != "expr" {
			i = scanMarkup(text, i, &c, paint)
			continue
		}
		r, n := utf8.DecodeRuneInString(text[i:])
		if unicode.IsSpace(r) {
			i += n
			continue
		}
		start := i
		if s.config.markup && r == '<' && (c.Last == "" || c.Last == "operand") && startsMarkup(text, i) {
			c.Markup = append(c.Markup, markup{Mode: "tag"})
			i = scanMarkup(text, i, &c, paint)
			continue
		}
		matched := false
		for _, rule := range s.config.dynamicComments {
			if q, ok := rule(text, i); ok {
				c.Mode, c.Open, c.Close, c.Depth = "comment", q.Open, q.Close, 1
				i += len(q.Open)
				consume(start)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		for _, mark := range s.config.comments {
			if strings.HasPrefix(text[i:], mark) && (s.config.commentBoundary == nil || s.config.commentBoundary(text, i)) {
				commentPaint(i, len(text))
				i = len(text)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		for _, b := range s.config.blocks {
			if strings.HasPrefix(text[i:], b.open) {
				c.Mode, c.Open, c.Close, c.Depth, c.Nested = "comment", b.open, b.close, 1, b.nested
				i += len(b.open)
				consume(start)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		for _, rule := range s.config.tokens {
			if end, st, ok := rule(text, i); ok && end > i && end <= len(text) {
				paint(i, end, st)
				tokens = append(tokens, Token{text[i:end], i, end, st})
				i = end
				c.Last = "value"
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		var q Quote
		for _, rule := range s.config.literals {
			if got, ok := rule(text, i); ok {
				q, matched = got, true
				break
			}
		}
		if !matched {
			for _, form := range s.config.quotes {
				if strings.HasPrefix(text[i:], form.Open) {
					q, matched = form, true
					break
				}
			}
		}
		if matched {
			c.Mode, c.Close, c.Escape, c.Style, c.Multiline, c.Continuation = "string", q.Close, q.Escape, q.Style, q.Multiline, q.Continuation
			c.Doubling = q.Doubling
			i += len(q.Open)
			consume(start)
			continue
		}
		if s.config.here && strings.HasPrefix(text[i:], "<<") && !strings.HasPrefix(text[i:], "<<<") {
			if end, h, ok := readHere(text, i); ok {
				paint(i, i+2, highlight.Operator)
				paint(i+2, end, highlight.String)
				c.Here = append(c.Here, h)
				i = end
				continue
			}
		}
		if s.config.regexp && r == '/' && (c.Last == "" || c.Last == "operand") {
			i++
			class := false
			for i < len(text) {
				if text[i] == '\\' {
					i = min(i+2, len(text))
					continue
				}
				if text[i] == '[' {
					class = true
				}
				if text[i] == ']' {
					class = false
				}
				if text[i] == '/' && !class {
					i++
					for i < len(text) && unicode.IsLetter(rune(text[i])) {
						i++
					}
					break
				}
				i++
			}
			paint(start, i, highlight.SpecialString)
			c.Last = "value"
			continue
		}
		if r >= '0' && r <= '9' || r == '.' && i+1 < len(text) && text[i+1] >= '0' && text[i+1] <= '9' {
			st := highlight.DecVal
			i += n
			if r == '.' {
				st = highlight.Float
			}
			base := r == '0' && i < len(text) && strings.ContainsRune("xXoObB", rune(text[i]))
			if base {
				st = highlight.BaseN
				i++
			}
			for i < len(text) {
				ch := text[i]
				if ch >= '0' && ch <= '9' || ch == '_' || base && strings.ContainsRune("abcdefABCDEF", rune(ch)) {
					i++
					continue
				}
				if !base && (ch == '.' && i+1 < len(text) && text[i+1] != '.' || ch == 'e' || ch == 'E') {
					st = highlight.Float
					i++
					if i < len(text) && (text[i-1] == 'e' || text[i-1] == 'E') && (text[i] == '+' || text[i] == '-') {
						i++
					}
					continue
				}
				break
			}
			paint(start, i, st)
			tokens = append(tokens, Token{text[start:i], start, i, st})
			c.Last = "value"
			continue
		}
		if r == '_' || unicode.IsLetter(r) || r == '$' && s.config.dollar {
			i += n
			for i < len(text) {
				rr, nn := utf8.DecodeRuneInString(text[i:])
				if rr != '_' && !unicode.IsLetter(rr) && !unicode.IsDigit(rr) && !(rr == '$' && s.config.dollar) {
					break
				}
				i += nn
			}
			word := text[start:i]
			st := s.config.words[word]
			if c.Last == "member" {
				st = highlight.Attribute
			}
			if st == highlight.Normal && strings.HasPrefix(strings.TrimLeft(text[i:], " \t"), "(") {
				st = highlight.Function
			}
			if st == highlight.Normal && unicode.IsUpper(r) {
				st = highlight.DataType
			}
			paint(start, i, st)
			tokens = append(tokens, Token{word, start, i, st})
			if s.config.expressionControls[word] {
				c.Last = "control"
			} else if s.config.expressionWords[word] || st == highlight.ControlFlow && word != "break" && word != "continue" {
				c.Last = "operand"
			} else {
				c.Last = "value"
			}
			continue
		}
		if len(c.Templates) > 0 && r == '}' && c.Templates[len(c.Templates)-1] == 0 {
			paint(i, i+1, highlight.Operator)
			i++
			c.Templates = c.Templates[:len(c.Templates)-1]
			c.Mode, c.Close, c.Escape, c.Style, c.Multiline = "string", "`", '\\', highlight.String, true
			consume(i)
			continue
		}
		if len(c.Markup) > 0 && c.Markup[len(c.Markup)-1].Mode == "expr" {
			m := &c.Markup[len(c.Markup)-1]
			if r == '}' && m.ExpressionDepth == 0 {
				paint(i, i+1, highlight.Operator)
				i++
				m.Mode = m.Resume
				continue
			}
			if r == '{' {
				m.ExpressionDepth++
			}
			if r == '}' {
				m.ExpressionDepth--
			}
		}
		if len(c.Templates) > 0 {
			if r == '{' {
				c.Templates[len(c.Templates)-1]++
			}
			if r == '}' {
				c.Templates[len(c.Templates)-1]--
			}
		}
		i += n
		for _, op := range []string{"===", "!==", "=>", "==", "!=", "<=", ">=", "::", "&&", "||", "++", "--", "..", "**", "?.", ":="} {
			if strings.HasPrefix(text[start:], op) {
				i = start + len(op)
				break
			}
		}
		word := text[start:i]
		st := highlight.Normal
		if strings.ContainsRune("+-*/%=<>!&|^~?:,;.", r) {
			st = highlight.Operator
			paint(start, i, st)
		}
		tokens = append(tokens, Token{word, start, i, st})
		if word == "(" {
			c.ExpressionParens = append(c.ExpressionParens, c.Last == "control")
		}
		controlClose := false
		if word == ")" && len(c.ExpressionParens) > 0 {
			controlClose = c.ExpressionParens[len(c.ExpressionParens)-1]
			c.ExpressionParens = c.ExpressionParens[:len(c.ExpressionParens)-1]
		}
		if controlClose {
			c.Last = "operand"
		} else if word == "." || word == "?." {
			c.Last = "member"
		} else if word == ")" || word == "]" || word == "}" {
			c.Last = "value"
		} else {
			c.Last = "operand"
		}
	}
	if !s.config.regexp {
		c.Last = ""
	}
	return spans, tokens, c
}

func readHere(text string, at int) (int, heredoc, bool) {
	i := at + 2
	h := heredoc{}
	if i < len(text) && text[i] == '-' {
		h.Tabs = true
		i++
	}
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	quote := byte(0)
	if i < len(text) && (text[i] == '\'' || text[i] == '"') {
		quote = text[i]
		i++
	}
	start := i
	for i < len(text) {
		if quote != 0 && text[i] == quote {
			h.Word = text[start:i]
			return i + 1, h, h.Word != ""
		}
		if quote == 0 && (unicode.IsSpace(rune(text[i])) || strings.ContainsRune(";&|<>()", rune(text[i]))) {
			break
		}
		i++
	}
	h.Word = text[start:i]
	return i, h, h.Word != "" && quote == 0
}

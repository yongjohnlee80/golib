package lexical

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"strings"
)

type rules struct {
	braces       bool
	suites       map[string]bool
	open         map[string]string
	close        map[string]bool
	continuation map[string]bool
	mapping      bool
}

// BracedIndent uses structural delimiter context with the language's default unit.
func BracedIndent(unit string) Option {
	return func(c *configuration) { c.unit = unit; c.rules.braces = true }
}

// SuiteIndent declares colon-suite openers and continuation words.
func SuiteIndent(unit, openers, continuations string) Option {
	return func(c *configuration) {
		c.unit, c.rules.braces = unit, true
		c.rules.suites, c.rules.continuation = wordSet(openers), wordSet(continuations)
	}
}

// KeywordIndent declares block keywords; values name their matching close token.
func KeywordIndent(unit string, open map[string]string, closers, continuations string) Option {
	return func(c *configuration) {
		c.unit, c.rules.braces = unit, true
		c.rules.open = make(map[string]string, len(open))
		for k, v := range open {
			c.rules.open[k] = v
		}
		c.rules.close, c.rules.continuation = wordSet(closers), wordSet(continuations)
	}
}

// MappingIndent enables mapping/sequence/scalar mechanics chosen by a data language.
func MappingIndent(unit string) Option {
	return func(c *configuration) { c.unit = unit; c.rules.mapping = true }
}

func wordSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(words) {
		out[w] = true
	}
	return out
}

func (s *Session) closer(c context, word string) (string, bool) {
	r := s.config.rules
	if len(c.Stack) == 0 {
		return "", false
	}
	for i := len(c.Stack) - 1; i >= 0; i-- {
		b := c.Stack[i]
		if b.Close == word || r.continuation[word] && (b.Kind == "suite" || b.Kind == "keyword") {
			return b.Prefix, true
		}
	}
	return "", false
}

func (s *Session) structure(line string, tokens []Token, before, after context) (context, bool) {
	r := s.config.rules
	if len(tokens) == 0 {
		return after, false
	}
	prefix := indent.Leading(line)
	if len(before.Stack) > 0 && prefix != "" && !(strings.Contains(prefix, "\t") && strings.Contains(prefix, " ")) {
		base := before.Stack[len(before.Stack)-1].Prefix
		if strings.HasPrefix(prefix, base) && len(prefix) > len(base) {
			after.Unit = prefix[len(base):]
		}
	}
	if base, ok := s.closer(before, tokens[0].Text); ok {
		prefix = base
	}
	opened := make([]bool, len(after.Stack))
	push := func(b block) { after.Stack = append(after.Stack, b); opened = append(opened, true) }
	pop := func(n int) { after.Stack = after.Stack[:n]; opened = opened[:n] }
	if r.suites != nil {
		for len(after.Stack) > 0 {
			b := after.Stack[len(after.Stack)-1]
			if b.Kind != "suite" || len(b.Prefix) < len(prefix) {
				break
			}
			pop(len(after.Stack) - 1)
		}
	}
	for i, t := range tokens {
		word := t.Text
		if r.braces {
			if close, ok := map[string]string{"{": "}", "(": ")", "[": "]"}[word]; ok {
				push(block{Kind: word, Close: close, Prefix: prefix})
				continue
			}
			if word == "}" || word == ")" || word == "]" {
				if len(after.Stack) > 0 && after.Stack[len(after.Stack)-1].Close == word {
					pop(len(after.Stack) - 1)
				}
				continue
			}
		}
		if r.open != nil && (r.close[word] || r.continuation[word]) {
			continuationClose := ""
			for j := len(after.Stack) - 1; j >= 0; j-- {
				b := after.Stack[j]
				if b.Kind == "keyword" && (b.Close == word || r.continuation[word]) {
					continuationClose = b.Close
					pop(j)
					break
				}
			}
			if word == "else" && continuationClose != "" {
				push(block{Kind: "keyword", Close: continuationClose, Prefix: prefix})
			}
		}
		if close, ok := r.open[word]; ok {
			// A loop's do, rather than for/while as well, opens its block.
			push(block{Kind: "keyword", Close: close, Prefix: prefix})
		}
		if r.suites != nil && i == len(tokens)-1 && word == ":" {
			first := tokens[0].Text
			if first == "async" && len(tokens) > 1 {
				first = tokens[1].Text
			}
			if r.suites[first] {
				push(block{Kind: "suite", Close: "suite", Prefix: prefix})
			}
		}
	}
	if r.mapping {
		for _, t := range tokens {
			if t.Text == ":" {
				value := strings.TrimSpace(line[t.End:])
				if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
					after.Scalar = len(prefix) + 1
				}
			}
		}
	}
	return after, len(opened) > 0 && opened[len(opened)-1]
}

// Indent analyzes only the current line using the source's verified incoming state.
// Scratch analysis does not allocate persistent state identifiers.
func (s *Session) Indent(req indent.Request) (indent.Decision, bool) {
	if !req.StateKnown || s.config.unit == "" {
		return indent.Decision{}, false
	}
	c, ok := s.store.Get(highlight.State(req.PreviousState))
	if !ok {
		return indent.Decision{}, false
	}
	prefix := indent.Leading(req.Line)
	d := indent.Decision{Prefix: prefix}
	if len(c.Here) > 0 || c.Scalar > 0 && len(prefix) >= c.Scalar {
		return d, true
	}
	column := max(0, min(req.Column, len(req.Line)))
	if req.Operation == indent.OpenAbove {
		return d, true
	}
	text := req.Line[:column]
	if req.Operation == indent.Closing || req.Operation == indent.OpenBelow {
		text = req.Line
	}
	_, tokens, after := s.scan(text, clone(c))
	if c.Mode != "" && len(tokens) == 0 || after.Mode != "" {
		return d, true
	}
	if req.Operation == indent.Closing {
		if len(tokens) > 0 {
			if base, found := s.closer(c, tokens[0].Text); found {
				d.Prefix = base
				return d, true
			}
		}
		return indent.Decision{}, false
	}
	after, newBody := s.structure(text, tokens, c, after)
	unit := req.Unit
	if unit == "" {
		unit = c.Unit
		if unit == "" {
			unit = s.config.unit
		}
		if prefix != "" && !strings.ContainsRune(prefix, '\t') && unit == "\t" {
			unit = "    "
		}
		if strings.Contains(prefix, "\t") {
			unit = "\t"
		}
	}
	if newBody {
		d.Prefix = after.Stack[len(after.Stack)-1].Prefix + unit
	}
	if len(tokens) > 0 {
		if base, found := s.closer(c, tokens[0].Text); found && !newBody {
			d.Prefix = base
		}
	}
	if s.config.rules.mapping && len(tokens) > 0 {
		last := tokens[len(tokens)-1]
		if last.Text == ":" && strings.TrimSpace(text[last.End:]) == "" || strings.TrimSpace(text) == "-" {
			d.Prefix = prefix + unit
		}
		if after.Scalar > 0 {
			d.Prefix = prefix + unit
		}
	}
	if req.Operation == indent.Newline && len(tokens) > 0 && newBody {
		last := tokens[len(tokens)-1].Text
		top := after.Stack[len(after.Stack)-1]
		right := strings.TrimSpace(req.Line[column:])
		if (last == "{" || last == "(" || last == "[") && strings.HasPrefix(right, top.Close) {
			d.SplitClosing, d.ClosingPrefix = true, top.Prefix
		}
	}
	return d, true
}

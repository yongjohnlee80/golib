package lexical

import (
	"github.com/yongjohnlee80/golib/highlight"
	"strings"
	"unicode"
	"unicode/utf8"
)

func startsMarkup(text string, at int) bool {
	if at+1 >= len(text) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(text[at+1:])
	if r != '>' && !unicode.IsLetter(r) {
		return false
	}
	// TypeScript generic arrows have a type parameter, not a tag body.
	if end := strings.IndexByte(text[at:], '>'); end >= 0 {
		rest := strings.TrimSpace(text[at+end+1:])
		if strings.HasPrefix(rest, "(") && strings.Contains(rest, "=>") {
			return false
		}
	}
	return true
}

func scanMarkup(text string, i int, c *context, paint func(int, int, highlight.Style)) int {
	for i < len(text) && len(c.Markup) > 0 {
		m := &c.Markup[len(c.Markup)-1]
		if m.Mode == "expr" {
			return i
		}
		if m.Mode == "text" {
			if text[i] == '<' {
				m.Mode = "tag"
				m.Name = false
				m.Closing = i+1 < len(text) && text[i+1] == '/'
				continue
			}
			if text[i] == '{' {
				paint(i, i+1, highlight.Operator)
				i++
				m.Resume = "text"
				m.Mode = "expr"
				m.ExpressionDepth = 0
				c.Last = "operand"
				return i
			}
			_, n := utf8.DecodeRuneInString(text[i:])
			i += n
			continue
		}
		if m.Quote != 0 {
			start := i
			for i < len(text) {
				ch := text[i]
				i++
				if ch == m.Quote {
					m.Quote = 0
					break
				}
			}
			paint(start, i, highlight.String)
			continue
		}
		if text[i] == '<' {
			end := i + 1
			if end < len(text) && text[end] == '/' {
				m.Closing = true
				end++
			}
			paint(i, end, highlight.Operator)
			i = end
			continue
		}
		if strings.HasPrefix(text[i:], "/>") || text[i] == '>' {
			self := text[i] == '/'
			end := i + 1
			if self {
				end++
			}
			paint(i, end, highlight.Operator)
			i = end
			if !self {
				if m.Closing {
					m.Depth--
				} else {
					m.Depth++
				}
			}
			if m.Depth == 0 {
				c.Markup = c.Markup[:len(c.Markup)-1]
				c.Last = "value"
			} else {
				m.Mode = "text"
			}
			continue
		}
		if text[i] == '\'' || text[i] == '"' {
			m.Quote = text[i]
			start := i
			i++
			for i < len(text) {
				ch := text[i]
				i++
				if ch == m.Quote {
					m.Quote = 0
					break
				}
			}
			paint(start, i, highlight.String)
			continue
		}
		if text[i] == '{' {
			paint(i, i+1, highlight.Operator)
			i++
			m.Resume = "tag"
			m.Mode = "expr"
			m.ExpressionDepth = 0
			c.Last = "operand"
			return i
		}
		r, n := utf8.DecodeRuneInString(text[i:])
		if unicode.IsLetter(r) || r == '_' {
			start := i
			i += n
			for i < len(text) {
				r, n = utf8.DecodeRuneInString(text[i:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_:-.", r) {
					break
				}
				i += n
			}
			st := highlight.Attribute
			if !m.Name {
				st = highlight.DataType
				m.Name = true
			}
			paint(start, i, st)
			continue
		}
		if text[i] == '=' {
			paint(i, i+1, highlight.Operator)
		}
		i += n
	}
	return i
}

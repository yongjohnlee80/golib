package lua

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
	"strings"
)

// Definition describes Lua files, long-bracket literals/comments and block keywords.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "Lua", Extensions: []string{"*.lua"}, Aliases: []string{"lua"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		return lexical.New(
			lexical.Words(highlight.Keyword, "and function local not or"), lexical.Words(highlight.ControlFlow, "break do else elseif end for goto if in repeat return then until while"), lexical.Words(highlight.Constant, "true false nil"),
			lexical.Words(highlight.BuiltIn, "assert collectgarbage dofile error getmetatable ipairs load loadfile next pairs pcall print rawequal rawget rawlen rawset require select setmetatable tonumber tostring type xpcall"),
			lexical.DynamicComments(longComment), lexical.LineComments(nil, "--"), lexical.Literals(longLiteral),
			lexical.Quotes(lexical.Quote{Open: "\"", Close: "\"", Escape: '\\', Continuation: true, Style: highlight.String}, lexical.Quote{Open: "'", Close: "'", Escape: '\\', Continuation: true, Style: highlight.String}),
			lexical.KeywordIndent("  ", map[string]string{"function": "end", "then": "end", "do": "end", "repeat": "until"}, "end until", "else elseif")).Source()
	}}
}

func longLiteral(line string, at int) (lexical.Quote, bool) {
	if line[at] != '[' {
		return lexical.Quote{}, false
	}
	i := at + 1
	for i < len(line) && line[i] == '=' {
		i++
	}
	if i >= len(line) || line[i] != '[' {
		return lexical.Quote{}, false
	}
	return lexical.Quote{Open: line[at : i+1], Close: "]" + strings.Repeat("=", i-at-1) + "]", Multiline: true, Style: highlight.VerbatimString}, true
}
func longComment(line string, at int) (lexical.Quote, bool) {
	if !strings.HasPrefix(line[at:], "--") || at+2 >= len(line) {
		return lexical.Quote{}, false
	}
	q, ok := longLiteral(line, at+2)
	if ok {
		q.Open = "--" + q.Open
	}
	return q, ok
}

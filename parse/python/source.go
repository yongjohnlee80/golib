package python

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
	"strings"
)

// Definition describes Python files with literal continuation and colon-suite rules.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "Python", Extensions: []string{"*.py", "*.pyw"}, Aliases: []string{"python", "py"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		return lexical.New(
			lexical.Words(highlight.Keyword, "and as assert async await class def del global is lambda nonlocal not or pass with"),
			lexical.Words(highlight.Import, "import from"), lexical.Words(highlight.ControlFlow, "break continue elif else except finally for if in match case raise return try while yield"),
			lexical.Words(highlight.Constant, "True False None Ellipsis NotImplemented"),
			lexical.Words(highlight.BuiltIn, "abs all any bool bytes dict enumerate filter float frozenset int isinstance len list map max min object open print range repr reversed set sorted str sum super tuple type zip"),
			lexical.LineComments(nil, "#"), lexical.Literals(literal),
			lexical.SuiteIndent("    ", "if elif else for while def class try except finally with match case", "elif else except finally")).Source()
	}}
}

func literal(line string, at int) (lexical.Quote, bool) {
	i := at
	for i < len(line) && i-at < 2 && strings.ContainsRune("rRbBuUfF", rune(line[i])) {
		i++
	}
	if i >= len(line) || (line[i] != '\'' && line[i] != '"') {
		return lexical.Quote{}, false
	}
	ch := string(line[i])
	close := ch
	multiline := strings.HasPrefix(line[i:], strings.Repeat(ch, 3))
	if multiline {
		close = strings.Repeat(ch, 3)
	}
	escape := byte('\\')
	if strings.ContainsAny(line[at:i], "rR") {
		escape = 0
	}
	return lexical.Quote{Open: line[at:i] + close, Close: close, Escape: escape, Multiline: multiline, Continuation: true, Style: highlight.String}, true
}

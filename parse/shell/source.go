package shell

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
	"strings"
	"unicode"
)

// Definition describes POSIX/Bash files, variables, heredocs and keyword blocks.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "Shell", Extensions: []string{"*.sh", "*.bash", ".bashrc", ".bash_profile"}, Aliases: []string{"shell", "sh", "bash"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		return lexical.New(
			lexical.Words(highlight.Keyword, "function local export readonly declare typeset unset alias unalias"), lexical.Words(highlight.ControlFlow, "if then else elif fi for in do done while until case esac select return break continue"),
			lexical.Words(highlight.BuiltIn, "echo printf read cd pwd source exit trap set shift eval exec test true false command builtin"),
			lexical.LineComments(commentBoundary, "#"), lexical.Tokens(variable), lexical.HereDocuments(),
			lexical.Quotes(lexical.Quote{Open: "'", Close: "'", Multiline: true, Style: highlight.String}, lexical.Quote{Open: "\"", Close: "\"", Escape: '\\', Multiline: true, Style: highlight.String}, lexical.Quote{Open: "`", Close: "`", Escape: '\\', Multiline: true, Style: highlight.VerbatimString}),
			lexical.KeywordIndent("  ", map[string]string{"then": "fi", "do": "done", "case": "esac"}, "fi done esac", "else elif")).Source()
	}}
}

func commentBoundary(line string, at int) bool {
	return at == 0 || unicode.IsSpace(rune(line[at-1])) || strings.ContainsRune(";&|()", rune(line[at-1]))
}
func variable(line string, at int) (int, highlight.Style, bool) {
	if line[at] != '$' || at+1 >= len(line) {
		return 0, 0, false
	}
	i := at + 1
	if line[i] == '{' {
		i++
		for i < len(line) && line[i] != '}' {
			i++
		}
		if i < len(line) {
			i++
		}
		return i, highlight.Variable, true
	}
	if strings.ContainsRune("@*#?$!-0123456789", rune(line[i])) {
		return i + 1, highlight.Variable, true
	}
	for i < len(line) && (line[i] == '_' || unicode.IsLetter(rune(line[i])) || unicode.IsDigit(rune(line[i]))) {
		i++
	}
	return i, highlight.Variable, i > at+1
}

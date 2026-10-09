package yaml

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
	"unicode"
)

// Definition describes editable YAML independently of strict document admission.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "YAML", Extensions: []string{"*.yaml", "*.yml"}, Aliases: []string{"yaml", "yml"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		return lexical.New(lexical.Words(highlight.Constant, "true false null True False Null TRUE FALSE NULL"),
			lexical.LineComments(func(line string, at int) bool { return at == 0 || unicode.IsSpace(rune(line[at-1])) }, "#"),
			lexical.Quotes(lexical.Quote{Open: "\"", Close: "\"", Escape: '\\', Multiline: true, Style: highlight.String}, lexical.Quote{Open: "'", Close: "'", Multiline: true, Doubling: true, Style: highlight.String}), lexical.MappingIndent("  ")).Source()
	}}
}

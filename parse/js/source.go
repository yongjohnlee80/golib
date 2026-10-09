package js

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/javascript"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
)

// Definition describes JavaScript source independently of the expression evaluator.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "JavaScript", Extensions: []string{"*.js", "*.mjs", "*.cjs", "*.jsx"}, Aliases: []string{"javascript", "js"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		return lexical.New(javascript.Options()...).Source()
	}}
}

package typescript

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/javascript"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
)

// Definition describes TypeScript source; it does not extend an expression evaluator.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "TypeScript", Extensions: []string{"*.ts", "*.tsx"}, Aliases: []string{"typescript", "ts", "tsx"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		opts := append(javascript.Options(),
			lexical.Words(highlight.Keyword, "interface type enum namespace module declare abstract implements public private protected readonly static override keyof infer is asserts satisfies unique constructor"),
			lexical.Words(highlight.DataType, "any unknown never void number string boolean bigint symbol object"))
		return lexical.New(opts...).Source()
	}}
}

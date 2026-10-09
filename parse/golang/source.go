package golang

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
)

// Definition describes Go files and creates independent source behavior.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "Go", Extensions: []string{"*.go"}, Aliases: []string{"go", "golang"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		return lexical.New(
			lexical.Words(highlight.Keyword, "package const var func type struct interface map chan go defer range select fallthrough"),
			lexical.Words(highlight.Import, "import"),
			lexical.Words(highlight.ControlFlow, "if else for switch case default return break continue goto"),
			lexical.Words(highlight.Constant, "true false nil iota"),
			lexical.Words(highlight.DataType, "bool byte rune string int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 uintptr float32 float64 complex64 complex128 any comparable"),
			lexical.Words(highlight.BuiltIn, "append cap clear close complex copy delete imag len make max min new panic print println real recover"),
			lexical.LineComments(nil, "//"), lexical.BlockComment("/*", "*/", false),
			lexical.Quotes(lexical.Quote{Open: "`", Close: "`", Multiline: true, Style: highlight.VerbatimString},
				lexical.Quote{Open: "\"", Close: "\"", Escape: '\\', Style: highlight.String}, lexical.Quote{Open: "'", Close: "'", Escape: '\\', Style: highlight.Char}),
			lexical.BracedIndent("\t")).Source()
	}}
}

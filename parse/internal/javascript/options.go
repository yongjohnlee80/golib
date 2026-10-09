package javascript

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
)

// Options is the shared JavaScript lexical base, extended by TypeScript vocabulary.
func Options() []lexical.Option {
	return []lexical.Option{
		lexical.Words(highlight.Keyword, "var let const function new delete typeof instanceof in of this class extends super yield async await void with debugger"),
		lexical.Words(highlight.Import, "import export from as"), lexical.Words(highlight.ControlFlow, "if else for while do switch case default break continue return try catch finally throw"), lexical.Words(highlight.Constant, "true false null undefined NaN Infinity"),
		lexical.Words(highlight.BuiltIn, "console Math JSON Object Array String Number Boolean Symbol BigInt Promise Set Map WeakMap WeakSet Date RegExp Error globalThis"),
		lexical.LineComments(nil, "//"), lexical.BlockComment("/*", "*/", false), lexical.DollarIdentifiers(), lexical.RegularExpressions(),
		lexical.Expressions("return throw yield await new typeof void delete case", "if while for switch catch"), lexical.MarkupExpressions(),
		lexical.Quotes(lexical.Quote{Open: "`", Close: "`", Escape: '\\', Multiline: true, Template: true, Style: highlight.String}, lexical.Quote{Open: "\"", Close: "\"", Escape: '\\', Continuation: true, Style: highlight.String}, lexical.Quote{Open: "'", Close: "'", Escape: '\\', Continuation: true, Style: highlight.String}), lexical.BracedIndent("  "),
	}
}

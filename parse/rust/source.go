package rust

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/internal/lexical"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Definition describes Rust files with nested comments, raw literals and lifetimes.
func Definition() highlight.Definition {
	return highlight.Definition{Name: "Rust", Extensions: []string{"*.rs"}, Aliases: []string{"rust", "rs"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		return lexical.New(
			lexical.Words(highlight.Keyword, "as const crate dyn enum extern fn impl let mod move mut pub ref self Self static struct super trait type unsafe use where async await"),
			lexical.Words(highlight.ControlFlow, "break continue else for if in loop match return while yield"),
			lexical.Words(highlight.Constant, "true false None Some Ok Err"),
			lexical.Words(highlight.DataType, "bool char str i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64"),
			lexical.LineComments(nil, "//"), lexical.BlockComment("/*", "*/", true), lexical.Literals(rawLiteral), lexical.Tokens(lifetime),
			lexical.Quotes(lexical.Quote{Open: "\"", Close: "\"", Escape: '\\', Multiline: true, Style: highlight.String}, lexical.Quote{Open: "'", Close: "'", Escape: '\\', Style: highlight.Char}),
			lexical.BracedIndent("    ")).Source()
	}}
}

func rawLiteral(line string, at int) (lexical.Quote, bool) {
	i := at
	if strings.HasPrefix(line[i:], "br") || strings.HasPrefix(line[i:], "cr") {
		i += 2
	} else if strings.HasPrefix(line[i:], "r") {
		i++
	} else {
		return lexical.Quote{}, false
	}
	start := i
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i >= len(line) || line[i] != '"' {
		return lexical.Quote{}, false
	}
	return lexical.Quote{Open: line[at : i+1], Close: "\"" + strings.Repeat("#", i-start), Multiline: true, Style: highlight.VerbatimString}, true
}

func lifetime(line string, at int) (int, highlight.Style, bool) {
	if line[at] != '\'' || at+1 >= len(line) {
		return 0, 0, false
	}
	i := at + 1
	r, n := utf8.DecodeRuneInString(line[i:])
	if r != '_' && !unicode.IsLetter(r) {
		return 0, 0, false
	}
	i += n
	for i < len(line) {
		r, n = utf8.DecodeRuneInString(line[i:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		i += n
	}
	if i < len(line) && line[i] == '\'' {
		return 0, 0, false
	}
	return i, highlight.DataType, true
}

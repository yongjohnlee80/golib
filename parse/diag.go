package parse

// Diagnostic helpers shared by every format in and below this package.
//
// They live here rather than in one format's file because a SyntaxError's Got
// field is written the same way by all of them, and a parser in a sub-package
// cannot reach an unexported helper next door. Two copies of "put quotes round
// it" is not a catastrophe; two copies that drift is a diagnostic that reads
// differently depending on which grammar failed.

// QuoteRune renders a rune for a [SyntaxError]'s Got field.
func QuoteRune(r rune) string { return `"` + string(r) + `"` }

// Quoted renders a string for a Want or Got field.
func Quoted(s string) string { return `"` + s + `"` }

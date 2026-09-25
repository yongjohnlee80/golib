// Package js parses the C-family expression and statement grammar that other formats embed.
//
// It is an autonomous grammar format in its own right, not an internal helper for one consumer.
// QML is its primary caller because a QML property value is a JavaScript expression and a
// QML signal handler is JavaScript statements, but the grammar remains faithful to the language.
// Consumers that evaluate only a subset of the syntax (such as [github.com/yongjohnlee80/golib/decl],
// which evaluates names, calls, and bitwise OR) reject unsupported constructs by name and position
// rather than truncating the parser's vocabulary.
//
// # Expression Precedence Ladder
//
// Expressions are parsed using recursive descent across precedence levels, loosest first:
//
//	Conditional    := Nullish [ '?' Assign ':' Assign ]
//	Nullish        := LogicalOr { '??' LogicalOr }
//	LogicalOr      := LogicalAnd { '||' LogicalAnd }
//	LogicalAnd     := BitOr { '&&' BitOr }
//	BitOr          := BitXor { '|' BitXor }
//	BitXor         := BitAnd { '^' BitAnd }
//	BitAnd         := Equality { '&' Equality }
//	Equality       := Relational { ( '===' | '!==' | '==' | '!=' ) Relational }
//	Relational     := Shift { ( '<=' | '>=' | '<' | '>' | 'instanceof' | 'in' ) Shift }
//	Shift          := Additive { ( '>>>' | '<<' | '>>' ) Additive }
//	Additive       := Multiplicative { ( '+' | '-' ) Multiplicative }
//	Multiplicative := Exponent { ( '*' | '/' | '%' ) Exponent }
//	Exponent       := Unary [ '**' Exponent ]            // right-associative
//	Unary          := ( '!' | '-' | '+' | '~' | 'typeof' | 'void' ) Unary | Postfix
//	Postfix        := Primary { '.' Ident | '?.' Ident | '[' Expr ']' | '(' Args ')' }
//	Primary        := Number | String | Template | Bool | Null | Undefined
//	                | Ident | Array | Object | '(' Expr ')'
//
// # Statement Grammar
//
// Statements represent sequences of commands (e.g. inside a signal handler body):
//
//	Block          := '{' { Statement } '}'
//	Statement      := Block | Declaration | If | Return | ExpressionStmt | Empty
//	Declaration    := ( 'let' | 'const' | 'var' ) Ident [ '=' Expression ] { ',' Ident [ '=' Expression ] } Terminator
//	If             := 'if' '(' Expression ')' Statement [ 'else' Statement ]
//	Return         := 'return' [ Expression ] Terminator
//	ExpressionStmt := Expression Terminator
//	Empty          := ';'
//
// Loops (`for`, `while`), `switch`, `try`/`catch`, and class declarations are refused by name
// with clear diagnostics rather than being misparsed as identifiers.
//
// # Dialects are Data
//
// C, C++, Java, C#, Go, JavaScript, TypeScript and PHP share this core expression precedence ladder.
// What differs between them is encoded as a declarative table ([ExprDialect]):
//   - [JavaScript]: Default dialect; supports `?:`, `?.`, template literals, `**`, and `null`.
//   - [C]: Supports `?:`, standard C operators, and `NULL`.
//   - [Go]: Replaces ternary and optional chaining with Go operators and `nil`.
//
// # Scanner Sharing and Unified Depth Budgets
//
// Embedding formats (such as [github.com/yongjohnlee80/golib/parse/qml]) hand their scanner to
// [NewDriver]. The driver parses expressions or statements directly from the active cursor and
// leaves the scanner positioned immediately after the parsed construct.
//
// Critically, the recursion depth budget ([DefaultExprMaxDepth]) is shared between the embedding
// format and the embedded JavaScript parser, preventing malicious or deeply nested files from
// causing stack overflows across grammar boundaries.
package js

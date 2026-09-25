// Package qml parses QML into a structured, typed abstract syntax tree ([SpecTree]).
//
// It is strictly faithful to QML syntax and contains no proprietary extensions. Styling
// and singletons follow standard QML conventions via module imports and singleton objects
// (e.g. `import editor.theme.retro 1.0` -> `Theme.menu.window`).
//
// # Syntax Grammar
//
// The parser evaluates the core QML object declaration language:
//
//	Root     := { Import } Node
//	Import   := 'import' DottedName [ Version ] [ 'as' Qualifier ]
//	Node     := TypeName '{' Body '}'                   // Text { }, Tui.Rectangle { }
//	Body     := ( Property | Handler | Group | Node )*
//	Property := PropName ':' Value [ ';' ]
//	Group    := PropName '{' ( Property )* '}'          // font { bold: true }
//	PropName := Ident { '.' Ident }                     // plain, grouped, or attached
//	Handler  := 'on' Ident ':' JavaScript               // statement or block
//	Value    := JavaScript                              // expression
//
// # Separation of Syntax from Semantics
//
// This package parses syntax, never semantics:
//   - It does not validate whether a type exists, whether a property accepts a specific type,
//     or whether a signal handler is reachable.
//   - The generated AST ([SpecTree]) faithfully records what was written. Consumers (such as
//     [github.com/yongjohnlee80/golib/decl]) validate names, types, and values against their own registries.
//   - Constructs parsed by this package that a consumer cannot evaluate are rejected by the consumer
//     with exact source positions (file, line, column).
//
// # Embedded JavaScript Parsing
//
// Property values (expressions) and signal handler bodies (statements) are parsed by
// [github.com/yongjohnlee80/golib/parse/js] using [js.NewDriver].
//
// The embedding driver operates over this parser's active [parse.Scanner] and shares a unified
// recursion depth budget ([DefaultQMLMaxDepth]), ensuring that nested QML objects and embedded
// JavaScript expressions cannot exceed recursion stack bounds.
//
// # Incremental Parsing and File Watching
//
// Input that terminates mid-construct produces a [parse.SyntaxError] with Incomplete=true.
// This allows live file watchers or hot reloader loops to distinguish between files that are
// actively being written to disk versus files containing syntax mistakes.
//
// # Syntax Highlighting
//
// [Highlighter] provides an incremental, line-by-line syntax highlighter for editor surfaces
// implementing [github.com/yongjohnlee80/golib/highlight.Highlighter]. It highlights QML keywords,
// properties, and embedded JavaScript expressions across multiline block comments and strings.
package qml

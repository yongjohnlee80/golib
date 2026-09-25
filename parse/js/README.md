# parse/js — the C-family expression and statement grammar

`js` parses the expressions and statements that C-family languages share, and
that other formats embed. QML is its first caller — a QML property value is a
JavaScript expression, a signal handler JavaScript statements — but nothing in
it knows that.

```go
import "github.com/yongjohnlee80/golib/parse/js"

expr, err := js.Expression{}.Parse([]byte(`a.b(c) + d ? "x" : 'y'`))
body, err := js.Statements{}.Parse([]byte(`if (dirty) { save(); } close()`))
```

## Dialects are data

C, C++, Java, C#, Go, JavaScript, TypeScript and PHP share one expression
grammar — the same precedence ladder, the same member/index/call chain, the same
unary and conditional forms. What differs is a TABLE: which operator spellings
exist, which keywords are literals, whether there are templates or a
conditional. So a dialect is an `ExprDialect` value, not a branch:

| dialect | notes |
| --- | --- |
| `js.JavaScript` | the default: `?:`, `?.`, template literals, `**`, `null` |
| `js.C` | `?:`; no `?.`, templates or `**`; `NULL` |
| `js.Go` | none of `?:`, `?.`, templates or `**`; `nil` |

```go
expr, err := js.Expression{Dialect: &js.Go}.Parse(src)
```

Adding a language is an entry in a table. Its precedence levels go loosest
first, and within a level the longest spelling first, so `>>>` is never read as
`>>` then `>`.

## The tree

`Expr` is one node: a `Kind` (identifier, literal, unary, binary, logical,
conditional, member, index, call, array, object, template…), its `Raw` text or
operator, its operands, and a `Pos`. `Stmt` is a statement: expression,
declaration (`let`/`const`/`var`), `if`, `return`, block. `WalkExprs` and
`WalkStmts` visit every node, template substitutions included.

The grammar is FAITHFUL: it reads the whole of the language's expression syntax.
A consumer that evaluates only part of it — golib's `decl` evaluates names,
calls and `|` — refuses the rest by name and position, rather than this package
pretending the syntax is wrong.

## Embedding

A format that embeds this grammar uses `Driver`, over a scanner the CALLER owns
and with the caller's depth budget:

```go
dr := js.NewDriver(scanner, &js.JavaScript, maxDepth, depthSpentSoFar)
value, err := dr.Expression()   // leaves the scanner where the expression ended
```

Both halves matter. The cursor has to come back where the embedded grammar left
it, and a budget per grammar would bound the outer document's nesting while
leaving the JavaScript inside it unbounded, in the same parse of the same file.

## Limits

`MaxDepth` bounds nesting (default `DefaultExprMaxDepth`, 64). One budget covers
statements and the expressions inside them, since the recursion alternates
between the two; an `else if` chain nests structurally and counts.

## Licence

See the repository's [LICENSE](../../LICENSE).

# parse/sql — SQL dialects as values

The third of three packages, and the one that makes the other two honest:

```
golib/streamcache   bytes, retention, views   — knows nothing of tokens
golib/parse         Source, Token, Form, Scan — knows nothing of SQL
golib/parse/sql     dialects as Form values   — knows nothing of a caller
```

Everything here is data — character classes, literal sets and an order — handed
to a lexer that has never heard of SQL. Adding a dialect means adding a function
here; nothing below it is edited. That is why the core is closed to no dialect:
it cannot name one.

```go
lex := parse.New(
    parse.WithForms(sql.PostgreSQL()...),
    parse.WithMaxDelimiter(1<<16),
)
err := lex.Validate(ctx, r) // or lex.Scan(ctx, r, parse.OwnReader)
```

## Install

```sh
go get github.com/yongjohnlee80/golib
```

```go
import (
    "github.com/yongjohnlee80/golib/parse"
    "github.com/yongjohnlee80/golib/parse/sql"
)
```

## Order is precedence

The slices come back in the order the lexer must try them, and the order carries
real decisions: `/*` before `/`, `--` before `-`, `E'` before the word run that
would otherwise eat the `E`. Every list ends with a one-byte fallback, so no byte
is ever left unclaimed.

## What a kind says

A kind says what a run of bytes **is**, never what it means. `SELECT` comes back
as a `Word`; that it is a verb in one statement and a column name in another is a
judgement for the layer above, made with the verbatim bytes still in front of it.
The forms carry no keywords for that reason; the highlighter, in its own file
(below), is where meaning is judged.

## Three constructs that are not what they look like

**PostgreSQL operator names are a grammar, not a list.** `CREATE OPERATOR` means
the set is open, so `PostgresOperator` implements the naming rules instead of
enumerating spellings: a run of ``+-*/<>=~!@#%^&|`?`` that may not contain `--`
or `/*`, and may not end in `+` or `-` unless it also contains one of
``~!@#%^&|`?``. That last rule is what keeps `1+-2` a sum while `@-` stays a
single name.

**MySQL's `--` is not ANSI's.** It is a comment only when whitespace or a control
byte follows, so `balance--1` is a subtraction. Reading it as a comment would
silently delete the rest of the line from the token stream.

**MySQL's `/*! … */` and `/*+ … */` are not comments at all** — the server
executes one and plans by the other. Trivia is what a consumer is invited to
discard, so `/*!50000 DROP TABLE t */` must not vanish inside a token someone was
told was safe to drop.

`MySQLExecutableOpen` matches the **opener alone** and hands back a delimiter,
and its `End` scans for the closer *without consuming it* — so the `*/` is still
required, and `/*!50000 DROP TABLE t` is an error rather than valid SQL. The
bytes between are left for the lexer to read as the tokens they are, which is why
`Word(DROP)` and `Word(TABLE)` show up in the stream. That is deliberately better
than one opaque token of some other kind: a caller looking for a dangerous
statement does not have to know to re-lex the inside of something it was handed
whole.

The ordinary `/* … */` sits behind that form, still `Comment`, still validating
its own closer, and still not nesting.

## Splitting a script into statements

`sql.SQL` splits a script at its semicolons, skipping the ones inside strings,
quoted identifiers and comments. The zero value reads the syntax the major
engines share; each field adds one engine's construct:

| field | engine | a `;` inside it does not end the statement |
|---|---|---|
| `Backticks` | MySQL, SQLite | `` `a;b` `` |
| `DollarQuotes` | PostgreSQL | `$$ … $$`, `$tag$ … $tag$` |
| `NestedBlockComments` | PostgreSQL | `/* … /* … */ … */` |
| `EStringEscapes` | PostgreSQL | `E'a\';b'` |
| `TriggerBodies` | SQLite | `CREATE [TEMP] TRIGGER … BEGIN a; b; END` |
| `HashComments` (option) | MySQL | `# a; b` to the end of the line |
| `MySQLDashComments` (option) | MySQL | `-- a; b`, but `--` before anything but a space is two minus signs |
| `BackslashEscapes` (option) | MySQL | `'a\';b'`, `"a\";b"` |
| `ExecutableComments` (option) | MySQL | none: a `/*! … */` body is statement text and its `;` ends one; a `/*+ … */` hint stays a comment |
| `BracketIdentifiers` (option) | SQLite | `[a;b]`, to the first `]`: nothing inside escapes, so `[a'b]` holds a quote |
| `TclParameters` (option) | SQLite | `$a(x;y)`: a `$name`, `@name`, `:name` or `#name` parameter's `(…)` suffix runs to the first `)` |

One preset per engine is the way to build a splitter for a known engine:
`PostgreSQLSplitter()`, `MySQLSplitter()` and `SQLiteSplitter()` are `New` with that engine's
options. `SQLiteSplitter()` is `TriggerBodies`, `Backticks`, `BracketIdentifiers` and
`TclParameters`.

`TclParameters` reads a parameter as SQLite's tokenizer does. Its name is letters, digits, `_`, `$`
and bytes past ASCII, with `::` allowed inside, so `$$x; SELECT 2 $$` is the parameter `$$x`, a
`;`, and a second statement, never a dollar-quoted string. A `(…)` suffix may follow a name and may
not hold whitespace. SQLite refuses a prefix with no name (`# a comment`, `x::text`, `@>`, `$ `)
and a suffix cut off by whitespace or the end of the text, and so does the splitter: under this
option `#` is never a comment. An unclosed `[` is unterminated, as an unclosed quote is.

`TriggerBodies` is the one place the splitter reads words, because SQLite does:
it decides whether a `CREATE TRIGGER` is complete by `CREATE`, `TEMP`/`TEMPORARY`,
`TRIGGER`, `END` and `EXPLAIN`, compared ignoring ASCII case only, and the
splitter follows `sqlite3_complete`'s state table for exactly those. A body a `;` has opened and no `END;` has closed is
unterminated, like an unclosed string.

```go
stmts, err := sql.New(sql.TriggerBodies()).Parse(script) // each with its line
```

Each `Statement` carries `Pos` and `End`: `Text` is the source between them, before the
semicolon. `StatementAt(src, offset)` is the statement holding a byte offset, as an editor's cursor
asks: a statement owns its source from its first character up to the next statement's first, so
its semicolon and a comment after it are its own; before the first statement is the first's, past
the last is the last's.

## Highlighting

`Definitions()` are the three dialects as source-language definitions — `SQL (PostgreSQL)` on
`*.sql` and `*.pgsql`, `SQL (SQLite)` on `*.sqlite.sql`, `SQL (MySQL)` on `*.mysql.sql`, with the
fence aliases `sql`, `postgresql`, `postgres`, `psql`, `pgsql`, `sqlite`, `mysql` and `mariadb` —
and `parse/languages` composes them. Each document gets a highlighter of its own
(`SourceFactory`). `Highlighter(dialect)` is one on its own.

The highlighter moved here from `highlight/sql`, which forwards to this package, deprecated, and
returns what it always did. A golden captured from `highlight/sql` before the move holds both.

## Numbers are a Form, not a Run

A run's membership predicate is position-aware but not content-aware — it is
handed a byte and an index, never what came before — so it cannot know a decimal
point has already been seen, and `1.2.3` would come back as one number. Nor can
it look forward, which the exponent needs: in `1e` the `e` belongs to the number
only if digits follow. `Number` sees the window and does both, so `1e` at end of
input is `Number(1) Word(e)`, mid-stream it defers rather than guessing, and
`1.2.3` is `1.2` followed by `.3`.

## Adding your own

A dialect is a slice, so extend one:

```go
forms := append([]parse.Form{
    parse.LineComment("#"),
    parse.QuoteForm("[", "]", parse.QuoteOpts{Kind: parse.Ident}),
}, sql.PostgreSQL()...)
```

Anything genuinely new is a `parse.Form` of your own, as `Number` and
`PostgresOperator` are here. Run it through `parsetest.Form` before trusting it:
the suite drives every split under both boundaries and will catch a form that
answers differently depending on how the bytes arrived.

## Assumed modes

`PostgreSQL()` assumes `standard_conforming_strings` is on, the default since
9.1: an ordinary `'…'` treats a backslash as an ordinary byte, and `E'…'` is
where escapes live. `MySQL()` assumes the default SQL mode, where a backslash
escapes in both quote styles and a double-quoted run is a string; under
`ANSI_QUOTES` it is an identifier, which is a session mode rather than a dialect
property, so swap that one form rather than expecting a flag here.

Deliberately not covered, because each wants a decision rather than a guess:
PostgreSQL's `U&'…'` unicode escapes, `B'…'` and `X'…'` bit and hex strings, and
operator names inside `OPERATOR(...)`.

## License

Apache-2.0, with the rest of golib — see [LICENSE](../../LICENSE).

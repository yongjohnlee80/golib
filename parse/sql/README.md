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

## Order is precedence

The slices come back in the order the lexer must try them, and the order carries
real decisions: `/*` before `/`, `--` before `-`, `E'` before the word run that
would otherwise eat the `E`. Every list ends with a one-byte fallback, so no byte
is ever left unclaimed.

## What a kind says

A kind says what a run of bytes **is**, never what it means. `SELECT` comes back
as a `Word`; that it is a verb in one statement and a column name in another is a
judgement for the layer above, made with the verbatim bytes still in front of it.
There are no keywords in this package for that reason.

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

**MySQL's `/*! … */` is not a comment at all** — the server executes what is
inside it. `MySQLBlockComment` therefore refuses it, and its contents are lexed
as the ordinary tokens they are. The delimiters arrive as operators, which is
untidy and deliberate: trivia is what a consumer is invited to discard, and
`/*!50000 DROP TABLE t */` must not vanish inside a token someone was told was
safe to drop.

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

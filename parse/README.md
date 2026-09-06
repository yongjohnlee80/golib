# parse — a streaming lexer core that names no dialect

**Status: partial.** The form contract and the generic forms are here. `Token`,
`Source` and `Scan` are not: their shape is under review (see the proposed
amendments in `docs/parse/adr-0001-streaming-lexer-foundation.md`).

This package answers *what a run of bytes IS*, never what it means. `SELECT` is
a `Word`; that it is a verb in one dialect, a column name in another and
reserved in a third is a judgement for the layer above, made with the verbatim
bytes still in front of it.

## Adding a construct

A construct is a `Form`, and adding one never edits the lexer:

```go
forms := []parse.Form{
    parse.BlockComment("/*", "*/", true),      // nesting
    parse.LineComment("--"),
    parse.QuoteForm("'", "'", parse.QuoteOpts{Doubling: true}),
    parse.QuoteForm(`"`, `"`, parse.QuoteOpts{Doubling: true}),
}
```

Order is precedence: the first match wins, and it is the caller's to arrange.
Declare `/*` before `/`, `--` before `-`. The core does not sort by length or
guess, because a dialect's precedence is a dialect's knowledge.

## Two answers are not enough

Both `Form` methods can be called with a window that does not contain the whole
construct — the ordinary case when the input is a stream.

`Starts` has three answers. `Incomplete` is the honest one at a window edge:
with a single `/` visible and `/*` declared, *matched* and *no match* are both
wrong, and declaration order cannot rescue it because the ambiguity is about
how much was seen, not about which form wins.

`End` is told whether more input may arrive, because **whether end-of-input
completes a construct is a property of the form**:

| at end of input, no terminator | line comment | quoted string |
|---|---|---|
| | complete, `n == len(src)` | `*UnterminatedError` |

No single rule is right for both, so the core cannot own this one. Under
`MoreInput` both defer with `ErrNeedMore`; input that already decides itself
gives the same answer under either boundary.

## Forms must be pure

A `Form` is called again from the same offset with more input, so anything
remembered between calls is a wrong answer waiting for an unlucky split. The
type system will not enforce this. Drive your form over every split of its
corpus and assert the answers do not depend on how the bytes arrived — the
tests here do exactly that (`everySplit`).

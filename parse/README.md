# parse — a streaming lexer core that names no dialect

**Status: partial.** The form contract, the delimited forms, the run/set forms,
`Token` and `Source` (offset → line/column) are here. `Scan` — the one pass that
drives the forms over a stream and emits the tokens — is the remaining piece. See
`docs/parse/adr-0001-streaming-lexer-foundation.md`.

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
    parse.DelimitedForm('$', '$', parse.DelimitedOpts{TagByte: pgTag}),

    // the kinds between the delimited ones — the classes are yours
    parse.RunForm(parse.Space, isSpace),
    parse.RunForm(parse.Word, isWordByte),
    parse.SetForm(parse.Operator, "<=", ">=", "<>", "::", "<", ">"),
    parse.SetForm(parse.Terminator, ";"),

    // the exact-one-byte fallback: nothing is left unclaimed
    parse.RunForm(parse.Operator, func(i int, _ byte) bool { return i == 0 }),
}
```

`RunForm` takes a maximal run of member bytes — a word, a number, a whitespace
gap — and the byte after the run belongs to the next token. `SetForm` takes the
longest of a set of literals, and handles a shared prefix on its own: `-` defers
mid-stream, is `-` at end of input, and becomes `--` when the second byte
arrives, because `Starts` fixes the *shortest* literal's width as a stable opener
and `End` extends it.

**Make the last form claim every byte.** A member true only at index 0 is the
exact-one-byte fallback. A member that is always true is *not*: with no byte to
refuse, its maximal run swallows the rest of the stream as one token.

`DelimitedForm` is the tag-carrying shape, and **the tag rule comes from you**:

```go
func pgTag(index int, b byte) bool {
    letter := b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
    if index == 0 {
        return letter          // rejects $1$, which is a parameter here
    }
    return letter || (b >= '0' && b <= '9')   // accepts $a1$
}
```

The predicate takes an index because a `func(byte) bool` could not do both. A
rejected tag is `NoMatch`, not an unterminated construct — the prefix belongs to
whatever form can lex it.

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
corpus and assert the answers do not depend on how the bytes arrived:

```go
func TestMyForm(t *testing.T) {
    parsetest.Form(t, myForm, []string{"...", "..."})
}
```

`parsetest` is one package out so the core does not import `testing`. It drives
every prefix of the input under **both** boundaries and enforces the full
answer matrix — under `MoreInput` the only refusal is `ErrNeedMore` with `n ==
0`, since a terminal error there decides against bytes that have not arrived;
at `EndOfInput` an unterminated construct must be reported as
`*parse.UnterminatedError` so a caller can name what was left open.

It checks the protocol, not the meaning: a form that recognises the wrong thing
consistently will pass, which is what your own tests are for. Its own suite
proves it fails on eleven decoys before it is trusted to pass anything —
including the ones that obey every individual rule while the SEQUENCE of
answers is nonsense, such as a form that finds its terminator and then asks for
more input on a larger window.

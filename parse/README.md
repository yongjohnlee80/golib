# parse — a streaming lexer core that names no dialect

**Status: the foundation is complete.** The form contract, the delimited forms,
the run/set forms, `Token`, `Source` (offset → line/column), the `Scan` engine,
`Validate` and `WriteTokens` are all here. The AST, the grammar tree and any
dialect leaf are above this layer and are not decided here. See
`docs/parse/adr-0001-streaming-lexer-foundation.md`.

## Stopping at the answer you need

```go
// Is it lexically valid? No Token in the signature, so a caller who wants only
// the verdict cannot come to depend on the token stream — and pays for neither
// the line index nor the tokens.
err := lex.Validate(ctx, r)

// The tokens, as bytes, without a []Token ever existing.
err = lex.WriteTokens(ctx, w, r)
```

Both build no line index and release each token as soon as it has been judged, so
under a finite `WithMaxDelimiter` their memory is constant however large the
stream is. Neither closes the reader.

## Scanning

```go
lex := parse.New(parse.WithForms(forms...), parse.WithMaxDelimiter(1<<16))

s := lex.Scan(ctx, r, parse.OwnReader) // or lex.ScanBytes(ctx, b)
defer s.Close()

for tok, err := range s.Tokens() {
    if err != nil { return err }
    // The bytes, with the lifetime that keeps them valid. Acquire while the
    // token is still the recent past: the scan releases behind itself.
    v, err := s.Acquire(tok)
    if err != nil { return err }
    text, _ := v.String()
    v.Close()
    // Acquire while the iteration is still inside this yield. Once you ask for
    // the next token the scan has released behind itself, and only a View you
    // already hold is guaranteed — a held View outlives the rest of the scan.

    loc, _ := s.LocationAt(tok.Start) // line:column, only when you ask
    fmt.Println(loc, tok.Kind, text)
}
```

The stream ends with an `EOF` token at a real position. `Close` is eager: it
releases retention and, under `OwnReader`, closes the reader and reports its
error — including for a `Scan` that was never ranged over.

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

    // the fallback: exactly one byte, so nothing is left unclaimed
    parse.ByteForm(parse.Operator),
}
```

`RunForm` takes a maximal run of member bytes — a word, a number, a whitespace
gap — and the byte after the run belongs to the next token. `SetForm` takes the
longest of a set of literals, and handles a shared prefix on its own: `-` defers
mid-stream, is `-` at end of input, and becomes `--` when the second byte
arrives, because `Starts` fixes the *shortest* literal's width as a stable opener
and `End` extends it.

**Make the last form claim every byte — with `ByteForm`.** Its width is
*intrinsic*, so it completes the moment its byte is seen. A `RunForm` cannot do
that job however you write the member: a run stops before the first byte it can
*see* refused, so at the end of a chunk it has to defer, and a fallback that
waits for a byte it will never use blocks on I/O. And a member that is always
true is worse still — with no byte to refuse, its maximal run swallows the rest
of the stream as one token.

`SetForm` copies its literals, so mutating the slice you passed does not change
the form afterwards.

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

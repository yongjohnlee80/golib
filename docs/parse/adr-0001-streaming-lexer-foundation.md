# ADR-0001 — `golib/parse`: a streaming lexer foundation

- **Status:** **Proposed (rev 2)** (2026-09-06 — authored by jarvis; awaiting
  lector design review).
- **Scope:** the foundational layer — input, the token model, and the streaming
  contract. The AST hierarchy and the risk analyzer layer on top and are **not**
  decided here.
- **Framing (Johno, 2026-09-06):** this is designed **from scratch** on
  architectural merit. The existing `parse` implementation is superseded;
  nothing here is justified by what that code does or fails to do. The existing
  public *interfaces* are kept for compatibility and adapted to — a decision
  deferred to §7, deliberately downstream of the core design so it cannot shape
  it.

---

## 1. What this layer is for

A caller hands us bytes that claim to be a language and needs, in order of
increasing commitment:

1. **Is it well formed?** — a yes/no, cheaply, over input of any size.
2. **Where are its pieces?** — a stream of lexical units with positions.
3. **What does it mean?** — structure, and then intent.

Each answer is strictly more expensive than the last. **The architecture's job
is to let a caller stop at the answer they need and pay for nothing beyond it.**
That single sentence generates most of what follows.

---

## 2. Principles this design is accountable to

| principle | how it binds here |
|---|---|
| **SRP** | Input, lexing, structure and meaning are four responsibilities in four types. A change of dialect touches configuration; a change of grammar touches the parser; neither touches input handling. |
| **OCP** | New dialects and new formats arrive as data and implementations, never as edits to the lexer core. |
| **LSP** | Any input source substitutes for any other. A file, a socket and a `[]byte` are indistinguishable to the lexer, and produce identical tokens. |
| **ISP** | A caller that wants validity must not compile against the token type; one that wants tokens must not compile against the AST. Small interfaces are how §1's "pay for nothing beyond it" is enforced at the type level rather than by discipline. |
| **DIP** | The lexer depends on `io.Reader`, not on a buffer. Nothing in it knows whether bytes came from memory or a network. |
| **DRY** | **One** lexing implementation. Not a fast path for `[]byte` and a slow path for readers — two implementations of one rule diverge, and the divergence surfaces as a dialect bug nobody can reproduce. |

---

## 3. The public boundary

**Public API speaks `io`. Internals speak `[]byte`.** (Johno, 2026-09-06.)

```go
// Lexer is the whole public surface of this layer.
type Lexer interface {
    // Tokens yields tokens until the input is exhausted or the caller stops.
    Tokens(r io.Reader) iter.Seq2[Token, error]
}

// Two conveniences, both defined in terms of the above — never beside it.
func TokensFrom(b []byte) iter.Seq2[Token, error]  // bytes.NewReader
func WriteTokens(w io.Writer, r io.Reader) error   // stream in, stream out
```

Three consequences worth stating:

- **`io.Reader` in.** A caller with a file, a socket, a decompressor or a
  `[]byte` uses one entry point. `io.ReadCloser` is accepted where ownership
  transfers; this layer never closes what it did not open.
- **`io.Writer` out.** `WriteTokens` gives a byte-in/byte-out pipeline for
  callers that want to persist, forward or diff a token stream without holding
  it. It is a *composition* of `Tokens`, not a second implementation.
- **`[]byte` is an internal concern.** The window, the slices handed to token
  construction, the scratch buffers — all `[]byte`, none of it in a signature.

### 3.1 Why `iter.Seq2` for the stream

It is already golib's idiom on Go 1.25 (`collections`, `tui/width`,
`tui/internal/grapheme`), so it costs a reader nothing new. Its pull semantics
are what make §1 real: **the loop body stopping stops production.** A caller
reading until the first error and returning has, by construction, done no work
past that error. `iter.Pull2` is available where a caller must interleave.

**Errors travel in the stream**, at the position they occurred, and end it. This
is ISP again: "is it valid?" is answered by the same seam that answers "what are
its tokens?", without a second entry point and without a token slice.

---

## 4. Input: one path, bounded memory

`Source` adapts an `io.Reader` into the rune-level primitives lexing needs —
peek, advance, unread, match a literal, slice the current lexeme — over a
**sliding window**, never the whole input.

**The window is the only design constraint that leaks upward**, so it is stated
plainly rather than hidden:

- The window must hold the longest lexeme the lexer recognises **atomically**.
  For SQL that is an operator, a keyword, a delimiter or a dollar-quote tag —
  all short and boundable. It is *not* the longest string literal or comment,
  because those are consumed incrementally and their content is not needed to
  find their end.
- It grows only when a construct genuinely spans the boundary, and growth is
  bounded by an explicit maximum. Exceeding that maximum is a **reported
  error naming the construct and its position**, never a silent truncation.
- **`Unread` past the window start is an error, not a silent wrong answer.**
  This is the one place a streaming source is genuinely weaker than a resident
  buffer, and the weakness is made explicit rather than discovered later.

`[]byte` input takes this same path through a `bytes.Reader`. That is DRY, and
it is also the only way to keep LSP honest: if the two paths differed, "the
tokens are the same" would be a hope rather than a property.

---

## 5. The token model

```go
type Kind uint8 // Word, Ident, String, Number, Operator, Punct, Comment, Terminator, EOF

type Token struct {
    Kind Kind
    Text string   // verbatim source text
    Pos  Position // offset, line, column of the first byte
}
```

Two properties carry weight:

**`Text` is verbatim.** No case folding, no unquoting, no unescaping. Those are
*interpretations*, and a lexer that performs them has taken a position on a
dialect it should not hold. A caller that wants the unquoted value asks the
layer that knows the dialect's escaping rules.

**`Kind` says what the source *is*, never what it *means*.** `Ident` covers a
quoted identifier; whether it names a table is the parser's question. This keeps
the keyword set — the largest, most dialect-specific artefact in any SQL
implementation — **out of the lexer entirely**, which is what lets one lexer
serve several dialects (OCP) instead of forking per dialect.

`Position` carries offset, line and column so a refusal can point at the
offending construct rather than at the statement containing it.

---

## 6. Dialects as configuration, not as code

A dialect is a value, not a type:

```go
type Dialect struct {
    Backticks           bool // `x` is a quoted identifier
    DollarQuotes        bool // $$…$$ and $tag$…$tag$ are strings
    NestedBlockComments bool // /* /* */ */ nests
    EStringEscapes      bool // E'…' gives backslash escapes
}
```

Adding a dialect adds a value; it does not touch the lexer (OCP). The axes are
chosen because real engines differ on exactly these points — and one subtlety is
recorded because it is easy to get wrong: **`EStringEscapes` keys off the
prefix, not the construct.** An ordinary `'…'` keeps the standard reading, so a
Windows path or a trailing-backslash regular expression still closes where it
should. Only a literal that announced itself with `E` changes meaning.

---

## 7. Compatibility with the existing interfaces — deferred, on purpose

The existing `Parser[T]`, `Validator`, `Splitter`, `StreamParser[T]` and `Named`
interfaces are **kept**, and the new implementation will be adapted to satisfy
them so current consumers compile unchanged.

**That adaptation is deliberately decided after this ADR, not within it.** An
adapter that must satisfy a fixed interface is a small, local problem. Letting
that interface reach backwards into the foundation's shape is how a design
inherits constraints nobody chose. The layering here is settled on its own
merits first; the adapter is fitted to it second.

---

## 8. What this ADR does not decide

Named so a reviewer knows they are absent by intent: the **AST hierarchy** and
its visitor; **intention and threat classification**; a **`pg_query_go` adapter
seam** (an adapter to a tree, and there is no tree yet); and **`dao` read-only
enforcement**, which consumes the analyzer two layers up.

---

## 9. Alternatives rejected

- **`pganalyze/pg_query_go`** — exact PostgreSQL fidelity by embedding the
  server parser, at the cost of CGo/Wasm, a large binary, ~50–150 µs per parse,
  and rejecting valid MySQL/SQLite. Fails DIP (binds the foundation to one
  engine's implementation) and the zero-dependency constraint. Retained as a
  possible **adapter** where exact fidelity is worth its price.
- **`pingcap/tidb/pkg/parser`** — pure Go and fast, but pulls TiDB's dependency
  graph and is MySQL-shaped: no dollar-quoting, no PostgreSQL operators. Fails
  OCP for our dialect range.
- **Materialising `[]Token`.** Simpler, and forfeits §1 entirely: the caller who
  wants a yes/no pays for every token in the file.
- **Separate `[]byte` and `io.Reader` implementations.** Faster in the resident
  case, and a direct DRY violation whose failure mode is a dialect behaving
  differently depending on how the caller happened to supply its input.

---

## 10. Acceptance criteria

1. The same source, supplied as a `[]byte` and as a slow `io.Reader` delivering
   one byte per call, yields **identical** token streams — including positions.
2. A caller that stops after *n* tokens leaves the reader positioned within one
   window of token *n*, demonstrated by an instrumented reader counting bytes
   consumed. *(Stated as a window bound, not as "no further work": a windowed
   reader legitimately consumes a whole window, and claiming otherwise would be
   a criterion the design cannot meet.)*
3. Validity checking over a large source allocates a bounded amount independent
   of source size.
4. Every token's `Pos` addresses its first byte in the original source,
   including after multi-byte runes and inside quoted bodies.
5. A malformed construct yields exactly one error, at the offending position,
   and ends the sequence.
6. A construct exceeding the maximum window yields a **named** error, not a
   truncation.
7. Each dialect axis is exercised in both settings, and a source that lexes
   differently between them is asserted to differ **in the expected way**.

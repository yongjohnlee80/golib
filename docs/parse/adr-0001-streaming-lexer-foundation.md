# ADR-0001 — `golib/parse`: a streaming lexer foundation

- **Status:** **Proposed (rev 1)** (2026-09-06 — authored by jarvis; awaiting
  lector design review).
- **Scope:** the **foundational layer only** — source handling, the token
  model, and the streaming contract. The AST hierarchy and the risk/intention
  analyzer are layered on top of this and are deliberately **not** decided here.
- **Supersedes:** the statement-splitting implementation in `parse/sql.go`.
  That code is replaced by this design rather than extended (Johno, 2026-09-06).

---

## 1. Why this ADR exists

`golib/parse` today offers a generic parser framework (`Parser[T]`,
`Validator`, `StreamParser[T]`, `Splitter`, capability detection) over a
position-tracking `Scanner`, with SQL as its only format. `SQL.Parse` walks the
source once, recognises comments, three quoting styles, E-strings and
dollar-quoted bodies, and returns `[]Statement{Text, Pos, Verb}`.

**Everything that walk learns about the source is discarded** except the
statement boundaries and the leading verb. A caller that needs to know whether
a `DELETE` carries a `WHERE` — or whether `1=1` inside it is a literal or an
identifier — has nothing to work with but the text again.

Two further properties make the current shape the wrong base to build on:

1. **`ParseStream` does not stream.** It is `io.ReadAll` followed by `Parse`.
   The whole source is resident before the first statement is produced. Its own
   doc comment explains the difficulty honestly — a terminator can appear inside
   a string — but the resolution taken was to give up streaming, not to solve it.
2. **The output is materialised whole.** `Parse` returns a slice. A caller that
   wants one statement, or wants to stop at the first refusal, pays for all of
   them.

For a front door that must decide about a statement **before** it reaches a
database, both of those are the wrong default.

---

## 2. Constraints

These are requirements, not preferences. Each is testable.

1. **Stream in.** The lexer consumes an `io.Reader` and must not require the
   whole source to be resident. A 100 MB dump and a 40-byte query use the same
   path, and the second must not pay for the first's design.
2. **Stream out.** Tokens are produced as they are recognised. A caller may
   stop at any point — and stopping must actually stop the work, not merely
   discard a finished slice.
3. **Discardable at zero cost.** *(Johno, 2026-09-06.)* A caller that wants only
   statement boundaries, or only a validity answer, must not pay to build tokens
   it never reads. **The token stream is opt-in by construction, not by a flag
   checked after the allocation.**
4. **Position fidelity.** Every token carries byte offset, line and column, so a
   refusal can be framed with `ErrorResponse.Position` pointing at the offending
   construct rather than at the statement.
5. **Zero external dependencies.** Consistent with the rest of `golib`, and the
   reason neither `pg_query_go` nor TiDB's parser is adopted wholesale (§6).
6. **Dialect-parameterised, not dialect-forked.** One lexer, configured.
7. **Additive to the existing framework.** Per golib's API-evolution convention,
   the new capability arrives as an interface a format may implement; existing
   `Parser[T]`/`Validator`/`Splitter` consumers are untouched.

---

## 3. Decision

### 3.1 Three independent layers, each usable alone

```
io.Reader ──▶ Source ──▶ Lexer ──▶ [Parser ──▶ AST]      (AST: later ADR)
              │          │          │
              │          │          └─ statements, risk analysis
              │          └─ tokens with position
              └─ windowed bytes, never the whole file
```

The layering is the decision. Each arrow is a **public seam**: a caller may
take tokens and never build an AST, or take statement boundaries and never look
at tokens. **Nothing above a layer is constructed unless asked for**, which is
how constraint 3 is met structurally rather than by a conditional.

### 3.2 `Source` — bounded windowed input

`Source` wraps an `io.Reader` and presents the rune-level primitives the lexer
needs (`Next`, `Peek`, `PeekAt`, `Unread`, `HasPrefix`, `Take`, `Slice`) over a
**sliding window**, not over the whole input.

The window must be large enough for the longest construct the lexer needs to
recognise atomically — a dollar-quote tag, an operator, a keyword. It grows on
demand and only when a construct genuinely spans the boundary. `[]byte` input
takes the same path via a reader over the slice, so there is **one
implementation**, not a fast path and a slow path that can disagree.

> **Why not keep today's `Scanner`.** Its API is the right shape and this
> borrows it deliberately, but it is defined over a resident `[]byte` and
> `Slice(from, to)` assumes any earlier offset is still addressable. Streaming
> makes that false. The names survive; the backing does not.

### 3.3 `Token` — what is retained

```go
type Token struct {
    Kind  Kind      // Word, Ident, String, Number, Operator, Punct, Comment, Terminator, EOF
    Text  string    // the source text, verbatim
    Pos   Position  // offset, line, column of the first byte
}
```

Two properties are load-bearing:

- **`Text` is verbatim, never normalised.** Case folding, unquoting and keyword
  recognition are *interpretations*, and they belong to the layer that has a
  dialect's opinion. A lexer that upper-cases has already decided something.
- **`Kind` distinguishes what the source said, not what it means.** `Ident`
  covers a quoted identifier; whether it names a table is the parser's question.
  This keeps the keyword set — the largest dialect-specific artefact — out of
  the lexer entirely.

### 3.4 The streaming contract

```go
// Tokens yields tokens until the source is exhausted or the caller stops.
Tokens(r io.Reader) iter.Seq2[Token, error]
```

`iter.Seq2` is chosen because it is **already golib's idiom** (`collections`,
`tui/width`, `tui/internal/grapheme`) on Go 1.25, and because its pull semantics
give constraint 2 for free: the loop body stopping stops production, and
`iter.Pull2` gives a caller explicit control when it needs to interleave.

**Errors travel in the stream, not out of band.** A malformed construct yields
its error at the position it occurred, and the sequence ends. A caller that
wants "is this valid?" reads until the first error and stops — without building
a token slice or a statement list.

### 3.5 Dialects

The four axes already proven necessary are carried forward **as requirements**,
each because a real engine differs:

| axis | on | off |
|---|---|---|
| `Backticks` | `` `x` `` is a quoted identifier (MySQL) | ordinary text |
| `DollarQuotes` | `$$…$$`, `$tag$…$tag$` are strings (PostgreSQL) | `$` ordinary |
| `NestedBlockComments` | `/* /* */ */` nests (PostgreSQL) | first `*/` closes |
| `EStringEscapes` | `E'…'` gives backslash escapes | backslash literal |

`EStringEscapes` keys off the **prefix**, not the construct: an ordinary `'…'`
keeps the standard reading, so a Windows path or a trailing-backslash regex
still closes where it should. That subtlety is preserved because it was learned
from a defect, not designed.

---

## 4. What this ADR does **not** decide

Named so that the next reviewer knows they are absent by intent:

- **The AST hierarchy** — node interfaces, expression trees, the visitor.
- **Intention and threat classification** — the risk analyzer.
- **The `pg_query_go` adapter seam** — worth having, but it is an adapter to a
  tree, and there is no tree yet.
- **`dao` read-only enforcement** — a consumer of the analyzer, two layers up.

---

## 5. Consequences

**Gained.** A front door can refuse a statement before it reaches a database,
citing a position. A validity check costs one pass and no allocation. A large
dump is processed without becoming resident. Formats other than SQL can reuse
`Source` and `Token`.

**Paid.** A windowed source is harder than a resident slice — `Unread` past the
window start must be an error rather than silently wrong, and that boundary
needs its own tests. The rewrite also discards working, tested code; the
dialect *behaviours* are preserved by porting the existing package's test
corpus forward as the acceptance floor, which is the only honest way to show
the replacement is not a regression.

**Migration.** `Statement{Text, Pos, Verb}` is retained as the statement-layer
output so existing consumers compile unchanged. `Verb` becomes a derived
convenience over the token stream rather than a second, independent scan.

---

## 6. Alternatives rejected

- **`pganalyze/pg_query_go`** — 100% PostgreSQL fidelity by embedding the server
  parser, at the cost of CGo/Wasm, a large binary, ~50–150 µs per parse, and
  rejecting valid MySQL/SQLite. Violates constraints 1, 5 and 6. Retained as a
  future *adapter* where exact server fidelity is worth its price.
- **`pingcap/tidb/pkg/parser`** — pure Go and fast, but pulls TiDB's dependency
  graph and is MySQL-dialect-shaped: no dollar-quoting, no PostgreSQL operators.
  Violates 5 and 6.
- **Extending the current splitter to emit tokens.** Considered and rejected by
  the owner: the splitter is superseded by this work rather than grown into it.
  Its *requirements* are preserved (§3.5) and its test corpus becomes the
  acceptance floor (§5); its implementation is not.
- **Materialising `[]Token`.** Simpler, and fails constraints 2 and 3: the
  caller who wants a boundary or a yes/no pays for every token in the file.

---

## 7. Acceptance criteria

1. Tokenising a source that exceeds the window yields identical tokens to the
   same source read from memory — the streaming and resident paths agree.
2. A caller that stops after *n* tokens causes no work beyond token *n+1*,
   demonstrated by an instrumented reader counting bytes consumed.
3. A validity check over a large source allocates a bounded amount independent
   of source size.
4. Every construct in the existing package's test corpus lexes to the expected
   kinds, on both dialect settings where an axis applies.
5. Every token's `Pos` addresses its first byte in the original source,
   including after multi-byte runes and inside dollar-quoted bodies.
6. A malformed construct yields exactly one error, at the offending position,
   and ends the sequence.

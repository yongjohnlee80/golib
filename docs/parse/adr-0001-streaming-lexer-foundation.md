# ADR-0001 — `golib/parse`: a streaming lexer foundation

- **Status:** **Proposed (rev 3)** (2026-09-06 — authored by jarvis; awaiting
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

1. **Is it lexically valid?** — do its comments, strings and quoted
   identifiers open and close? A yes/no, cheaply, over input of any size.
   *(Not "is it well formed": that is a grammar question and this layer has no
   grammar. Lector r2 MF5; grammar-level validity removed from scope by Johno,
   2026-09-06.)*
2. **Where are its pieces?** — a stream of lexical units with positions.
3. **What does it mean?** — structure, and then intent.

Each answer is strictly more expensive than the last. **The architecture's job
is to let a caller stop at the answer they need and pay for nothing beyond it.**
That single sentence generates most of what follows.

---

## 2. Principles this design is accountable to

| principle | how it binds here |
|---|---|
| **SRP** | Input, lexing, structure and meaning are four responsibilities in four types — and the cache is a fifth, in its own **package** (§4.4), because it knows nothing about tokens. A change of dialect touches configuration; a change of grammar touches the parser; neither touches input handling. |
| **OCP** | New dialects and new formats arrive as data and implementations, never as edits to the lexer core. |
| **LSP** | Any input source substitutes for any other. A file, a socket and a `[]byte` are indistinguishable to the lexer, and produce identical tokens. |
| **ISP** | A caller that wants validity must not compile against the token type; one that wants tokens must not compile against the AST. Small interfaces are how §1's "pay for nothing beyond it" is enforced at the type level rather than by discipline. |
| **DIP** | The lexer depends on `io.Reader`, not on a buffer. Nothing in it knows whether bytes came from memory or a network — and because the source caches (§4), a reader is a *first-class* input rather than one whose limits leak into every construct. |
| **DRY** | **One** lexing implementation. Not a fast path for `[]byte` and a slow path for readers — two implementations of one rule diverge, and the divergence surfaces as a dialect bug nobody can reproduce. |

---

## 3. The public boundary

**Public API speaks `io`. Internals speak `[]byte`.** (Johno, 2026-09-06.)

```go
// Construction: a dialect (§6) selects lexical forms; nothing else configures.
func New(d Dialect) *Lexer

// The token seam.
func (l *Lexer) Tokens(ctx context.Context, r io.Reader) iter.Seq2[Token, error]

// The VALIDITY seam — deliberately free of Token, so a caller asking
// "is this lexically valid?" does not compile against the token type (ISP).
func (l *Lexer) Validate(ctx context.Context, r io.Reader) error

// Conveniences, defined in terms of the seams above — never beside them.
func (l *Lexer) TokensFrom(ctx context.Context, b []byte) iter.Seq2[Token, error]
func (l *Lexer) WriteTokens(ctx context.Context, w io.Writer, r io.Reader) error

// Ownership transfers only where it is in the signature.
func (l *Lexer) TokensAndClose(ctx context.Context, rc io.ReadCloser) iter.Seq2[Token, error]
```

Four corrections from lector r2 MF2, each a gap rather than a preference:
**dialect selection had no constructor**; **the helpers could not compose an
unspecified interface**, so `Lexer` is a concrete type with methods;
**`context.Context` was missing** on operations that block on I/O; and the
ReadCloser ownership rule was **prose with no signature** — it is now the one
function that closes, named so it cannot be assumed of the others.

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

## 4. Input: one path, cached, with a stated bound

`Source` adapts an `io.Reader` into the rune-level primitives lexing needs —
peek, advance, unread, match a literal, slice a lexeme — and **retains what it
has consumed in an internal cache** (Johno, 2026-09-06).

### 4.1 Why caching is a requirement, not an optimisation

An `io.Reader` is single-pass and forward-only. Three things this layer owes its
callers are impossible against a bare reader and trivial against a cached one:

1. **Backtracking wider than one peek.** A lexer that must look ahead several
   runes to decide a construct, and then retreat, needs the retreated bytes to
   still exist. Without a cache this becomes "unread is an error past *n*", a
   limit that leaks into every construct's implementation and is discovered by
   whoever writes the *n+1*th.
2. **Diagnostics that show the source.** A `Position` is only half an error
   message; the other half is the line it points into, with a caret. That line
   is behind the read head by the time the error is known.
3. **Re-reading a region.** A layer above may need to re-lex a span under a
   different assumption. Without a cache that requires the caller to have kept
   the bytes — which pushes this layer's problem outward, and is exactly what an
   abstraction is supposed to prevent (DIP).

So the cache is not a performance tactic. **It is what makes `io.Reader` a
first-class input rather than a degraded one**, and it is what lets §3's promise
— one path for readers and slices — be true rather than aspirational.

### 4.2 The bound: a mark, not a window

Caching everything would trade one honest limitation for a dishonest one:
unbounded memory on a large input, discovered in production.

The retained region is therefore delimited by an explicit **mark**:

```go
func (s *Source) Mark() Mark          // pin: nothing at or after this is released
func (s *Source) Release(m Mark)      // the caller is done with everything before m
func (s *Source) Slice(from, to Position) []byte   // valid within the retained region
```

- The lexer marks the start of the lexeme it is building; the layer above marks
  the start of the statement it is assembling.
- Everything before the **oldest live mark** is releasable, and released
  eagerly.
- **Retention is the caller's choice.** A validity check sets no mark and
  retains **nothing**: memory is O(window), independent of source size — a
  gigabyte literal streams through without ever being held. A statement
  assembler marks a statement and pays for that statement.
- **Peak memory is therefore the largest span the caller chose to retain**, and
  for a caller that retains nothing it is constant. Stating it as "the largest
  statement" was wrong for the cheapest seam, which is the one that most needed
  the guarantee (lector r2 MF1).
- `Slice` outside the retained region is a **programming error and reports as
  one**, never a silent wrong answer. This is the single place the abstraction
  can be misused, so it is loud.

### 4.3 One path

`[]byte` input takes this same path through a `bytes.Reader`. Its cache
degenerates to a view over the original slice — **no copy** — so the resident
case pays nothing for the mechanism the streaming case needs.

That is DRY, and it is also the only way to keep LSP honest: if the two paths
had separate implementations, "the tokens are the same" would be a hope rather
than a property.

### 4.4 The cache belongs in its own package

*(Johno, 2026-09-06: "cache can be a separate pkg if general enough".)*

**It is general enough.** "Retain a bounded, mark-delimited region of a
forward-only stream, and hand out stable views into it" describes a log reader,
a protocol framer and a diff tool as readily as a lexer. Nothing in it knows
what a token is. Keeping it inside `parse` would make the next consumer either
import a parser to get a buffer, or write a second one — the shape this project
has been repeatedly bitten by.

So: **`golib/streamcache`** (name provisional), with `parse` depending on it.
SRP at the package level, and the dependency points the honest way — the
general thing does not know about the specific one.

**Prior art checked, and it is not this** *(Johno: "the ingestor perhaps already
has the memory implementation we can look at")*. `ingestor/memory.go` is
`MemoryLoader[T]` — a typed row accumulator that batches `[]T` for a loader
pipeline. It shares no concern with a byte cache and reusing it would be forcing
a fit. What it *does* give us is the house pattern for shared mutable state,
which §4.5 adopts.

### 4.5 Shareable, and the cost of it placed where it belongs

*(Johno, 2026-09-06: "incorporate thread_safe for sharable cache structure".)*

A cache that several readers hold views into is shared mutable state, and golib
already has the vocabulary for that: `threadsafe.Value[T]`, with
`SynchronizedValue`, `MultiReadSyncValue` and `AtomicValue` behind one
interface. `ingestor` uses it; so does this.

**The design detail that makes sharing cheap is storage layout, not locking.**
Storage is **append-only segments**, never a slice that grows by reallocation:

- A view handed out earlier can never be invalidated by a later append, because
  nothing moves. **Concurrent readers therefore need no lock at all** to read
  bytes they already hold a view into.
- Only the *bookkeeping* — the live mark set, and which segments may be
  released — is shared mutable state, and that is a `threadsafe.Value` over a
  small struct. `MultiReadSyncValue` is the right flavour: many readers
  consulting the retained bound, an occasional writer moving it.

The result is that the **single-threaded lexer pays approximately nothing** for
a property only shared consumers need, which is the usual objection to making a
hot structure thread-safe. Had storage been one growing buffer, every read would
have needed the lock, and the objection would have been correct.

### 4.6 A tree-structured cache — analysed, and rejected for now

*(Johno raised it, 2026-09-06; decision: keep the cache flat, and convert later
behind a wrapper.)*

**What a tree would buy.** Ordered lookup from an offset to the segment holding
it, structural release (drop a subtree when no mark points into it), and a shape
that resembles the span-indexed thing a grammar tree wants.

**Why it does not earn its place here.** With **fixed-size segments**, offset →
segment is O(1) arithmetic and release is dropping a prefix of a ring. A tree
buys O(log n) where the flat structure already has O(1), and pays for it in the
hot path of every token. A tree earns its keep with *variable-size* segments or
ordered queries over spans — and this cache has neither concern.

**Where a tree does belong.** One layer up. A grammar tree and an interval index
over spans are the *domain model* of the structure layer, not of the byte cache.
Putting the tree in the cache would be solving the tree layer's problem in the
wrong package, and would couple byte retention to a grammar shape that has not
been designed yet.

**So the cache stays flat, and conversion is deferred behind a seam**: a
wrapper or middleware converts the token stream into whatever structure grammar
needs, *when the need arises and its shape is known*. That keeps this layer's
cost O(1) and keeps the structural decision where the information to make it
will be.

## 5. The token model — fit for an AST *and* a grammar tree

**Requirement (Johno, 2026-09-06): this layer must be sound and performant
enough to carry both an AST and a grammar (concrete syntax) tree.** Those two
consumers want different things, and the lexer must not choose between them.

```go
type Kind uint8 // Word, Ident, String, Number, Operator, Punct, Comment, Space, Terminator, EOF

type Token struct {
    Kind  Kind
    Start Position // offset, line, column of the first byte
    End   Position // one past the last byte
}

func (t Token) Bytes(s *Source) []byte  // zero-copy, valid while the span is retained
func (t Token) Text(s *Source) string   // materialised copy, outlives release
```

### 5.1 Soundness: a grammar tree needs what an AST throws away

An AST keeps meaning; a **concrete syntax tree keeps the source** — every
comment, every run of whitespace, in order — because that is what lets a tool
re-emit the input unchanged, attach a comment to the node it documents, or
report a span the user can see.

So **trivia is emitted, never silently dropped.** `Comment` and `Space` are
token kinds like any other. An AST builder filters them in one line; a grammar
tree cannot recover them if the lexer discarded them. **The layer that cannot
undo a decision does not get to make it.**

**Tokens carry `Start` *and* `End`.** A tree node spans a range, and a range
needs both ends. Deriving `End` by looking at the next token's `Start` is wrong
in the presence of trivia and impossible at EOF.

### 5.2 Performance: offsets, not strings

A tree builder allocates per node. If the lexer also allocates per token, the
cost lands twice on the same traversal, and the second one buys nothing — most
tokens are punctuation and keywords whose text a tree never stores.

So **a token carries positions, not a string.** Its bytes are obtained on demand
from the cache (§4), which is what makes this possible at all:

- `Bytes()` is **zero-copy** — a view into the retained region. Free.
- `Text()` **materialises**, for the minority of tokens whose text is kept
  (identifiers, literals) or that must outlive their span's release.

This is the direct payoff of the caching requirement: without a retained region
there is nothing for a token to point *at*, and `Token` would be forced to carry
a string it allocated whether or not anyone wanted it.

> **The one hazard, stated rather than discovered.** `Bytes()` is valid only
> while the token's span is retained. A consumer that keeps tokens past a
> `Release` must call `Text()` first. This is the cost of zero-copy and it is
> made explicit here, in the doc comment, and in a test that exercises the
> misuse — see §10.

### 5.3 What stays out of the lexer

**`Kind` says what the source *is*, never what it *means*.** `Ident` covers a
quoted identifier; whether it names a table is the parser's question. Keeping
the keyword set — the largest, most dialect-specific artefact in any SQL
implementation — out of the lexer is what lets **one** lexer serve several
dialects (OCP) instead of forking per dialect.

Likewise the lexer never folds case, unquotes or unescapes. Those are
*interpretations*; a lexer that performs them has taken a position on a dialect
it should not hold, and has destroyed the verbatim text a grammar tree needs.

## 6. Dialects as configuration, not as code

**Four booleans are not OCP** (lector r2 MF4, and the finding is correct): they
select among forms the lexer already hard-codes. Adding a form the lexer does
not know — MySQL's `#` line comment, T-SQL's `[bracketed]` identifier — would
require editing the lexer, which is precisely what OCP forbids.

A dialect is therefore a **table of lexical forms**, and a form is data:

```go
type Quote struct {
    Open, Close string // "'" / "'", "`" / "`", "[" / "]"
    Kind        Kind   // String or Ident
    Doubling    bool   // '' inside '…' is a literal quote
    Backslash   bool   // \ escapes the next rune
    Prefix      string // "" or "E" — the E'…' opt-in keys off the PREFIX
}

type Comment struct {
    Open, Close string // "--" / "\n",  "/*" / "*/",  "#" / "\n"
    Nests       bool
}

type Dialect struct {
    Quotes   []Quote
    Comments []Comment
    Dollar   bool   // $tag$…$tag$, whose tag is matched structurally
    MaxAtomicLexeme int // default 63; see §4.2
}
```

Now a new form is **a table entry**, and the lexer is closed to modification
while open to extension. The four original axes survive as the PostgreSQL and
MySQL *presets* built from this table — the behaviour is preserved, the
mechanism is not. The axes are
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
   read-buffer of token *n*, demonstrated by an instrumented reader counting
   bytes consumed. *(Stated as a buffer bound, not as "no further work": a
   buffered reader legitimately consumes a whole buffer, and claiming otherwise
   would be a criterion the design cannot meet.)*
3. `Validate` over a source of arbitrary size — including one containing a
   single literal larger than any buffer — allocates a **constant** amount,
   independent of source size. This is the criterion MF1 showed the rev-2
   wording did not actually promise.
4. Every token's `Pos` addresses its first byte in the original source,
   including after multi-byte runes and inside quoted bodies.
5. A malformed construct yields exactly one error, at the offending position,
   and ends the sequence.
6. **Peak retained bytes, lexing a source of arbitrary size, are bounded by the
   largest single retained span** — not by the input — demonstrated on an input
   several times any plausible buffer. This is the criterion the cache's bound
   lives or dies on.
7. `Slice` outside the retained region reports a programming error; it never
   returns wrong bytes.
8. For `[]byte` input the cache performs **no copy** of the source.
9. Each dialect axis is exercised in both settings, and a source that lexes
   differently between them is asserted to differ **in the expected way**.
10. **Round-trip:** concatenating every token's bytes, trivia included,
    reproduces the source **exactly**. This is the single criterion that proves
    the stream is sound for a grammar tree; if it fails, something was dropped.
11. **Spans nest:** for any two tokens, their `[Start, End)` ranges are disjoint
    and ordered — a tree built over them cannot produce overlapping nodes.
12. Lexing allocates **O(1) per token** for tokens whose text is never
    materialised, demonstrated by an allocation benchmark over a source of
    mostly punctuation and keywords.
13. A token whose span has been released reports an error from `Bytes()` rather
    than returning bytes from an unrelated part of the cache.
14. **A view handed out before an append is byte-identical after it** — the
    append-only segment layout is what lets readers go unlocked, so it is
    asserted rather than assumed.
15. Concurrent readers over a shared cache, with a writer appending and
    releasing throughout, are **race-clean under `-race`** and every view stays
    valid for the life of its mark.

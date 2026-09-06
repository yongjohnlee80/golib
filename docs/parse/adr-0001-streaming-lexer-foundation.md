# ADR-0001 — `golib/parse`: a streaming lexer foundation

- **Status:** **Proposed (rev 5 — consolidated)** (2026-09-06, jarvis).
- **Scope:** the foundation only — retention, the token model, the lexical-form
  mechanism, and the streaming contract. The AST, the grammar tree and the risk
  analyzer layer above and are **not** decided here.
- **Review:** lector r1 (5 must-fix), r2 (5), r3 (6), r4 (6 — *architecture
  approved in substance*, one consolidation pass required). **This revision is
  that pass.** Rev 4 carried patched-in text from three earlier designs
  alongside the current one; every superseded seam is deleted here rather than
  argued with. If a mechanism is not in this document, it is not proposed.

---

## 1. What this layer is for

A caller hands us bytes and needs, in increasing order of cost:

1. **Is it lexically valid?** — do comments, strings and quoted identifiers open
   and close? *(Not "is it well formed": that is a grammar question, and this
   layer has no grammar.)*
2. **Where are its pieces?** — lexical units with exact spans.
3. **What does it mean?** — structure, then intent. **Above this layer.**

**A caller must be able to stop at the answer they need and pay for nothing
beyond it.** Everything below derives from that plus DRY/SOLID.

## 2. Three packages, each ignorant of the one above

```
golib/streamcache   bytes, retention, views   — knows nothing of tokens
golib/parse         Source, Token, Form, Scan — knows nothing of SQL
golib/parse/sql     Form values, presets      — knows nothing of a caller
```

The core **names no dialect**. `PostgreSQL()` and `MySQL()` are `Form` values in
`parse/sql`, built from core constructors. A core that cannot name a dialect
cannot be closed to new ones — which is a stronger guarantee than a table of
options (lector r3 B5, sharpened by Johno).

| principle | obligation here |
|---|---|
| **SRP** | retention, lexing, structure, meaning — four packages, four concerns |
| **OCP** | a new lexical form is a `Form` value; the lexer is not edited |
| **LSP** | a file, a socket and a `[]byte` produce identical tokens |
| **ISP** | `Validate` does not mention `Token`; the type system enforces §1 |
| **DIP** | the lexer depends on `io.Reader`, never on a buffer |
| **DRY** | one lexing implementation; the `[]byte` case is the same path |

## 3. Retention — `streamcache`

An `io.Reader` is single-pass. Without retention, backtracking, diagnostics that
quote their line, and re-reading a span are each impossible, and each becomes a
limit that leaks outward.

### 3.1 Views own their lifetime

**Every access to retained bytes returns an owning, closable view.** There is no
accessor that hands out bytes without also handing out the lifetime that keeps
them valid (lector r4 B3: a plain `io.Reader` over a span can be recycled
before or during `Read`).

```go
type Cache struct{ … }
func New(r io.Reader, opts ...Option) *Cache

// Acquire is the ONLY way to reach retained bytes. It atomically resolves the
// span and takes a reference on every segment covering it, so no window exists
// between finding a segment and owning it (lector r4 B5: lookup-then-increment
// is an ABA race).
func (c *Cache) Acquire(from, to int64) (*View, error)

type View struct{ … }
func (v *View) Reader() io.Reader          // no copy; walks segments
func (v *View) AppendTo(dst []byte) []byte // correct across boundaries
func (v *View) Len() int64
func (v *View) Close() error               // releases; idempotent
```

`Acquire` and `Close` are the linearization points. Between them the bytes
cannot be recycled; outside them no reference to them is reachable, which is
what keeps `threadsafe.Value`'s `RDo` contract intact rather than quietly
broken.

### 3.2 Retention never blocks the writer

**A pinned segment is never reused, and reuse never waits.** When the writer
needs space and every segment is held, it **allocates** (lector r4 B4).

This is not an optimisation, it is a correctness requirement. "Reuse waits"
deadlocks the ordinary case: a caller holds a view on the first token of a
statement while scanning forward for its terminator; the writer needs a segment;
all are held; the writer blocks; the caller never releases, because it is
waiting for the writer. **A forward scan must always be able to make progress.**

Growth is therefore the release valve, and memory is bounded by *what callers
retain*, not by a segment count.

### 3.3 Memory, stated honestly

Rev 4 claimed constant-memory `Validate` **and** unbounded delimiters. Those are
incompatible (lector r4 B2). The truthful statement:

> **Peak memory is `O(buffer + longest active delimiter + retained views)`.**

- A caller that acquires nothing and configures a finite `MaxDelimiter` has
  **constant** memory, whatever the size of the stream.
- With `MaxDelimiter = 0` (unbounded, full PostgreSQL fidelity) memory is
  constant *plus the longest delimiter the source contains*. That term is
  unbounded because the grammar is: `scan.l`'s `dolqdelim` is unbounded,
  `pstrdup` stores the whole delimiter, and `truncate_identifier` is called by
  the **identifier** action, not the dollar path *(lector r3 B4, verified in the
  PostgreSQL source)*.

**Constant memory and unrestricted derived delimiters cannot both be
unconditional.** The caller chooses, and the error names the delimiter, its
length and the limit.

## 4. The public boundary

```go
// Construction — functional options, immutable configuration.
func New(opts ...Option) *Lexer
func WithForms(f ...Form) Option
func WithMaxDelimiter(n int) Option   // 0 = unbounded; see §3.3

// One pass over one input. Ownership of the reader is an ARGUMENT, never an
// effect hidden in a name (lector r4 B6).
type Ownership bool
const (BorrowReader Ownership = false; OwnReader Ownership = true)

func (l *Lexer) Scan(ctx context.Context, r io.Reader, own Ownership) *Scan
func (l *Lexer) ScanBytes(ctx context.Context, b []byte) *Scan  // no copy

func (s *Scan) Tokens() iter.Seq2[Token, error]
func (s *Scan) Acquire(t Token) (*streamcache.View, error)
func (s *Scan) Close() error   // eager; closes r only under OwnReader

// The validity seam — no Token in its signature, so ISP is enforced by the
// compiler rather than by intent.
func (l *Lexer) Validate(ctx context.Context, r io.Reader) error
func (l *Lexer) WriteTokens(ctx context.Context, w io.Writer, r io.Reader) error
```

`ScanBytes` restores the borrowed, no-copy path (lector r4 B6): its cache is a
single immutable segment over the caller's slice, never written to and never
recycled, so `Acquire` is free and `Close` releases nothing.

`Scan` is a resource with an eager `Close`. A lazily-evaluated sequence that
owned an `io.ReadCloser` would close nothing if never ranged, and would have
nowhere to report a `Close` failure after an early stop.

## 5. The token model

```go
type Kind uint8 // Word, Ident, String, Number, Operator, Punct, Comment, Space, Terminator, EOF

type Token struct {
    Kind  Kind
    Start Position // offset, line, column
    End   Position // one past the last byte — half-open
}
```

A `Token` is **inert**: kind and span, nothing more. It cannot answer for its own
bytes and does not pretend to; bytes come from `Scan.Acquire`, which returns a
lifetime along with them.

**Trivia is emitted, never dropped.** `Comment` and `Space` are kinds like any
other. An AST builder filters them in one line; a concrete syntax tree cannot
recover what the lexer discarded. *The layer that cannot undo a decision does
not get to make it.*

**Spans are exact and half-open**, because a tree node spans a range and
deriving `End` from the next token's `Start` is wrong across trivia and
impossible at EOF.

**No case folding, unquoting or unescaping.** Those are interpretations, and a
lexer that performs them has taken a dialect's position and destroyed the
verbatim text a grammar tree needs. `Kind` says what the source **is**, never
what it **means** — which is what keeps the keyword set, the largest
dialect-specific artefact, out of the core entirely.

## 6. Lexical forms

```go
// A Form recognises one construct. New forms are new implementations; the
// lexer is never edited to accept them.
type Form interface {
    Starts(src []byte) (n int, ok bool)
    End(src []byte, openedWith []byte) (n int, err error)
    Kind() Kind
}

// Generic constructors in the core. None names a dialect.
func QuoteForm(open, close string, o QuoteOpts) Form
func LineComment(open string) Form
func BlockComment(open, close string, nests bool) Form
func DelimitedForm(prefix, suffix byte) Form // the $tag$ SHAPE, unnamed
```

`DelimitedForm` generalises the tag-carrying shape, so PostgreSQL's
dollar-quoting is one call from a leaf rather than a branch in the core.

## 7. Deferred, deliberately

The **AST and grammar tree**; **intention and threat classification**; a
`pg_query_go` **adapter**; **`dao` read-only enforcement**. A
**tree-structured cache** was analysed and deferred (Johno): with fixed-size
segments, offset→segment is O(1) arithmetic; a tree buys O(log n) over a set
that is almost always tiny and charges every lookup for it. Trees belong one
layer up, where spans are the domain model. **A wrapper converts to a
grammar-fit structure when its shape is known.**

## 8. Alternatives rejected

**`pg_query_go`** — exact fidelity via the server parser, at CGo/Wasm, a large
binary, ~50–150 µs/parse, PostgreSQL only. Retained as a possible *adapter*.
**`pingcap/tidb/pkg/parser`** — pure Go, but a heavy dependency graph and
MySQL-shaped. **Materialising `[]Token`** — forfeits §1. **Separate `[]byte`
and reader implementations** — a DRY violation whose failure mode is a dialect
behaving differently depending on how input was supplied.

## 9. Acceptance criteria

1. A source supplied as `[]byte` and as a one-byte-at-a-time reader yields
   **identical** tokens, positions included.
2. Concatenating every token's bytes, trivia included, **reproduces the source
   exactly**.
3. Token spans are disjoint and ordered — a tree over them cannot overlap.
4. `Validate` with a finite `MaxDelimiter`, over a source of arbitrary size
   containing a literal larger than any buffer, allocates a **constant** amount.
5. With `MaxDelimiter = 0`, a delimiter longer than any buffer lexes correctly;
   with a limit set, the same input is **rejected naming delimiter, length and
   limit**.
6. A span straddling a segment boundary is returned correctly by `Reader` and
   `AppendTo`.
7. **A view's bytes stay valid under reuse pressure** until `Close` — proven by
   acquiring, forcing the writer to need space, and reading through afterwards.
8. **A held view never stalls the writer:** a scan that acquires the first token
   and does not release it until the last completes, on a source far larger than
   any segment budget.
9. `Acquire` on a released span returns `ErrReleased`; it never returns bytes
   from a recycled segment.
10. Concurrent readers with a writer appending throughout are **race-clean under
    `-race`**, and every acquired view stays valid for its own lifetime.
11. A `Scan` created and never ranged still releases on `Close`; a `Close` error
    after an early stop reaches the caller.
12. `OwnReader` closes the reader; `BorrowReader` does not.
13. `ScanBytes` performs **no copy** of the caller's slice.
14. A form the lexer has never seen — `#` comment, `[bracket]` identifier,
    `~tag~` body — is added **as a value, with no lexer change**.
15. Lexing allocates **O(1) per token** for tokens never acquired.
16. **The core names no dialect:** grep `golib/parse` for `sql`, `postgres`,
    `mysql`, case-insensitive → nothing.

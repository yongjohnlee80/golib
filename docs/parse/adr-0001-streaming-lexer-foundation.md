# ADR-0001 — `golib/parse`: a streaming lexer foundation

- **Status:** **Proposed (rev 18)** (2026-09-06, jarvis). Rev 17 made every
  `Location` coordinate `int64`, gave `reclaim` a line-safe effective watermark
  shared with the cache (memory term in §3.4), and corrected the `SetForm`/fallback
  direction (§6); lector r17 approved all of that and asked for one error-domain
  fix. Rev 18: an early accessor `io.EOF` is an under-delivered range
  (`io.ErrUnexpectedEOF`), not an interior-rune offset; plus the precise §6 opener
  wording and the bounded-decode-buffering correction to the no-whole-range-copy
  claim.
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
func (v *View) AppendTo(dst []byte) ([]byte, error) // correct across boundaries
func (v *View) Len() int64
func (v *View) Close() error               // releases; idempotent
```

`Acquire` and `Close` are the linearization points. Between them the bytes
cannot be dropped or written to; outside them no reference to them is reachable.

**On `threadsafe.Value`** (lector r5 F5, and the correction is mine): rev 5
claimed the directory would be a `threadsafe.Value`/`MultiReadSyncValue`, and
the implementation uses a plain `sync.Mutex`. The claim was aspirational and the
code was right, so the ADR moves to the code rather than the reverse.
`threadsafe.Value[T]` guards a **value that is replaced wholesale** and its
`RDo` contract forbids retaining anything reachable past the callback — which is
precisely what handing out a view does. A cache with one short critical section
covering a slice, a watermark and a set of reference counts is what a mutex is
for. Using `Value` here would have meant either copying the directory on every
lookup or breaking its contract, and rev 5 was on course to do the second.

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

### 3.3 `Release` is a watermark, not a pass over the directory

**What is still acquirable is defined by the release offset alone**, and is
deliberately independent of which buffers have actually been freed. The two must
not be conflated, and rev 7 conflated them (lector r7): reclaiming only the
directory's *leading run* stopped at the first segment a view held, so
everything behind it was neither freed nor refused — a span the caller had
released stayed acquirable, and one view of the first byte pinned the whole
stream.

```
            held by a view
                |
       +----+----+----+----+----+----+----+----+
   seg |  0 |  1 |  2 |  3 |  4 |  5 |  6 |  7 |     released = Head
       +----+----+----+----+----+----+----+----+
          ^    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
          |                 |
       KEEP: a live view     FREE: nothing holds these, and the caller
       owns these bytes           has said it is done with them
```

Whether a released span's bytes happen to survive depends on whether an
*unrelated* view is holding an *unrelated* segment in front of it. That is not
something a caller can reason about, so it cannot be allowed to change an
answer. `Acquire` therefore refuses anything below the watermark outright, and
freeing proceeds independently:

- a segment the watermark passes while **unheld** is freed immediately;
- one passed while **held** is marked, and freed by its last `Close`;
- old views keep reading their own bytes throughout.

The pass is a **cursor**, not a scan: the watermark only rises, so each segment
is examined once over the life of the stream — `O(log n + segments newly
crossed)` per `Release` and `O(segments held)` per `Close`, both **amortised**,
`O(total segments)` over a stream, in any close order. Amortised rather than
worst case because an individual call may also pay for a compaction pass; that
pass runs only when freed entries are the majority and removes all of them, so
its cost is `O(1)` per entry spread over the calls that created them, and no
single call is bounded by the total.

`off` may be **beyond the current head**, meaning *skip forward*: the bytes are
dropped as they arrive, without a second call once they have been read. A
watermark is a statement about the stream, not about how much of it happens to
have been read when it is set. It only ever rises, so a later, smaller `off`
changes nothing.

**"As they arrive" is a claim about the PEAK, and the peak is where it was
wrong** (lector r9). Reclaiming once the requested range is complete gives the
correct *final* state and a peak of the entire range — measured at 1 MiB, every
byte resident at once. The cursor therefore runs **inside** the fill loop, and
a probe for this must record the maximum retained size *while reading*: a probe
that reads the final state cannot see the difference at all.

| skipped range | peak, reclaiming after the fill | peak, reclaiming during it |
|---|---|---|
| 1 KiB | 1,024 B / 16 entries | 64 B / 1 entry |
| 2 KiB | 2,048 B / 32 entries | 64 B / 1 entry |
| 4 KiB | 4,096 B / 64 entries | 64 B / 1 entry |
| 8 KiB | 8,192 B / 128 entries | 64 B / 1 entry |

*(64-byte segments; final retained size is zero in both columns, which is
exactly why the final state proves nothing.)*

Two ordering rules fall out of separating the two concerns, and both were
defects first (lector r8):

- **A freed entry is not a write target.** Its `n` survives — that is what
  bounds it for lookups — so a fullness test of `n == len(buf)` compares a live
  count against a nil buffer, reads *not full*, and slices `buf[n:]` on nil.
  Emptiness of the buffer, not arithmetic over it, is what says an entry can
  still be written to.
- **The watermark is checked before the source is touched.** Reading forward to
  serve an already-released span is work that cannot help, and it lets a source
  failure arrive first — telling the caller their *source* broke when their
  *request* was invalid, which points the diagnosis at the wrong component
  entirely. The check is repeated after the read, which is the correctness half:
  a `Release` may land while the source is being read.

### 3.4 Memory, stated honestly

Rev 4 claimed constant-memory `Validate` **and** unbounded delimiters. Those are
incompatible (lector r4 B2). The truthful statement:

> **Peak memory is `O(buffer + longest active delimiter + retained views)`**,
> where a retained view costs its segments' **full buffers** — including the
> unwritten tail of a held partial segment. A view on one byte of a 32 KiB
> segment retains 32 KiB (lector r6: the earlier wording implied span-sized
> cost). Smaller segments trade allocations for finer reclamation.

Plus one term that is not bytes of stream: **directory entries**. An entry whose
buffer has been freed cannot be removed from the middle of the directory without
a copy, so removal is amortised — the leading run is truncated for free, and
stranded entries are compacted only once they are the majority, which halves
them and costs `O(1)` per entry. Between compactions the directory carries at
most twice the entries it needs: `O(bytes retained ÷ segment size)`, tens of
bytes per segment.

- A caller that acquires nothing and configures a finite `MaxDelimiter` has
  **constant** memory, whatever the size of the stream.
- With `MaxDelimiter = 0` (unbounded, full PostgreSQL fidelity) memory is
  constant *plus the longest delimiter the source contains*. That term is
  unbounded because the grammar is: `scan.l`'s `dolqdelim` is unbounded,
  `pstrdup` stores the whole delimiter, and `truncate_identifier` is called by
  the **identifier** action, not the dollar path *(lector r3 B4, verified in the
  PostgreSQL source)*.

**A location-bearing scan carries one term more** *(lector r16)*. Resolving a
token's line and column needs the bytes from its line's start, so a scan that
will answer `LocationAt` keeps the current line resident and holds its line
index: peak gains **the longest live line, plus `O(lines in the live window)`**
for the index. `Source.reclaim` drops both with the watermark — but only to a
line boundary, so the retained line's true start is always present. A
multi-gigabyte single line is the control that tells this policy apart from
releasing at an arbitrary token boundary. `Validate` builds no `Source`, so it
carries neither term and stays constant — which is why the two paths are kept
apart.

**Constant memory and unrestricted derived delimiters cannot both be
unconditional.** The caller chooses, and the error names the delimiter, its
length and the limit.

## 4. The public boundary

```go
// Construction — functional options, immutable configuration.
func New(opts ...Option) *Lexer
func WithForms(f ...Form) Option
func WithMaxDelimiter(n int) Option   // 0 = unbounded; see §3.4

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

> **PROPOSED AMENDMENT — the span is offsets, and line/column is derived.**
> Two problems with `Start`/`End` as `Position`, one measured and one a
> collision:
>
> | shape | size |
> |---|---|
> | `Token{Kind, Position, Position}` | **56 bytes** — 56 MiB per million tokens |
> | `Token{Kind, int64, int64}` | **24 bytes** — 24 MiB per million tokens |
>
> And line/column is not free at any size: it costs a per-byte newline scan of
> the entire source, performed whether or not anyone ever asks. For a lexer
> whose whole point is streaming and O(1) per token, charging every token for a
> diagnostic that a minority of them will ever need is the wrong default. It is
> also derivable — the definition of data that should not be stored twice.
>
> The second problem is concrete: `parse.Position` **already exists** in this
> package, with `Offset int`. streamcache is `int64` throughout, so either the
> new core converts at every boundary and truncates on a 32-bit build, or the
> legacy type changes and that is a breaking change to an interface Johno asked
> to keep for now.
>
> Accepted in direction (lector r11): `Token{Kind Kind; Start, End int64}`, with
> coordinates resolved on demand from a line index the lexer builds out of the
> newlines it already passes over.
>
> **With one refinement, which is his: converting `int64` to the legacy
> `Position` truncates `Position.Offset` on a 32-bit build at the moment a
> diagnostic is requested** — the truncation is moved, not removed. So the
> lookup returns a NEW non-truncating location type, and `Position` keeps both
> its definition and its callers. The line/column semantics are pinned to what
> `Scanner` already does, so a diagnostic does not change meaning under the new
> core: **one-based lines, columns counted in RUNES, and invalid UTF-8 consuming
> one byte and one column.**
>
> **Landed (rev 15, corrected rev 16–17): `Token` and `Source`.** `Token{Kind
> Kind; Start, End int64}` is in `token.go`. The location type is `Location{Offset,
> Line, Column int64}` — every coordinate is `int64`, so none truncates on a
> 32-bit build (rev 17, lector r16) — resolved by `Source.LocationAt(off)`
> (`source.go`).
>
> `Source` resolves locations over the **live window** of the stream, which the
> streaming rewrite makes honest where rev 15 was not (lector r15):
>
> - **The byte accessor is lifetime-bearing.** Column is a rune count over the
>   line's bytes, and streamcache lends those bytes only while a `View` is open —
>   a span can cross segments, and `AppendTo` is the only way to a flat slice, by
>   copying. So `Source` reads through a `read(from, to, func(io.Reader) error)`
>   seam that holds a `View` for the call and closes it after: no borrowed slice
>   outlives a lookup, and no whole-range copy or materialization is made — only
>   the bounded decode buffering an `io.Reader`/`bufio` require (lector r17). Rev
>   15's `func(from,to)([]byte,error)` could not be both no-copy and lifetime-safe.
> - **The line index is reclaimed with the watermark, at a line boundary.** It is
>   not a copy of the source, but it is one `int64` per line, which is O(lines) —
>   not free. `reclaim` snaps the watermark DOWN to the greatest known line start
>   at or below it, drops the earlier starts and frees them, and **returns that
>   effective offset** so `Scan` releases the cache to the same place — releasing
>   mid-line would strand a column with no line-start bytes to count. `Validate`
>   builds **no** `Source` at all, keeping finite-`MaxDelimiter` validation
>   constant-memory (criterion 4).
> - **A released offset is unavailable for line AND column** (`ErrLocationReleased`),
>   never one exact while the other is gone. `reclaim` advances at line boundaries,
>   so a retained offset's line begins at a retained offset. Resolve a token's
>   location before the retention behind it drops.
> - **`LocationAt`'s domain is defined.** An offset that is negative, past the
>   known head, or **inside a multibyte rune** is refused (`ErrLocationRange`) —
>   the interior of a rune is not a position, and rev 15 answered one with a
>   column that marched forward then fell back as the rune closed.
>
> `source_test.go` drives the `Scanner` oracle at every boundary AND every byte
> offset (each is either a boundary location or a refusal, never an invented one),
> plus beyond-head, interior-rune, released, and reclaim cases. `Scan` — which
> builds the `Source`, advances its head, notes its newlines and drives its
> reclaim — is next, and the non-delimited kinds it emits are covered by the
> token-production decision in §6.*

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
    // Starts reports whether src opens this form. THREE ANSWERS, not two.
    Starts(src []byte) (n int, r Match)

    // End reports where the construct closes. It is told whether more input
    // may arrive, because whether EOF COMPLETES a construct is a property of
    // the form, not of the core.
    End(src []byte, openedWith []byte, boundary InputBoundary) (n int, err error)

    Kind() Kind
}

// InputBoundary says whether src is all there will ever be.
type InputBoundary uint8
const (
    MoreInput   InputBoundary = iota // src may grow; a decision can be deferred
    EndOfInput                       // src is the whole remainder; decide now
)

type Match uint8
const (
    NoMatch    Match = iota // not this form, whatever arrives later; n == 0
    Matched                 // opens this form, consuming n bytes; 0 < n <= len(src)
    Incomplete              // CANNOT DECIDE with these bytes; n == 0
)

// Generic constructors in the core. None names a dialect.
func QuoteForm(open, close string, o QuoteOpts) Form
func LineComment(open string) Form
func BlockComment(open, close string, nests bool) Form
func DelimitedForm(prefix, suffix byte, o DelimitedOpts) Form // the $tag$ SHAPE, unnamed
```

`DelimitedForm` generalises the tag-carrying shape, so PostgreSQL's
dollar-quoting is one call from a leaf rather than a branch in the core.

**The tag rule is the leaf's** *(lector r11)*. Unconstrained, the shape is too
permissive to be useful — `$1$` would open a literal in a dialect where `$1` is
a parameter — and with PostgreSQL's charset written into the core, the core has
named a dialect, which criterion 16 forbids. So:

```go
type DelimitedOpts struct {
    TagByte     func(index int, b byte) bool // POSITION-AWARE; joins the purity contract
    AllowEmpty  bool                          // is `$$` a form?
    MaxTagBytes int                           // 0 = bounded only by the scan's limit
}
```

`TagByte` takes the index because a single `func(byte) bool` cannot express the
ordinary rule: a leading digit is illegal where a trailing one is fine, so it
could not both reject `$1$` and accept `$a1$`. It is called again on the same
bytes as the window widens, so it joins the purity contract.

**A rejected byte, or a tag past its bound, is `NoMatch` — not an unterminated
construct.** The prefix belongs to some later form, and claiming it here would
take a token away from whatever can lex it. The bound is decidable before the
suffix arrives: once the tag is longer than it may ever be, no later byte can
rescue it.

**The conformance suite is `parse/parsetest`** *(accepted, lector r11)*. Rev 8
named it `parse.TestForm`, which puts `import "testing"` in the core and
registers testing's flags in every binary that links the parser, for a symbol no
production caller uses. The standard library keeps exactly this helper one
package out — `testing/fstest`, `testing/iotest`, `net/http/httptest`.
`parsetest.Form(t, f, corpus)` drives `Starts` and `End` across every split and
both boundaries, and its own suite proves it fails on four decoys.

**Every kind is a form, including the runs between the delimited ones**
*(accepted in direction, Johno on resume 2026-09-06; lands with `Scan`, not
written yet)*. The `Kind` enum has `Word`, `Number`, `Space`, `Operator`,
`Punct` and `Terminator` besides the delimited `Comment`/`String`/`Ident`, and
criterion 2 requires **every** byte to fall in some token. Rather than a second
extension mechanism beside forms — a default classifier the lexer would consult
where no form matched — these are forms too, so the lexer still only ever walks
one list and the OCP claim holds without an asterisk:

```go
// Generic, and the character classes come from the LEAF, so the core still
// names no dialect. Runs join the purity contract like every other form.
func RunForm(k Kind, member func(index int, b byte) bool) Form // maximal run of member bytes
func SetForm(k Kind, lits ...string) Form                      // longest of a fixed set; -- before -
```

A `RunForm` opens on its first member byte and ends at the first non-member one
(the boundary byte is the next token's, not consumed — the `LineComment`
shape); at a window full of member bytes under `MoreInput` it defers with
`ErrNeedMore`, because the run may continue. `End` indexes the run as
`len(openedWith)+i`, so the opener's WIDTH is load-bearing even though its bytes
go unused: `Starts` consumed run index 0, and `End` continues from there
*(lector r16)*.

A `SetForm` recognises the longest of a fixed set of literals with a **stable
first-terminal opener**: `Starts` answers `Incomplete` until the observed bytes
reach the shortest literal on their path, then returns that literal's width and
holds it as the window grows, and `End` resolves the longest completed
descendant, deferring only while a longer one is still possible. This is
chunk-invariant even for a shared prefix like `-`/`--`: `Starts` fixes the short
literal's WIDTH as the opener but does not commit to it as the final token — `End`
may still extend it — so the `Starts` answer never has to be withdrawn *(lector
r16–17, correcting an earlier claim that a shared prefix could only be resolved by
a catch-all)*.

Total coverage is the leaf's to arrange — `SetForm` for its operators and
punctuation, and where a final fallback is still needed an **exact-one-byte**
form. An always-true `RunForm` is NOT that fallback: with no terminating byte its
maximal-run contract swallows the whole remainder as one token.

### 6.1 The incremental contract

A `[]byte`-in/`int`-out signature cannot express a form whose construct **spans
more input than it has been shown**, which is the ordinary case for a streaming
lexer: a delimiter, a long comment, a literal larger than a segment.

**`Starts` needs the third answer just as much as `End` does**, and rev 6 gave
it only to `End`. The minimal counterexample is `/` against `/*`: with a single
`/` visible at the window edge, a two-valued `Starts` must answer *matched* or
*no match*, and **both are wrong** — declaration order cannot rescue it, because
the ambiguity is about how much input was seen, not about which form wins.
`Incomplete` is the only honest answer, and without it every shared-prefix
opener is a latent bug that appears only when a chunk boundary lands inside it.

```go
// ErrNeedMore reports that a decision cannot be made with the bytes supplied.
// The lexer widens the window and calls again with a superset, starting at the
// same offset. A Form MUST be a pure function of (src, openedWith): it may not
// carry state between calls, because it will be called again with more.
var ErrNeedMore = errors.New("parse: need more input")
```

Three rules the core enforces so a `Form` cannot get them wrong:

1. **`Incomplete` and `ErrNeedMore` are requests, not failures.** The lexer
   widens the window and calls again from the same offset, up to
   `MaxDelimiter` (§3.4), and only then reports a bounded error naming the
   construct.
2. **Forms are pure**, and *the interface cannot enforce it*. A `Form` may not
   remember anything between calls: the same `(src, openedWith)` must always
   give the same answer, because it will be called again from the same offset
   with more input. Statefulness is invisible until an input straddles a
   boundary — the hardest case to test and the easiest to ship broken.

   Since the type system will not hold this, a **conformance suite must**:
   `parsetest.Form(t, f, corpus)` ships alongside the interface, for form
   authors to run, and drives every input **at every split**, asserting the
   answer is identical however the bytes arrive. A stateful `Form` fails it; a
   `Form` whose author never runs it is that author's risk — stated here so it
   is a choice rather than an accident.
3. **Precedence is declaration order**, first match wins, and it is the
   caller's to arrange. `--` before `-`, `/*` before `/`. The core does not
   sort by length or guess, because a dialect's precedence is a dialect's
   knowledge — and a core that guessed would be naming a dialect by implication.

#### The resolution rules

A third answer is only worth having if it is fully specified. What follows is
the whole contract; anything a `Form` does outside it is a **form contract
violation** (`ErrFormContract`), reported with the form's position in the list,
not absorbed.

**What `n` means.**

| answer       | `n`            | meaning                                            |
|--------------|----------------|----------------------------------------------------|
| `Matched`    | `0 < n ≤ len(src)` | the opener consumed `n` bytes                  |
| `NoMatch`    | must be `0`    | not this form, whatever arrives later               |
| `Incomplete` | must be `0`    | cannot decide with these bytes                      |

`Matched` with `n ≤ 0` is a violation, not a no-op: the scan would return to
the same offset with the same input forever. `n > len(src)` is a violation
because the form is claiming bytes it was not shown. `Incomplete` carries **no**
byte count on purpose — a form that could say how many more bytes it needs
would be a form that had already decided, and a hint the core must range-check
and cannot trust buys nothing the retry bound does not already give.

**`Incomplete` blocks the forms behind it.** At offset `p`, the core walks the
list in declaration order. The first `Matched` wins. The first `Incomplete`
**stops the walk**: forms declared later are not consulted, because a later form
matching now could be overruled by the earlier one once more bytes arrive, and
a token emitted cannot be taken back. The core then widens the window and
restarts the walk **at `p`, from the top of the list**.

**At EOF, `Incomplete` degrades to `NoMatch`.** `Incomplete` is a claim
*conditional on more input existing*. When the source is exhausted the condition
is false, so the walk resumes past it and the shorter form wins:

```
input ends with "/"        forms: [ blockComment "/*", opSlash "/" ]

  more input possible                    EOF
  ───────────────────                    ───
  blockComment.Starts("/")               blockComment.Starts("/")
      → Incomplete                           → Incomplete → treated as NoMatch
      → STOP, widen the window               → continue the walk
      → retry the whole walk at p            opSlash.Starts("/")
                                                 → Matched(1)  ⇒ operator token
```

This is the case that a `MaxDelimiter` error would get wrong. A source ending in
a single `/` is not an unterminated block comment; it is a division operator,
and reporting a bounded-construct error there would reject a **valid** input.
The bounded error is for a construct that genuinely runs past the limit with
input still arriving.

**`End` needs the boundary; `Starts` does not.** The asymmetry is not an
inconsistency, and it is worth stating why, because the obvious move is to give
both the same treatment. `Starts` degrades uniformly — an `Incomplete` opener
that never completes is *not that form*, for every form there is or will be — so
the core owns the rule. `End` does not degrade uniformly:

| at end of input | line comment | quoted string |
|---|---|---|
| no terminator seen | **complete** — a comment may end at EOF | **error** — unterminated literal |

There is no policy the core could apply to both without being wrong about one,
so the form decides, and to decide it must be told. A raw `bool` would carry the
answer but not the question — `End(src, opener, true)` says nothing at the call
site — so the boundary is a named type.

The rules, complete:

1. **`MoreInput`, terminator absent** → `ErrNeedMore`, `n == 0`. The core widens
   the window and calls again from the same offset.
2. **`EndOfInput`, terminator absent** → the form's choice. A form that may end
   at EOF returns `n == len(src)` and a nil error, consuming the remainder. A
   form that may not returns its typed unterminated error and `n == 0`; the
   error names the construct and where it started.
3. **Terminator present** → identical in both modes: `n` counts the bytes after
   the opener up to and including the terminator. The boundary must not change
   the answer for input that already decides itself, or the same bytes would
   lex differently depending on how they arrived.
4. **`ErrNeedMore` at `EndOfInput` is a contract violation** (`ErrFormContract`).
   It asks for input that cannot exist; honouring it is an infinite loop and
   ignoring it silently picks one of the two answers in the table above.

   *Status, stated precisely (lector r11): the generic forms OBEY this rule and
   `parsetest` DETECTS its violation, shown against a decoy. Enforcement by the
   core — turning an arbitrary form's `ErrNeedMore` at EOF into
   `ErrFormContract` — lands with `Scan`, which does not exist yet. Criterion 19
   is not met until then, and saying otherwise would claim a guarantee nothing
   currently makes.*

**Retry bounds.** The core retries while both hold: more input may exist, and
the window **actually grew** since the last walk. A retry that widens by nothing
is not a retry — it is the spin that this design already refused once, and it
surfaces as the cache's own `io.ErrNoProgress` rather than a new failure mode.
The walk at one offset is bounded by `MaxDelimiter` bytes from `p`; exceeding it
reports a bounded error naming the construct and the offset it started at.

## 7. Deferred, deliberately

The **AST and grammar tree**; **intention and threat classification**; a
`pg_query_go` **adapter**; **`dao` read-only enforcement**. A
**tree-structured cache** was analysed and deferred (Johno). The original
argument said offset→segment is O(1) arithmetic with fixed-size segments; that
is **false in the implementation** and the ADR was stale (lector r6). A held
partial segment is left short and the next begins after it, so sizes vary and
arithmetic indexing is unavailable. Lookup is a binary search — done **once**
per span, then a cursor walk — which is O(log n + k) and still well inside what
a tree would buy over a set this size. Trees belong one
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
7. **A view's bytes stay valid under release pressure** until `Close` — proven
   by acquiring, driving the writer to the end, asking for everything to be
   released, and reading through afterwards. *(Nothing is recycled; dropped
   segments are garbage, so "reuse" overstated it.)*
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
17. **`Starts` answers `Incomplete`, not a guess, at a window edge** — driven
    over **every split** of a shared-prefix opener pair (`/` / `/*`, `-` /
    `--`), asserting the same token stream at each, and asserting the
    two-valued answers are never reached with the prefix alone. A positive
    control that removes `Incomplete` must redden it.
18. **A source ending in a shared prefix lexes as the shorter form** — the
    one-byte case: input ending in `/` with `/*` declared first yields an
    operator token, NOT a bounded-construct error. Driven at EOF and with the
    same bytes mid-stream, which must differ: mid-stream it waits.
19. **A form contract violation is reported, not absorbed** *(the core's half
    lands with `Scan`; `parsetest` already detects each of these)* — `Matched` with
    `n == 0` (which would loop at one offset forever), `n > len(src)`, a
    non-zero `n` alongside `NoMatch`/`Incomplete`, and `ErrNeedMore` returned
    at `EndOfInput` each produce `ErrFormContract` naming the offending form.
20. **EOF completes a line comment and fails an unterminated quote** — the same
    unterminated bytes, the same call, one boundary value apart: `EndOfInput`
    gives `n == len(src)` and nil for a line comment, and a typed unterminated
    error with `n == 0` for a quote. Under `MoreInput` both return
    `ErrNeedMore`, proving the boundary is what decides and not the bytes.
21. **A present terminator is unaffected by the boundary** — driven under both
    values, asserting an identical `n`: input that already decides itself must
    not lex differently because of how it arrived.
22. **the conformance suite enforces the WHOLE matrix it documents, and is
    proven to** — driven at **every prefix of the remainder under both
    boundaries**, not once on the whole of it, because a form is free to
    misbehave on every shorter window and that is where a stream spends its
    time. Per prefix:

    | | permitted | forbidden |
    |---|---|---|
    | `MoreInput` | `(n, nil)` with `0 ≤ n ≤ len(window)`, or exactly `(0, ErrNeedMore)` | any terminal or untyped error — the bytes that would settle it may still arrive |
    | `EndOfInput` | `(n, nil)` in range, or `(0, *UnterminatedError)` | `ErrNeedMore`, an untyped error, or a non-zero `n` beside one |

    Where `MoreInput` succeeds, `EndOfInput` on the **same window** must give
    the identical `n`. Successful `n` is **not** compared across prefixes at
    `EndOfInput`: a form that may end at EOF completes every prefix at its own
    length, and that is correct rather than drift.

    **And the SEQUENCE is checked, not only the cells.** Under `MoreInput` the
    answers must be monotone in information: once a terminator has been found,
    a larger window may not return `ErrNeedMore`, because appending bytes
    cannot un-find it. Every per-call cell can be obeyed while the sequence is
    nonsense, which is why this cannot live in the cells. It is the `End`
    analogue of `Starts` refusing `Incomplete` after a decision — and the
    absence of it was a real hole, found by review rather than by the suite.

    Eleven decoys, each breaking exactly ONE rule and each shown failing — a decoy
    that breaks two lets each check hide behind the other and neither gets
    proven. The green baseline comes first: all six generic forms pass, because
    a suite that fails everything detects nothing. Every check carries a
    mutation control; the one whose violation is also caught by a neighbouring
    check is pinned by asserting the **diagnosis**, since being detected under
    the wrong name sends the author to fix the wrong thing.
23. **A multi-byte closer is not ambiguous once a visible byte disagrees** —
    with doubling enabled, deferring requires what follows the closer to be a
    PROPER PREFIX of it (empty included). Testing only "fewer bytes remain than
    the closer needs" defers on every short remainder and stalls a live stream
    on input that has decided itself. Driven with a multi-byte closer, which is
    the only width that can expose it.
24. **A tag rule is the leaf's, and position-aware** — the same predicate
    rejects `$1$` and accepts `$a1$`, which no `func(byte) bool` can do. An
    over-long tag answers `NoMatch` without waiting for a suffix that cannot
    change it.
25. A form whose construct straddles the window returns `ErrNeedMore` and is
    retried with a superset until it decides or `MaxDelimiter` is reached — and
    the same input supplied in one chunk and in many yields identical tokens.
26. Reclamation is **linear in the segments actually freed**, not in the
    directory — an isolated close benchmark over independently held segments
    whose time roughly doubles when their number doubles, run in **both close
    orders**, since closing newest-first strands every freed entry behind a held
    one. *(Measured at 512/1024/2048/4096 segments: oldest-first 17/30/55/140 µs,
    newest-first 14/29/67/124 µs, 0 allocs — against 0.19/0.71/2.8/12.5 ms for a
    whole-directory scan.)*
27. **A released span stays released, and its memory goes** — proven separately,
    because freeing the bytes makes the refusal happen for free and hides
    whether the rule exists at all. One probe refuses a span below the watermark
    whose bytes are demonstrably still present (a segment straddling the
    watermark; a segment another view still holds); another asserts an oldest
    view pins its own segment and neither the bytes nor the directory entries
    behind it.
28. **A freed entry is never written to** — the sequence that produced a nil
    slice: a full, unheld tail segment freed by the watermark while older held
    entries keep compaction from removing it, then a fill. Asserted on the
    bytes as well as the absence of a panic, since reusing a dead entry
    corrupts offsets too.
29. **A released request performs no I/O and reports no source error** — a
    source that fails on its second read, a span below the watermark: the
    answer is `ErrReleased`, not the source's error, and the reader is not
    called again. Both halves matter; the second is what points a diagnosis at
    the right component.
30. **A watermark set beyond head applies to bytes that arrive later** —
    released, then read: retention stays flat **at its peak, measured while
    reading**, and the span is refused, with no second `Release`. The final
    state is not the claim: reclaiming once the range completes leaves the same
    final zero with a peak of the whole range, so the probe samples from inside
    the reader. Its control removes in-loop reclamation and must show the peak
    growing linearly with the range while the final state is unchanged.
31. Span access is **linear in the segments a span covers**, demonstrated by a
    benchmark whose time roughly doubles when the span doubles. *(Measured
    after the O(k²) fix: 172/342/630 µs at 64/128/256 KiB, against 0.4/1.5/6.0
    ms before.)*

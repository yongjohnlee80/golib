# ADR-0018 — `golib/dao`: session-pinned connections with raw extended-protocol execution

- **Status:** **Proposed** — rev 6; design r0's 4 MF (rev 1), r1's 3 MF +
  1 SF + the boundary supplement (rev 2), r2's 4 protocol/interface
  corrections (rev 3), r3's 3 implementability/consistency corrections
  (rev 4), r4's 2 private-path corrections + naming cleanup (rev 5), and
  r5's error-cleanup ordering correction (folded here). The ADR-0017
  follow-up that ADR-0075 (autodb's wire front door) filed: "requires a
  scoped golib seam (pinned-conn extended-exec capability)".
- **Date:** 2026-09-02
- **Module:** `github.com/yongjohnlee80/golib`
- **Related:** ADR-0017 (the capability-interface idiom this follows; the
  `SessionTxBeginner`/`ContextTxConn` contracts this builds on), ADR-0015
  (transaction ownership), ADR-0008 (capability honesty / `ErrUnsupported`),
  the `interface-evolution-capability-interfaces` KB convention, autodb
  ADR-0075 §"Extended query" (the consumer contract), autodb ADR-0074 §9
  (session-lifetime target lease)

## 1. Context

autodb's front door (ADR-0075) relays the PostgreSQL wire protocol to
pinned target connections. F1 (simple protocol) is served by the existing
dao surface: one-shot units through `DataConn`, session transactions through
`SessionTxBeginner`/`ContextTxConn`. F2 (extended query protocol) cannot be:
JDBC, pgx-class clients, and lib/pq all default to extended, and relaying it
**byte-faithfully** — raw Bind parameter formats and OIDs, requested
per-column result formats, Describe-before-Execute metadata, portal
`maxRows` and `PortalSuspended`, named/unnamed statement and portal
lifetimes, discard-through-Sync after error — is not expressible through
pgx's high-level API:

- pgx **does not use portals at all** internally (`execExtendedPrefix` /
  `execExtendedSuffix` bind and execute in one motion); there is no public
  surface for binding a portal, executing it with a row limit, or receiving
  `PortalSuspended`.
- Named statement lifetimes are managed by pgx's own statement cache, not
  by the caller; a pass-through relay must own them per the client's §4a
  rules.
- Per-column result formats and raw parameter OIDs are normalized away by
  the high-level query path.

The only layer that can honor the wire contract is `pgconn.PgConn`'s raw
frame face (`Frontend()` → `*pgproto3.Frontend`, `ReceiveMessage`). Today no
dao consumer can reach it — and reaching it *naively* is unsafe, because
pgx's own state machine (pending results, statement cache, tx status
tracking) believes it owns the connection.

There is also a **lifecycle coupling** the seam must solve: an autodb
session may hold an open transaction (begun via `BeginSessionTx`, finalized
with ADR-0017's outcome contract) while extended segments stream on the
same connection. The transaction must live on the *pinned* connection — not
on whichever pool member happens to be free — or a client's
`Parse`…`Execute` inside a transaction would execute against a different
backend transaction than the one the engine believes it is guarding.

## 2. Decision

Two additive capabilities, hosted in **`dao/postgres`** (the leaf package
where pgx is already the dependency), probed by type assertion at the
consumer's call site. `dao` core gains nothing — no new types, no pgx
vocabulary — keeping the zero-dependency rule and the published
`DataConn`/`TxConn` interfaces untouched (KB convention
interface-evolution-capability-interfaces; golib policy: no base-interface
growth for one consumer).

### 2.1 The capability: `SessionPinner`

```go
// SessionPinner is an optional postgres-driver capability: the ability to
// pin ONE connection exclusively, for a session's lifetime, and hand back a
// handle that can both run raw extended-protocol segments and host the
// session's transaction on that same connection.
type SessionPinner interface {
    PinSessionConn(ctx context.Context) (PinnedConn, error)
}
```

`pgxConn` (the `dao.DataConn` behind `dao/postgres.Open`) implements it by
acquiring one `*pgxpool.Conn` and holding it for the handle's lifetime.
Other drivers do not implement it; autodb asserts it where it already
asserts `SessionTxBeginner` (the session profile is PostgreSQL-only by
product ruling), and a miss reports `dao.ErrUnsupported` — never a panic,
never a silent pool fallback (a fallback would put the session's
transaction and its segments on different connections, which is the defect
this seam exists to make impossible).

### 2.2 The handle: `PinnedConn`

```go
// PinnedConn is one PostgreSQL connection, pinned for a session's lifetime.
// It has two faces — raw extended-protocol execution, and the session
// transaction — and they share ONE wire, so the handle serializes them
// through an explicit state machine (§2.3) rather than a bare mutex.
type PinnedConn interface {
    // Send queues ONE extended-protocol frontend frame. It does not flush
    // and does not block on the server (§2.3): Parse and Bind may be
    // queued back-to-back before a single Flush, exactly as a wire client
    // emits them.
    Send(ctx context.Context, op ExtendedOp) error
    // Flush writes all queued frames to the wire. It blocks only on the
    // socket write, bounded by ctx.
    Flush(ctx context.Context) error
    // Receive returns the next backend message of the current response
    // group, blocking bounded by ctx. DataRow payloads are BORROWED: valid
    // until the next Receive (the driver owns the buffer; the consumer
    // that keeps a row copies it — bytes.Clone, per the RawRows rule).
    // An *pgconn.PgError arriving as protocol data is returned as a
    // typed ExtendedError value, NOT a Go error — after it the inbound
    // track enters the discarding phase (§2.3) and the consumer's next
    // Sync is the single call that ends it; the caller classifies and
    // re-frames it for its own client.
    Receive(ctx context.Context) (ExtendedMessage, error)
    // Sync sends ONE Sync frame and consumes through the terminal
    // ReadyForQuery, returning the ReadyForQuery status byte. This is the
    // ONLY call that returns the wire to the quiescent state (both state
    // tracks reset; §2.3); discard-through-Sync after an ErrorResponse is
    // this same single call — the terminal ReadyForQuery ends the discard.
    Sync(ctx context.Context) (byte, error)

    // BeginSessionTx opens the session's transaction ON THE PINNED
    // CONNECTION, returning a guarded ContextTxConn (§2.4). Requires the
    // quiescent state: a call in any other state returns
    // ErrSegmentInFlight immediately — inspected under the handle's
    // synchronization, never by waiting for the segment to end
    // (serialization is not refusal).
    BeginSessionTx(ctx context.Context, opts dao.TxOptions) (dao.ContextTxConn, error)

    // Release returns the connection to the pool. Requires the quiescent
    // state and no open transaction; otherwise it returns
    // ErrSegmentInFlight / ErrTxStillOpen immediately.
    Release(ctx context.Context) error

    // Discard is the idempotent TERMINAL operation: it always relinquishes
    // the pool lease. When the wire is quiescent and the transaction is
    // closed, the connection returns to the pool; when safe reuse cannot
    // be proven (mid-flight segment, poisoned wire, failed Sync, open
    // transaction whose rollback did not confirm), the physical connection
    // is CLOSED instead — a discarded member, never a dirty one.
    // Every PinSessionConn success must have a deferred Discard; repeated
    // calls are no-ops. Discard never blocks on the server beyond a
    // best-effort bounded teardown.
    Discard()
}
```

`Release` refuses; `Discard` reclaims. Together they close the leak hole a
refusing-only API leaves open (r0 MF3): a cancelled context or a poisoned
wire can make `Release` refuse forever, and the pool slot must not follow
it.

### 2.3 The wire contract and the handle's state machine

The extended protocol is ASYNCHRONOUS, and the seam must say so rather than
paper over it (r0 MF1). One `Send` = one queued frame. Nothing reaches the
server until `Flush`. Responses arrive in groups whose boundaries the
CONSUMER knows (it sent the frames): `Receive` returns one message at a
time, and DataRow arrives as an unbounded stream — one message per call,
borrowed buffers, never accumulated by the driver.

**ORTHOGONAL STATE, NOT A SCALAR** (r1 MF2). Pipelining is real — after a
`PortalSuspended` the consumer may queue a resumed `Execute` while group
A's messages are still outstanding — so the handle's state is two
independent tracks:

```go
// outbound: what the CONSUMER has built on the wire.
type outboundState int
const (
    idleOut    outboundState = iota // nothing queued, socket drained
    building                       // Send queued ≥1 frame, not flushed
    flushed                        // Flush wrote them; bytes on the wire
)

// inbound: where the RESPONSE stream stands.
type inboundState int
const (
    noInbound   inboundState = iota // no response group expected/started
    receiving                       // messages of the group still arriving
    discarding                      // ErrorResponse seen; consuming to Sync
)
```

Plus one flag that outranks both: **poisoned** (a transport-level Go error;
§ poison rule below). Every guard inspects `(outbound, inbound, poisoned)`
as a triple; the legal-operation table:

| Call | Legal when | Effect |
|---|---|---|
| `Send` | not poisoned, not discarding | outbound: idleOut→building, building→building, or **flushed→building** (r2 MF1: the resume case — group B queues while group A's responses are still inbound; inbound is UNCHANGED by Send) |
| `Flush` | not poisoned, outbound==building, inbound ∈ {noInbound, receiving} | outbound→flushed; an INBOUND receiving state is PRESERVED — group A keeps streaming while group B's bytes go out |
| `Receive` | not poisoned, outbound ∈ {flushed, building}, inbound ∈ {receiving, noInbound→receiving on first call} | returns one message; borrowed buffers. **While outbound==building (resume frames queued but unflushed), Receive serves group A's still-arriving messages — the tracks are independent — and a resumed Execute's group cannot be received until its Flush** (r2 MF1: the unavailability is a consequence of the tracks, not a prohibition) |
| `Sync` | not poisoned | sends ONE Sync frame, sets inbound=discarding if receiving, consumes through the terminal ReadyForQuery, returns its status byte, and resets BOTH tracks to idleOut/noInbound |
| `BeginSessionTx` / `Release` / `pinnedTx` op | not poisoned, outbound==idleOut, inbound==noInbound | proceeds |
| `Discard` | always | terminal (§2.2) |

`Sync` from any non-poisoned state is legal and is ONE call — it emits one
Sync frame and consumes until the terminal `ReadyForQuery`; the r0 prose's
"repeatedly" is removed (multiple Sync frames are never emitted by one
call, and the consumer never needs a second: the terminal ReadyForQuery
ends every group and every discard).

**Portal resume and abandonment** (r1 MF2, corrected r2 MF2). 
`PortalSuspended` is protocol data in the `receiving` track, and the driver
cannot know the consumer's intent — so the state machine does not guess.
Either next move is legal and mechanical: `Send(Execute resume)` +
`Flush` continues the portal (the Flush row above preserves the inbound
receiving track), and the consumer's next `Sync` **abandons further
execution for this segment — it is NOT a claim that the portal object is
destroyed.** Portals close at TRANSACTION END in PostgreSQL (the autodb
matrix's own §4a rule: "transaction end for portals"), and Sync ends only
an implicit transaction; inside an explicit transaction a suspended NAMED
portal survives Sync, and its ownership/lifetime remains the CONSUMER's
until the consumer sends `Close portal`, the transaction ends, or the
session does. If the consumer wants immediate release, it must
`Send(Close portal)` before `Sync` when the protocol state permits. The
seam's Sync contract is therefore about the WIRE (tracks reset, group
ended), never about server-side object lifetimes — those belong to the
protocol rules the consumer is relaying.

**`ReadyForQuery` ownership:** the terminal `ReadyForQuery` belongs to
`Sync` (and to `Discard`'s teardown, which never surfaces it). A `Receive`
that would return a `ReadyForQuery` is a contract violation by the
consumer — it sent no Sync — and returns a typed error; the driver does not
silently absorb protocol skew.

**The poison rule.** A transport-level Go error (EOF, cancelled context at
dispatch, socket failure) — as opposed to protocol data — sets `poisoned`.
Every subsequent operation except `Discard` returns the typed poisoned
error. No repair is attempted: a connection whose frame boundaries are
unprovable is a discarded member, and pretending otherwise hands the next
pool consumer a dirty wire.

Blocking and cancellation: `Send` is non-blocking (queue only); `Flush`
blocks on the write; `Receive` blocks on the read; all bounded by ctx. A
ctx cancellation during `Flush`/`Receive` is a transport-level outcome →
poisoned, reported as the raw context error with the poisoned state
recorded.

### 2.4 The guarded pinned transaction

The handle does NOT return the pool-path `pgxTx`. That wrapper's methods
call pgx directly and know nothing of the handle's state (r0 MF2), so
handing it out would make the §2.3 enforcement a lie: a finalizer or
high-level query could run mid-segment with no inspection. Instead
`BeginSessionTx` returns a **`pinnedTx`** that:

- shares the handle's lock and state machine: every one of its operations
  (`CommitContext`, `RollbackContext`, and the base `Query`/`Exec`/
  `Commit`/`Rollback` surface) first inspects the handle state under the
  handle's synchronization and proceeds only from quiescent — anything else
  is an immediate typed `ErrSegmentInFlight`, never a wait;
- drives its BEGIN/COMMIT/ROLLBACK through the pinned connection's RAW
  face — simple-protocol text commands (`BEGIN`/`COMMIT`/`ROLLBACK` are
  single statements; verified pgx v5.10 itself finalizes transactions
  with `tx.conn.Exec(ctx, "commit")` over the simple path), so no pgx
  cache machinery ever runs on the pinned wire;
- delegates OUTCOME CLASSIFICATION to the existing ADR-0017 helper
  (`classifyCommit`): the pinned transaction's `ErrTxRolledBack` /
  `ErrTxOutcomeUnknown` / preserved `*pgconn.PgError` causes are the same
  code the pool path uses, shared, not duplicated.

**The query/exec path on pinnedTx (r1 MF3, corrected r2/r3/r4 MF3).**
`pinnedTx` satisfies `dao.TxConn`, so `QueryContext` and `ExecContext`
must honor the FULL existing signatures — `args ...any` included — and
must not silently narrow public behavior. They use a **private
driver-owned path** on the same raw face:

- **Parameters, byte-for-byte, via a private unnamed extended sequence
  (r3 MF1 — the rev-2 sanitizer plan was unimplementable).** The rev 2
  text proposed reusing pgx's `sanitizeForSimpleQuery`; that method is
  UNEXPORTED and its helpers (`convertSimpleArgument`,
  `internal/sanitize`) are unreachable from another module — the plan
  called an API golib cannot access, and the exported `Conn.Exec`/
  `Conn.Query` simple-protocol modes are no escape: they enter
  `Conn` bookkeeping (including cached-statement deallocation) and
  break the structural exclusion. The defined path is instead a private
  UNNAMED extended sequence on the raw face, built only from exported
  facilities: `Parse` (unnamed, no OIDs) → `Describe`(S) → the server's
  `ParameterDescription` supplies the parameter OIDs → each argument is
  encoded with the exported `pgtype.Map.Encode(oid, format, value, buf)`
  (text format; a NULL is the wire's own -1 length) → `Bind` (unnamed
  portal) → `Execute` — and then, per the exit-aware cleanup below
  (r5 MF1), either the NORMAL tail (explicit unnamed Close frames, then
  the private Sync consuming its own terminal `ReadyForQuery`, the §
  rev-3 carve-out) or the ERROR tail (recovery Sync first, then the
  Close+Sync cleanup exchange). This is the same encoding pgx itself
  performs for an extended ExecParams, driven through the raw frames
  instead of the `Conn` machinery.
- **The dispatch is METHOD/ARGUMENT-SHAPED, not SQL-text-shaped** (r4
  MF1 — the rev-3 statement-split rule was unimplementable: golib
  receives no statement list, the leaf cannot depend on a consumer's
  classifier, and reliably splitting PostgreSQL SQL is parser work).
  The rule mirrors pgx's own, VERIFIED against v5.10 source:
  - `ExecContext` with `len(args)==0` → ONE simple-protocol Query frame,
    draining every result group to the terminal state — pgx itself always
    uses the simple protocol for a no-arguments exec
    (`conn.go`: "Always use simple protocol when there are no
    arguments"), and multi-statement no-args text keeps the behavior it
    has always had through it.
  - `ExecContext` WITH args → the private unnamed extended sequence
    above.
  - `QueryContext` → the private unnamed extended sequence, for zero or
    more args — preserving the EXISTING pool-path default: pgx's
    default Query modes are extended (`ExecParams`-shaped), and a
    multi-command statement is rejected BY THE SERVER at Parse with the
    server's own error, verbatim. The rev-3 "LAST result group" rule is
    withdrawn — pgx never exposed such a rule and pinnedTx will not
    invent one. Criterion 14 pins both arms: bound args through the
    extended sequence by decoded semantic equality, and the no-args
    multi-statement ExecContext draining both groups through the simple
    frame, and a multi-command QueryContext rejected with the server's
    Parse error.
- **Object-lifetime parity, achieved by exit-aware cleanup** (r4 MF2,
  corrected r5 MF1 — Close frames queued before a recovery Sync are
  DISCARDED by the server). In an explicit transaction, a Sync ends the
  exchange but destroys nothing: the client's unnamed statement/portal
  were overwritten by the sequence's `Parse`/`Bind`, and the private
  objects would otherwise REMAIN until replacement or transaction end.
  The invariant is "identical in effect to the simple Query this path
  replaces" — post-state: unnamed objects GONE — but PostgreSQL discards
  frontend messages after an extended-query ErrorResponse until Sync, so
  one Close-then-Sync order cannot serve every exit. The exits are
  defined SEPARATELY, and the driver tracks which private objects
  `ParseComplete`/`BindComplete` PROVED were created, so Close is never
  sent blindly for an object the server never made:
  - **(a) Normal completion / `Rows.Close` after a successfully streamed
    group:** close the known-created unnamed portal and statement, then
    the private Sync — the rev-5 normal tail.
  - **(b) Protocol ErrorResponse:** issue the RECOVERY Sync first and
    consume its terminal `ReadyForQuery` — only then, if the creation
    acknowledgements proved the private objects exist AND the error's
    transaction state did not already destroy them (a transaction-ending
    error such as a failed COMMIT leaves nothing to close), run a
    SECOND private Close+Sync cleanup exchange before returning. The
    wire returns quiescent with the same post-state as (a).
  - **(c) Transport/context failure:** mark the handle POISONED and let
    `Discard` close the physical connection — NO wire cleanup is
    promised on a connection whose frame boundaries are unprovable.
  Named client objects are untouched on every exit. Criterion 12
  observes the cleanup on the normal path, and criterion 16 drives the
  error path: an Execute-stage ErrorResponse inside an explicit
  transaction, then proof the wire is quiescent and the private unnamed
  objects are absent before reuse (an extended Describe of the unnamed
  statement returns the server's no-such-statement error).
- `QueryContext` streams the result group's rows into a driver-owned
  `dao.Rows` implementation whose `RawValues` carries the borrowed wire
  bytes (the ADR-0012/0017 raw-rows rules); `Close` completes the
  cleanup contract above (explicit unnamed Close frames + the private
  Sync) and returns the wire to quiescent, and an undrained `Rows`
  keeps the inbound track at receiving — the guards refuse while it
  does.
- **ReadyForQuery ownership carve-out (r2 MF3):** the private
  driver-owned path consumes its own terminal `ReadyForQuery` — that is
  HOW it returns the wire to quiescent — so §2.3's "the terminal
  ReadyForQuery belongs to Sync" is scoped to the EXTENDED vocabulary
  surface: a `Receive` through `ExtendedMessage` never yields one, while
  the private path's internal consumption is invisible to the consumer
  and is the mechanism, not an exception, of its quiesce.
- **Object-lifetime effect, stated rather than implied:** a simple-protocol
  Query destroys the unnamed statement AND the unnamed portal (PostgreSQL
  semantics — the same rule the finalizers trigger). Named objects owned
  by the relayed client survive. The ADR records this as a property the
  CONSUMER's §4a bookkeeping must already account for: autodb's engine
  never issues high-level statements inside a relayed session's segments
  (its own gate matrix routes everything through the relay), so the pinned
  query path exists for ENGINE-INTERNAL statements only (the target-xid
  capture, the server-belt arming) — and criterion 11 proves those work.

**Legacy finalizers reuse the BEGIN context** (r2 MF4, corrected r3
MF2): the no-context `Commit`/`Rollback` behave exactly as the existing
`pgxTx` and as ADR-0017 preserves — they dispatch on the context captured
at `BeginSessionTx` (`t.tx.Commit(t.ctx)` shape, verified in the pool
path), NOT on `context.Background()` (which would silently remove the
bound the existing contract gives the caller). **And their OBSERVABLE
failure shape is the legacy one, not the context-finalizer one** (r3
MF2): the base methods mark the handle closed BEFORE dispatch — as the
existing `pgxTx` does — so a base finalizer attempted with a cancelled
BEGIN context fails with the context error and leaves the handle
TERMINAL (subsequent finalizers report `dao.ErrTransactionClosed`; the
pinned lease's cleanup is `Discard`, which closes the physical
connection). The open-and-retryable-after-refusal property belongs to
`CommitContext`/`RollbackContext`'s explicit pre-dispatch `ctx.Err()`
check — ADR-0017 fault state 1 — and pinnedTx must not silently graft
that property onto methods whose existing contract does not have it.
`CommitContext`/`RollbackContext` take the caller's finalizer context
and preserve the four-state ADR-0017 outcome contract through the
shared classification helper. Criterion 15 asserts the legacy shape
exactly: context error, then terminal handle, then `Discard` reclaims
the lease.

**Quiescent operation, not refusal, is the rule:** these methods are
SUPPORTED on pinnedTx — first-class, guarded by the same state inspection
as everything else. Silently returning `dao.ErrUnsupported` from a method
on a type whose interface promise says otherwise is capability dishonesty
in the other direction (a `ContextTxConn` that is not one), and the
acceptance criteria require the quiescent-path cells, not only the
mid-segment refusals.

Because the pinned connection is exclusively held by the handle for its
lifetime and NO operation exposes pgx's high-level `Conn` query/exec path
(the handle never hands the `*pgx.Conn` out, and its own methods use only
the raw face), pgx's statement-cache and prepared-statement maps are
**structurally unreachable on a pinned connection** — the cache cannot be
stale because it cannot be consulted. This is deliberately stronger than
"Sync repairs the caches" (r0 MF4): the seam does not claim to
resynchronize pgx high-level state; it makes the collision impossible by
never entering that machinery. The high-level pool `DataConn` remains
usable for OTHER sessions and one-shot units while one of its members is
pinned; the pinned member itself is reachable only through this contract.

Transaction-relative-to-named-objects: `pinnedTx` finalizers are legal
only from quiescent, which by §2.3 means the consumer has already driven
any failed segment to its terminal `ReadyForQuery` — so the engine's own
cancel-then-join-then-finalize ordering (the synchronous-demotion
lifecycle) maps one-to-one onto the seam's states, and a finalizer can
never interleave with client frames. The named-object lifetimes SPLIT per
the protocol's own rules (r3 MF3): **named prepared statements** survive
the transaction boundary; **named portals are destroyed at transaction
end** (absent a holdable-cursor mechanism outside this seam) — so a
relayed client's named statement outlives a COMMIT while its named
portal does not, exactly as against a real PostgreSQL, and the
consumer's §4a bookkeeping owns tracking both. The unnamed statement and
unnamed portal are destroyed by the finalizer's frame semantics like any
other unnamed object.

### 2.5 The vocabulary: a CLOSED neutral set, not pgproto3 re-typing

`ExtendedOp` (the Send argument) and `ExtendedMessage` (the Receive
result) are small **neutral types declared in `dao/postgres`**, mirroring
the wire messages a relay must forward and carrying the raw fields
byte-faithfully (parameter `[][]byte` values, format codes, OIDs,
per-column result formats, `maxRows`). The conversion to and from
`pgproto3` frames happens inside the driver, against the pinned
`pgconn.PgConn`'s frontend.

**The vocabulary is a CLOSED sum** (r1 MF1, jarvis's boundary audit):

- `ExtendedOp` is exactly: `Parse`, `Bind`, `Describe`, `Execute`,
  `Close`. No `RawFrame`, no `Other`, no `[]byte` passthrough, no embedded
  `pgproto3` value, no extension hook — any of those would recreate
  rejected alternative 3 under another name, and a closed sum is the only
  shape a scoped seam can prove. `Flush` is NOT an op: it is the handle
  method (§2.2), and listing it as both an op and a method would create
  two spellings with different state accounting.
- **The simple-protocol `Query` frame is EXCLUDED from `ExtendedOp` by
  design.** autodb's classifier/grant gate runs at Parse (and its simple
  path has its own gated entry); a Query op on this seam would bypass the
  gate entirely. The exclusion is compile-enforced — the sum has no such
  constructor — and test-enforced: a criterion-12 cell attempts the
  forbidden frame shape and is refused at the vocabulary boundary, not at
  the wire.
- **No aliases or re-exports from `dao` core.** The capability and its
  vocabulary live solely in `dao/postgres`; `dao` core gains nothing.
  The design-stage proof is the docs-only diff; the criterion-13 proof is
  re-run on the implementation HEAD (implementors enumerated at checkout
  HEAD, not from the module cache — nested modules are invisible there).

**Rejected:** exposing `*pgproto3.Frontend` directly — it would hand the
consumer pgconn's internal locking discipline as an implicit contract, and
is the F2 criteria's named escape-hatch rejection besides. The
op-struct boundary keeps the driver owning the wire exclusivity rules
while leaving every wire-level decision (which frame, which order, which
lifetime) with the consumer, which is where ADR-0075 put them.

**Rejected:** mirroring the full extended vocabulary in `dao` core — the
message set is large, churns with the wire, and duplicates types
`pgproto3` already defines; it would also put a PostgreSQL-specific
vocabulary in a driver-neutral package, against the layering ADR-0017
established.

## 3. Alternatives

1. **Grow `dao.DataConn`/`TxConn` with extended methods.** Rejected: breaks
   every implementor (mysql, sqlite, bigquery nested module, fakes) for one
   consumer; violates the binding capability convention; mixed-version
   builds go red.
2. **Do it all in autodb (acquire a `*pgxpool.Pool` and drive pgconn
   directly).** Rejected: the consumer would then hold pool internals —
   autodb would need the pool handle, the conn, and pgx state knowledge,
   duplicating this seam's exclusivity rules in consumer code with no
   library honesty guarantees; `dao.DataConn` abstraction would be
   bypassed, and the session-capable profile would silently depend on
   untracked behavior.
3. **Expose the raw `*pgproto3.Frontend`/`*pgconn.PgConn` from the handle.**
   Rejected: implicit internal-contract leakage, and a general-purpose
   escape hatch — the F2 review criteria's named rejection ("the ADR's
   word is SCOPED: expose the extended-query segment and nothing more").
4. **Neutral extended vocabulary in `dao` core.** Rejected in §2.3:
   layering inversion; churn; duplication.
5. **Return the pool-path `pgxTx` from pinned `BeginSessionTx`.** Rejected
   (r0 MF2): its methods bypass the handle entirely, so no handle-side
   state inspection can be enforced; §2.4's guarded `pinnedTx` shares the
   lock and delegates classification instead.
6. **A single `ExtendedExec(op) (result, error)` call.** Rejected (r0
   MF1): the extended protocol is asynchronous with unbounded row streams;
   one call cannot define queue/flush/receive boundaries, borrowed-buffer
   lifetime, or discard-through-Sync. §2.3's Send/Flush/Receive/Sync
   separation is the contract those properties need.

## 4. Consequences

- Additive only: `dao` core, mysql, sqlite, bigquery, fakes untouched.
  Compile-time assertions in `dao/postgres`
  (`var _ SessionPinner = (*pgxConn)(nil)`).
- `dao/postgres` gains the extended op/message structs — its surface grows,
  which is where the convention says a driver-specific capability lives.
- pgx version floor stays `v5.10.0` (no new dependency; pgproto3 arrives
  transitively via the existing pgx dependency, and only inside the leaf).
- autodb repins golib to the release carrying this, then builds F2 against
  `PinnedConn`; its front door keeps its own pgproto3 dependency for the
  CLIENT side and maps client frames to these ops.
- The state machine is part of the public contract: documented, typed on
  misuse (`ErrSegmentInFlight`, poisoned-state errors), race-tested
  (concurrent finalizer attempts vs in-flight segments must fail cleanly,
  not corrupt).
- A pinned connection removed from pgx's high-level machinery for its
  whole pinned lifetime: pgx's caches are never consulted for it, so they
  can never disagree with it (§2.4). The cost is that the pinned conn
  cannot serve high-level queries while pinned — by design; the handle's
  faces are the whole surface.

## 5. Acceptance criteria

1. `var _ SessionPinner = (*pgxConn)(nil)` in `dao/postgres`; `dao` core
   diff is empty — proven at IMPLEMENTATION head, not only design stage
   (r1 MF1); mysql/sqlite/bigquery compile untouched, implementors
   enumerated at checkout HEAD.
2. **The asynchrony contract (r0 MF1):** a pinned handle queues Parse and
   Bind back-to-back before ONE Flush and receives their response group
   message-at-a-time — no deadlock, no blocking in Send; and an Execute's
   DataRow stream is consumed in bounded memory (a large result streams
   one borrowed row per Receive without accumulation; the driver holds at
   most one row buffer).
3. **The state machine:** ErrorResponse arrives as protocol data, drives
   the inbound track to discarding, and the single terminal Sync returns
   the ReadyForQuery status byte and reopens the wire; a transport error
   poisons the handle and every subsequent operation except `Discard`
   returns the typed
   poisoned error; a premature `ReadyForQuery` in Receive is a typed
   contract violation, not silent absorption.
4. `BeginSessionTx` on the pinned conn returns the guarded `pinnedTx`
   whose commit/rollback outcomes are the ADR-0017 sentinels — proven by
   the same fault-state matrix as the pool path (server-confirmed
   rollback, `pgx.ErrTxCommitRollback`, dispatch-proven-never-written,
   outcome-unknown), via the SHARED classification helper.
5. **One wire, one backend transaction:** a transaction begun on the
   pinned conn is observably the SAME backend transaction the raw segments
   execute in (`txid_current()` captured via a raw Execute inside the
   pinned tx matches the handle's tx).
6. **Refusal, not serialization (r0 MF2, r1 SF):** `BeginSessionTx`,
   `Release`, and every `pinnedTx` operation invoked while a segment is
   mid-flight return `ErrSegmentInFlight` IMMEDIATELY. Immediacy is proven
   DETERMINISTICALLY, not by elapsed time: a test hook holds the segment
   in the receiving state on a barrier, the guarded call runs and returns
   while the barrier is provably still closed, and the segment then resumes
   and completes coherently. A watchdog timeout guards the mutation that
   breaks the refusal; scheduler load cannot flake the property.
7. **No lease leak (r0 MF3):** after a failed Sync / poisoned wire /
   cancelled context mid-segment, a deferred `Discard` reclaims the pool
   slot — proven by a cell that exhausts the pool's max size through
   repeated poison-and-discard cycles and then acquires again. A dirty
   connection is closed, never reused: the cell proves the next pool
   consumer sees a healthy wire.
8. **No cache collision, structurally (r0 MF4):** adversarial cell — while
   a client's named statement exists on the pinned wire, the pool `DataConn`
   (other members) runs high-level cached queries concurrently; the named
   object's behavior on the pinned wire is unchanged. And the unnamed-
   lifetime rule: a pinned-tx finalize destroys the unnamed statement per
   PostgreSQL semantics, observed through the raw face.
9. Miss reported honestly: a `DataConn` without the capability probed via
   the typed helper returns `dao.ErrUnsupported` (capability-honesty test).
10. Race detector clean over concurrent segment steps vs finalizer/Release
    attempts on one handle, and over deferred Discard racing a mid-flight
    Receive.
11. **Portal resume and abandonment (r1 MF2, r2 MF1/MF2):** the
    deterministic sequence with EXACT STATE TUPLES asserted at each step —
    Execute(maxRows) [outbound flushed, inbound receiving] → receive
    `PortalSuspended` [(flushed, receiving)] → Send(Execute resume)
    [(**building**, receiving) — the flushed→building transition with the
    inbound track preserved] → Flush [(flushed, receiving)] → Receive the
    resumed group's rows → Sync [both tracks reset, status byte returned].
    The abandonment branch: Execute → `PortalSuspended` → Sync (no resume)
    ends the SEGMENT. And the named-portal-lifetime cell (r2 MF2): inside
    an EXPLICIT transaction, a suspended NAMED portal SURVIVES Sync —
    resumed execution works afterwards — and an explicit Send(Close
    portal) releases it; only transaction end destroys it, per the
    protocol's own rules.
12. **The pinned query/exec path (r1 MF3, r4 MF2):** from quiescent, pinnedTx
    `QueryContext` streams rows through the driver's private path
    (RawValues byte-faithful; Rows.Close returns the wire to quiescent),
    and `ExecContext` returns the command tag — plus the object-lifetime
    observation IN BOTH DIRECTIONS: the client's unnamed statement/portal
    are gone after a pinnedTx query (an extended Describe of the unnamed
    statement returns the server's no-such-statement error — the explicit
    Close cleanup proven, not the sequence's Sync), and NAMED client
    objects survive. The mid-segment refusal of both is criterion 6's
    cell.
13. **The closed vocabulary (r1 MF1):** the Query-frame exclusion is
    test-proven — the forbidden shape cannot be constructed through
    `ExtendedOp` (compile) and the boundary test proves no spelling of a
    simple Query reaches the wire through the seam; no alias or re-export
    of the capability exists in `dao` core (grep/compile proof on the
    implementation HEAD, with implementors enumerated at checkout HEAD).
14. **Args and multi-statement, per the method-shaped dispatch (r2 MF3,
    r3 MF1, r4 MF1):** pinnedTx `QueryContext`/`ExecContext` with bound
    arguments — including NULL and binary-sensitive values — work
    through the private unnamed extended sequence, compared against the
    POOL path by DECODED semantic equality (the raw text/binary format
    difference between the pool's result-format negotiation and this
    path's is a format choice, not a value difference; the comparator
    decodes both sides rather than diffing raw bytes). A no-args
    MULTI-STATEMENT `ExecContext` drains both groups through the simple
    frame (pgx's own no-args behavior, verified v5.10). And a
    MULTI-COMMAND `QueryContext` is rejected with the server's own Parse
    error, verbatim — the existing pool-path default behavior, preserved
    rather than invented around.
15. **Legacy finalizer shape (r2 MF4, r3 MF2):** pinnedTx's no-context
    `Commit`/`Rollback` dispatch on the context captured at
    `BeginSessionTx` — proven by cancelling that context and observing
    the base method fail with the context error AND the handle go
    TERMINAL (a subsequent finalizer reports `dao.ErrTransactionClosed`),
    with `Discard` then reclaiming the pinned lease — the legacy
    observable behavior, not the context-finalizer retry shape (whose
    own cell remains criterion 4's fault-state-1).

16. **The ErrorResponse cleanup tail (r5 MF1):** an Execute-stage
    ErrorResponse inside an EXPLICIT transaction is driven through the
    error tail — recovery Sync consumed to its terminal ReadyForQuery,
    then the tracked-creation Close+Sync cleanup exchange — and the cell
    proves the wire quiescent and the private unnamed objects ABSENT
    before reuse: an extended Describe of the unnamed statement returns
    the server's no-such-statement error, and a subsequent pinned
    segment runs normally. The blind-Close guard is proven in the same
    cell's negative arm: a Parse-stage ErrorResponse (nothing was
    created) issues no Close.

## 6. Implementation record (2026-09-02)

Shipped in `dao/postgres` as `extendedops.go` (vocabulary), `pinned.go` (capability,
handle, state machine) and `pinnedtx.go` (guarded transaction, private query path,
driver-owned rows), with `pinned_test.go` (server-free) and
`pinned_integration_test.go` (one cell per criterion, `-tags integration`). dao core is
untouched. Refinements the implementation made explicit, each within the letter of §2:

- **`Flush` emits the protocol Flush (`H`) frame** after the queued frames. Without it
  the server buffers every response until Sync and `Receive` cannot serve a group
  message-at-a-time (criterion 2). Corollary observed live: the server processes frames
  in order, so a Flush queued behind an Execute that blocks server-side is not reached
  until the Execute completes — a consumer that wants Parse/Bind acknowledgements before
  a long Execute flushes them first.
- **`ExtendedMessage` also surfaces the three asynchronous backend messages** —
  `NoticeResponse`, `ParameterStatus`, `NotificationResponse` — as protocol data with
  their own Kinds, because a relay must forward them verbatim and the server may
  interleave them with any group. `ExtendedOp` is unchanged: still exactly five shapes.
  The zero `ExtendedOp{}` (the one spelling constructible without a constructor) is
  refused by `Send` with `ErrInvalidExtendedOp`.
- **Typed capability helpers**: `SupportsSessionPinning(conn) bool` and
  `PinSessionConn(ctx, conn) (PinnedConn, error)`, the latter reporting
  `dao.ErrUnsupported` on a miss (criterion 9), mirroring `dao.BeginConnTx`.
- **`Discard` closes the physical connection whenever reuse is unprovable** (poisoned,
  mid-flight, private exchange, or an unfinalized transaction) before returning the
  lease, because the pool's own dirty test — busy / non-idle TxStatus / closed — does
  not see a read-timeout-poisoned wire with unread responses and would recycle it. From
  a quiescent, transaction-closed handle it recycles. It interrupts an in-flight read
  with a past socket deadline and barriers on the wire mutex before closing, since
  closing a pgconn under an active operation is a data race (criterion 10).
- **`Release` reports a mid-flight segment before an open transaction** — both refusals
  are immediate; the segment is the more immediate fact about the wire (criterion 6).
- **Dispatch-aware finalizer errors feed the shared classifier.** pgconn marks every
  `ReceiveMessage` failure SafeToRetry because its own exec path checks the context
  before writing; on the raw face the COMMIT frame is already on the wire when the read
  runs, so that flag would turn a lost answer into a "proven rollback". The raw path
  therefore returns a not-dispatched shape (SafeToRetry, nothing written, wire clean,
  server transaction still open — `Release` refuses with `ErrTxStillOpen`, `Discard`
  closes) when the context is done before the write, and a dispatched-and-lost shape
  (never SafeToRetry, handle poisoned) after it; `classifyCommit` is reused unchanged and
  the fault-state matrix (criterion 4) passes through it.
- **Empty is not NULL on the private Bind.** `pgtype.Map.Encode` signals NULL with a nil
  slice, and appending zero bytes to a nil scratch buffer also returns nil; encoding into
  a shared non-nil scratch (as pgx's ExtendedQueryBuilder does) keeps an empty string an
  empty value. Caught live by criterion 14.
- **Criterion 16 has a third arm.** PostgreSQL folds `1 / $1::int` at BIND, so that SQL
  produces a Bind-stage error: statement created, portal not, exactly one Close. The
  Execute-stage arm uses a set-returning function the planner cannot fold; the Parse-stage
  arm sends no Close at all.
- **Two protocol facts the cells depend on**, observed against PostgreSQL 17: in an
  aborted transaction a Describe of an EXISTING data-returning statement is refused with
  25P02 while a MISSING statement/portal still reports 26000/34000, and Close succeeds
  (pgconn's own `Deallocate` relies on this).

### Review round r1 — three lifecycle/concurrency defects folded (2026-09-02)

Both reviewers independently reproduced all three on head `f74bfd1`; each fix has a
regression cell and a named mutation that reddens it.

- **Release terminalizes the handle (MF1).** `Release` returned the member to the pool
  but left the handle live: `frontend`/`pgConn`/`netConn` still pointed at a connection
  the pool could hand to another caller, so a later `Send` queued bytes onto a
  stranger's wire (mutation `MF1-release-not-terminal`: `Send after Release = <nil>`).
  A successful `Release` now sets a `released` terminal flag UNDER `mu`, before the
  lease goes back, and every face refuses with the new `ErrReleased`. Terminality is
  claimed before `acq.Release` precisely so no concurrent call can slip into the gap.
  Cell: `TestPinned_MF1_ReleasedHandleIsTerminal` — the full face sweep plus a
  max-pool-1 arm that re-borrows the same member and proves the stale handle cannot
  touch it mid-borrow, and that a stale `Discard` does not reclaim the new owner's
  connection. Criterion 10's oracle was tightened accordingly: `Release` is no longer in
  its spinner mix (it terminalizes the handle, which would make the liveness assertion
  vacuous), so any post-storm segment error is now a defect rather than an accepted
  outcome.
- **Writes are bounded by cancellation, not only by a deadline (MF2).**
  `writeBuffered` installed a socket deadline only when `ctx` carried one, so a
  cancellable no-deadline context — and a cancellation landing before a later deadline —
  could not interrupt a `frontend.Flush` already parked in `Write`, contrary to
  `PinnedConn.Flush`'s contract and §2.3 (mutation
  `MF2-write-bounded-by-deadline-only`). A watcher now shortens the socket deadline the
  instant `ctx` is done; its teardown is synchronized (`wg.Wait`) before the deadline is
  cleared so it can never strand a past deadline on the next operation, and the raw
  context cause is preserved over pgconn's timeout wrapper. A cancellation mid-write
  stays a transport-level outcome: the handle is poisoned.
- **`Send` and `Flush` are mutually exclusive over the write buffer (MF3).** Both touch
  pgproto3's shared `wbuf`. `Send` encoded under `mu` but never owned the wire, so a
  `Send` arriving while `Flush` was inside `Write` appended a frame that the subsequent
  buffer reset erased, and `Flush` then overwrote the `building` state that `Send` had
  just produced (mutation `MF3-send-during-flush-admitted`). `Flush` and `Sync` now
  claim a `writing` flag under `mu` BEFORE the write begins; `Send`, `Flush` and `Sync`
  refuse immediately with `ErrSegmentInFlight` while it is held, and the claim is
  released together with the state transition in one critical section. The
  Send-during-**Receive** resume path is untouched — `Receive` does not write — and has
  its own cell (`TestPinned_MF3_ResumeSendDuringReceiveStillAllowed`) guarding against
  the fix over-reaching.

The two server-free cells drive a `net.Pipe` whose peer is not reading and use a
gate writer that signals the instant the write parks, so each window is entered
deliberately rather than raced for.

### Review round r2 — MF4, the poison-publication gap (2026-09-02)

Independently reproduced by both reviewers (at iterations 348 and 18) and confirmed
here. `Sync`'s completion block cleared the `writing` claim and released `mu`, and only
then called `poison()`, which reacquires it. `Sync` is legal from quiescent and resets
nothing on failure, so in that gap the handle read as `(idleOut, noInbound, !writing,
!poisoned)` — indistinguishable from a healthy idle handle. A concurrent `Release` would
hand the failed wire back to the pool and `Discard` would compute it reusable.

The failure state and the claim release are now published in ONE critical section, in
both `Sync` and `Flush`. `Flush` was not directly exploitable — it retains `out ==
building` on failure, which already blocks clean reuse — but the invariant is normalized
rather than left resting on that incidental fact. `Receive` is audited and deliberately
unchanged: it is legal only from `out ∈ {flushed, building}` and resets nothing, so the
handle is never quiescent in its error path; a comment records this so the pairing is not
"simplified" away later.

Discard's reuse test is now the named `reusableLocked` predicate, so the regression cell
observes the REAL decision function rather than a copy that could drift from it. The cell
(`TestPinned_MF4_FailedSyncCannotCleanReleaseOrCleanDiscard`) asserts both consequences —
`Release` never accepts and `reusableLocked` never reads true — with both spinners
starting only after the write is under way, because a clean `Release` before that is
entirely legal. A writer handshake parks the failing write until the spinners are
provably hot, which makes the window reachable on demand: the mutation
`MF4-poison-published-after-claim-release` reddens at iteration 7 rather than by luck.
`TestPinned_MF4_QuiescentReleaseStillAccepted` is its positive control — without it
"Release never returned nil" would also pass on a handle that can never be released.


---

## Amendment 1 (2026-09-02, PROPOSED by jarvis — awaiting lector design review; re-scopes acceptance criterion 13, so Johno's ratification is requested)

**A result-bearing simple-protocol query on the raw face, reachable only through the consumer's own gate.**

### A1.1 The gap

autodb's F1 wire loop (ADR-0075) must forward the TARGET's `RowDescription`, `DataRow`, `CommandComplete` and target `ErrorResponse` frames **verbatim** (autodb protocol matrix §5, rows 285–286). Two facts, both verified by reading, make that impossible with what ships today:

1. autodb's `core/exec.Result` is `Columns []string; Rows [][]any; More bool` — decoded through `database/sql`, paged at `maxRows` (500). No OIDs, no per-column formats, no raw bytes. It cannot be re-framed faithfully.
2. This ADR's §2.5 excludes the simple-protocol `Query` frame from `ExtendedOp` by design, and criterion 13 proves *"no spelling of a simple Query reaches the wire through the seam."* The raw face DOES send a simple Query internally — `execSimpleLocked` drives `BEGIN`/`COMMIT`/`ROLLBACK` and `pinnedTx.ExecContext` with no args (§2.4) — but that path returns only `(tag, *PgError, status)` and **discards** every `RowDescription`/`DataRow` on the way to the terminal `ReadyForQuery`.

So the F1 simple path has no way to obtain the target's frames, and the loop owner (Ultron) and its first reviewer (Zen) both stopped at this point rather than improvise. Correctly: it is a decision about this seam.

### A1.2 What §2.5 actually excluded, and what this amendment reverses

§2.5's rationale, verbatim: *"autodb's classifier/grant gate runs at Parse (and its simple path has its own gated entry); a Query op on this seam would bypass the gate entirely."* The threat model is a **relay consumer** feeding client frames through the seam so that a `Query` skips the Parse-time gate. Two things follow:

- The exclusion has two halves. The `ExtendedOp` **sum** has no `Query` constructor, and that stays exactly as it is — this amendment adds no constructor, and that compile boundary is untouched. But criterion 13 also made *no simple Query at all through the pinned connection* an accepted security boundary, and **that half is reversed** here (A1.4). Both statements are true and the second is the one that needs an owner's ratification.
- §2.5 itself names *"its simple path has its own gated entry."* That entry is autodb's engine: for simple-protocol text, the classifier, capability profile, grants and the F3a shared unit policy run on the SQL **text** before any dispatch — the same gate `WireExecute` applies today. The engine is the gate; the wire is downstream of it. A raw simple query invoked **by the engine after its gate** is the gated entry §2.5 already assumes exists, made real.

The alternatives are worse in ways that matter:

- **(b) Emulate simple via the private unnamed extended sequence.** Wire-faithful frames, no seam change — but `Parse` rejects multi-statement strings with the server's error, and the implicit-transaction-block semantics differ from the simple protocol. ADR-0075 MF2 makes those semantics load-bearing: lib/pq emits **simple** frames for parameterless queries. Emulation would make a real client see behaviour PostgreSQL never produces for the frame it sent. Rejected.
- **(c) Text-re-encode the decoded rows in the loop.** Functional for `psql`; violates "forwarded verbatim" and "no silent truncation" outright, and could be cited for nothing in §5. Rejected.

### A1.3 The decision

A **separate capability interface**, declared in `dao/postgres` beside `SessionPinner` (§2.5: no alias or re-export in `dao` core), and deliberately **not** added to `PinnedConn`:

```go
// SimpleQuerier is the simple-protocol face of a pinned connection: ONE Query
// frame, the response streamed as protocol data until the terminal
// ReadyForQuery. It exists for a consumer that gates SQL TEXT before dispatch
// (autodb's engine). It is declared as a SEPARATE interface rather than on
// PinnedConn so that reaching it requires an explicit type assertion — which
// an acceptance grep can see. That is VISIBILITY, not prevention: the dynamic
// *pinnedConn implements both faces, so any holder of the handle can assert
// for this one. golib does not and cannot make the method engine-only; the
// consumer does (A1.4).
type SimpleQuerier interface {
    // SimpleQuery sends ONE simple-protocol Query frame carrying sql, then
    // calls emit for every backend message of the response — RowDescription,
    // DataRow, CommandComplete, EmptyQueryResponse, ErrorResponse (as
    // protocol data, ExtendedMessage.Err, never a Go error), and the
    // asynchronous NoticeResponse/ParameterStatus/NotificationResponse —
    // in wire order, and consumes the terminal ReadyForQuery itself,
    // returning its status byte. Multi-statement text produces several
    // groups and ONE terminal ReadyForQuery, exactly as psql sees it.
    //
    // DataRow values are BORROWED for the duration of the emit call (the
    // RawRows rule: a kept row is bytes.Clone'd by the consumer). Nothing is
    // accumulated, paged, or truncated by the driver: there is no maxRows
    // on this path, by construction — a cap is the consumer's policy, and
    // it is applied where it can be audited.
    //
    // Legal only from the quiescent segment state (outbound idleOut,
    // inbound noInbound, not poisoned) — the same guard as pinnedTx's own
    // operations (§2.3 table). The transaction track is orthogonal: an OPEN
    // session transaction is the normal case, since ADR-0075's F1 runs every
    // Query inside the session transaction. Any other state returns
    // ErrSegmentInFlight immediately, never a wait.
    //
    // A non-nil error from emit stops the stream: the driver still drains to
    // the terminal ReadyForQuery (so the wire returns to quiescent) and then
    // returns emit's error. A transport-level failure poisons the handle per
    // the §2.3 poison rule. An ErrorResponse does NOT poison and needs no
    // Sync: the simple protocol is self-terminating, and the server sends
    // ReadyForQuery after the error itself.
    SimpleQuery(ctx context.Context, sql string, emit func(ExtendedMessage) error) (status byte, err error)
}
```

`*pinnedConn` implements it, reusing `execSimpleLocked`'s frame send and read loop and the existing `decodeMessage` (§2.5 vocabulary) — the only new code is *emitting* messages the current loop discards, and the guard. `ExtendedMessage` gains no fields: `RowDescription`/`DataRow`/`CommandComplete`/`ErrorResponse`/`EmptyQueryResponse` and the three asynchronous kinds are already in the closed set.

### A1.4 Criterion 13 — this is a REVERSAL, and it is called one

Criterion 13 currently makes *"no simple Query reaches the wire through the pinned connection"* an **accepted, compile-enforced security boundary**. This amendment **reverses that premise**: after it, a simple Query CAN reach the wire through the pinned connection, via `SimpleQuerier`. The `ExtendedOp` compile boundary stands unchanged; the *no-simple-Query-at-all* boundary does not. That is why Johno's ratification is required and not merely requested (lector ruling, 2026-09-02).

**The honesty constraint, stated rather than papered over (lector):** an exported golib method is callable by ANY holder of the pinned handle. Documentation saying "engine-only" is not enforcement, and putting the method on a separate interface makes the assertion *visible*, not *impossible*. So the boundary is split across the two repos and named as such:

- **golib exposes a narrow leaf capability** — one real Query frame, streamed, self-terminating, no accumulation — and proves only what golib can prove: the `ExtendedOp` sum is closed, the state machine is shared, nothing is re-exported from `dao` core.
- **autodb structurally enforces engine-only ownership**: classification + capability profile + grants + F3a unit policy + dispatch are ONE `core/exec`-owned operation over the **exact same SQL bytes** (no re-read of the text between gate and dispatch); `frontdoor/` never receives the raw pinned capability — it receives neutral messages from the engine and nothing else. Proven by dependency/grep on autodb's implementation HEAD **and** by bypass/substitution mutations (a `frontdoor` reference to `SimpleQuerier`, or a dispatch path that skips the gate, must redden).

Criterion 13 becomes:

> 13. **The closed vocabulary (r1 MF1, A1):** the Query-frame exclusion from `ExtendedOp` is test-proven — the forbidden shape cannot be constructed through `ExtendedOp` (compile) and the boundary test proves no spelling of a simple Query reaches the wire through the **extended seam** (`Send`/`Flush`/`Receive`/`Sync`); no alias or re-export of either capability exists in `dao` core (grep/compile proof on the implementation HEAD, implementors enumerated at checkout HEAD). The simple-protocol path exists **only** as `SimpleQuerier` (A1.3) and is governed by A1-C1..A1-C4 below. *(This narrows 13's former "no simple Query at all" half; see A1.4.)*

Amendment criteria, numbered `A1-Cn` so they cannot collide with the existing §5 list (lector r1):

> **A1-C1 — Reachable only through the consumer's gate; enforced in autodb, stated honestly in golib.** golib cannot compile-enforce "engine-only" for an exported method and does not claim to; it exposes a narrow leaf capability. In autodb: the sole type assertion for `postgres.SimpleQuerier` lives in `core/exec`; gate (classifier, profile, grants, F3a unit policy) and dispatch are ONE operation over the **exact same SQL bytes**; `frontdoor/` never holds the raw capability. Proven by dependency/grep on autodb's implementation HEAD (no reference in `tui/`, `webserver/`, `rpc/`, `frontdoor/`) **and** by bypass/substitution mutations: a dispatch that skips the gate, a re-read of the text between gate and dispatch, or a `frontdoor` reference to the capability must each redden.
>
> **A1-C2 — Verbatim and unbounded.** For a query returning more rows than autodb's `DefaultMaxRows`, every `DataRow` reaches `emit` with byte-equal payloads to a direct `pgconn` execution (no paging, no `More`, no re-encoding); each `RowDescription` field's `TypeOID`/`Format`/`TypeModifier` equals the direct execution's; `CommandComplete` carries the server's tag verbatim; an empty string yields `EmptyQueryResponse`; a target `ErrorResponse` arrives as `ExtendedMessage.Err` with every `*pgconn.PgError` field byte-equal to a direct execution's and does **not** poison; asynchronous `NoticeResponse`/`ParameterStatus` (e.g. after `SET`) are emitted in their wire position relative to the data messages, not reordered or dropped; a multi-statement string yields each group in order and exactly one terminal `ReadyForQuery`. Mutations that must redden: applying a cap; dropping or rewriting any message kind; rewriting any `PgError` field; reordering an asynchronous message.
>
> **A1-C3 — Streaming, and the callback contract.** `emit` is called for each message **as it is decoded**, before the tail is consumed — proven by a cell whose emitter observes the first `DataRow` while the server is still blocked producing later rows (buffer-then-emit reddens it). A nil `emit` fails **before dispatch** with a typed error and sends no frame. `emit` is explicitly non-reentrant: calling any handle method from inside it returns a typed error, never deadlocks. A non-nil error from `emit` stops delivery, the driver **drains to the terminal `ReadyForQuery`**, returns the emitter's error, and the wire is quiescent — proven by a following `SimpleQuery` on the same handle succeeding. The terminal `ReadyForQuery` is **never emitted**; it returns only as the status byte. Mutations: emitting `ReadyForQuery`; returning before the drain completes; buffering rows.
>
> **A1-C4 — Failure ordering and poison.** A context cancellation or transport failure during send, read, or the post-emitter-error drain **poisons** the handle per §2.3 and the caller must `Discard`; the returned error is the transport error, which **outranks** a prior emitter error (the emitter's error is not what the wire's state means). A `SimpleQuery` on a non-quiescent segment state returns `ErrSegmentInFlight` immediately; with the session transaction OPEN it succeeds and the transaction remains open (the F1 case); a transaction-track change the statement itself causes (`BEGIN`/`COMMIT` in the text) is reflected in the returned status byte and in the handle's transaction track. Mutations: swallowing the transport error behind the emitter error; treating an `ErrorResponse` as poison; allowing dispatch mid-segment.

### A1.5 What this does NOT change

The `ExtendedOp` sum, `PinnedConn`'s surface, `pinnedTx`'s dispatch rules (§2.4), the poison rule, and the `dao` core (nothing added). `SessionPinner` is unchanged; `SimpleQuerier` is asserted separately on the same handle.

### A1.6 Sequence

golib: this amendment → lector design review → implementation with criteria 15–16 → release. autodb: repin → `core/exec.WireQuery` (gate the text, then `SimpleQuery`, emitting neutral messages to the loop) → the F1 loop's §5 rows. Until then the loop builds against a message interface with an interim producer cited for nothing in §5.

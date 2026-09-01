# ADR-0018 — `golib/dao`: session-pinned connections with raw extended-protocol execution

- **Status:** **Proposed** — rev 1, design review r0 findings folded
  (lector: 4 MF, all confirmed). The ADR-0017 follow-up that ADR-0075
  (autodb's wire front door) filed: "requires a scoped golib seam
  (pinned-conn extended-exec capability)".
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
    // typed ExtendedError value, NOT a Go error — after it the segment
    // enters discard-until-sync (§2.3) and the consumer must drive
    // Sync; the caller classifies and re-frames it for its own client.
    Receive(ctx context.Context) (ExtendedMessage, error)
    // Sync sends Sync and consumes through ReadyForQuery, returning the
    // ReadyForQuery status byte. This is the ONLY call that returns the
    // wire to the quiescent state; discard-through-Sync on error is driven
    // by calling Sync repeatedly until it reports the terminal
    // ReadyForQuery (§2.3).
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
borrowed buffers, never accumulated by the driver. The handle's states:

```text
                 Send (queues only)
   quiescent ────────────────────────────▶ building
   ▲    ▲                                       │ Flush (writes)
   │    │                                       ▼
   │    │             ReadyForQuery        sent ──Receive──▶ receiving
   │    │        ◀────────────────────────────┘                 │
   │    │                                                      │
   │    │ Sync (sent + consumed                          ErrorResponse /
   │    │      through ReadyForQuery)             PortalSuspended / any
   │    │                                          protocol outcome
   │    ▼                                                 ▼
   │ quiescent ◀── Sync's terminal ReadyForQuery   receiving (client
   │                                              keeps pulling rows)
   │                                                      │
   └──────────── Sync, repeatedly until the terminal ReadyForQuery
                                              ▼
                                       discard-until-sync
```

- **quiescent**: nothing queued, nothing pending. Only here may
  `BeginSessionTx`, `Release`, or a new first `Send` run. A call from any
  other state returns `ErrSegmentInFlight` immediately, decided under the
  handle's lock — the check is a state inspection, not a mutex wait
  (r0 MF2: serialization is not refusal).
- **building** → **sent** → **receiving**: the ordinary segment. `Send` is
  legal in all three (pipelined groups); `Receive` blocks only in
  receiving.
- **discard-until-sync**: entered on any `ErrorResponse` returned as
  protocol data, or on a `PortalSuspended` the consumer does not intend to
  resume. `Receive` in this state consumes and drops messages; `Sync`
  drives it to the terminal `ReadyForQuery` and returns the wire to
  quiescent. Exactly `Sync`'s terminal `ReadyForQuery` performs that
  transition — `Sync` is the owner of quiescence.
- **poisoned**: a transport-level Go error (EOF, cancelled context at
  dispatch, socket failure) — as opposed to protocol data. `Receive`/`Send`
  return the error; the ONLY legal next call is `Discard`, which closes
  the physical connection. No repair is attempted: a connection whose
  frame boundaries are unprovable is a discarded member, and pretending
  otherwise hands the next pool consumer a dirty wire.

`ReadyForQuery` ownership: the terminal `ReadyForQuery` belongs to `Sync`
(and to `Discard`'s teardown, which never surfaces it). A `Receive` that
would return a `ReadyForQuery` in a non-terminal position is a contract
violation by the consumer — it sent no Sync — and returns a typed error;
the driver does not silently absorb protocol skew.

Blocking and cancellation: `Send` is non-blocking (queue only); `Flush`
blocks on the write; `Receive` blocks on the read; all bounded by ctx. A
ctx cancellation during `Flush`/`Receive` is a transport-level outcome →
poisoned (the frame boundary is unprovable), reported as the raw context
error with the poisoned state recorded.

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
never interleave with client frames. Named statements/portals owned by the
relayed client survive the transaction boundary exactly as the server
keeps them (server-side objects); the unnamed statement is destroyed by the
simple-protocol finalizer commands per PostgreSQL semantics — the same
behavior a real backend has, and the consumer's §4a bookkeeping owns
tracking it.

### 2.5 The vocabulary: neutral op/message structs, not pgproto3 re-typing

`ExtendedOp` (the Send argument) and `ExtendedMessage` (the Receive
result) are small **neutral types declared in `dao/postgres`**, mirroring
the wire messages a relay must forward (Parse/Bind/Describe/Execute/
Close/Flush in; ParseComplete/BindComplete/RowDescription/DataRow/
CommandComplete/PortalSuspended/CloseComplete/NoData/ErrorResponse/
ReadyForQuery out) and carrying the raw fields byte-faithfully (parameter
`[][]byte` values, format codes, OIDs, per-column result formats,
`maxRows`). The conversion to and from `pgproto3` frames happens inside
the driver, against the pinned `pgconn.PgConn`'s frontend.

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
   diff is empty; mysql/sqlite/bigquery compile untouched.
2. **The asynchrony contract (r0 MF1):** a pinned handle queues Parse and
   Bind back-to-back before ONE Flush and receives their response group
   message-at-a-time — no deadlock, no blocking in Send; and an Execute's
   DataRow stream is consumed in bounded memory (a large result streams
   one borrowed row per Receive without accumulation; the driver holds at
   most one row buffer).
3. **The state machine:** ErrorResponse arrives as protocol data, drives
   discard-until-sync, and the terminal Sync returns the ReadyForQuery
   status byte and reopens the wire; a transport error poisons the handle
   and every subsequent operation except `Discard` returns the typed
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
6. **Refusal, not serialization (r0 MF2):** `BeginSessionTx`, `Release`,
   and every `pinnedTx` operation invoked while a segment is mid-flight
   return `ErrSegmentInFlight` IMMEDIATELY (state inspection under the
   handle's synchronization — measured, not asserted: the cell proves no
   waiting occurred) and the wire stays coherent afterwards.
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
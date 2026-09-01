# ADR-0018 — `golib/dao`: session-pinned connections with raw extended-protocol execution

- **Status:** **Proposed** — awaiting design review. The ADR-0017 follow-up
  that ADR-0075 (autodb's wire front door) filed: "requires a scoped golib
  seam (pinned-conn extended-exec capability)".
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
// transaction — and they share ONE wire, so the handle serializes them.
type PinnedConn interface {
    // ExtendedExec drives ONE extended-protocol segment step against the
    // pinned connection's raw frame face (§2.3).
    ExtendedExec(ctx context.Context, op ExtendedOp) (ExtendedResult, error)

    // BeginSessionTx opens the session's transaction ON THE PINNED
    // CONNECTION, returning the same dao.ContextTxConn contract the pool
    // path returns (ADR-0017 outcomes preserved verbatim).
    BeginSessionTx(ctx context.Context, opts dao.TxOptions) (dao.ContextTxConn, error)

    // Sync drains the connection to a message boundary (Sync + consume
    // through ReadyForQuery) — the quiesce a finalizer or close needs, per
    // the engine's established cancel/finalizer ordering.
    Sync(ctx context.Context) error

    // Release returns the connection to the pool. Refuses (typed error),
    // rather than silently discarding state, while a transaction is open or
    // a segment is mid-flight.
    Release(ctx context.Context) error
}
```

`BeginSessionTx` on the pinned handle **reuses the existing `pgxTx`**
wrapper, so commit/rollback outcome classification (`ErrTxRolledBack`,
`ErrTxOutcomeUnknown`, preserved `*pgconn.PgError` causes) is one
implementation, not two. A pinned transaction and a pool transaction behave
identically at finalize time; only the connection's provenance differs.

### 2.3 The raw face: neutral op structs, not pgproto3 re-typing

`ExtendedOp`/`ExtendedResult` are small **neutral structs declared in
`dao/postgres`**, mirroring the wire messages a relay must forward
(Parse/Bind/Describe/Execute/Close/Flush in; ParseComplete/BindComplete/
RowDescription/DataRow/CommandComplete/PortalSuspended/… out) and carrying
the raw fields byte-faithfully (parameter `[][]byte` values, format codes,
OIDs, per-column result formats, `maxRows`). The conversion to and from
`pgproto3` frames happens inside the driver, against the pinned
`pgconn.PgConn`'s frontend.

**Rejected:** exposing `*pgproto3.Frontend` directly (alternative 3.1) — it
would hand the consumer pgconn's internal locking discipline as an implicit
contract, and would make the consumer's correctness depend on pgx's private
state machine staying out of the way, which no consumer can promise. The
op-struct boundary keeps the driver owning the wire exclusivity rules while
leaving every wire-level decision (which frame, which order, which
lifetime) with the consumer, which is where ADR-0075 put them.

**Rejected:** mirroring the full extended vocabulary in `dao` core
(alternative 3.2) — the message set is large, churns with the wire, and
duplicates types `pgproto3` already defines; it would also put a
PostgreSQL-specific vocabulary in a driver-neutral package, against the
layering ADR-0017 established.

### 2.4 Wire exclusivity: one face at a time, enforced

While a raw segment is active (`ExtendedExec` between `Sync` points), the
pinned pgx connection's high-level machinery must not run — no pool query,
no statement-cache hit, no finalizer dispatch. The handle enforces this
itself:

- The handle does not expose the `*pgx.Conn`; the only operations available
  are the four methods above, and they serialize on the handle.
- `BeginSessionTx` requires a quiesced wire (nothing pending); if a segment
  is mid-flight it returns a typed `ErrSegmentInFlight` rather than
  interleaving pgx's BEGIN with the client's frames.
- Finalizers on the returned `ContextTxConn` require the same quiesce —
  which the consumer (autodb's engine) already guarantees by its own
  slot/quiesce ordering (the synchronous-demotion lifecycle's
  cancel-then-join-then-finalize rule); the seam repeats the check so the
  safety does not depend on one caller's discipline.
- pgx's cached connection state (tx status, statement cache) is bypassed
  deliberately while raw segments run; `Sync` re-synchronizes what pgx
  needs before any high-level operation is allowed again.

A misused handle fails loudly with typed errors
(`ErrSegmentInFlight`, `dao.ErrTransactionClosed`-shaped finalize-on-open-
segment), never with protocol corruption.

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
   Rejected in §2.3: implicit internal-contract leakage.
4. **Neutral extended vocabulary in `dao` core.** Rejected in §2.3:
   layering inversion; churn; duplication.

## 4. Consequences

- Additive only: `dao` core, mysql, sqlite, bigquery, fakes untouched.
  Compile-time assertions in `dao/postgres`
  (`var _ SessionPinner = (*pgxConn)(nil)`).
- `dao/postgres` gains the extended op/result structs — its surface grows,
  which is where the convention says a driver-specific capability lives.
- pgx version floor stays `v5.10.0` (no new dependency; pgproto3 arrives
  transitively via the existing pgx dependency, and only inside the leaf).
- autodb repins golib to the release carrying this, then builds F2 against
  `PinnedConn`; its front door keeps its own pgproto3 dependency for the
  CLIENT side and maps client frames to these ops.
- The exclusivity rules are part of the public contract: documented, typed
  on misuse, race-tested (`ExtendedExec` concurrent with finalize must fail
  cleanly, not corrupt).

## 5. Acceptance criteria

1. `var _ SessionPinner = (*pgxConn)(nil)` in `dao/postgres`; `dao` core
   diff is empty; mysql/sqlite/bigquery compile untouched.
2. A pinned handle drives a full extended segment against a live
   PostgreSQL: named Parse → Bind (binary params, explicit result formats) →
   Describe(S) → Describe(P) → Execute(maxRows) → `PortalSuspended` →
   re-Execute → Sync, with byte-identical parameter bytes and formats in
   and raw messages out (pg VM evidence, same harness as ADR-0017's
   integration suite).
3. `BeginSessionTx` on the pinned conn returns a `ContextTxConn` whose
   commit/rollback outcomes are the ADR-0017 sentinels — proven by the
   same fault-state matrix as the pool path (server-confirmed rollback,
   `pgx.ErrTxCommitRollback`, dispatch-proven-never-written,
   outcome-unknown).
4. A transaction begun on the pinned conn is observably the SAME backend
   transaction the raw segments execute in (`txid_current()` captured via
   a raw Execute inside the pinned tx matches the handle's tx).
5. Exclusivity: finalize/`BeginSessionTx`/`Release` during a mid-flight
   segment returns the typed error, and the wire stays coherent afterwards
   (a subsequent `Sync` + query succeeds).
6. Miss reported honestly: a `DataConn` without the capability probed via
   the typed helper returns `dao.ErrUnsupported` (capability-honesty test).
7. Race detector clean over concurrent segment steps vs finalizer attempts
   on one handle.
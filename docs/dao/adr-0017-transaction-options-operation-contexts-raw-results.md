# ADR-0017 — `golib/dao`: transaction options, per-operation contexts, raw result access

- **Status:** **Accepted (rev 5) — implemented** (2026-08-30. Authored by jarvis
  from the autodb session-engine contract; six lector design rounds
  (r0 4MF+3SF → r5 clean, 0 findings) before Johno's acceptance stamp.
  Implemented on `dao-tx-options`. KB:
  `shared/adrs/golib-dao-0017-tx-options-per-operation-contexts-raw-results.md`,
  review `agents/lector/reviews/2026-08-30-golib-dao-0017-r0-design-review.md`.)
- **Date:** 2026-08-30
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** nothing. **Amends** the `DataConn.Begin` / `TxConn` surfaces of
  ADR-0005 as refined by ADR-0015 — additively, through capability interfaces.
  ADR-0015's `RunTx` ownership model, its options tail, and the two-phase
  machinery all stand unchanged.
- **Related:** ADR-0005 (transactions), ADR-0015 (transaction/connection
  ownership), ADR-0008 (`ErrUnsupported` capability honesty), ADR-0012
  (`RowsColumns`, the row-extension precedent), ADR-0013 (`TableQuoter`), the
  `interface-evolution-capability-interfaces` KB convention, and the downstream
  consumer contract (autodb ADR-0074, session-capable execution engine).

## 1. Context

A consumer that pins one transaction across several RPC calls — autodb's
session engine — cannot be built on the surface as it stood. Three verified
defects:

1. **No transaction options.** `DataConn.Begin(ctx)` cannot express `READ ONLY`,
   an isolation level, or `DEFERRABLE`: postgres began with `pool.Begin`, mysql
   and sqlite with `BeginTx(ctx, nil)`. Engine-enforced read-only is
   `Access: ReadOnly` and nothing else — a transport-level "does this statement
   look like a write?" check is explicitly insufficient, because the guarantee
   has to come from the server.
2. **`Commit()`/`Rollback()` take no context.** The postgres driver captured the
   `Begin` context and reused it for both, and `database/sql` auto-rolls-back
   when the `BeginTx` context is cancelled. A session-lifetime context fixes
   handler-return cancellation but leaves **no usable context for cleanup after
   that context is cancelled** — exactly the moment cleanup is needed.
3. **No raw result access.** `Rows` exposes only `Scan`. A pass-through consumer
   forwards the target's own bytes and the server's column metadata; pgx holds
   both (`RawValues`, `FieldDescriptions`) and dao erased them, forcing a decode
   into Go types the consumer has no reason to name and a re-encode after.

## 2. Decision

### 2.1 A distinct tri-state `TxOptions`

The coordinator's `TxOption` (a functional option on a logical `Transaction`,
`transaction.go`) and the package-level `dao.Begin` already exist and are
untouched. The driver-level value is a **separate struct**, tri-state so an
explicit `READ WRITE` / `NOT DEFERRABLE` is expressible against a server whose
default is the opposite:

```go
type TxAccess int          // TxAccessDefault | TxReadOnly | TxReadWrite
type TxIsolation int       // TxIsoDefault | TxReadUncommitted | TxReadCommitted |
                           // TxRepeatableRead | TxSerializable
type TxDeferrableMode int  // TxDeferrableDefault | TxDeferrable | TxNotDeferrable

type TxOptions struct {
    Access     TxAccess
    Isolation  TxIsolation
    Deferrable TxDeferrableMode
}
```

The zero value is "server defaults" — what `Begin` has always sent, so it is
byte-compatible with existing behavior. `READ UNCOMMITTED` is in the domain
because both pgx and mysql accept it; a matrix that omitted it would have been
dishonest.

**Scope:** single-connection transactions only. The multi-connection
`Transaction` / `RunTx` / two-phase machinery is required by other applications
and is neither extended nor shrunk here.

### 2.2 Capability interfaces — the base interfaces stay byte-identical

`DataConn`, `TxConn` and `Rows` are published interfaces with implementors
outside this module (the nested `dao/bigquery`, external fakes, pinned
consumers). Growing them would break every one of them, so evolution is
additive — the binding
`interface-evolution-capability-interfaces` convention, ratified as golib policy:
existing capabilities are not broken for the rest of the consumers unless the
change is a warranted general improvement for all.

```go
type TxBeginner interface {         // options, without context finalizers
    BeginTx(ctx context.Context, opts TxOptions) (TxConn, error)
}
type ContextTxConn interface {      // finalizers with their own context
    TxConn
    CommitContext(ctx context.Context) error
    RollbackContext(ctx context.Context) error
}
type SessionTxBeginner interface {  // both at once, assertable on the CONNECTION
    BeginSessionTx(ctx context.Context, opts TxOptions) (ContextTxConn, error)
}
type RawRows interface {            // the driver's wire bytes + the server's descriptors
    Rows
    RawValues() [][]byte
    Fields() []FieldDescription
}
```

with typed helpers `BeginConnTx`, `CommitTx`, `RollbackTx`, `RawRowsOf` and the
validator `TxOptions.Validate`.

**Nothing degrades silently.** Zero options take the unchanged `Begin`;
non-default options require `TxBeginner` and are otherwise refused;
`CommitTx`/`RollbackTx` require `ContextTxConn` and never fall back to the
context-free finalizers, which would discard the deadline that made the caller
pass a context at all. Legacy callers keep using `tx.Commit()` directly.

**`ContextTxConn` is pg-native only.** `*sql.Tx` exposes no context finalizers
and its `BeginTx` context owns the transaction's lifetime and auto-rollback. A
pre-check cannot bound an in-flight COMMIT, and a goroutine wrapper would race a
false completion report — so mysql and sqlite honestly do not implement it.

**`SessionTxBeginner` exists to be assertable early.** A consumer that requires
bounded cleanup must be able to reject a connection when it is *marked*
session-capable, not at the user's first BEGIN; `ContextTxConn` is only
observable on a `TxConn` that already exists.

### 2.2a Per-driver matrix (normative)

| Driver | `TxBeginner` | Access | Isolation | Deferrable | `ContextTxConn` | `RawRows` |
|---|---|---|---|---|---|---|
| postgres | yes (+ `SessionTxBeginner`) | full (RO / explicit RW) | full | with SERIALIZABLE+RO only | **yes** | **yes** |
| mysql | yes | RO honored; **explicit RW refused** (`sql.TxOptions` carries a bool; `ReadOnly=false` is a plain `START TRANSACTION`, a request for the default rather than an override) | full incl. READ UNCOMMITTED | refused | no | no |
| sqlite (modernc) | **no** — non-default options refused by the helper | — | — | — | no | no |
| bigquery (nested module) | no (no transactions) | — | — | — | no | no |

**Error taxonomy and validation order:**

1. `*ErrTxOptionInvalid{Option, Value, Reason}` — out-of-range value or an
   invalid combination; **checked first**, and deliberately does **not** match
   `ErrUnsupported`: malformed input is not a capability miss. `DEFERRABLE` /
   `NOT DEFERRABLE` are valid **only** with `SERIALIZABLE` + `READ ONLY`, the
   one combination PostgreSQL gives them effect in — refused elsewhere rather
   than silently ignored.
2. `*ErrTxOptionUnsupported{Driver, Option}` — a valid option the driver cannot
   honor; checked second; **matches `ErrUnsupported`** via `Unwrap` (ADR-0008).
3. Both are returned **before any BEGIN reaches the wire**.

**Finalization outcome contract.** A failed commit is not one fact, and the DAL
classifies what is actually known rather than handing back a raw driver error
for the consumer to guess from. Two sentinels, matched with `errors.Is`; the
driver's own cause stays reachable with `errors.As`:

| Fault state | Result | Handle / connection |
|---|---|---|
| 1. Context already cancelled when the finalizer is called — nothing dispatched | the **raw context error**; neither sentinel matches | handle stays **open**, rollable with a fresh context |
| 2. Dispatched, and the driver proves no bytes were written | `ErrTxRolledBack` | handle closed, connection discarded |
| 3. Server-confirmed rollback (`pgx.ErrTxCommitRollback`, or a server `ErrorResponse` to the COMMIT) | `ErrTxRolledBack`, cause preserved | handle closed, connection discarded |
| 4. COMMIT written, response lost | `ErrTxOutcomeUnknown` | handle closed, connection **discarded, not pooled** |

A failed rollback returns an observable cleanup error and discards the
connection. The DAL never names a consumer's audit vocabulary: autodb maps
`ErrTxOutcomeUnknown` to its own nonterminal `unknown_pending` state and
`ErrTxRolledBack` to a definite `rolled_back`.

### 2.3 Raw result access

```go
type FieldDescription struct {
    Name         string
    TableOID     uint32
    ColumnAttr   uint16
    TypeOID      uint32
    TypeSize     int16
    TypeModifier int32
    Format       int16   // 0 text, 1 binary
}

func RawRowsOf(rows Rows) (RawRows, bool)
```

`RawValues` returns the driver's own receive buffers, **borrowed until the next
`Next` or `Close`** — copying them here would defeat the point of the
capability. `RawRowsOf` is a probe, not an error path: absence is `(nil, false)`,
because raw access is an optimization a consumer falls back from, unlike
`Columns`, where the caller asked a question that has no answer without the
capability.

## 3. Implementation notes

- **`dao`** — `txoptions.go` (the value domain, `Validate`, the two option error
  types), `txcapabilities.go` (the three transaction capabilities, the two
  outcome sentinels, the helpers), `rawrows.go`.
- **`dao/postgres`** — `txcapabilities.go` (`BeginTx`, `BeginSessionTx`, the
  pgx option mapping, `CommitContext`/`RollbackContext`, and `classifyCommit`,
  which answers exactly one question: *is it proven that the transaction did not
  commit?*), `rawrows.go`. `pgxTx` gained one `closed` field; the base
  `Commit`/`Rollback` return exactly what they returned before.
- **`dao/mysql`** — `txcapabilities.go`: `TxBeginner` plus the refusals its
  matrix row owns.
- **`dao/sqlite`** — `txcapabilities.go`: compile assertions only, and a note on
  why no capability is claimed.
- `dataconn.go`, `errors.go` and `transaction.go` are **untouched** — the
  byte-identity claim is a fact about the diff, not an inference.
- The `txOutcomeError` that joins a sentinel to its cause is **unexported**: the
  public error surface is the two sentinels plus the preserved cause, so there
  is no third thing to type-assert.

## 4. Acceptance

1. **Base interfaces byte-identical**, with in-package pointer compile
   assertions covering every opting-in capability: postgres asserts `DataConn`,
   `TxConn`, `TxBeginner`, `SessionTxBeginner`, `ContextTxConn`, `RawRows`;
   mysql the two bases plus `TxBeginner`; sqlite the two bases only. ✅
2. **The §2.2a matrix is fully tested per driver** — every
   default/read-only/read-write cell, the full isolation domain, deferrable and
   not, and the invalid combinations; refusal before BEGIN (proven on a
   connection with a nil pool/DB, which would panic if the wire were reached);
   `ErrTxOptionUnsupported` naming driver and option and matching
   `ErrUnsupported`, `ErrTxOptionInvalid` not matching it. The postgres row is
   additionally run live against a server, asserting `SHOW
   transaction_isolation` / `SHOW transaction_read_only`. ✅
3. **Engine-enforced read-only through the session path**: `BeginSessionTx(ctx,
   TxOptions{Access: TxReadOnly})` returns an asserted `ContextTxConn` in which
   a write fails with SQLSTATE **25006**. ✅ (integration)
4. **Finalization**: `CommitContext` and `RollbackContext` tested separately on
   a fresh context after the begin context is cancelled — the ctx-capture
   defect's regression suite — plus all four fault states, each asserting the
   required sentinel and the handle/connection state. States 1–4 are reached
   through the live driver; the classification is additionally unit-tested with
   the driver's own error values. ✅ (integration + unit)
5. **`RawRows`**: `RawRowsOf` succeeds on postgres (pool and transaction paths)
   and fails on mysql/sqlite; NULL vs empty stay distinguishable; text and
   binary format codes are both produced from real query paths (extended and
   simple protocol) rather than one asserted and the other assumed; metadata
   fidelity against the server's OIDs, sizes, typmods and column ordinals; the
   borrowed-buffer contract tested through consumer copies and a buffer-reusing
   fake **with a positive control** proving the fake actually reuses. ✅
6. **Known-failure waiver:** the nested `dao/bigquery` module and the
   local-replace `example/` have pre-existing, unrelated build failures
   (grpc/http2 drift; stale go.mod). This change does not repair them and does
   not depend on them; acceptance rests on criterion 1's compile assertions and
   on the unchanged `dataconn.go` in review. ✅

Run the live half with:

```sh
TEST_PGURL='postgres://user:pass@localhost:5432/db?sslmode=disable' \
  go test -tags integration ./dao/...
```

## 5. Consequences

**Easier:** a consumer can ask for the transaction it actually needs and get it
or a refusal, never a quiet downgrade; cleanup survives the cancellation of the
context that made cleanup necessary; a pass-through consumer stops decoding
values it has no reason to name. Downstream, autodb's session engine is
unblocked.

**Harder:** capability probing costs one type assertion per call path (mitigated
by the typed helpers), and the pg-only `ContextTxConn` means session-capable
connections are structurally PostgreSQL — already the product ruling.

**Unchanged:** `DataConn`, `TxConn`, `Rows`, ADR-0015 ownership, `RunTx`
semantics, two-phase commit, and every existing driver's behavior when it is
handed zero options.

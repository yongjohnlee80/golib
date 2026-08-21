# ADR-0015 — `golib/dao`: transaction connection ownership

- **Status:** **Accepted** (2026-08-21 — accepted by Johno as written, including
  the options tail over his `...DataConn` sketch (§4 alternative 1) and the §2.4
  cross-connection guard. Authored by jarvis from Johno's report that `RunTx`'s
  `[]DataConn` parameter is an anti-pattern; lector design r1
  `change_requested` folded, r2 **approved** at `6282278` with no findings
  outstanding. See Review history. Implemented on `dao-tx`.)
- **Date:** 2026-08-21
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** ADR-0005 §2 (the `RunTx`/`Begin` signatures and the
  `Transaction.conns` registry), §2.1 (`executorFor`), §2.3 (`TwoPhase()` as a
  post-`Begin` method), and **refines** ADR-0005 §7 criterion 6 (unsupported
  2PC now fails at two points, not one — §2.5). ADR-0005's fixes F1–F4 —
  error-returning commit, deterministic touch order, `*CommitError`, no
  `.Use(tx)` per call — and its acceptance criteria 1–5 stand unchanged.
- **Related:** ADR-0006 (`Schema` owns the `DataConn`), ADR-0008 §2.3
  (no-transaction drivers gate on first touch), the
  `interface-evolution-capability-interfaces` KB convention (why 2PC becomes an
  optional participant capability), [[golib]] Part 2 "API design" (functional
  options are the house idiom; positional config args are a legacy shape)

## 1. Context

Every entity's `Schema` is built from a `DataConn` and holds it for the process
lifetime (ADR-0006). `schema.On(tx)` already resolves its executor through
`tx.executorFor(s.conn.Name())` — the connection is **already known** at
statement time. Yet the caller must name that same connection again, up front,
in a positional slice:

```go
err := dao.RunTx(ctx, []dao.DataConn{conn}, func(tx *dao.Transaction) error {
    id, err := artists.On(tx).Set(ArtistName, "X").Insert()
    ...
})
```

Three concrete costs:

1. **The call site plumbs connections it has no business knowing.** DAO-level
   code works in entities, not pools. `example/`'s `Store` — the package we
   ship as the pattern to copy — grew a `Conn() gdao.DataConn` accessor whose
   doc comment reads *"for RunTx, EnsureLocalSchema, etc."* and which **nothing
   calls**: exported surface that exists only to feed a parameter the library
   could have derived. That is the [[golib]] "no dead surface" and "no
   positional config args" rules failing at the same call.
2. **The list can be wrong, and only execution says so.** Pass the wrong pool
   (a second, differently-named connection; a mock; the Gold pool instead of
   the LM one) and every statement fails with `ErrUnknownConnection` at run
   time. The schema knew the right answer the whole time.
3. **`Transaction` is coupled to `DataConn` for the wrong reason.** It keeps a
   `conns map[string]DataConn` registry purely so `executorFor(name)` can look
   a connection back up by string — a name→conn indirection that exists only
   because the conn arrived from the caller instead of from the participant.

**Prior art (LM `pkg/transaction`, which ADR-0005 deliberately followed) got
this part right.** `transaction.Tx(fn)` takes **no connections**:
`transaction.New()` starts with an empty `Contexts` map, and each participant
supplies its own database at first touch —
`SqlxConnProvider.UseWithError(t)` calls
`GetSqlxTransactionContext(s.DB, t, s.contextName)` with the `*sqlx.DB` the
provider already holds. LM's `Transaction` never holds a connection registry;
it holds `map[string]Context` and knows only `Commit()`/`Rollback()`. golib
inherited LM's *lazy per-connection BEGIN* but replaced its
participant-supplies-the-conn model with a caller-supplied list — keeping the
ceremony without the decoupling. This ADR takes the rest of the shape.
Everything golib *fixed* about LM (F1–F4) stays fixed: LM's `.Use(tx)`
per-call-site bind, its swallowed commit errors and its map-order commit are
not coming back.

## 2. Decision

### 2.1 The participant owns its connection; `Transaction` holds none

`Transaction` keeps no `DataConn` — not in a registry, not in a field. A
database participant is a `txContext` that *carries* its connection, and the
transaction drives it through unexported interfaces:

```go
// txContext is one participant: a database's driver tx, or a non-DB Resource.
type txContext interface {
	name() string
	commit() error
	rollback() error
}

// dbTxContext is a database participant — a txContext with a live executor.
// Probed by assertion, so a Resource is never mistaken for a database.
type dbTxContext interface {
	txContext
	executor() TxConn
}

// twoPhaseContext is the OPTIONAL two-phase-commit capability of a database
// participant (KB convention: capabilities are optional interfaces probed by
// assertion, never new methods on a published seam). *sqlTxContext implements
// it by delegating to the connection it carries — which is why Transaction
// itself needs no DataConn to run 2PC.
type twoPhaseContext interface {
	twoPhaseSupported() bool
	prepare(ctx context.Context, gid string) error
	commitPrepared(ctx context.Context, gid string) error
	rollbackPrepared(ctx context.Context, gid string) error
}

// sqlTxContext wraps one database's driver transaction and the connection it
// came from.
type sqlTxContext struct {
	n    string
	conn DataConn
	tx   TxConn
}

func (c *sqlTxContext) executor() TxConn         { return c.tx }
func (c *sqlTxContext) twoPhaseSupported() bool  { return c.conn.Dialect().TwoPhaseSupported() }
func (c *sqlTxContext) prepare(ctx context.Context, gid string) error {
	return c.conn.Dialect().Prepare(ctx, c.tx, gid)
}
func (c *sqlTxContext) commitPrepared(ctx context.Context, gid string) error {
	return c.conn.Dialect().CommitPrepared(ctx, c.conn, gid) // pool: the preparing session is gone
}
func (c *sqlTxContext) rollbackPrepared(ctx context.Context, gid string) error {
	return c.conn.Dialect().RollbackPrepared(ctx, c.conn, gid)
}

type Transaction struct {
	mu       sync.Mutex
	ctx      context.Context
	span     map[string]struct{} // declared allowlist by name; nil = undeclared (§2.4)
	contexts map[string]txContext
	order    []string            // commit order = first-touch order
	closed   bool
	twoPhase bool
	initErr  error               // construction failure, surfaced before work (§2.5)
}
```

`commitTwoPhase`/`checkTwoPhase` stop reaching into `t.conns[name].Dialect()`
and drive `twoPhaseContext` instead. `Dialect` is **unchanged** — its
`TwoPhaseSupported`/`Prepare`/`CommitPrepared`/`RollbackPrepared` methods keep
their ADR-0005 contract; only the caller moves.

### 2.2 `join(conn)` replaces `executorFor(name)`

The participant hands the transaction its connection at first touch. Lazy
`BEGIN` (ADR-0005 §2.1, criterion 3) and the ADR-0008 §2.3 first-touch
capability gate are preserved exactly:

```go
// join enlists conn in the transaction and returns its transaction-scoped
// executor, issuing BEGIN on first touch and caching it. The caller — a
// tx-bound DAO — supplies the connection from its Schema, so the transaction
// keeps no connection registry and no name->conn indirection.
func (t *Transaction) join(conn DataConn) (TxConn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTransactionClosed
	}
	if t.initErr != nil {
		return nil, t.initErr // no work on a mis-constructed transaction
	}
	name := conn.Name()
	if c, ok := t.contexts[name]; ok {
		db, ok := c.(dbTxContext)
		if !ok {
			return nil, fmt.Errorf("dao: transaction context %q is not a database connection", name)
		}
		return db.executor(), nil
	}
	if err := t.admits(name); err != nil { // §2.4
		return nil, err
	}
	// First-touch capability gate: a no-transaction connection (BigQuery) is an
	// error only when actually touched (ADR-0008 §2.3).
	if !conn.Dialect().SupportsTransactions() {
		return nil, fmt.Errorf("%s: %w: transactions", name, ErrUnsupported)
	}
	tx, err := conn.Begin(t.ctx)
	if err != nil {
		return nil, err
	}
	t.contexts[name] = &sqlTxContext{n: name, conn: conn, tx: tx}
	t.order = append(t.order, name)
	return tx, nil
}
```

Call sites (`query_dao.go`): `d.tx.executorFor(d.schema.conn.Name())` becomes
`d.tx.join(d.schema.conn)` — in `handle()` and in `Batch()`.

`join` stays **unexported**. A raw-statement seam (`tx.Exec` on a connection
with no `Schema`) is now expressible without another signature change, but
nothing needs it today and [[golib]] forbids shipping surface nothing uses; an
exported `Join` returning a bare `TxConn` would also be the "raw handle" shape
the house rules push away from (`RunTx(fn)` over handles). Deferred, not
designed out.

### 2.3 Options tail replaces the positional connection slice

```go
// TxOption configures a Transaction at construction (Begin/RunTx).
type TxOption func(*txConfig)

// Spanning declares the connections this transaction MAY span. Declaring it is
// only necessary to write to MORE THAN ONE database in one transaction (§2.4);
// a single-database transaction needs no option at all.
func Spanning(conns ...DataConn) TxOption

// TwoPhase opts into true two-phase commit (ADR-0005 §2.3). Validated against
// the participants that actually join, and additionally pre-flighted at Begin
// when Spanning is declared (§2.5).
func TwoPhase() TxOption

func Begin(ctx context.Context, opts ...TxOption) *Transaction
func RunTx(ctx context.Context, fn func(tx *Transaction) error, opts ...TxOption) error
```

`fn` moves ahead of the variadic tail (Go requires it) and `ctx` stays first.
The common case loses the parameter entirely:

```go
// Single database — the schemas' connection is the transaction's connection.
err := dao.RunTx(ctx, func(tx *dao.Transaction) error {
	id, err := artists.On(tx).Set(ArtistName, "X").Insert()
	if err != nil {
		return err
	}
	_, err = albums.On(tx).Set(AlbumArtistID, id).Insert()
	return err
})

// Two databases — the span is declared, because partial commit is a real risk.
err = dao.RunTx(ctx, func(tx *dao.Transaction) error {
	if err := labels.On(tx).With(LabelID, id).Set(LabelName, n).Update(); err != nil {
		return err
	}
	return goldLabels.On(tx).With(GoldLabelID, id).Set(GoldLabelName, n).Update()
}, dao.Spanning(lmConn, goldConn), dao.TwoPhase())
```

An options tail rather than `conns ...DataConn`: a bare connection list is a
positional config argument that says nothing about what the connections *mean*
(participants? an allowlist? pre-`BEGIN`?), and it cannot express the knobs
that already exist (`TwoPhase`) or the ones LM proves callers want next
(`NewSnapshot`'s read-only `REPEATABLE READ` — golib has no equivalent, and an
`Isolation`/`ReadOnly` option is the obvious home for it). Functional options
are the house idiom for exactly this.

**`Spanning` normalization (no silent degrade, no panic).** The option collects
into an ordered, name-deduplicated slice on `txConfig`:

- **Declaration order is preserved** — it is the pre-flight reporting order
  (§2.5), so a multi-connection failure names the same connection every run.
- **Repeated `Spanning` options MERGE** rather than replace, matching the
  house "a later option overrides an earlier one *for the same setting*" rule
  read at the level of a set. A name already declared keeps its first
  connection and its first position; passing the same connection twice is not
  an error.
- **A nil connection is a construction error**, not an ignored entry: `initErr`
  is set to `dao: Spanning: nil connection at index N`. The pre-flight
  dereferences every declared connection, so silently dropping nils would move
  a caller's bug to a later, stranger failure.
- **`Spanning()` with no connection is also a construction error**
  (`dao: Spanning: no connections declared; omit the option for a
  single-database transaction`). "A span of nothing" has no meaning, and
  silently reverting to §2.4's undeclared lock-in would be exactly the silent
  degrade the house rules forbid — including for the dynamic
  `Spanning(conns...)` case, where an unexpectedly empty slice is the bug worth
  surfacing.

`initErr` never panics and never opens a driver transaction: `RunTx` returns it
before `fn`, `join` returns it before any statement, and `Commit` returns it
while **closing** the transaction and rolling back anything already registered
(§2.5), so the manual `Begin` path cannot leak a registered `Resource` or
report success on a mis-constructed transaction.

### 2.4 The cross-connection guard: keep today's protection, drop the ceremony

Today's required list quietly buys one safety property: a body that touches a
database the author did not intend fails immediately with
`ErrUnknownConnection`. Lazy join must not lose that — an undeclared second
database silently upgrades a transaction to a **non-atomic** multi-database
commit (`*CommitError.AlreadyDurable`), the exact hazard ADR-0005 §2.3 says
callers must choose consciously.

So the gate moves rather than disappears:

```go
// admits reports whether a NEW participant named name may join.
//
//   - With a declared span (dao.Spanning), membership is the gate.
//   - Undeclared, the first database to join locks the transaction to itself:
//     a second, different database would be a cross-database span the caller
//     never asked for, so it fails typed instead of committing non-atomically.
//
// Registered non-DB Resources never gate anything.
func (t *Transaction) admits(name string) error
```

Net effect versus today: the single-database case (the overwhelming majority)
gets the same protection with no parameter, and the multi-database case must
say so out loud — where today two connections in a slice, typed once and never
read again, is enough to opt into partial-commit risk by accident.

**The name is the transaction layer's identity for a logical database, and that
contract becomes load-bearing.** Admission, context keying, commit order and
`*CommitError` reporting are all name-based, so `DataConn.Name`'s doc comment
gains the invariant the layer has always relied on:

```go
// Name identifies the connection for transaction-context keying and logs,
// e.g. "postgres" or "postgres-gold". It is the transaction layer's identity
// for a logical database and MUST be:
//
//   - stable for the connection's lifetime;
//   - unique across DIFFERENT logical databases in one process;
//   - shared ONLY by handles to the SAME logical database (two pools against
//     one database may share a name; two databases must never).
//
// A transaction admits participants and orders its commit by this name.
// Capability decisions are NOT made from it: two-phase commit is validated
// against the participant that actually joined (ADR-0015 §2.5).
Name() string
```

### 2.5 Two-phase commit: a construction option, validated against the participants that actually joined

`(*Transaction).TwoPhase()` is **removed**. It is currently callable at any
time — including after statements have run on a dialect that cannot prepare —
flipping the commit protocol mid-flight and failing only at `Commit`, after all
the work. `TwoPhase()` as a `TxOption` fixes the choice before any participant
joins.

That gives 2PC capability validation **two** distinct jobs, and only the second
is authoritative:

**(a) `Begin` pre-flight — an early diagnostic, over declared connections.**
When `TwoPhase()` and `Spanning(...)` are both given, `Begin` walks the
declared connections in declaration order and stores the first incapable one in
`initErr`. `RunTx` returns it **before calling `fn`**, so an impossible
transaction performs no side effects at all. `Begin` still returns no error and
still issues no driver `BEGIN`.

```go
if cfg.twoPhase {
	for _, c := range cfg.span { // ordered slice: deterministic reporting
		if !c.Dialect().TwoPhaseSupported() {
			t.initErr = fmt.Errorf("%w: connection %q", ErrTwoPhaseUnsupported, c.Name())
			break
		}
	}
}
```

**(b) `Commit` validation — authoritative, over actual participants, always.**
`checkTwoPhase` runs on every 2PC commit, declared span or not, before any
`prepare`:

```go
// checkTwoPhase verifies every joined database participant can prepare. It runs
// on EVERY two-phase Commit: the Begin pre-flight inspects the connections the
// caller DECLARED, while join enlists the connection each Schema SUPPLIED, and
// admission is by name (§2.4) — so a capable declaration and an incapable
// participant can share a name. The pre-flight is a diagnostic; this is the
// authority. Called with t.mu held.
func (t *Transaction) checkTwoPhase() error {
	for _, name := range t.order {
		c := t.contexts[name]
		if _, isDB := c.(dbTxContext); !isDB {
			continue // resources cannot prepare; they commit last (ADR-0005 §2.3)
		}
		tp, ok := c.(twoPhaseContext)
		if !ok || !tp.twoPhaseSupported() {
			return fmt.Errorf("%w: connection %q", ErrTwoPhaseUnsupported, name)
		}
	}
	return nil
}
```

A missing `twoPhaseContext` capability is treated exactly like a `false`
capability: `ErrTwoPhaseUnsupported`, nothing prepared, and `Commit`'s existing
`rollbackUncommitted(nil)` rolls back every touched context (ADR-0005 §2.3
behavior, preserved verbatim).

Consequently ADR-0005 §7 criterion 6 is **refined, not preserved**: an
unsupported 2PC attempt fails fast from `RunTx` before `fn` when the span is
declared, and at `Commit` with nothing durably written otherwise — and the
`Commit` check is what actually guarantees the property in both cases.

### 2.6 Errors

No new sentinel. `ErrUnknownConnection` keeps its identity and its message
("dao: connection not in transaction") and gains the wider meaning its name
already implies — *this connection may not participate* — wrapped with the
connection name and, in the undeclared case, with the name of the connection
the transaction is locked to plus the remedy (`declare dao.Spanning`). Callers
branching on `errors.Is(err, dao.ErrUnknownConnection)` are unaffected.
`ErrTwoPhaseUnsupported` keeps its meaning and gains the earlier pre-flight
site (§2.5). Option-construction bugs (`Spanning(nil)`, empty span) surface as
plain wrapped errors through `initErr`, not sentinels: no caller can branch
usefully on a programming error, and [[golib]] forbids sentinels nothing
consumes.

## 3. What does not change

- `schema.On(tx)` / `OnCtx(ctx)` / `WithTx`, and the F3 fix: binding still
  happens once, every statement still runs on the transaction.
- Lazy per-connection `BEGIN`, first-touch order, `Commit`'s ordered
  error-returning walk, `*CommitError` (`Failed`/`AlreadyDurable`/
  `PreparedPending`), `Rollback` in reverse touch order, panic-and-re-panic.
- `Register` + `Resource`/`ResourceFunc` compensating participants.
- `Dialect` (including all four 2PC methods), `DataConn` and `TxConn`
  **signatures** — `DataConn.Name`'s doc comment gains the invariant the
  transaction layer already relied on (§2.4); no method changes.
- Single-goroutine-per-`Transaction`; background work uses `schema.DAO()`.

## 4. Alternatives considered

1. **`RunTx(ctx, fn, conns ...DataConn)`** (Johno's sketch). Smallest diff,
   mirrors today's `Begin(ctx, conns...)`. Rejected: positional config args are
   a legacy shape in this codebase, the list's *meaning* stays implicit, and it
   cannot carry `TwoPhase` — leaving 2PC reachable only through the mid-flight
   mutation §2.5 removes.
2. **Unrestricted lazy join, no guard.** Simplest. Rejected: silently converts
   a single-database transaction into a non-atomic cross-database one on an
   accidental `otherSchema.On(tx)` — a strict regression against today.
3. **Connection *identity* checking** (same `Name()`, different `DataConn`
   value ⇒ error). Tempting: two pools sharing a name currently collapse into
   one participant silently. Rejected: comparing two `DataConn` interface
   values panics when the dynamic type is non-comparable, and the only
   alternative is `reflect` in a core path — both forbidden ([[golib]]
   principle 4, "no `panic` on API misuse reachable at runtime"). A
   comparable-only check would also make behavior depend on the driver's
   concrete type, which is worse than one clear contract. Instead the name
   invariant is stated explicitly on `DataConn.Name` (§2.4) and the one
   decision that must not trust it — 2PC capability — is validated against the
   actual participant (§2.5).
4. **Keep `RunTx(ctx, conns, fn)` and add a conn-free sibling.** Rejected: two
   entry points for one job, and the anti-pattern stays the documented default.
5. **Exported `tx.Join(conn) (TxConn, error)`** for non-`Schema` participants.
   Deferred (§2.2): no caller today, and a raw handle is the shape the house
   rules steer away from.
6. **Keeping `(*Transaction).TwoPhase()` as a deprecated alias** for one
   release. Rejected: the alias preserves precisely the mid-flight mutation
   hazard §2.5 removes, and `Begin`/`RunTx` are already changing signature in
   the same commit — there is no smaller migration to protect.

## 5. Migration

Mechanical, but larger than the `RunTx` count alone. Verified inventory in
golib (`dao-tx` at base `main` 82e74ed):

| Surface | Count | Where |
| --- | --- | --- |
| `RunTx(...)` call sites | 28 | 15 core (`transaction_test.go` 12, `capabilities_test.go` 3) + 13 driver (`postgres` 5, `twophase_integration` 3, `sqlite` 2, `mysql` 2, `bigquery` 1) |
| package-level `Begin(...)` | 5 (+1 in `RunTx`) | `transaction_test.go:149,281,287`; `hooks_test.go:362,378` |
| `tx.TwoPhase()` | 8 | `transaction_test.go` (5), `postgres/twophase_integration_test.go` (3) |
| `executorFor` | 2 production + 2 white-box | `query_dao.go:60,648`; `transaction_test.go:282,289` |
| prose & snippets | — | `dao/README.md` §transactions, `dao/USAGE.md` (4 snippets), `dao/dataconn.go` (`DataConn.Name` doc) |

| Before | After |
| --- | --- |
| `RunTx(ctx, []DataConn{c}, fn)` | `RunTx(ctx, fn)` |
| `RunTx(ctx, []DataConn{a, b}, fn)` | `RunTx(ctx, fn, Spanning(a, b))` |
| `Begin(ctx, c)` / `Begin(ctx, a, b)` | `Begin(ctx)` / `Begin(ctx, Spanning(a, b))` |
| `tx := Begin(ctx, a, b); tx.TwoPhase()` | `Begin(ctx, Spanning(a, b), TwoPhase())` |
| `tx.executorFor(name)` (white-box tests) | `tx.join(conn)` |
| `Store.Conn()` exposed "for RunTx" | delete (unused) |

**`example/` is a separate repository, and its migration is a coordinated
companion commit** — not part of the golib commit.
`/home/johno/Source/Projects/personal/example/main` is its own git top-level
(`module github.com/yongjohnlee80/example`) and reaches golib through
`replace github.com/yongjohnlee80/golib => ../../golib/server`. Two
consequences:

1. That replace target **does not exist** — `golib/server` is a deleted
   worktree, so `example` does not currently build
   (`replacement directory ../../golib/server does not exist`). Pre-existing
   and unrelated to this ADR, but it must be re-pointed (`../../golib/dao-tx`
   while this lands, `../../golib/main` after merge) before the companion
   commit can be compiled or tested at all.
2. The companion commit carries `store.go:175`'s `RunTx` migration and the
   deletion of the unused `Store.Conn()`, and lands with or after the golib
   change.

After acceptance, the KB follows the code: the `golib-dao` convention's
"Transactions (ADR-0005)" section, the ADR-0005 KB page's superseded
paragraphs, and this ADR imported as
`shared/adrs/golib-dao-0015-transaction-connection-ownership.md`.

## 6. Files / acceptance

`dao/transaction.go` (participant interfaces, `join`, `admits`, `txConfig` +
`TxOption`/`Spanning`/`TwoPhase`, `Begin`/`RunTx` signatures, `initErr`, 2PC
through `twoPhaseContext`), `dao/dataconn.go` (`DataConn.Name` invariant),
`dao/query_dao.go` (`handle`, `Batch`), `dao/errors.go`
(`ErrUnknownConnection` doc), `dao/transaction_test.go`,
`dao/capabilities_test.go`, `dao/hooks_test.go`, driver test suites,
`dao/README.md`, `dao/USAGE.md`; separately, `example/main` (`go.mod` replace,
`internal/dao/store.go`).

**`dao/bigquery` migrates on a release bump, not with this change.** The nested
module pins a *released* golib (`require github.com/yongjohnlee80/golib v0.1.0`,
no `replace`), so its one `RunTx` site compiles against the pre-ADR-0015
signature and must keep it until the same commit bumps that require to a release
carrying this ADR — migrating it earlier stops that module building. This is the
cross-module lag the `interface-evolution-capability-interfaces` convention
warns about (rule 6), showing up on a signature change instead of an interface
one. The call site carries a NOTE naming the sequencing.

**ADR-0005 §7 criteria 1–5 must still hold verbatim. Criterion 6 is replaced by
criteria 4–6 below.** New criteria:

1. `RunTx(ctx, fn)` with no option commits work done through
   `schema.On(tx)` on the schema's own connection; `Begin` issues no driver
   `BEGIN`, and the first statement issues exactly one on that connection.
2. A second, differently-named connection touched inside an undeclared
   transaction returns `ErrUnknownConnection` (wrapped with both names) on its
   first statement, before any `BEGIN` on it (`txConn.beginCount == 0`); the
   first connection's work rolls back with the transaction.
3. `Spanning(a, b)` admits both and only those two; a third connection returns
   `ErrUnknownConnection`. Commit order is still first-touch order, not
   declaration order.
4. `Spanning(a, b) + TwoPhase()` where `b`'s dialect cannot prepare returns
   `ErrTwoPhaseUnsupported` from `RunTx` **without invoking fn** (a closure
   sentinel proves it) and with no driver `BEGIN` anywhere.
5. **Capability is decided by the participant, not the declaration.** With
   `Spanning(cap)` + `TwoPhase()` where `cap` is 2PC-capable, and a `Schema`
   whose own connection shares `cap.Name()` but is **not** capable, `Commit`
   returns `ErrTwoPhaseUnsupported`, no `Prepare`/`CommitPrepared` runs
   (assert on the dialect's call recorder), and every touched context rolls
   back. The same holds when the joined participant implements no
   `twoPhaseContext` at all.
6. `TwoPhase()` without `Spanning` fails at `Commit` over the touched
   contexts, with nothing prepared and nothing durably committed.
7. A no-transaction dialect (BigQuery profile) still returns `ErrUnsupported`
   on first touch only, and an untouched declared connection in a `Spanning`
   set issues no `BEGIN`.
8. A `Resource` registered under a name that a database connection later joins
   still fails with the "not a database connection" error; a `Resource`
   registered alongside a single database does not trip `admits`.
9. `Spanning(nil)` / `Spanning()` set `initErr`: `RunTx` returns it without
   invoking `fn`; on the manual path `join` returns it before any statement and
   `Commit` returns it, closes the transaction, and rolls back an already
   registered `Resource`. Neither panics.
10. `Transaction` holds no `DataConn` field: the 2PC integration suite
    (`dao/postgres/twophase_integration_test.go`) passes unchanged in behavior
    against a real Postgres, driven through `twoPhaseContext`.

## 7. Resolved review questions

Lector's design r1 answered the open questions; they are recorded here as
decisions, not questions:

1. **First-join lock-in stays** — it preserves the accidental-cross-DB guard
   without reinstating single-database ceremony.
2. **The name contract is strengthened instead of identity-checked** (§2.4),
   with actual-participant 2PC validation as the backstop (§2.5).
3. **`initErr` is returned from `RunTx` (before `fn`), `join`, and `Commit`** —
   the third site is what stops a no-touch manual transaction reporting
   success.
4. **`(*Transaction).TwoPhase()` is removed outright**, no deprecated alias.
5. **Exported raw join/exec and an LM-style `MustTx` stay out of scope** —
   neither has a caller or an agreed contract today.

## Review history

- **r1 (2026-08-21, lector — `change_requested`, folded in this revision).**
  Must-fix 1: ADR-0005 criterion 6 could not both "survive unchanged" and be
  superseded by the declared-span pre-flight — supersession and acceptance now
  say criteria 1–5 survive and 6 is refined (§2.5, §6). Must-fix 2: the
  `Spanning` pre-flight inspects *declared* connections while `join` enlists
  *schema-supplied* ones under name-based admission, so `Commit` must always
  validate the actual participants — `checkTwoPhase` is now specified as
  unconditional and authoritative, with new acceptance criterion 5. Should-fix
  1: the `DataConn.Name` invariant is stated on the method and
  `dao/dataconn.go` added to the file set. Should-fix 2: migration inventory
  corrected (28 is `RunTx` only; +5 `Begin`, +8 `TwoPhase`, +4 `executorFor`,
  prose) and `example/` restated as a separate repository needing a coordinated
  companion commit — which also surfaced that its `replace` target is a deleted
  worktree, so it does not currently build. Should-fix 3: `Spanning`
  normalization (nil / empty / duplicate / repeated) and `initErr` closure on
  the manual `Begin` path specified in §2.3.
  Review doc: `$KB_ROOT/agents/lector/reviews/2026-08-21-golib-dao-0015-transaction-connection-ownership-review.md`
- **r2 (2026-08-21, lector — `approved`).** Reviewed the complete
  `1be8ac6..6282278` amendment diff: all two must-fix and three should-fix
  findings resolved without changing the approved architecture — the
  criterion-6 refinement is internally consistent, `checkTwoPhase` is
  unconditionally authoritative over actual joined participants before any
  prepare and fails closed on missing capability, the `DataConn.Name` contract
  and file scope are explicit, migration accounting is complete with
  `example/main` correctly treated as a separate companion commit, and
  `Spanning` normalization plus `initErr` manual-path cleanup are specified and
  testable. Both retained choices accepted: nil/empty `Spanning` failures need
  no sentinel (programming errors with no recovery branch), and duplicate names
  may merge first-wins under the strengthened one-logical-database name
  contract, because the actual-participant `Commit` check preserves 2PC safety
  even when wrapper capabilities differ. Lector independently reproduced the
  `example/` build failure recorded in §5. No implementation exists at this
  revision, so the r1 `go test ./dao/...` baseline remains the applicable
  result. **Ready for Johno's acceptance, then implementation on `dao-tx`.**

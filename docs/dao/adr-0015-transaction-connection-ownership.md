# ADR-0015 — `golib/dao`: transaction connection ownership

- **Status:** **Proposed** (2026-08-21 — authored by jarvis from Johno's
  report that `RunTx`'s `[]DataConn` parameter is an anti-pattern. Awaiting
  lector design review; lands on `dao-tx`.)
- **Date:** 2026-08-21
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** ADR-0005 §2 (the `RunTx`/`Begin` signatures and the
  `Transaction.conns` registry), §2.1 (`executorFor`), §2.3 (`TwoPhase()` as a
  post-`Begin` method). ADR-0005's fixes F1–F4 — error-returning commit,
  deterministic touch order, `*CommitError`, no `.Use(tx)` per call — all stand
  unchanged.
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
	initErr  error               // pre-flight failure, surfaced before fn runs (§2.5)
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
		return nil, t.initErr
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
// a single-database transaction needs no option at all. Order is preserved for
// deterministic pre-flight reporting.
func Spanning(conns ...DataConn) TxOption

// TwoPhase opts into true two-phase commit (ADR-0005 §2.3). Combined with
// Spanning it is pre-flight checked at Begin, before fn runs.
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

### 2.5 Two-phase commit is a construction option, not a post-hoc mutation

`(*Transaction).TwoPhase()` is **removed**. It is currently callable at any
time — including after statements have run on a dialect that cannot prepare —
flipping the commit protocol mid-flight and failing only at `Commit`, after all
the work. As a `TxOption` the choice is made before any participant joins, and
when the span is declared the capability check runs at `Begin`:

```go
// Begin: after applying options, when TwoPhase is set and Spanning declared,
// verify every declared dialect can prepare, in declaration order, and store
// the failure in initErr. Begin still returns no error (ADR-0005) and still
// issues no driver BEGIN.
if cfg.twoPhase {
	for _, c := range cfg.span { // slice: deterministic reporting
		if !c.Dialect().TwoPhaseSupported() {
			t.initErr = fmt.Errorf("%w: connection %q", ErrTwoPhaseUnsupported, c.Name())
			break
		}
	}
}
```

`RunTx` returns `initErr` **before calling fn**, so an impossible transaction
performs no side effects at all; `join` and `Commit` return it too, for the
manual `Begin` path. Undeclared spans keep failing at `Commit` over the touched
contexts exactly as today (`checkTwoPhase`, now via `twoPhaseContext`).

### 2.6 Errors

No new sentinel. `ErrUnknownConnection` keeps its identity and its message
("dao: connection not in transaction") and gains the wider meaning its name
already implies — *this connection may not participate* — wrapped with the
connection name and, in the undeclared case, with the name of the connection
the transaction is locked to plus the remedy (`declare dao.Spanning`). Callers
branching on `errors.Is(err, dao.ErrUnknownConnection)` are unaffected.

## 3. What does not change

- `schema.On(tx)` / `OnCtx(ctx)` / `WithTx`, and the F3 fix: binding still
  happens once, every statement still runs on the transaction.
- Lazy per-connection `BEGIN`, first-touch order, `Commit`'s ordered
  error-returning walk, `*CommitError` (`Failed`/`AlreadyDurable`/
  `PreparedPending`), `Rollback` in reverse touch order, panic-and-re-panic.
- `Register` + `Resource`/`ResourceFunc` compensating participants.
- `Dialect` (including all four 2PC methods), `DataConn`, `TxConn`.
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
   one participant silently. Rejected here because comparing two `DataConn`
   interface values panics when the dynamic type is non-comparable, and the
   only alternative is `reflect` in a core path — both forbidden ([[golib]]
   principle 4, "no `panic` on API misuse reachable at runtime"). The invariant
   stays what it already is: **one logical database ⇔ one stable `Name()`**,
   documented on `DataConn.Name`. Unchanged from today, and flagged in §7.
4. **Keep `RunTx(ctx, conns, fn)` and add a conn-free sibling.** Rejected: two
   entry points for one job, and the anti-pattern stays the documented default.
5. **Exported `tx.Join(conn) (TxConn, error)`** for non-`Schema` participants.
   Deferred (§2.2): no caller today, and a raw handle is the shape the house
   rules steer away from.

## 5. Migration

Mechanical, and every call site is in-repo (28 in golib: 15 in `dao/` tests,
13 in the `dao/{postgres,sqlite,mysql,bigquery}` suites, plus 1 in `example/`; no other
consumer of `golib/dao` uses transactions):

| Before | After |
| --- | --- |
| `RunTx(ctx, []DataConn{c}, fn)` | `RunTx(ctx, fn)` |
| `RunTx(ctx, []DataConn{a, b}, fn)` | `RunTx(ctx, fn, Spanning(a, b))` |
| `Begin(ctx, c)` / `Begin(ctx, a, b)` | `Begin(ctx)` / `Begin(ctx, Spanning(a, b))` |
| `tx := Begin(ctx, a, b); tx.TwoPhase()` | `Begin(ctx, Spanning(a, b), TwoPhase())` |
| `tx.executorFor(name)` (white-box tests) | `tx.join(conn)` |
| `Store.Conn()` exposed "for RunTx" | delete (unused) |

Docs to follow the code: `dao/README.md` §transactions, `dao/USAGE.md`
(4 snippets), and — after acceptance — the KB `golib-dao` convention's
"Transactions (ADR-0005)" section and the ADR-0005 KB page's superseded
paragraphs.

## 6. Files / acceptance

`dao/transaction.go` (participant interfaces, `join`, `admits`, `txConfig` +
`TxOption`/`Spanning`/`TwoPhase`, `Begin`/`RunTx` signatures, 2PC through
`twoPhaseContext`), `dao/query_dao.go` (`handle`, `Batch`), `dao/errors.go`
(`ErrUnknownConnection` doc), `dao/transaction_test.go`,
`dao/capabilities_test.go`, `dao/hooks_test.go`, driver test suites,
`example/main/internal/dao/store.go`.

Acceptance criteria — ADR-0005 §7 criteria 1–6 must all still hold, plus:

1. `RunTx(ctx, fn)` with no option commits work done through
   `schema.On(tx)` on the schema's own connection; `Begin` issues no driver
   `BEGIN`, and the first statement issues exactly one on that connection.
2. A second, differently-named connection touched inside an undeclared
   transaction returns `ErrUnknownConnection` (wrapped with both names) on its
   first statement, before any `BEGIN` on it; the first connection's work is
   untouched and rolls back with the transaction.
3. `Spanning(a, b)` admits both and only those two; a third connection returns
   `ErrUnknownConnection`. Commit order is still first-touch order, not
   declaration order.
4. `Spanning(a, b) + TwoPhase()` where `b`'s dialect cannot prepare returns
   `ErrTwoPhaseUnsupported` from `RunTx` **without invoking fn** (assert with a
   sentinel the closure would set).
5. `TwoPhase()` without `Spanning` still fails at `Commit` over the touched
   contexts, with nothing durably committed.
6. A no-transaction dialect (BigQuery profile) still returns `ErrUnsupported`
   on first touch only, and an untouched declared connection in a `Spanning`
   set issues no `BEGIN`.
7. A `Resource` registered under a name that a database connection later joins
   still fails with the "not a database connection" error; a `Resource`
   registered alongside a single database does not trip `admits`.
8. `Transaction` holds no `DataConn` field: 2PC integration tests
   (`dao/postgres/twophase_integration_test.go`) pass unchanged in behavior
   against a real Postgres, driven through `twoPhaseContext`.

## 7. Open questions for review

1. **`admits` default.** First-join lock-in versus requiring `Spanning` even
   for one connection (explicit, but reinstates the ceremony) versus no guard
   at all (alternative 2). Confidence in lock-in: 85%.
2. **Name collision (alternative 3).** Leave "one database ⇔ one `Name()`" as
   a documented invariant, or spend a `Comparable`-guarded identity check to
   make two same-named pools loud? Today it is silent either way.
3. **`initErr` reach.** `RunTx` fail-before-`fn` is clearly right; is
   returning it from `join` *and* `Commit` (rather than `Commit` alone) worth
   the third touch point on the manual `Begin` path?
4. **Removing `(*Transaction).TwoPhase()`** versus keeping it as a deprecated
   alias for one release. Nothing outside golib's own tests calls it.
5. **Scope check:** `tx.Exec`-style raw statements and an LM-style
   `MustTx(tx, fn)` (reuse an ambient transaction or start one) are both left
   out. Right call for this ADR?

## Review history

- **r1 (pending)** — lector design review requested 2026-08-21.

# ADR-0009 — `golib/dao`: Query-Time Options & the Hook/Middleware Seam

- **Status:** **Accepted** (2026-07-04; revision 3 — lector r2 approved_with_amendments, nits applied, see §7)
- **Date:** 2026-07-04
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive to ADR-0002/0003/0006)
- **Related:** ADR-0002 (interfaces), ADR-0003 (DAL impl), ADR-0006
  (factory/builder), ADR-0005 (transactions), ADR-0010 (partial-update rules —
  attaches to this seam), golib conventions (ctx-first)

> **Self-containment contract.** Like ADR-0001…0008, this document is written
> so an implementer with no prior context can build the feature: it restates
> the context, gives concrete Go signatures and representative bodies, names
> the files to create/modify, and lists acceptance criteria.

---

## 1. Context

### 1.1 The gap

Every configuration point in `golib/dao` today is **schema-build-time**: the
14 functional options consumed by `dao.New` (`dao/options.go`) configure the
immutable `Schema`, and nothing can be attached to an individual query. The
only runtime behavior toggle is the debug SQL log baked into the engine
(`queryDAO.log`, `dao/query_dao.go`).

The 2026-07-04 audit rated query-time extensibility the package's weakest axis
and the single largest "too simplified vs LM" risk. The prior art this package
rewrote (Label Manager's DAL) leaned heavily on a query-time seam — the
`dao.Option` hook family (`internal/dao/option.go:40-102`) with per-verb
`Preprocess{Select,Get,Update,Insert,Delete}Query` methods — to implement:

1. **Access scoping**: `DaoAccessOption` injected permission `WHERE` clauses
   into SELECTs. (Its known flaw: it covered *only* the SELECT phase — writes
   were unscoped. We fix that asymmetry here: hooks see every verb.)
2. **Logging**: `LoggerOption` logged SQL per call.

golib/dao has no home for these cross-cutting concerns. A consumer wanting a
tenant filter, a soft-delete filter, metrics, or tracing must today edit every
call site (`.WithPredicate(...)` everywhere) or fork the engine.

Two further items land naturally on the same seam:

- **Per-call `context.Context`.** The audit flagged that a pool-bound DAO runs
  statements on `context.Background()` (`queryDAO.ctx()`); the conventions
  contract requires ctx-first APIs. The last open row of the conventions
  divergences table is resolved by this ADR.
- **ADR-0010 (partial-update rules)** needs a write-phase attachment point so
  a PATCH's field rules can shape the staged SET without per-entity code.

### 1.2 Where statements already funnel

Every verb follows one shape (see `Get`, `dao/query_dao.go`): resolve columns
→ build SQL (`builder`) → debug-log → resolve executor (`handle()`) →
`QueryContext`/`ExecContext` → translate/scan. The engine therefore needs only
**one** interception pipeline, not per-verb variants — a deliberate departure
from LM's five `Preprocess*Query` methods, which this design replaces with a
single hook interface plus an `Op` discriminator.

### 1.3 Goals

- **G1 — One seam, every verb.** Reads, writes, and batch all pass through the
  same hook pipeline; access scoping cannot be verb-asymmetric by construction.
- **G2 — Two registration scopes.** Schema-wide (declared once, applies to
  every DAO the schema produces) and per-call (attach/skip on one query).
- **G3 — Full phase coverage.** Mutate staged intent before SQL exists;
  observe/rewrite SQL+args before execution; observe outcome (duration,
  rows, error) after; transform errors.
- **G4 — ctx-first.** A per-call context option with defined precedence,
  propagated to the driver and to every hook.
- **G5 — Zero cost when unused.** No hooks registered → no allocations, no
  behavior change; the existing API keeps compiling unchanged.
- **G6 — House style.** Small interfaces, functional options, embed-a-Nop
  extension pattern (mirroring `GenericDialect`).

### 1.4 Non-goals

- Not a rewrite of the fluent staging surface (ADR-0002 stands).
- Not a general plugin system: hooks observe and augment statements; they do
  not add verbs, change scanning, or reach into drivers.
- Row-level post-processing (mutating scanned models) is out of scope.

---

## 2. Decision

### 2.1 The vocabulary (`dao/hooks.go`, new)

```go
package dao

// Op identifies the statement kind a hook observes.
type Op string

const (
	OpGet     Op = "get"
	OpSelect  Op = "select"
	OpIterate Op = "iterate"
	OpExists  Op = "exists"
	OpCount   Op = "count"
	OpInsert  Op = "insert"
	OpUpdate  Op = "update"
	OpUpsert  Op = "upsert"
	OpDelete  Op = "delete"

	// OpBatch is one chunked multi-row INSERT statement emitted by a batch
	// flush: a real SQL statement, full BeforeExec rewrite contract applies.
	OpBatch Op = "batch"

	// OpBatchCopy is a bulk-load fast path (dialect COPY): there is no SQL
	// statement to rewrite — the event is OBSERVE-ONLY (§2.6). QueryInfo.SQL
	// carries a synthetic descriptor ("COPY <table> (<cols>) — <n> rows").
	OpBatchCopy Op = "batch-copy"
)

// IsWrite reports whether the op mutates data
// (insert/update/upsert/delete/batch/batch-copy).
func (o Op) IsWrite() bool

// QueryInfo describes one statement to the hook pipeline. BeforeBuild sees it
// without SQL (not rendered yet); BeforeExec and AfterExec see SQL and Args.
// A BeforeExec hook may REPLACE SQL and Args (consistently — replacing one
// without the other is a bug the engine cannot detect); all other fields are
// informational.
type QueryInfo struct {
	Op    Op
	Table string // the schema's table
	Conn  string // DataConn name, e.g. "postgres-gold"
	SQL   string // set for BeforeExec / AfterExec
	Args  []any  // ditto
}

// Outcome reports a statement's result to AfterExec.
type Outcome struct {
	Duration time.Duration
	Rows     int   // rows scanned; -1 when unknown (Iterate streams, see §2.6)
	Affected int64 // driver-reported affected rows; -1 when not applicable
	Err      error // the statement's error after dialect/ErrorMap translation
}

// Stager is the type-erased staging surface a BeforeBuild hook mutates. It is
// implemented by the engine over the current queryDAO's per-call state; the
// generic With/Set surface (typed by field enum) remains the caller-facing
// API — Stager exists so schema-agnostic hooks can be written once and shared
// across entities.
type Stager interface {
	// Where ANDs a predicate into the statement on the where-capable ops:
	// Get/Select/Iterate/Exists/Count/Update/Delete. INSERT and UPSERT have no
	// WHERE clause — calling Where there FAILS THE STATEMENT with
	// ErrHookWhereUnsupported rather than silently not scoping a write (a
	// scoping hook must branch on q.Op and use SetColumn for insert-like ops;
	// see §2.7). Where never silently no-ops anywhere.
	Where(p Predicate)
	// OrderBy appends ordering (reads only; no-op for writes).
	OrderBy(sorts ...Sort)
	// Limit caps row count when none is set yet (reads only; no-op for writes).
	Limit(n uint64)
	// SetColumn stages a write value by SQL column name, quoted via the
	// dialect (write ops incl. INSERT/UPSERT; no-op for reads). The column is
	// a developer-declared identifier — request input may enter ONLY as the
	// bound value, never as the column or any SQL text.
	SetColumn(column string, value any)
}

// ErrHookWhereUnsupported reports a BeforeBuild hook calling Stager.Where on
// an op with no WHERE clause (insert/upsert). Loud by design: the alternative
// is an unscoped write that looks scoped.
var ErrHookWhereUnsupported = errors.New(
	"dao: hook Where is not supported on insert/upsert — branch on Op and use SetColumn")

// Hook observes and augments statement execution. Implementations embed
// NopHook and override only the phases they need (the GenericDialect pattern).
type Hook interface {
	// BeforeBuild runs before SQL is rendered. Mutations through s become part
	// of the statement. Returning an error aborts the statement.
	BeforeBuild(ctx context.Context, q *QueryInfo, s Stager) error
	// BeforeExec runs after SQL is rendered and before execution. It may
	// replace q.SQL / q.Args. Returning an error aborts the statement.
	BeforeExec(ctx context.Context, q *QueryInfo) error
	// AfterExec runs after execution (success or failure). The returned error
	// REPLACES out.Err as the statement's result — return out.Err unchanged to
	// pass through, wrap it to enrich, or return nil to suppress (rare;
	// document loudly when you do).
	AfterExec(ctx context.Context, q *QueryInfo, out Outcome) error
}

// NopHook implements Hook with no-ops; embed it and override selectively.
type NopHook struct{}

func (NopHook) BeforeBuild(context.Context, *QueryInfo, Stager) error { return nil }
func (NopHook) BeforeExec(context.Context, *QueryInfo) error          { return nil }
func (NopHook) AfterExec(_ context.Context, _ *QueryInfo, out Outcome) error {
	return out.Err
}

// NamedHook optionally gives a hook an identity so a call site can skip it
// (see SkipHooks). Anonymous hooks cannot be skipped.
type NamedHook interface {
	Hook
	HookName() string
}
```

### 2.2 Registration — schema-wide and per-call

**Schema-wide** (one more build-time option, `dao/options.go`):

```go
// Hooks registers hooks on every DAO the schema produces, in order.
func Hooks[R any, C ~string, K ~string, ID any](hs ...Hook) Option[R, C, K, ID]
```

**Per-call** (`dao/schema.go` — the acquisition methods grow a variadic tail;
existing call sites compile unchanged):

```go
type QueryOption func(*queryConfig)

func (s *Schema[R, C, K, ID]) DAO(opts ...QueryOption) DAO[R, C, ID]
func (s *Schema[R, C, K, ID]) On(tx *Transaction, opts ...QueryOption) DAO[R, C, ID]
func (s *Schema[R, C, K, ID]) OnCtx(ctx context.Context, opts ...QueryOption) DAO[R, C, ID]

// WithHooks appends per-call hooks after the schema's hooks.
func WithHooks(hs ...Hook) QueryOption

// SkipHooks disables the named hooks for this DAO only (the soft-delete
// "include deleted" escape hatch). It skips EVERY hook bearing the name;
// unknown names are ignored. Duplicate names among a schema's registered
// hooks are rejected at dao.New (construction panic, like every other
// misconfiguration — ADR-0006).
func SkipHooks(names ...string) QueryOption

// WithQueryContext binds ctx to this DAO's statements (and hooks).
func WithQueryContext(ctx context.Context) QueryOption
```

`queryConfig` is carried on `queryDAO` (`hooks []Hook`, `skip map[string]bool`,
`ctx context.Context`); building the effective hook slice is done once at
acquisition, not per statement.

### 2.3 Context propagation (closes the last divergences-table row)

`queryDAO.ctx()` resolves in this precedence order:

1. `WithQueryContext(ctx)` (explicit per-call option)
2. `OnCtx(ctx, …)` / `WithTx`-carried context
3. the bound transaction's context (`tx.ctx`)
4. `context.Background()`

The resolved context is passed to every driver call (as today) **and to every
hook phase**. Per-verb context variants (`GetCtx`, `SelectCtx`, …) were
rejected: 20+ new methods for what one acquisition-time option expresses
without disturbing the fluent chain (see §4).

**Late `Use(tx)` does not demote an explicit context.** The fluent
`DAO.Use(tx)` today overwrites the DAO's context with `tx.ctx`
(`dao/query_dao.go:200-205`). With this ADR the explicit query context is
sticky: `schema.DAO(WithQueryContext(reqCtx)).Use(tx).Update()` binds the
transaction but keeps `reqCtx` (the engine tracks an explicit-ctx bit; only a
DAO without one adopts `tx.ctx` on `Use`). Acceptance criterion 5 tests this
edge.

### 2.4 Pipeline semantics

- **Order.** Effective hooks = schema hooks (registration order) then per-call
  hooks (registration order), minus skipped names. `BeforeBuild` and
  `BeforeExec` run first→last; `AfterExec` runs last→first (middleware onion:
  the outermost registered hook sees the final error).
- **Abort.** An error from any `BeforeBuild`/`BeforeExec` aborts the
  statement: nothing executes, no `AfterExec` fires, the error returns to the
  caller untranslated (it is the hook's own error, not a driver error).
- **Error replacement.** The statement's translated error enters the
  `AfterExec` chain as `out.Err`; each hook's return value becomes `out.Err`
  for the next. The final value is what the verb returns.
- **Engine placement.** The verb bodies factor their shared tail into two
  funnels — `runQuery(op, build func(*builder) string, scan func(Rows) error)`
  and `runExec(op, build …) (Result, error)` — which fire the pipeline. This
  is a refactor of duplication that already exists in every verb body; no verb
  semantics change.
- **Fast path.** With zero effective hooks the funnels skip the pipeline
  entirely — no `QueryInfo` allocation, no time measurement beyond what the
  debug logger already did.

### 2.5 The debug logger becomes a hook

`dao.WithLogger` + `dao.Debug(true)` keep their exact surface and output, but
the engine implements them as an internal `logHook` (a `NamedHook`, name
`"dao.log"`). It is appended as the **final hook of the effective per-call
slice** — after schema hooks *and* after per-call hooks — so it always logs
the FINAL SQL/args as executed, including any per-call `BeforeExec` rewrite
(matching what the old inline logger showed: the statement actually sent).
One pipeline, no bespoke logging branch in the verbs; `SkipHooks("dao.log")`
can silence one call.

### 2.6 Verb-specific notes

- **Iterate**'s `AfterExec` is **execution-only**: it fires when the statement
  executes, with `Rows: -1`, duration covering execution not consumption — and
  it can neither observe nor replace errors that occur during later stream
  consumption (scan failures, `rows.Err()`), which surface through the
  iterator untransformed. Hooks needing consumption-side visibility would
  require an iterator wrap; that is explicitly out of scope for this ADR.
- **Batch** fires `BeforeBuild` never (there is no staged WHERE/SET to
  mutate; batch shaping is ADR-0010 territory), and per emitted statement:
  - chunked INSERTs → `Op: OpBatch`, full `BeforeExec` rewrite contract;
  - the COPY fast path → `Op: OpBatchCopy`, **observe-only**: there is no SQL
    statement (the dialect drives the driver's bulk API), `QueryInfo.SQL` is a
    synthetic descriptor, and a hook that mutates `SQL`/`Args` on an
    `OpBatchCopy` event FAILS the flush with a descriptive error — never a
    silent ignore. There is no non-error "veto" signal: a hook error during
    `BeforeExec(OpBatchCopy)` fails the flush per the §2.4 abort rule, exactly
    like any other phase error. A caller that needs the rewrite contract on
    bulk loads must avoid the COPY fast path (e.g. the batch API's existing
    controls / not qualifying for COPY), getting rewriteable chunked-INSERT
    `OpBatch` statements instead.
- **Exists/Count** are reads; `Stager.Where` applies; `Stager.SetColumn` is a
  documented no-op for reads. On INSERT/UPSERT the inverse holds: `SetColumn`
  applies and `Where` fails loudly (§2.1) — scoped writes stay the point
  (§1.1 item 1), they just scope by the correct mechanism per verb.

### 2.7 North-star usage

```go
// ---- tenant scoping: declared once, enforced on EVERY verb -----------------
type tenantHook struct{ dao.NopHook }

func (tenantHook) HookName() string { return "tenant" }
func (tenantHook) BeforeBuild(ctx context.Context, q *dao.QueryInfo, s dao.Stager) error {
	org, ok := auth.OrgID(ctx) // caller-owned ctx extraction
	if !ok {
		return fmt.Errorf("tenant: no org in context for %s on %s", q.Op, q.Table)
	}
	switch q.Op {
	case dao.OpInsert, dao.OpUpsert:
		// No WHERE clause exists here; scoping means forcing the tenant column.
		s.SetColumn("org_id", org)
	default:
		// Reads AND filterable writes (UPDATE/DELETE) get the same guard.
		s.Where(dao.Eq("org_id", org))
	}
	return nil
}

// ---- soft delete: filter by default, opt out per call ----------------------
type softDeleteHook struct{ dao.NopHook }

func (softDeleteHook) HookName() string { return "softdelete" }
func (softDeleteHook) BeforeBuild(_ context.Context, q *dao.QueryInfo, s dao.Stager) error {
	if !q.Op.IsWrite() {
		s.Where(dao.IsNull("deleted_at"))
	}
	return nil
}

// ---- metrics: duration + outcome, no Debug flag -----------------------------
type metricsHook struct{ dao.NopHook }

func (m metricsHook) AfterExec(_ context.Context, q *dao.QueryInfo, out dao.Outcome) error {
	queryDuration.Observe(q.Table, string(q.Op), out.Duration)
	return out.Err
}

// ---- wiring -----------------------------------------------------------------
artists := dao.New[*model.Artist, ArtistField, string](conn,
	dao.Table("artist"),
	/* fields ... */
	dao.Hooks(tenantHook{}, softDeleteHook{}, metricsHook{}),
)

// every call is scoped + filtered + measured:
list, err := artists.OnCtx(reqCtx).Select()

// admin path: include soft-deleted rows for this one query:
all, err := artists.OnCtx(reqCtx, dao.SkipHooks("softdelete")).Select()

// explicit per-call ctx on a pool DAO (no transaction):
n, err := artists.DAO(dao.WithQueryContext(reqCtx)).Count()
```

Note what is absent: no per-verb `Preprocess*` methods, no per-entity wiring,
no SELECT-only asymmetry, and no change to any existing call site.

---

## 3. Consequences

**Positive.** Cross-cutting concerns (scoping, soft delete, observability,
error enrichment) become declarations, matching the package's data-over-code
principle; ADR-0010 gains its attachment point; the ctx-first divergence
closes; LM's access-scoping SELECT-only flaw is structurally impossible.

**Negative / costs.** The verbs' shared-tail refactor touches every verb body
(mechanical, behavior-preserving, guarded by the existing engine tests). Hooks
are a power tool: a `BeforeExec` SQL rewrite can break invariants the builder
guarantees — the escape-hatch documentation must say so plainly (same posture
as `dao.Raw`). `Stager.SetColumn`/`Where` take SQL identifiers, inheriting the
same developer-declared-identifier trust rule as `Field.Column`.

**Migration.** None: all additions are optional and variadic; existing code
compiles and behaves identically.

---

## 4. Alternatives considered

- **Per-verb hook methods (LM's shape).** Five near-identical interfaces and
  the proven SELECT-only asymmetry trap. Rejected: one pipeline + `Op`.
- **Single-method interceptor (`Handle(ctx, q, next) error`, gRPC style).**
  Maximum power, but forces every hook to reimplement phase dispatch and makes
  the common cases (add a predicate; observe duration) verbose. The three-phase
  interface with `NopHook` embedding keeps common hooks 5-liners. Rejected.
- **Per-verb ctx variants (`GetCtx`, `SelectCtx`, …).** 20+ methods, breaks
  the fluent chain's symmetry, still needs precedence rules for `OnCtx`/tx.
  Rejected for the acquisition-time option.
- **Context-carried hooks (values in `ctx`).** Implicit, unauditable, breaks
  the "explicit over magic" principle. Rejected.
- **Exposing the internal `builder` to hooks.** Powerful but freezes the
  builder's internals as public API, blocking ADR-0003 evolution. The
  `QueryInfo.SQL/Args` replacement point covers the rewrite need at a stable
  boundary. Rejected.

---

## 5. Acceptance criteria

1. The §2.7 tenant hook (Op-branching) scopes **every op**: a test proves the
   predicate lands in SELECT/UPDATE/DELETE SQL and the forced `org_id` column
   lands in INSERT/UPSERT SQL, emitted through a fake `DataConn`.
2. A hook calling `Stager.Where` during `OpInsert`/`OpUpsert` fails the
   statement with `ErrHookWhereUnsupported`; nothing executes.
3. A named soft-delete hook filters reads by default; the same query with
   `SkipHooks("softdelete")` emits SQL without the filter. Duplicate hook
   names across a schema's registered hooks panic at `dao.New`.
4. A `BeforeExec` hook replacing `SQL`/`Args` is honored verbatim by the
   executor on statement ops (incl. `OpBatch` chunks); the same mutation
   during an `OpBatchCopy` event fails the flush with a descriptive error
   (never silently ignored); an erroring `BeforeExec` prevents execution
   (fake records no call) and no `AfterExec` fires.
5. `AfterExec` receives measured `Duration`, correct `Rows`/`Affected`, and
   the translated error; its return value replaces the verb's error; with two
   hooks registered, `AfterExec` order is the reverse of `BeforeBuild` order.
6. `WithQueryContext(ctx)` reaches the driver's `QueryContext`/`ExecContext`
   and every hook phase; precedence over `OnCtx` and tx context matches §2.3,
   including the sticky case `DAO(WithQueryContext(ctx)).Use(tx)` — the
   explicit context survives the late bind.
7. `dao.WithLogger` + `Debug(true)`: with no rewriting hooks the output is
   byte-identical to today; with a per-call `BeforeExec` rewrite the log shows
   the FINAL SQL as executed (`dao.log` is the last effective hook);
   `SkipHooks("dao.log")` silences one call.
8. With no hooks registered, the engine allocates nothing for the pipeline
   (benchmark against the pre-ADR baseline) and the full pre-ADR test suite
   passes unmodified.
9. Batch fires `OpBatch` per chunked INSERT and `OpBatchCopy` (observe-only)
   for the COPY path; Iterate reports `Rows: -1` and its `AfterExec` is
   execution-only — consumption/scan errors surface through the iterator
   untransformed (§2.6).

## 6. File plan

| File | Change |
|---|---|
| `dao/hooks.go` | new — `Op`, `QueryInfo`, `Outcome`, `Stager`, `Hook`, `NopHook`, `NamedHook`, `WithHooks`, `SkipHooks`, `WithQueryContext`, `QueryOption` |
| `dao/options.go` | `Hooks(...)` build-time option |
| `dao/schema.go` | `DAO`/`On`/`OnCtx` variadic `QueryOption` tail; effective-hook assembly |
| `dao/query_dao.go` | `runQuery`/`runExec` funnels; verbs refactored onto them; `logHook` replaces the inline debug branch; `stager` impl over `queryState`/`writeState` |
| `dao/batch.go` | per-chunk `OpBatch` events |
| `dao/hooks_test.go` | new — acceptance criteria 1–9 against the fake `DataConn` |
---

## 7. Review history

- **r1 (2026-07-04, lector): `change_requested`** — review doc:
  `agents/lector/reviews/2026-07-04-golib-dao-adr-0009-review.md`.
  Amendments applied in revision 2:
  - **must-fix #1**: `Stager.Where` no longer claims INSERT/UPSERT coverage —
    per-op semantics defined; `Where` on insert-like ops fails loudly with
    `ErrHookWhereUnsupported`; tenant example branches on `Op` and uses
    `SetColumn` for insert-like writes (§2.1, §2.7, criteria 1–2).
  - **must-fix #2**: batch split into `OpBatch` (chunked INSERT, full rewrite
    contract) and `OpBatchCopy` (observe-only; SQL/Args mutation fails the
    flush loudly; hooks may veto COPY) (§2.1, §2.6, criterion 4).
  - **should-fix #1**: `dao.log` defined as the FINAL hook of the effective
    per-call slice, logging final SQL after per-call rewrites (§2.5,
    criterion 7).
  - **should-fix #2**: explicit-context stickiness across late `Use(tx)`
    (§2.3, criterion 6).
  - **should-fix #3**: Iterate `AfterExec` declared execution-only; cannot
    observe/replace consumption errors (§2.6, criterion 9).
  - **notes**: duplicate hook names panic at `dao.New`; `SkipHooks` skips all
    bearers of a name; identifier trust wording strengthened on
    `Stager.SetColumn`/`Where` (§2.1, §2.2).
- **r2 (2026-07-04, lector): `approved_with_amendments`** — review doc:
  `agents/lector/reviews/2026-07-04-golib-dao-adr-0009-rereview.md`. R1
  blockers confirmed closed. Non-blocking nits applied in revision 3: §2.6
  COPY wording tightened (no non-error veto signal exists — a hook error
  fails the flush per §2.4; callers wanting rewriteable bulk statements use
  chunked INSERT), file-plan criteria count corrected to 1–9. **Accepted
  2026-07-04** (review + implement-immediately authorized by Johno).

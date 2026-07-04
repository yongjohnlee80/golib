# ADR-0009 — `golib/dao`: Query-Time Options & the Hook/Middleware Seam

- **Status:** Proposed
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
	OpBatch   Op = "batch" // one event per emitted chunk / COPY
)

// IsWrite reports whether the op mutates data (insert/update/upsert/delete/batch).
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
	// Where ANDs a predicate into the statement (reads AND writes — a scoped
	// UPDATE/DELETE gets the same guard as a scoped SELECT).
	Where(p Predicate)
	// OrderBy appends ordering (reads; no-op for writes).
	OrderBy(sorts ...Sort)
	// Limit caps row count when none is set yet (reads; no-op for writes).
	Limit(n uint64)
	// SetColumn stages a write value by SQL column name, quoted via the
	// dialect (writes; no-op for reads). The column is developer-declared —
	// never pass request input.
	SetColumn(column string, value any)
}

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

// SkipHooks disables the named schema-wide hooks for this DAO only (the
// soft-delete "include deleted" escape hatch). Unknown names are ignored.
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
`"dao.log"`) appended last at schema build. One pipeline, no bespoke logging
branch in the verbs; `SkipHooks("dao.log")` can silence one call.

### 2.6 Verb-specific notes

- **Iterate** fires `AfterExec` when the statement executes, with `Rows: -1`
  (the stream length is unknowable until the caller finishes). Duration covers
  execution, not consumption.
- **Batch** fires `BeforeExec`/`AfterExec` once per emitted statement (each
  chunked INSERT, or the COPY) with `Op: OpBatch`. `BeforeBuild` does not fire
  for batch (there is no staged WHERE/SET to mutate); a batch-shaping need is
  ADR-0010 territory.
- **Exists/Count** are reads; `Stager.Where` applies. `Stager.SetColumn` is a
  no-op for reads, as `Where` is never a no-op anywhere — scoped writes are
  the point (§1.1 item 1).

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
	s.Where(dao.Eq("org_id", org)) // SELECT *and* UPDATE/DELETE are scoped
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

1. A schema-wide `BeforeBuild` hook adding `Where(Eq("org_id", …))` scopes
   **all ten ops** — a test proves the predicate lands in SELECT, UPDATE and
   DELETE SQL emitted through a fake `DataConn`.
2. A named soft-delete hook filters reads by default; the same query with
   `SkipHooks("softdelete")` emits SQL without the filter.
3. A `BeforeExec` hook replacing `SQL`/`Args` is honored verbatim by the
   executor; an erroring `BeforeExec` prevents execution (fake records no
   call) and no `AfterExec` fires.
4. `AfterExec` receives measured `Duration`, correct `Rows`/`Affected`, and
   the translated error; its return value replaces the verb's error; with two
   hooks registered, `AfterExec` order is the reverse of `BeforeBuild` order.
5. `WithQueryContext(ctx)` reaches the driver's `QueryContext`/`ExecContext`
   and every hook phase; precedence over `OnCtx` and tx context matches §2.3.
6. `dao.WithLogger` + `Debug(true)` output is byte-identical to today and can
   be silenced per call with `SkipHooks("dao.log")`.
7. With no hooks registered, the engine allocates nothing for the pipeline
   (benchmark against the pre-ADR baseline) and the full pre-ADR test suite
   passes unmodified.
8. Batch fires `OpBatch` events per chunk/COPY; Iterate reports `Rows: -1`.

## 6. File plan

| File | Change |
|---|---|
| `dao/hooks.go` | new — `Op`, `QueryInfo`, `Outcome`, `Stager`, `Hook`, `NopHook`, `NamedHook`, `WithHooks`, `SkipHooks`, `WithQueryContext`, `QueryOption` |
| `dao/options.go` | `Hooks(...)` build-time option |
| `dao/schema.go` | `DAO`/`On`/`OnCtx` variadic `QueryOption` tail; effective-hook assembly |
| `dao/query_dao.go` | `runQuery`/`runExec` funnels; verbs refactored onto them; `logHook` replaces the inline debug branch; `stager` impl over `queryState`/`writeState` |
| `dao/batch.go` | per-chunk `OpBatch` events |
| `dao/hooks_test.go` | new — acceptance criteria 1–8 against the fake `DataConn` |
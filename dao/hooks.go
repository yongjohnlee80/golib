package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// Op identifies the statement kind a hook observes (ADR-0009 §2.1).
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
	// flush: a real SQL statement, the full BeforeExec rewrite contract applies.
	OpBatch Op = "batch"

	// OpBatchCopy is a bulk-load fast path (dialect COPY): there is no SQL
	// statement to rewrite — the event is observe-only (ADR-0009 §2.6).
	// QueryInfo.SQL carries a synthetic descriptor; mutating SQL/Args fails
	// the flush.
	OpBatchCopy Op = "batch-copy"
)

// IsWrite reports whether the op mutates data
// (insert/update/upsert/delete/batch/batch-copy).
func (o Op) IsWrite() bool {
	switch o {
	case OpInsert, OpUpdate, OpUpsert, OpDelete, OpBatch, OpBatchCopy:
		return true
	}
	return false
}

// whereCapable reports whether the op's SQL form carries a WHERE clause.
func (o Op) whereCapable() bool {
	switch o {
	case OpGet, OpSelect, OpIterate, OpExists, OpCount, OpUpdate, OpDelete:
		return true
	}
	return false
}

// QueryInfo describes one statement to the hook pipeline. BeforeBuild sees it
// without SQL (not rendered yet); BeforeExec and AfterExec see SQL and Args.
// A BeforeExec hook may REPLACE SQL and Args on statement ops (consistently —
// replacing one without the other is a bug the engine cannot detect); on
// OpBatchCopy events any mutation fails the flush (ADR-0009 §2.6). All other
// fields are informational.
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
	Rows     int   // rows scanned; -1 when unknown (Iterate streams, ADR-0009 §2.6)
	Affected int64 // driver-reported affected rows; -1 when not applicable
	Err      error // the statement's error after dialect/ErrorMap translation
}

// Stager is the type-erased staging surface a BeforeBuild hook mutates. It is
// implemented by the engine over the query-scoped DAO's per-call state; the
// generic With/Set surface (typed by field enum) remains the caller-facing
// API — Stager exists so schema-agnostic hooks can be written once and shared
// across entities (ADR-0009 §2.1).
type Stager interface {
	// Where ANDs a predicate into the statement on the where-capable ops:
	// Get/Select/Iterate/Exists/Count/Update/Delete. INSERT and UPSERT have
	// no WHERE clause — calling Where there fails the statement with
	// ErrHookWhereUnsupported rather than silently not scoping a write; a
	// scoping hook branches on QueryInfo.Op and uses SetColumn for insert-like
	// ops. Where never silently no-ops anywhere. Request input may enter only
	// as bound predicate values, never as identifiers or SQL text.
	Where(p Predicate)
	// OrderBy appends ordering (reads only; no-op for writes). Keys resolve
	// against the schema's SortMap exactly as DAO.OrderBy.
	OrderBy(sorts ...Sort)
	// Limit caps row count when none is set yet (reads only; no-op for writes).
	Limit(n uint64)
	// SetColumn stages a write value by SQL column name (write ops including
	// INSERT/UPSERT; no-op for reads). The column is a developer-declared
	// identifier, quoted by the builder — request input may enter only as the
	// bound value, never as the column name.
	SetColumn(column string, value any)
}

// ErrHookWhereUnsupported reports a BeforeBuild hook calling Stager.Where on
// an op with no WHERE clause (insert/upsert). Loud by design: the alternative
// is an unscoped write that looks scoped (ADR-0009 §2.1).
var ErrHookWhereUnsupported = errors.New(
	"dao: hook Where is not supported on insert/upsert — branch on Op and use SetColumn")

// Hook observes and augments statement execution (ADR-0009 §2.1). Embed
// [NopHook] and override only the phases you need (the GenericDialect pattern).
type Hook interface {
	// BeforeBuild runs before SQL is rendered. Mutations through s become part
	// of the statement. Returning an error aborts the statement.
	BeforeBuild(ctx context.Context, q *QueryInfo, s Stager) error
	// BeforeExec runs after SQL is rendered and before execution. It may
	// replace q.SQL / q.Args (statement ops only — see OpBatchCopy). Returning
	// an error aborts the statement.
	BeforeExec(ctx context.Context, q *QueryInfo) error
	// AfterExec runs after execution (success or failure). The returned error
	// REPLACES out.Err as the statement's result — return out.Err unchanged to
	// pass through, wrap it to enrich, or return nil to suppress.
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
// (see [SkipHooks]). Anonymous hooks cannot be skipped. Duplicate names among
// one schema's registered hooks panic at [New].
type NamedHook interface {
	Hook
	HookName() string
}

// queryConfig is the per-acquisition state assembled from QueryOptions.
type queryConfig struct {
	hooks       []Hook
	skip        map[string]bool
	ctx         context.Context
	explicitCtx bool
}

// QueryOption configures one DAO instance at acquisition time
// (Schema.DAO / On / OnCtx).
type QueryOption func(*queryConfig)

// WithHooks appends per-call hooks after the schema's hooks (ADR-0009 §2.2).
func WithHooks(hs ...Hook) QueryOption {
	return func(c *queryConfig) { c.hooks = append(c.hooks, hs...) }
}

// SkipHooks disables the named hooks for this DAO only (e.g. a soft-delete
// hook's "include deleted" escape hatch). It skips every hook bearing the
// name; unknown names are ignored.
func SkipHooks(names ...string) QueryOption {
	return func(c *queryConfig) {
		if c.skip == nil {
			c.skip = make(map[string]bool, len(names))
		}
		for _, n := range names {
			c.skip[n] = true
		}
	}
}

// WithQueryContext binds ctx to this DAO's statements and hooks. It is the
// top of the context precedence order (ADR-0009 §2.3) and is sticky: a later
// Use(tx) binds the transaction without demoting this context.
func WithQueryContext(ctx context.Context) QueryOption {
	return func(c *queryConfig) { c.ctx = ctx; c.explicitCtx = true }
}

// hookName returns h's identity, or "" for anonymous hooks.
func hookName(h Hook) string {
	if n, ok := h.(NamedHook); ok {
		return n.HookName()
	}
	return ""
}

// --- pipeline -----------------------------------------------------------------

// pipeline carries one statement through the hook phases. A nil *pipeline is
// the no-hooks fast path: every method no-ops (ADR-0009 §2.4).
type pipeline struct {
	hooks []Hook
	ctx   context.Context
	info  QueryInfo
	start time.Time
}

// beforeBuild fires the BeforeBuild phase in order; sink applies stager
// mutations. Returns the first hook error (statement aborts).
func (p *pipeline) beforeBuild(s Stager) error {
	if p == nil {
		return nil
	}
	for _, h := range p.hooks {
		if err := h.BeforeBuild(p.ctx, &p.info, s); err != nil {
			return err
		}
	}
	return nil
}

// beforeExec fires the BeforeExec phase in order, allowing SQL/args
// replacement through the pointers, and starts the duration clock.
func (p *pipeline) beforeExec(sql *string, args *[]any) error {
	if p == nil {
		return nil
	}
	p.info.SQL, p.info.Args = *sql, *args
	for _, h := range p.hooks {
		if err := h.BeforeExec(p.ctx, &p.info); err != nil {
			return err
		}
	}
	*sql, *args = p.info.SQL, p.info.Args
	p.start = time.Now()
	return nil
}

// beforeExecFrozen fires BeforeExec for observe-only events (OpBatchCopy):
// any SQL/Args mutation fails loudly instead of being silently ignored.
func (p *pipeline) beforeExecFrozen(sql string) error {
	if p == nil {
		return nil
	}
	p.info.SQL, p.info.Args = sql, nil
	for _, h := range p.hooks {
		if err := h.BeforeExec(p.ctx, &p.info); err != nil {
			return err
		}
		if p.info.SQL != sql || p.info.Args != nil {
			return fmt.Errorf("dao: a hook mutated SQL/Args on an observe-only %s event; "+
				"COPY has no statement to rewrite — use chunked INSERT batches for "+
				"the rewrite contract (%w)", p.info.Op, errs.ErrPrecondition)
		}
	}
	p.start = time.Now()
	return nil
}

// finish fires the AfterExec phase in reverse order (middleware onion); each
// hook's return value replaces the error seen by the next. On a nil pipeline
// it returns err unchanged.
func (p *pipeline) finish(rows int, affected int64, err error) error {
	if p == nil {
		return err
	}
	out := Outcome{Duration: time.Since(p.start), Rows: rows, Affected: affected, Err: err}
	for i := len(p.hooks) - 1; i >= 0; i-- {
		out.Err = p.hooks[i].AfterExec(p.ctx, &p.info, out)
	}
	return out.Err
}

// --- the built-in debug logger hook --------------------------------------------

// logHook reimplements the schema debug logger as the final hook of every
// effective slice, so it logs the FINAL SQL/args as executed — including any
// per-call BeforeExec rewrite (ADR-0009 §2.5). Skippable as "dao.log".
type logHook struct {
	NopHook
	log func(sql string, args []any)
}

func (logHook) HookName() string { return "dao.log" }

func (l logHook) BeforeExec(_ context.Context, q *QueryInfo) error {
	l.log(q.SQL, q.Args)
	return nil
}

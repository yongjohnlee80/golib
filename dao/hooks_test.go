package dao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/logger"
)

// hookOption adapts the generic Hooks option for the artist test schema.
func hookOption(hs ...Hook) Option[*artist, artistField, artistSort, string] {
	return Hooks[*artist, artistField, artistSort, string](hs...)
}

// tenantHook mirrors the ADR's north-star example: Where on where-capable ops,
// SetColumn on insert-like ops.
type tenantHook struct{ NopHook }

func (tenantHook) HookName() string { return "tenant" }
func (tenantHook) BeforeBuild(_ context.Context, q *QueryInfo, s Stager) error {
	switch q.Op {
	case OpInsert, OpUpsert:
		s.SetColumn("org_id", "org-1")
	default:
		s.Where(Eq("org_id", "org-1"))
	}
	return nil
}

// whereEverywhereHook calls Where unconditionally — the loud-failure case.
type whereEverywhereHook struct{ NopHook }

func (whereEverywhereHook) BeforeBuild(_ context.Context, _ *QueryInfo, s Stager) error {
	s.Where(Eq("org_id", "org-1"))
	return nil
}

// softDeleteHook filters reads unless skipped.
type softDeleteHook struct{ NopHook }

func (softDeleteHook) HookName() string { return "softdelete" }
func (softDeleteHook) BeforeBuild(_ context.Context, q *QueryInfo, s Stager) error {
	if !q.Op.IsWrite() {
		s.Where(IsNull("deleted_at"))
	}
	return nil
}

// recHook records every phase invocation into a shared trace.
type recHook struct {
	NopHook
	name  string
	trace *[]string
}

func (h recHook) HookName() string { return h.name }
func (h recHook) BeforeBuild(context.Context, *QueryInfo, Stager) error {
	*h.trace = append(*h.trace, "build:"+h.name)
	return nil
}
func (h recHook) BeforeExec(context.Context, *QueryInfo) error {
	*h.trace = append(*h.trace, "exec:"+h.name)
	return nil
}
func (h recHook) AfterExec(_ context.Context, _ *QueryInfo, out Outcome) error {
	*h.trace = append(*h.trace, "after:"+h.name)
	return out.Err
}

// --- tenant hook scopes every op --------------------------------------------

func TestHooks_TenantScopesEveryOp(t *testing.T) {
	t.Parallel()

	type schemaT = Schema[*artist, artistField, artistSort, string]
	run := func(name string, rows *fakeRows, exercise func(s *schemaT) error, wantSQL func(c *fakeConn) string, want string) {
		t.Run(name, func(t *testing.T) {
			conn := newConn()
			conn.rows = rows
			s := buildSchema(conn, hookOption(tenantHook{}))
			_ = exercise(s)
			if got := wantSQL(conn); !strings.Contains(got, want) {
				t.Errorf("SQL %q does not contain %q", got, want)
			}
		})
	}

	lastQ := func(c *fakeConn) string { return c.lastQuery }
	lastE := func(c *fakeConn) string { return c.lastExec }
	entityRows := func() *fakeRows { return &fakeRows{data: [][]any{{"1", "A", "a"}}} }

	run("select", entityRows(), func(s *schemaT) error {
		_, err := s.DAO().Select()
		return err
	}, lastQ, `org_id = $`)
	run("get", entityRows(), func(s *schemaT) error {
		_, err := s.DAO().With(aID, "1").Get()
		return err
	}, lastQ, `org_id = $`)
	run("exists", &fakeRows{data: [][]any{{true}}}, func(s *schemaT) error {
		_, err := s.DAO().Exists()
		return err
	}, lastQ, `org_id = $`)
	run("count", &fakeRows{data: [][]any{{uint64(1)}}}, func(s *schemaT) error {
		_, err := s.DAO().Count()
		return err
	}, lastQ, `org_id = $`)
	run("update", &fakeRows{}, func(s *schemaT) error {
		return s.DAO().With(aID, "1").Set(aName, "x").Update()
	}, lastE, `org_id = $`)
	run("delete", &fakeRows{}, func(s *schemaT) error {
		return s.DAO().With(aID, "1").Delete()
	}, lastE, `org_id = $`)
	// insert-like ops get the forced column, not a WHERE
	run("insert", &fakeRows{data: [][]any{{"1"}}}, func(s *schemaT) error {
		_, err := s.DAO().Set(aName, "x").Insert()
		return err
	}, lastQ, `"org_id"`)
	run("upsert", &fakeRows{}, func(s *schemaT) error {
		return s.DAO().Set(aName, "x").Set(aURI, "u").Upsert()
	}, lastE, `"org_id"`)
}

// --- Where on insert/upsert fails loudly ------------------------------------

func TestHooks_WhereOnInsertFailsLoudly(t *testing.T) {
	t.Parallel()
	conn := newConn()
	s := buildSchema(conn, hookOption(whereEverywhereHook{}))

	_, err := s.DAO().Set(aName, "x").Insert()
	if !errors.Is(err, ErrHookWhereUnsupported) {
		t.Fatalf("Insert err = %v, want ErrHookWhereUnsupported", err)
	}
	if conn.lastQuery != "" || conn.lastExec != "" {
		t.Error("nothing may execute after a hook Where failure")
	}

	if err := s.DAO().Set(aName, "x").Set(aURI, "u").Upsert(); !errors.Is(err, ErrHookWhereUnsupported) {
		t.Fatalf("Upsert err = %v, want ErrHookWhereUnsupported", err)
	}
}

// --- SkipHooks + duplicate names --------------------------------------------

func TestHooks_SoftDeleteAndSkip(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.rows = &fakeRows{}
	s := buildSchema(conn, hookOption(softDeleteHook{}))

	if _, err := s.DAO().Select(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.lastQuery, "deleted_at IS NULL") {
		t.Errorf("default read not filtered: %q", conn.lastQuery)
	}

	conn.lastQuery = ""
	conn.rows = &fakeRows{}
	if _, err := s.DAO(SkipHooks("softdelete")).Select(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conn.lastQuery, "deleted_at") {
		t.Errorf("SkipHooks did not remove the filter: %q", conn.lastQuery)
	}
}

func TestHooks_DuplicateNamesPanicAtNew(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for duplicate hook names")
		}
	}()
	_ = buildSchema(newConn(), hookOption(softDeleteHook{}, softDeleteHook{}))
}

// --- BeforeExec rewrite honored; error aborts -------------------------------

type rewriteHook struct{ NopHook }

func (rewriteHook) BeforeExec(_ context.Context, q *QueryInfo) error {
	q.SQL = "SELECT 1 /* rewritten */"
	q.Args = []any{42}
	return nil
}

type failExecHook struct{ NopHook }

func (failExecHook) BeforeExec(context.Context, *QueryInfo) error {
	return errors.New("exec vetoed")
}

type afterSpy struct {
	NopHook
	fired *bool
}

func (h afterSpy) AfterExec(_ context.Context, _ *QueryInfo, out Outcome) error {
	*h.fired = true
	return out.Err
}

func TestHooks_BeforeExecRewriteHonored(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.rows = &fakeRows{}
	s := buildSchema(conn, hookOption(rewriteHook{}))

	_, _ = s.DAO().Select()
	if conn.lastQuery != "SELECT 1 /* rewritten */" {
		t.Errorf("rewrite not honored: %q", conn.lastQuery)
	}
	if len(conn.lastArgs) != 1 || conn.lastArgs[0] != 42 {
		t.Errorf("rewritten args not honored: %v", conn.lastArgs)
	}
}

func TestHooks_BeforeExecErrorAborts(t *testing.T) {
	t.Parallel()
	conn := newConn()
	var afterFired bool
	s := buildSchema(conn, hookOption(failExecHook{}, afterSpy{fired: &afterFired}))

	_, err := s.DAO().Select()
	if err == nil || err.Error() != "exec vetoed" {
		t.Fatalf("err = %v, want the hook error", err)
	}
	if conn.lastQuery != "" {
		t.Error("statement must not execute after a BeforeExec error")
	}
	if afterFired {
		t.Error("no AfterExec may fire after an aborted statement")
	}
}

// --- AfterExec outcome, replacement, onion order ----------------------------

type enrichHook struct{ NopHook }

func (enrichHook) AfterExec(_ context.Context, q *QueryInfo, out Outcome) error {
	if out.Err != nil {
		return fmt.Errorf("enriched %s on %s: %w", q.Op, q.Table, out.Err)
	}
	return nil
}

func TestHooks_AfterExecOutcomeAndReplacement(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.queryErr = errors.New("boom")
	var got Outcome
	captured := false
	cap := hookFunc(func(_ context.Context, _ *QueryInfo, out Outcome) error {
		got, captured = out, true
		return out.Err
	})
	s := buildSchema(conn, hookOption(cap, enrichHook{}))

	_, err := s.DAO().Select()
	if !captured {
		t.Fatal("AfterExec did not fire")
	}
	if got.Rows != 0 || got.Duration < 0 {
		t.Errorf("outcome = %+v", got)
	}
	// enrichHook (inner, registered later) wrapped first; cap (outer) saw the
	// wrapped error — and the verb returns it.
	if err == nil || !strings.Contains(err.Error(), "enriched select on artist") || !errors.Is(err, conn.queryErr) {
		t.Errorf("final err = %v", err)
	}
}

// hookFunc adapts an AfterExec func to Hook.
type hookFunc func(context.Context, *QueryInfo, Outcome) error

func (hookFunc) BeforeBuild(context.Context, *QueryInfo, Stager) error { return nil }
func (hookFunc) BeforeExec(context.Context, *QueryInfo) error          { return nil }
func (f hookFunc) AfterExec(ctx context.Context, q *QueryInfo, out Outcome) error {
	return f(ctx, q, out)
}

func TestHooks_OnionOrdering(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.rows = &fakeRows{}
	var trace []string
	s := buildSchema(conn, hookOption(
		recHook{name: "a", trace: &trace},
		recHook{name: "b", trace: &trace},
	))
	if _, err := s.DAO(WithHooks(recHook{name: "c", trace: &trace})).Select(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"build:a", "build:b", "build:c",
		"exec:a", "exec:b", "exec:c",
		"after:c", "after:b", "after:a", // reverse — middleware onion
	}
	if fmt.Sprint(trace) != fmt.Sprint(want) {
		t.Errorf("trace = %v, want %v", trace, want)
	}
}

// --- WithQueryContext propagation + stickiness ------------------------------

type ctxKey struct{}

type ctxSpy struct {
	NopHook
	sawHook *bool
}

func (h ctxSpy) BeforeExec(ctx context.Context, _ *QueryInfo) error {
	if ctx.Value(ctxKey{}) == "yes" {
		*h.sawHook = true
	}
	return nil
}

// ctxConn records the ctx value seen by the driver.
type ctxConn struct {
	fakeConn
	sawDriver bool
}

func (c *ctxConn) QueryContext(ctx context.Context, q string, args ...any) (Rows, error) {
	if ctx.Value(ctxKey{}) == "yes" {
		c.sawDriver = true
	}
	return c.fakeConn.QueryContext(ctx, q, args...)
}

func TestHooks_WithQueryContextPropagates(t *testing.T) {
	t.Parallel()
	conn := &ctxConn{fakeConn: fakeConn{d: returningDialect{}, rows: &fakeRows{}}}
	var sawHook bool
	s := buildSchema(conn, hookOption(ctxSpy{sawHook: &sawHook}))

	ctx := context.WithValue(context.Background(), ctxKey{}, "yes")
	if _, err := s.DAO(WithQueryContext(ctx)).Select(); err != nil {
		t.Fatal(err)
	}
	if !conn.sawDriver {
		t.Error("explicit query context did not reach the driver")
	}
	if !sawHook {
		t.Error("explicit query context did not reach the hooks")
	}
}

func TestHooks_ExplicitContextStickyAcrossUse(t *testing.T) {
	t.Parallel()
	conn := &ctxConn{fakeConn: fakeConn{d: returningDialect{}, rows: &fakeRows{}}}
	s := buildSchema(conn)

	explicit := context.WithValue(context.Background(), ctxKey{}, "yes")
	tx := Begin(context.Background()) // tx with a different (background) ctx
	_ = tx.Rollback()                 // not exercising the tx path; just its ctx

	d := s.DAO(WithQueryContext(explicit)).Use(nil)
	if _, err := d.Select(); err != nil {
		t.Fatal(err)
	}
	if !conn.sawDriver {
		t.Error("explicit ctx lost after Use(nil)")
	}

	// With a live tx the explicit ctx must still win.
	conn2 := &ctxConn{fakeConn: fakeConn{d: returningDialect{}, rows: &fakeRows{}}}
	txc := newTxConn("db1")
	s2 := buildSchema(txc)
	_ = conn2
	tx2 := Begin(context.Background()) // txc joins lazily via s2's schema (ADR-0015 §2.2)
	d2 := s2.DAO(WithQueryContext(explicit)).Use(tx2)
	_ = stageUpdate(d2)
	if txc.tc == nil {
		t.Fatal("statement did not run on the transaction")
	}
	_ = tx2.Rollback()
}

// --- debug logger as final hook ---------------------------------------------

// sqlLogger records debug lines.
type sqlLogger struct {
	lines []map[string]any
}

func (c *sqlLogger) Log(_ logger.Severity, payload any) {
	if m, ok := payload.(map[string]any); ok {
		c.lines = append(c.lines, m)
	}
}

func TestHooks_DebugLoggerLogsFinalSQL(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.rows = &fakeRows{}
	lg := &sqlLogger{}
	s := buildSchema(conn,
		WithLogger[*artist, artistField, artistSort, string](lg),
		Debug[*artist, artistField, artistSort, string](true))

	// Baseline: no rewriting hooks — logs the built SQL.
	if _, err := s.DAO().Select(); err != nil {
		t.Fatal(err)
	}
	if len(lg.lines) != 1 || !strings.Contains(lg.lines[0]["sql"].(string), "SELECT") {
		t.Fatalf("debug log = %+v", lg.lines)
	}

	// With a per-call rewrite the log shows the FINAL SQL.
	conn.rows = &fakeRows{}
	if _, err := s.DAO(WithHooks(rewriteHook{})).Select(); err != nil {
		t.Fatal(err)
	}
	if got := lg.lines[len(lg.lines)-1]["sql"].(string); got != "SELECT 1 /* rewritten */" {
		t.Errorf("debug log shows %q, want the rewritten SQL", got)
	}

	// SkipHooks silences it per call.
	n := len(lg.lines)
	conn.rows = &fakeRows{}
	if _, err := s.DAO(SkipHooks("dao.log")).Select(); err != nil {
		t.Fatal(err)
	}
	if len(lg.lines) != n {
		t.Error("SkipHooks(\"dao.log\") did not silence the debug log")
	}
}

// --- zero-cost fast path ----------------------------------------------------

func BenchmarkSelect_NoHooks(b *testing.B) {
	conn := newConn()
	s := buildSchema(conn)
	b.ReportAllocs()
	for b.Loop() {
		conn.rows = &fakeRows{}
		_, _ = s.DAO().Select()
	}
}

// --- batch events -----------------------------------------------------------

type batchSpy struct {
	NopHook
	ops   *[]Op
	after *[]Op
}

func (h batchSpy) BeforeExec(_ context.Context, q *QueryInfo) error {
	*h.ops = append(*h.ops, q.Op)
	return nil
}
func (h batchSpy) AfterExec(_ context.Context, q *QueryInfo, out Outcome) error {
	*h.after = append(*h.after, q.Op)
	return out.Err
}

type copyMutator struct{ NopHook }

func (copyMutator) BeforeExec(_ context.Context, q *QueryInfo) error {
	if q.Op == OpBatchCopy {
		q.SQL = "COPY mutated"
	}
	return nil
}

func TestHooks_BatchChunkEvents(t *testing.T) {
	t.Parallel()
	conn := newConn()
	var ops, after []Op
	s := buildSchema(conn, hookOption(batchSpy{ops: &ops, after: &after}))

	b := s.DAO().Batch()
	b.Add(map[artistField]any{aName: "a", aURI: "u1"})
	b.Add(map[artistField]any{aName: "b", aURI: "u2"})
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(ops) == 0 || ops[0] != OpBatch {
		t.Errorf("BeforeExec ops = %v, want OpBatch events", ops)
	}
	if len(after) != len(ops) {
		t.Errorf("AfterExec fired %d times for %d chunks", len(after), len(ops))
	}
	if !strings.Contains(conn.lastExec, "INSERT INTO") {
		t.Errorf("chunk SQL = %q", conn.lastExec)
	}
}

// copyConn reports COPY support so the batch takes the fast path.
type copyDialectT struct{ GenericDialect }

func (copyDialectT) CopySupported() bool { return true }
func (copyDialectT) Copy(_ context.Context, _ any, _ string, _ []string, rows [][]any) (int64, error) {
	return int64(len(rows)), nil
}

func TestHooks_BatchCopyObserveOnly(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{d: copyDialectT{}}
	var ops, after []Op
	s := buildSchema(conn, hookOption(batchSpy{ops: &ops, after: &after}))

	// Force the COPY path.
	b := s.DAO().Batch().ForceCopy()
	b.Add(map[artistField]any{aName: "a", aURI: "u1"})
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0] != OpBatchCopy {
		t.Fatalf("ops = %v, want one OpBatchCopy event", ops)
	}

	// A hook that mutates the observe-only event fails the flush loudly.
	s2 := buildSchema(&fakeConn{d: copyDialectT{}}, hookOption(copyMutator{}))
	b2 := s2.DAO().Batch().ForceCopy()
	b2.Add(map[artistField]any{aName: "a", aURI: "u1"})
	err := b2.Flush()
	if err == nil || !strings.Contains(err.Error(), "observe-only") {
		t.Fatalf("err = %v, want observe-only mutation failure", err)
	}
}

// --- Iterate: execution-only AfterExec ----------------------------------------------

func TestHooks_IterateExecutionOnly(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{"1", "A", "a"}}}
	var after []Op
	var ops []Op
	s := buildSchema(conn, hookOption(batchSpy{ops: &ops, after: &after}))

	it, err := s.DAO().Iterate()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0] != OpIterate {
		t.Fatalf("AfterExec must fire at execution time, got %v", after)
	}
	for it.Next() {
	}
	_ = it.Close()
	if len(after) != 1 {
		t.Error("consumption must not fire additional AfterExec events")
	}
}

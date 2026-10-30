//go:build integration

// Live acceptance cells for dao.WithLocalSettings,
// against PostgreSQL 18 with pgvector. Each cell opens its own pool capped at
// one connection, so "the next user of the connection" and "a fresh
// connection" are facts of the cell, not of the pool's mood.
//
//	go test -race -tags integration -run 'TestLocalSettings_Live' ./dao/postgres/
package postgres

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// lsRow is the settings in force, as one statement reads them: each column is
// current_setting for one allowlisted name, "<none>" for a name the server has
// not defined.
type lsRow struct {
	ID                   int
	Plan, Iter, Ef, Dims string
}

type lsField string

const (
	lsID   lsField = "id"
	lsPlan lsField = "plan"
	lsIter lsField = "iter"
	lsEf   lsField = "ef"
	lsDims lsField = "dims"
	lsBad  lsField = "bad"
	lsZero lsField = "zero"
)

const lsTable = "golib_dao_localset"

func setting(name string) string {
	return "COALESCE(current_setting('" + name + "', true), '<none>')"
}

var lsFields = map[lsField]dao.Field[*lsRow]{
	lsID:   {Column: "id", Scan: func(r *lsRow) any { return &r.ID }},
	lsPlan: {Column: setting(settingPlanCacheMode), ReadOnly: true, Scan: func(r *lsRow) any { return &r.Plan }},
	lsIter: {Column: setting(settingHNSWIterativeScan), ReadOnly: true, Scan: func(r *lsRow) any { return &r.Iter }},
	lsEf:   {Column: setting(settingHNSWEfSearch), ReadOnly: true, Scan: func(r *lsRow) any { return &r.Ef }},
	// Reading a vector loads pgvector's library in the session, which defines
	// the hnsw.* names (cell 12's second half).
	lsDims: {Column: "vector_dims('[1,2]'::vector)::text", ReadOnly: true, Scan: func(r *lsRow) any { return &r.Dims }},
	// text scanned into an int: a client-side scan error, the server's
	// transaction untouched
	lsBad: {Column: "'x'::text", ReadOnly: true, Scan: func(r *lsRow) any { return &r.ID }},
	// a division by zero: an error the server reports, aborting the transaction
	lsZero: {Column: "(1 / (id - id))::text", ReadOnly: true, Scan: func(r *lsRow) any { return &r.Dims }},
}

type lsSchema = dao.Schema[*lsRow, lsField, string, int]

// lsSetup makes the one-row table (and pgvector's extension) on a connection
// of its own, then opens the cell's pool of one connection, not yet used.
func lsSetup(t *testing.T) (*pgxConn, *lsSchema) {
	t.Helper()
	admin := openPG(t)
	for _, q := range []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`DROP TABLE IF EXISTS ` + lsTable,
		`CREATE TABLE ` + lsTable + ` (id int PRIMARY KEY)`,
		`INSERT INTO ` + lsTable + ` VALUES (1)`,
	} {
		if _, err := admin.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+lsTable) })
	conn := openPG(t, MaxOpenConns(1))
	s := dao.New[*lsRow, lsField, string, int](conn,
		dao.Table[*lsRow, lsField, string, int](lsTable),
		dao.ID[*lsRow, lsField, string, int](lsID),
		dao.Fields[*lsRow, lsField, string, int](lsFields),
		dao.Default[*lsRow, lsField, string, int](lsID, lsPlan, lsIter, lsEf),
	)
	return conn, s
}

// read answers the one row, through d.
func read(t *testing.T, d dao.DAO[*lsRow, lsField, int], cols ...lsField) *lsRow {
	t.Helper()
	r, err := d.Get(cols...)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return r
}

// rewrite replaces the verb's statement (never the option's own) with sql:
// how a cell sends SAVEPOINT, SET LOCAL or EXPLAIN on the dao transaction's
// own connection, which products cannot do and the cells must.
type rewrite struct {
	dao.NopHook
	sql string
}

func (r rewrite) BeforeExec(_ context.Context, q *dao.QueryInfo) error {
	if q.Op == dao.OpSelect {
		q.SQL, q.Args = r.sql, nil
	}
	return nil
}

func raw(t *testing.T, s *lsSchema, tx *dao.Transaction, sql string) {
	t.Helper()
	if _, err := s.On(tx, dao.WithHooks(rewrite{sql: sql})).Select(lsID); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func begin(t *testing.T) *dao.Transaction {
	t.Helper()
	tx := dao.Begin(context.Background())
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

// opHook records each BeforeExec and AfterExec with its op, and the SQL and
// arguments of the option's two statements; failRestore fails the restore's
// BeforeExec.
type opHook struct {
	dao.NopHook
	log         *[]string
	sql         map[dao.Op]string
	args        map[dao.Op][]any
	failRestore bool
}

var errRestoreRefused = errors.New("opHook: restore refused")

func newOpHook() *opHook {
	return &opHook{log: new([]string), sql: map[dao.Op]string{}, args: map[dao.Op][]any{}}
}

func (h *opHook) BeforeExec(_ context.Context, q *dao.QueryInfo) error {
	*h.log = append(*h.log, "before "+string(q.Op))
	h.sql[q.Op], h.args[q.Op] = q.SQL, slices.Clone(q.Args)
	if h.failRestore && q.Op == dao.OpLocalRestore {
		return errRestoreRefused
	}
	return nil
}

func (h *opHook) AfterExec(_ context.Context, q *dao.QueryInfo, out dao.Outcome) error {
	*h.log = append(*h.log, "after "+string(q.Op))
	return out.Err
}

// Cell 1. On a fresh connection (pgvector's names still placeholders the server
// has not defined), the statement sees its settings.
func TestLocalSettings_Live1_TheStatementSeesItsSettings(t *testing.T) {
	_, s := lsSetup(t)
	tx := begin(t)
	if got := read(t, s.On(tx)); got.Ef != "<none>" {
		t.Fatalf("hnsw.ef_search before = %q; want <none>, the cell needs a fresh connection", got.Ef)
	}
	got := read(t, s.On(tx, dao.WithLocalSettings(HNSWEfSearch(123), PlanCacheMode("force_custom_plan"))))
	if got.Ef != "123" || got.Plan != "force_custom_plan" {
		t.Fatalf("in the statement: ef_search %q, plan_cache_mode %q; want 123, force_custom_plan", got.Ef, got.Plan)
	}
}

// Cells 2 and 4. Commit ends them, and so does rollback; the next transaction
// on the connection, and the next pool user of it, read what they read before.
// pgvector's library is loaded first, so hnsw.ef_search is a defined name with
// its default: a placeholder that set_config creates stays defined, as "",
// for the rest of the session, which no transaction undoes.
func TestLocalSettings_Live2_TheTransactionEndsThem(t *testing.T) {
	for _, commit := range []bool{true, false} {
		t.Run(map[bool]string{true: "commit", false: "rollback"}[commit], func(t *testing.T) {
			conn, s := lsSetup(t)
			if _, err := conn.ExecContext(context.Background(), `LOAD 'vector'`); err != nil {
				t.Fatalf("LOAD 'vector': %v", err)
			}
			before := read(t, s.DAO())
			if before.Ef != "40" {
				t.Fatalf("hnsw.ef_search before = %q; want pgvector's default, 40", before.Ef)
			}
			tx := dao.Begin(context.Background())
			got := read(t, s.On(tx, dao.WithLocalSettings(PlanCacheMode("force_generic_plan"), HNSWEfSearch(321))))
			if got.Plan != "force_generic_plan" || got.Ef != "321" {
				t.Fatalf("in the statement: %+v", got)
			}
			end := tx.Rollback
			if commit {
				end = tx.Commit
			}
			if err := end(); err != nil {
				t.Fatalf("end: %v", err)
			}
			tx2 := begin(t)
			if after := read(t, s.On(tx2)); after.Plan != before.Plan || after.Ef != before.Ef {
				t.Fatalf("the next transaction reads %+v; want %+v", after, before)
			}
			_ = tx2.Rollback()
			if after := read(t, s.DAO()); after.Plan != before.Plan || after.Ef != before.Ef {
				t.Fatalf("the next pool user reads %+v; want %+v", after, before)
			}
		})
	}
}

// Cell 3. A rolled-back savepoint reverts them, the restore having failed.
func TestLocalSettings_Live3_ARolledBackSavepointRevertsThem(t *testing.T) {
	_, s := lsSetup(t)
	tx := begin(t)
	before := read(t, s.On(tx))
	raw(t, s, tx, "SAVEPOINT ls3")
	h := newOpHook()
	h.failRestore = true
	if _, err := s.On(tx, dao.WithHooks(h), dao.WithLocalSettings(PlanCacheMode("force_generic_plan"))).Get(); !errors.Is(err, errRestoreRefused) {
		t.Fatalf("Get = %v; want the refused restore", err)
	}
	if mid := read(t, s.On(tx)); mid.Plan != "force_generic_plan" {
		t.Fatalf("with no restore, plan_cache_mode = %q; want the setting still in force (the cell needs it)", mid.Plan)
	}
	raw(t, s, tx, "ROLLBACK TO SAVEPOINT ls3")
	if after := read(t, s.On(tx)); after.Plan != before.Plan {
		t.Fatalf("after the savepoint's rollback plan_cache_mode = %q; want %q", after.Plan, before.Plan)
	}
}

// Cell 5. Refusals send nothing, and answer their sentinels.
func TestLocalSettings_Live5_RefusalsSendNothing(t *testing.T) {
	_, s := lsSetup(t)
	for _, tc := range []struct {
		name string
		set  dao.LocalSetting
		pool bool
		want error
	}{
		{"role", dao.LocalSetting{Name: "role", Value: "postgres"}, false, errs.ErrInvalidArgument},
		{"ef_search 0", HNSWEfSearch(0), false, errs.ErrInvalidArgument},
		{"ef_search 1001", HNSWEfSearch(1001), false, errs.ErrInvalidArgument},
		{"iterative_scan fast", HNSWIterativeScan("fast"), false, errs.ErrInvalidArgument},
		{"a pool DAO", HNSWEfSearch(10), true, dao.ErrLocalSettingsNeedTx},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newOpHook()
			d := s.DAO(dao.WithHooks(h), dao.WithLocalSettings(tc.set))
			if !tc.pool {
				d = s.On(begin(t), dao.WithHooks(h), dao.WithLocalSettings(tc.set))
			}
			if _, err := d.Get(); !errors.Is(err, tc.want) {
				t.Fatalf("Get = %v; want %v", err, tc.want)
			}
			if len(*h.log) != 0 {
				t.Fatalf("sent %q; want nothing", *h.log)
			}
		})
	}
}

// Cell 6. Bound, not spliced: the two statements' text holds placeholders
// only, every name and value in their arguments.
func TestLocalSettings_Live6_BoundNotSpliced(t *testing.T) {
	_, s := lsSetup(t)
	h := newOpHook()
	read(t, s.On(begin(t), dao.WithHooks(h), dao.WithLocalSettings(HNSWEfSearch(123), PlanCacheMode("force_custom_plan"))))
	for _, op := range []dao.Op{dao.OpLocalSettings, dao.OpLocalRestore} {
		for _, lit := range []string{"hnsw", "ef_search", "123", "plan_cache_mode", "force_custom_plan", "'"} {
			if strings.Contains(h.sql[op], lit) {
				t.Errorf("%s SQL %q holds %q", op, h.sql[op], lit)
			}
		}
	}
	if got, want := h.args[dao.OpLocalSettings], []any{"hnsw.ef_search", "123", "plan_cache_mode", "force_custom_plan"}; !slices.Equal(got, want) {
		t.Errorf("settings args = %v; want %v", got, want)
	}
	args := h.args[dao.OpLocalRestore]
	if len(args) != 4 || args[0] != "hnsw.ef_search" || args[1] != nil || args[2] != "plan_cache_mode" || args[3] == "force_custom_plan" {
		t.Errorf("restore args = %v; want the names with their previous values (ef_search none)", args)
	}
}

// Cell 7. dao's bounds are the server's: min_val and max_val for each number,
// enumvals for each enum, from pg_settings once pgvector's library is loaded.
func TestLocalSettings_Live7_TheBoundsAreTheServers(t *testing.T) {
	conn, _ := lsSetup(t)
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `LOAD 'vector'`); err != nil {
		t.Fatalf("LOAD 'vector': %v", err)
	}
	for name, dom := range localSettings {
		rows, err := conn.pool.Query(ctx, `SELECT vartype, coalesce(min_val, ''), coalesce(max_val, ''), coalesce(enumvals, '{}') FROM pg_settings WHERE name = $1`, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var vartype, minv, maxv string
		var enum []string
		found := rows.Next()
		if found {
			err = rows.Scan(&vartype, &minv, &maxv, &enum)
		}
		rows.Close()
		if !found || err != nil {
			t.Fatalf("%s: not in pg_settings (%v)", name, err)
		}
		if dom.enum != nil {
			if got, want := slices.Sorted(slices.Values(dom.enum)), slices.Sorted(slices.Values(enum)); !slices.Equal(got, want) {
				t.Errorf("%s: dao's members %v; the server's %v", name, got, want)
			}
			continue
		}
		if want := map[bool]string{true: "integer", false: "real"}[dom.integer]; vartype != want {
			t.Errorf("%s: the server's type %s; dao's %s", name, vartype, want)
		}
		smin, _ := strconv.ParseFloat(minv, 64)
		smax, _ := strconv.ParseFloat(maxv, 64)
		if dom.min != smin || dom.max != smax {
			t.Errorf("%s: dao's bounds %v to %v; the server's %s to %s", name, dom.min, dom.max, minv, maxv)
		}
	}
}

// explainRow is EXPLAIN's one text column.
type explainRow struct{ Line string }

// Cell 8. Prepared execution with a generic baseline: with the session at
// force_generic_plan, a prepared statement executed under
// PlanCacheMode("force_custom_plan") plans with its bound value, and without
// the option it does not.
func TestLocalSettings_Live8_ACustomPlanUnderAGenericBaseline(t *testing.T) {
	conn, _ := lsSetup(t)
	ctx := context.Background()
	for _, q := range []string{
		`SET plan_cache_mode = force_generic_plan`,
		`PREPARE ls8(int) AS SELECT id FROM ` + lsTable + ` WHERE id = $1`,
	} {
		if _, err := conn.pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	ex := dao.New[*explainRow, string, string, int](conn,
		dao.Table[*explainRow, string, string, int](lsTable),
		dao.Fields[*explainRow, string, string, int](map[string]dao.Field[*explainRow]{
			"line": {Column: "id::text", Scan: func(r *explainRow) any { return &r.Line }},
		}))
	plan := func(opts ...dao.QueryOption) string {
		t.Helper()
		tx := begin(t)
		rows, err := ex.On(tx, append(opts, dao.WithHooks(rewrite{sql: `EXPLAIN (COSTS OFF) EXECUTE ls8(1)`}))...).Select()
		if err != nil {
			t.Fatalf("EXPLAIN: %v", err)
		}
		_ = tx.Rollback()
		var lines []string
		for _, r := range rows {
			lines = append(lines, r.Line)
		}
		return strings.Join(lines, "\n")
	}
	if generic := plan(); !strings.Contains(generic, "$1") {
		t.Fatalf("without the option the plan is not generic, so the cell cannot tell:\n%s", generic)
	}
	if custom := plan(dao.WithLocalSettings(PlanCacheMode("force_custom_plan"))); strings.Contains(custom, "$1") || !strings.Contains(custom, "= 1") {
		t.Fatalf("under the option the plan is not custom:\n%s", custom)
	}
}

// Cell 9, live. Hooks see every statement, in the order sent: for Select, and
// for Iterate with its restore at the rows' close.
func TestLocalSettings_Live9_HooksSeeEveryStatementInOrder(t *testing.T) {
	_, s := lsSetup(t)
	pairs := func(op dao.Op) []string {
		return []string{"before local-settings", "after local-settings",
			"before " + string(op), "after " + string(op), "before local-restore", "after local-restore"}
	}
	// One transaction at a time: the pool has one connection.
	h := newOpHook()
	tx := dao.Begin(context.Background())
	if _, err := s.On(tx, dao.WithHooks(h), dao.WithLocalSettings(HNSWEfSearch(50))).Select(); err != nil {
		t.Fatalf("Select: %v", err)
	}
	_ = tx.Rollback()
	if want := pairs(dao.OpSelect); !slices.Equal(*h.log, want) {
		t.Fatalf("Select: %q; want %q", *h.log, want)
	}
	h = newOpHook()
	tx = dao.Begin(context.Background())
	it, err := s.On(tx, dao.WithHooks(h), dao.WithLocalSettings(HNSWEfSearch(50))).Iterate()
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if want := pairs(dao.OpIterate)[:4]; !slices.Equal(*h.log, want) {
		t.Fatalf("Iterate, rows open: %q; want %q", *h.log, want)
	}
	if err := it.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = tx.Rollback()
	if want := pairs(dao.OpIterate); !slices.Equal(*h.log, want) {
		t.Fatalf("Iterate, rows closed: %q; want %q", *h.log, want)
	}
	h = newOpHook()
	if _, err := s.On(begin(t), dao.WithHooks(h)).Select(); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if want := []string{"before select", "after select"}; !slices.Equal(*h.log, want) {
		t.Fatalf("without the option: %q; want %q", *h.log, want)
	}
}

// lsTxValues opens a transaction on a connection with pgvector's library loaded
// (so iterative_scan and ef_search have their defaults) and sets the
// transaction's own plan_cache_mode and ef_search, as an earlier statement
// would.
func lsTxValues(t *testing.T) (*lsSchema, *dao.Transaction) {
	t.Helper()
	conn, s := lsSetup(t)
	if _, err := conn.ExecContext(context.Background(), `LOAD 'vector'`); err != nil {
		t.Fatalf("LOAD 'vector': %v", err)
	}
	tx := begin(t)
	raw(t, s, tx, "SET LOCAL plan_cache_mode = force_generic_plan")
	raw(t, s, tx, "SET LOCAL hnsw.ef_search = 77")
	return s, tx
}

var lsOption = dao.WithLocalSettings(HNSWIterativeScan("relaxed_order"), PlanCacheMode("force_custom_plan"), HNSWEfSearch(400))

func wantTxValues(t *testing.T, got *lsRow) {
	t.Helper()
	if got.Iter != "off" || got.Plan != "force_generic_plan" || got.Ef != "77" {
		t.Fatalf("after the statement: iterative_scan %q, plan_cache_mode %q, ef_search %q; want off, force_generic_plan, 77 (the transaction's own)",
			got.Iter, got.Plan, got.Ef)
	}
}

// Cell 10. A later statement of the transaction sees the transaction's values,
// not the option's; a released savepoint around the statement changes nothing.
func TestLocalSettings_Live10_ALaterStatementSeesTheTransactionsValues(t *testing.T) {
	for _, savepoint := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "released savepoint"}[savepoint], func(t *testing.T) {
			s, tx := lsTxValues(t)
			if savepoint {
				raw(t, s, tx, "SAVEPOINT ls10")
			}
			got := read(t, s.On(tx, lsOption))
			if got.Iter != "relaxed_order" || got.Plan != "force_custom_plan" || got.Ef != "400" {
				t.Fatalf("in the statement: %+v", got)
			}
			if savepoint {
				raw(t, s, tx, "RELEASE SAVEPOINT ls10")
			}
			wantTxValues(t, read(t, s.On(tx)))
		})
	}
}

// Cell 11. The same after Iterate, once its rows have closed: at the
// iteration's end, and at an early Close.
func TestLocalSettings_Live11_TheSameAfterIterate(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "end", true: "early close"}[early], func(t *testing.T) {
			s, tx := lsTxValues(t)
			if _, err := s.On(tx).Set(lsID, 2).Insert(); err != nil {
				t.Fatalf("a second row: %v", err)
			}
			it, err := s.On(tx, lsOption).Iterate()
			if err != nil {
				t.Fatalf("Iterate: %v", err)
			}
			n := 0
			for it.Next() {
				n++
				if r := it.Value(); r.Ef != "400" {
					t.Fatalf("in the statement ef_search = %q; want 400", r.Ef)
				}
				if early {
					break
				}
			}
			if err := it.Err(); err != nil {
				t.Fatalf("Err: %v", err)
			}
			if err := it.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if want := map[bool]int{false: 2, true: 1}[early]; n != want {
				t.Fatalf("read %d rows; want %d", n, want)
			}
			wantTxValues(t, read(t, s.On(tx)))
		})
	}
}

// Cell 12. A name with no previous value is reset: never the option's value,
// but the one it would have with no SET in the transaction, which is pgvector's
// default once the statement has loaded the library.
func TestLocalSettings_Live12_ANameWithNoPreviousValueIsReset(t *testing.T) {
	t.Run("a placeholder", func(t *testing.T) {
		_, s := lsSetup(t)
		tx := dao.Begin(context.Background())
		if got := read(t, s.On(tx, dao.WithLocalSettings(HNSWEfSearch(123)))); got.Ef != "123" {
			t.Fatalf("in the statement ef_search = %q; want 123", got.Ef)
		}
		after := read(t, s.On(tx)).Ef
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if after == "123" {
			t.Fatal("after the statement ef_search is still 123")
		}
		if next := read(t, s.On(begin(t))).Ef; after != next {
			t.Fatalf("after the statement ef_search = %q; the next transaction, with no SET, reads %q", after, next)
		}
	})
	t.Run("pgvector loaded by the statement", func(t *testing.T) {
		_, s := lsSetup(t)
		tx := begin(t)
		if got := read(t, s.On(tx), lsID, lsEf); got.Ef != "<none>" {
			t.Fatalf("before: ef_search = %q; want <none>, the cell needs pgvector not yet loaded", got.Ef)
		}
		if got := read(t, s.On(tx, dao.WithLocalSettings(HNSWEfSearch(123))), lsID, lsEf, lsDims); got.Ef != "123" || got.Dims != "2" {
			t.Fatalf("in the statement: ef_search %q, dims %q; want 123, 2", got.Ef, got.Dims)
		}
		if after := read(t, s.On(tx), lsID, lsEf).Ef; after != "40" {
			t.Fatalf("after the statement ef_search = %q; want 40, pgvector's default", after)
		}
	})
}

// Cell 13. A failed restore fails the verb with its error and no result; the
// caller rolls back, and no later statement sees the settings.
func TestLocalSettings_Live13_AFailedRestoreFailsTheVerb(t *testing.T) {
	_, s := lsSetup(t)
	before := read(t, s.DAO())
	tx := dao.Begin(context.Background())
	h := newOpHook()
	h.failRestore = true
	rows, err := s.On(tx, dao.WithHooks(h), dao.WithLocalSettings(PlanCacheMode("force_generic_plan"))).Select()
	if !errors.Is(err, errRestoreRefused) || rows != nil {
		t.Fatalf("Select = %v, %v; want no rows and the refused restore", rows, err)
	}
	_ = tx.Rollback()
	if after := read(t, s.On(begin(t))); after.Plan != before.Plan {
		t.Fatalf("after the rollback plan_cache_mode = %q; want %q", after.Plan, before.Plan)
	}
}

// Cell 14. A client-side failure is restored: a destination of the wrong type
// fails Get's scan, and Iterate's, while the server's transaction goes on, so
// a later statement of it sees the transaction's own values.
func TestLocalSettings_Live14_AClientSideFailureIsRestored(t *testing.T) {
	t.Run("Get", func(t *testing.T) {
		s, tx := lsTxValues(t)
		if _, err := s.On(tx, lsOption).Get(lsID, lsBad); err == nil {
			t.Fatal("Get into an int from text: no error; the cell needs a scan error")
		}
		wantTxValues(t, read(t, s.On(tx)))
	})
	t.Run("Iterate", func(t *testing.T) {
		s, tx := lsTxValues(t)
		it, err := s.On(tx, lsOption).Iterate(lsID, lsBad)
		if err != nil {
			t.Fatalf("Iterate: %v", err)
		}
		for it.Next() {
		}
		if it.Err() == nil {
			t.Fatal("Iterate into an int from text: no error; the cell needs a scan error")
		}
		if err := it.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		wantTxValues(t, read(t, s.On(tx)))
	})
}

// Cell 15. After an error the server reports, nothing more is sent: the
// transaction is aborted, which ends the settings, and a restore could only
// fail.
func TestLocalSettings_Live15_AServerErrorSendsNoRestore(t *testing.T) {
	s, tx := lsTxValues(t)
	h := newOpHook()
	if _, err := s.On(tx, dao.WithHooks(h), lsOption).Get(lsID, lsZero); err == nil {
		t.Fatal("a division by zero: no error; the cell needs the server's")
	}
	if slices.Contains(*h.log, "before local-restore") {
		t.Fatalf("a restore was sent after the server's error: %q", *h.log)
	}
}

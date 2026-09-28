package deploy_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/dao/deploy"
	"github.com/yongjohnlee80/golib/dao/mysql"
	"github.com/yongjohnlee80/golib/dao/postgres"
	"github.com/yongjohnlee80/golib/dao/sqlite"
	"github.com/yongjohnlee80/golib/errs"
)

func openSQLite(t *testing.T) dao.DataConn {
	t.Helper()
	c, err := sqlite.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "s.db"), sqlite.MaxOpenConns(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func tables(t *testing.T, c dao.DataConn) string {
	t.Helper()
	ts, err := dao.ListTables(context.Background(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range ts {
		out = append(out, x.Name)
	}
	return fmt.Sprint(out)
}

func scripts(engine string, files map[string]string) fstest.MapFS {
	fs := fstest.MapFS{}
	for name, body := range files {
		fs[engine+"/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fs
}

var two = map[string]string{
	"000001_update_initialize_tables.sql": "CREATE TABLE IF NOT EXISTS a (x INTEGER);\n-- a trigger's body is one statement\nCREATE TRIGGER IF NOT EXISTS a_ins AFTER INSERT ON a BEGIN SELECT 1; SELECT 2; END;",
	"000002_update_add_b.sql":             "CREATE TABLE b (y INTEGER);",
	"000002_revert_add_b.sql":             "DROP TABLE b;",
}

func TestApply_RunsThePendingScriptsOnceAndRecordsThem(t *testing.T) {
	ctx := context.Background()
	c := openSQLite(t)
	r := deploy.New(scripts("sqlite", two))
	st, err := r.Pending(ctx, c)
	if err != nil || fmt.Sprint(st.Pending) != "[000001_update_initialize_tables.sql 000002_update_add_b.sql]" {
		t.Fatalf("Pending on a new database = %+v, %v", st, err)
	}
	if got := tables(t, c); got != "[]" {
		t.Fatalf("Pending changed the database: %s", got)
	}
	st, err = r.Apply(ctx, c)
	if err != nil || fmt.Sprint(st.Pending) != "[000001_update_initialize_tables.sql 000002_update_add_b.sql]" {
		t.Fatalf("Apply = %+v, %v", st, err)
	}
	if got := tables(t, c); got != "[a b schema_version]" {
		t.Errorf("tables after Apply = %s", got)
	}
	st, err = r.Apply(ctx, c)
	if err != nil || len(st.Pending) != 0 || len(st.Applied) != 2 {
		t.Errorf("a second Apply = %+v, %v; want nothing pending, two applied", st, err)
	}
}

func TestApply_AFailureLeavesTheDatabaseAsItWas(t *testing.T) {
	ctx := context.Background()
	c := openSQLite(t)
	bad := map[string]string{
		"000001_update_initialize_tables.sql": "CREATE TABLE a (x INTEGER);",
		"000002_update_add_b.sql":             "CREATE TABLE b (y INTEGER);\nCREATE TABLE b (y INTEGER);",
		"000002_revert_add_b.sql":             "DROP TABLE b;",
	}
	_, err := deploy.New(scripts("sqlite", bad)).Apply(ctx, c)
	if err == nil || !strings.Contains(err.Error(), "000002_update_add_b.sql, line 2") {
		t.Fatalf("Apply = %v, want the failure named with its script and line", err)
	}
	if got := tables(t, c); got != "[]" {
		t.Errorf("after a failed Apply the database has %s: the scripts before the failure, and the ledger, must roll back with it", got)
	}
}

func TestApply_TheDowngradeGuardAndTheDigestWarning(t *testing.T) {
	ctx := context.Background()
	c := openSQLite(t)
	if _, err := deploy.New(scripts("sqlite", two)).Apply(ctx, c); err != nil {
		t.Fatal(err)
	}
	older := map[string]string{"000001_update_initialize_tables.sql": two["000001_update_initialize_tables.sql"]}
	if _, err := deploy.New(scripts("sqlite", older)).Apply(ctx, c); !errors.Is(err, deploy.ErrDowngrade) || !errors.Is(err, errs.ErrPrecondition) {
		t.Errorf("an older binary's Apply = %v, want ErrDowngrade", err)
	}
	if _, err := deploy.New(scripts("sqlite", older)).Pending(ctx, c); !errors.Is(err, deploy.ErrDowngrade) {
		t.Errorf("an older binary's Pending = %v, want ErrDowngrade", err)
	}
	edited := map[string]string{}
	for k, v := range two {
		edited[k] = v
	}
	edited["000002_update_add_b.sql"] = "CREATE TABLE b (y INTEGER, z INTEGER);"
	st, err := deploy.New(scripts("sqlite", edited)).Apply(ctx, c)
	if err != nil || len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], "000002_update_add_b.sql") || len(st.Pending) != 0 {
		t.Errorf("an edited released script: %+v, %v; want one warning naming it and nothing re-run", st, err)
	}
}

func TestRevert_OnlyTheLatestAndNeverTheBaseline(t *testing.T) {
	ctx := context.Background()
	c := openSQLite(t)
	r := deploy.New(scripts("sqlite", two))
	if _, err := r.Revert(ctx, c, 2); !errors.Is(err, deploy.ErrNotLatest) {
		t.Errorf("a revert before anything is applied = %v, want ErrNotLatest", err)
	}
	if got := tables(t, c); got != "[]" {
		t.Errorf("a refused revert left %s", got)
	}
	if _, err := r.Apply(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Revert(ctx, c, 1); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("reverting the baseline = %v, want ErrInvalidArgument", err)
	}
	if _, err := r.Revert(ctx, c, 9); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("reverting a script that does not exist = %v, want ErrInvalidArgument", err)
	}
	name, err := r.Revert(ctx, c, 2)
	if err != nil || name != "000002_revert_add_b.sql" {
		t.Fatalf("Revert(2) = %q, %v", name, err)
	}
	if got := tables(t, c); got != "[a schema_version]" {
		t.Errorf("after the revert: %s", got)
	}
	st, err := r.Pending(ctx, c)
	if err != nil || fmt.Sprint(st.Pending) != "[000002_update_add_b.sql]" {
		t.Errorf("after the revert, Pending = %+v, %v; want 000002 again", st, err)
	}
}

func TestLoad_OrderAndRefusals(t *testing.T) {
	all, err := deploy.Load(scripts("sqlite", two), "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range all {
		names = append(names, s.Name)
	}
	if got := fmt.Sprint(names); got != "[000001_update_initialize_tables.sql 000002_update_add_b.sql 000002_revert_add_b.sql]" {
		t.Errorf("order = %s", got)
	}
	stmts, err := all[0].Statements()
	if err != nil || len(stmts) != 2 || stmts[1].Line != 3 {
		t.Errorf("the baseline's statements = %+v, %v; want two, the trigger on line 3", stmts, err)
	}
	if _, err := deploy.Load(scripts("sqlite", map[string]string{"2_update_x.sql": "SELECT 1;"}), "sqlite"); err == nil || !strings.Contains(err.Error(), "2_update_x.sql") {
		t.Errorf("a misnamed file: %v", err)
	}
	if _, err := deploy.Load(scripts("sqlite", two), "postgres"); err == nil {
		t.Error("an engine with no directory: want an error")
	}
	if _, err := deploy.Load(scripts("oracle", two), "oracle"); !errors.Is(err, dao.ErrUnsupported) {
		t.Errorf("an engine without lexical rules: %v, want ErrUnsupported", err)
	}
	open := scripts("sqlite", map[string]string{"000001_update_initialize_tables.sql": "SELECT 'unclosed;"})
	s, _ := deploy.Load(open, "sqlite")
	if _, err := s[0].Statements(); err == nil || !strings.Contains(err.Error(), "000001_update_initialize_tables.sql") {
		t.Errorf("an unclosed string: %v, want the script named", err)
	}
}

// fakeMySQL is a MySQL connection that never connects: the refusal comes first.
type fakeMySQL struct{ dao.DataConn }

func (fakeMySQL) Dialect() dao.Dialect { return mysql.MysqlDialect{} }

func TestApply_AnEngineWithoutTransactionalDDLIsRefused(t *testing.T) {
	r := deploy.New(scripts("mysql", map[string]string{"000001_update_initialize_tables.sql": "CREATE TABLE a (x INT);"}))
	for name, f := range map[string]func() error{
		"Apply":   func() error { _, err := r.Apply(context.Background(), fakeMySQL{}); return err },
		"Pending": func() error { _, err := r.Pending(context.Background(), fakeMySQL{}); return err },
		"Revert":  func() error { _, err := r.Revert(context.Background(), fakeMySQL{}, 1); return err },
	} {
		if err := f(); !errors.Is(err, dao.ErrUnsupported) {
			t.Errorf("%s on MySQL = %v, want ErrUnsupported: MySQL commits DDL as it runs", name, err)
		}
	}
}

// Two processes applying at once take turns on the lock: each script runs
// once, and neither fails. Skips when TEST_PGURL is unset.
func TestApply_ConcurrentAppliesOnPostgresTakeTurns(t *testing.T) {
	url := os.Getenv("TEST_PGURL")
	if url == "" {
		t.Skip("TEST_PGURL not set; skipping postgres integration test")
	}
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	ledger := "golib_deploy_ledger_" + suffix
	a, b := "golib_deploy_a_"+suffix, "golib_deploy_b_"+suffix
	files := map[string]string{
		"000001_update_initialize_tables.sql": "CREATE TABLE " + a + " (x INTEGER);\nCREATE FUNCTION " + a + "_f() RETURNS integer AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql;",
		"000002_update_add_b.sql":             "CREATE TABLE " + b + " (y INTEGER);",
		"000002_revert_add_b.sql":             "DROP TABLE " + b + ";",
	}
	var conns []dao.DataConn
	for i := range 2 {
		c, err := postgres.OpenNamed(ctx, fmt.Sprintf("deploy-%d", i), url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		conns = append(conns, c)
	}
	t.Cleanup(func() {
		_, _ = conns[0].ExecContext(context.Background(), "DROP TABLE IF EXISTS "+b+"; DROP FUNCTION IF EXISTS "+a+"_f(); DROP TABLE IF EXISTS "+a+"; DROP TABLE IF EXISTS "+ledger)
	})
	r := deploy.New(scripts("postgres", files), deploy.Ledger(ledger))
	var wg sync.WaitGroup
	sts := make([]deploy.Status, 2)
	errc := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); sts[i], errc[i] = r.Apply(ctx, conns[i]) }()
	}
	wg.Wait()
	if errc[0] != nil || errc[1] != nil {
		t.Fatalf("concurrent Apply: %v, %v; want both to succeed, one after the other", errc[0], errc[1])
	}
	if n := len(sts[0].Pending) + len(sts[1].Pending); n != 2 {
		t.Errorf("the two applies ran %d scripts between them (%v, %v); want 2, each once", n, sts[0].Pending, sts[1].Pending)
	}
	if name, err := r.Revert(ctx, conns[0], 2); err != nil || name != "000002_revert_add_b.sql" {
		t.Errorf("Revert on Postgres = %q, %v", name, err)
	}
}

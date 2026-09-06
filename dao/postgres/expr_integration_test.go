//go:build integration

// Executed coverage for the expression helpers on Postgres — the wide
// net. Everything here runs real SQL against a real server, so it proves not
// just that the generated text is shaped correctly but that Postgres accepts it
// and returns the right rows.
//
// Requires a reachable Postgres. Set TEST_PGURL and run:
//
//	TEST_PGURL='postgres://user:pass@localhost:5432/db?sslmode=disable' \
//	  go test -tags integration ./dao/postgres/
//
// With TEST_PGURL unset every test here skips (openTP handles the gate). Each
// test owns its objects and drops them on the way in, so a crashed earlier run
// cannot poison a later one.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// --- entity: a reserved-word table with a reserved-word column --------------

type xuser struct {
	ID    int
	Order int
	Name  string
	Group string
	MyCol string
}

type xfield string

const (
	xID    xfield = "id"
	xOrder xfield = "order" // reserved
	xName  xfield = "name"
	xGrpID xfield = "grp_id"
	xGroup xfield = "group_name"
	xMyCol xfield = "MyCol" // created quoted, so case-sensitive
)

type xsort string

const (
	xTblUser = "golib_dao_user" // plain
	xTblRsvd = "user"           // RESERVED: unquoted, this is not addressable
	xTblGrp  = "golib_dao_group"
	xSchema  = "golib_dao_app"
)

func exprDDL(schema string) string {
	return fmt.Sprintf(`
DROP SCHEMA IF EXISTS %[1]s CASCADE;
CREATE SCHEMA %[1]s;
DROP TABLE IF EXISTS "user" CASCADE;
DROP TABLE IF EXISTS golib_dao_group CASCADE;
CREATE TABLE golib_dao_group (
	id   serial PRIMARY KEY,
	name text NOT NULL
);
CREATE TABLE "user" (
	id      serial PRIMARY KEY,
	"order" int  NOT NULL DEFAULT 0,
	name    text NOT NULL UNIQUE,
	grp_id  int REFERENCES golib_dao_group(id),
	"MyCol" text
);
CREATE TABLE %[1]s.golib_dao_user (
	id   serial PRIMARY KEY,
	name text NOT NULL UNIQUE
);`, schema)
}

func exprSetup(t *testing.T) dao.DataConn {
	t.Helper()
	conn := openTP(t, "pg-expr") // skips when TEST_PGURL is unset
	mustExecTP(t, conn, exprDDL(xSchema))
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(),
			fmt.Sprintf(`DROP TABLE IF EXISTS "user" CASCADE; DROP TABLE IF EXISTS golib_dao_group CASCADE; DROP SCHEMA IF EXISTS %s CASCADE;`, xSchema))
	})
	return conn
}

func rsvdSchema(t *testing.T, conn dao.DataConn) *dao.Schema[*xuser, xfield, xsort, int] {
	t.Helper()
	fields := map[xfield]dao.Field[*xuser]{
		xID:    {Expr: dao.T(xTblRsvd, xID), Scan: func(u *xuser) any { return &u.ID }},
		xOrder: {Expr: dao.T(xTblRsvd, xOrder), Scan: func(u *xuser) any { return &u.Order }, Value: func(u *xuser) any { return u.Order }},
		xName:  {Expr: dao.T(xTblRsvd, xName), Scan: func(u *xuser) any { return &u.Name }, Value: func(u *xuser) any { return u.Name }},
		xGrpID: {Expr: dao.T(xTblRsvd, xGrpID), Value: func(u *xuser) any { return nil }},
		xMyCol: {Expr: dao.T(xTblRsvd, xMyCol), Scan: func(u *xuser) any { return &u.MyCol }, Value: func(u *xuser) any { return u.MyCol }},
		xGroup: {
			Expr:     dao.Coalesce(dao.T(xTblGrp, xName), "«none»"),
			Join:     dao.JoinKey(xTblGrp),
			ReadOnly: true,
			Scan:     func(u *xuser) any { return &u.Group },
		},
	}
	return dao.New(conn,
		dao.Table[*xuser, xfield, xsort, int](xTblRsvd),
		dao.ID[*xuser, xfield, xsort, int](xID),
		dao.Fields[*xuser, xfield, xsort, int](fields),
		dao.Default[*xuser, xfield, xsort, int](xID, xOrder, xName),
		dao.Conflict[*xuser, xfield, xsort, int](xName),
		dao.OptionalJoinExpr[*xuser, xfield, xsort, int](dao.JoinKey(xTblGrp),
			dao.LeftJoin(xTblGrp, dao.T(xTblGrp, xID), dao.T(xTblRsvd, xGrpID))),
	)
}

// TestExprPG_ReservedWordCRUD is the motivating case: a table and a column whose
// names are SQL reserved words. Unquoted they are not addressable at all, so this
// exercises the whole verb surface through dao.T-declared columns.
func TestExprPG_ReservedWordCRUD(t *testing.T) {
	conn := exprSetup(t)
	s := rsvdSchema(t, conn)

	id, err := s.DAO().Set(xName, "ada").Set(xOrder, 3).Insert()
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id == 0 {
		t.Error("RETURNING id produced 0")
	}

	got, err := s.DAO().With(xID, id).Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "ada" || got.Order != 3 {
		t.Errorf("got %+v, want ada/3", got)
	}

	// Predicates on reserved columns: equality, IN, negation, range.
	if n, err := s.DAO().With(xOrder, 3).Count(); err != nil || n != 1 {
		t.Errorf("With(order) count = %d err=%v", n, err)
	}
	if n, err := s.DAO().With(xOrder, 3, 4, 5).Count(); err != nil || n != 1 {
		t.Errorf("IN count = %d err=%v", n, err)
	}
	if n, err := s.DAO().Excluding(xOrder, 3).Count(); err != nil || n != 0 {
		t.Errorf("Excluding count = %d err=%v", n, err)
	}
	// NOTE: the raw predicate constructors take a raw column
	// STRING, and expression helpers were deliberately left out of predicate
	// position. For a reserved word that means the caller must quote it by hand
	// — dao.Between(string(xOrder), …) emits `order BETWEEN …` and Postgres
	// rejects it. With() and Excluding() above are unaffected because they
	// resolve through the schema's declared column.
	if ok, err := s.DAO().WithPredicate(dao.Between(`"user"."order"`, 1, 5)).Exists(); err != nil || !ok {
		t.Errorf("Between exists = %v err=%v", ok, err)
	}

	if err := s.DAO().With(xID, id).Set(xOrder, 9).Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, err = s.DAO().With(xID, id).Get(); err != nil || got.Order != 9 {
		t.Errorf("after Update: %+v err=%v", got, err)
	}

	// Upsert on a dao.T-declared conflict column.
	if err := s.DAO().Set(xName, "ada").Set(xOrder, 42).Upsert(); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got, err = s.DAO().With(xName, "ada").Get(); err != nil || got.Order != 42 {
		t.Errorf("after Upsert: %+v err=%v", got, err)
	}
	if n, _ := s.DAO().Count(); n != 1 {
		t.Errorf("upsert inserted a duplicate: count = %d", n)
	}

	if err := s.DAO().With(xID, id).Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.DAO().With(xID, id).Get(); !errors.Is(err, dao.ErrNoRows) {
		t.Errorf("after Delete err = %v, want ErrNoRows", err)
	}
}

// TestExprPG_CoalesceOverJoin proves the Coalesce fallback and the join clause
// both work against the server, including the NULL path.
func TestExprPG_CoalesceOverJoin(t *testing.T) {
	conn := exprSetup(t)
	s := rsvdSchema(t, conn)

	mustExecTP(t, conn, `INSERT INTO golib_dao_group (id, name) VALUES (1, 'alpha');
		INSERT INTO "user" (name, grp_id) VALUES ('has', 1), ('none', NULL);`)

	rows, err := s.DAO().Select(xID, xName, xGroup)
	if err != nil {
		t.Fatalf("Select with join: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	byName := map[string]string{}
	for _, r := range rows {
		byName[r.Name] = r.Group
	}
	if byName["has"] != "alpha" {
		t.Errorf("joined value = %q, want alpha", byName["has"])
	}
	if byName["none"] != "«none»" {
		t.Errorf("COALESCE fallback = %q, want «none»", byName["none"])
	}

	// Filtering on a joined column for a WRITE needs the join forced. Field.Join's
	// doc comment claims a filter triggers it ("selected, ordered-by, or filtered
	// on"), but Update passes collectJoins(nil), so only DAO.Join or a sort does —
	// which is what DAO.Join's own doc says ("when only filtering on a joined
	// table"). The two doc comments contradict each other; the behavior below is
	// the real contract, and the id-subselect path it drives is what makes a
	// joined filter work at all for UPDATE.
	if err := s.DAO().Join(dao.JoinKey(xTblGrp)).With(xGroup, "alpha").
		Set(xOrder, 7).Update(); err != nil {
		t.Fatalf("Update filtered by a joined column: %v", err)
	}
	got, err := s.DAO().With(xName, "has").Get()
	if err != nil || got.Order != 7 {
		t.Errorf("joined-filter update: %+v err=%v", got, err)
	}
}

// TestExprPG_SchemaQualifiedTable proves the table part goes through the
// TableQuoter capability end to end: "golib_dao_app"."golib_dao_user".
func TestExprPG_SchemaQualifiedTable(t *testing.T) {
	conn := exprSetup(t)

	qualified := xSchema + "." + xTblUser
	fields := map[xfield]dao.Field[*xuser]{
		xID:   {Expr: dao.T(qualified, xID), Scan: func(u *xuser) any { return &u.ID }},
		xName: {Expr: dao.T(qualified, xName), Scan: func(u *xuser) any { return &u.Name }, Value: func(u *xuser) any { return u.Name }},
	}
	s := dao.New(conn,
		dao.Table[*xuser, xfield, xsort, int](qualified),
		dao.ID[*xuser, xfield, xsort, int](xID),
		dao.Fields[*xuser, xfield, xsort, int](fields),
		dao.Default[*xuser, xfield, xsort, int](xID, xName),
	)

	id, err := s.DAO().Set(xName, "in-schema").Insert()
	if err != nil {
		t.Fatalf("Insert into a schema-qualified table: %v", err)
	}
	got, err := s.DAO().With(xID, id).Get()
	if err != nil || got.Name != "in-schema" {
		t.Errorf("round trip: %+v err=%v", got, err)
	}
}

// TestExprPG_MixedCaseColumn pins case-sensitive quoting against a real
// server: "MyCol"
// was created quoted, so the quoted reference dao.T emits is the one that finds
// it — and the folded, unquoted spelling genuinely does not exist.
func TestExprPG_MixedCaseColumn(t *testing.T) {
	conn := exprSetup(t)
	s := rsvdSchema(t, conn)

	if _, err := s.DAO().Set(xName, "mc").Set(xMyCol, "v").Insert(); err != nil {
		t.Fatalf("Insert with a mixed-case column: %v", err)
	}
	got, err := s.DAO().With(xName, "mc").Get(xID, xName, xMyCol)
	if err != nil || got.MyCol != "v" {
		t.Errorf("mixed-case round trip: %+v err=%v", got, err)
	}

	// The folded spelling must NOT resolve — that is exactly why quoting is not
	// semantically neutral and why SQL() is documented as the escape hatch.
	var probe int
	rows, err := conn.QueryContext(context.Background(), `SELECT mycol FROM "user" LIMIT 1`)
	if err == nil {
		if rows.Next() {
			_ = rows.Scan(&probe)
		}
		_ = rows.Close()
		t.Error("unquoted mycol resolved; the §2.4 hazard note would be wrong")
	}
}

// TestExprPG_BatchAndCopy drives the batch writer (and the COPY fast path) with
// dao.T-declared columns, which is the other consumer of the derived write
// column.
func TestExprPG_BatchAndCopy(t *testing.T) {
	conn := exprSetup(t)
	s := rsvdSchema(t, conn)

	b := s.DAO().Batch()
	for i := range 5 {
		b.AddRow(&xuser{Name: fmt.Sprintf("b%d", i), Order: i})
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("batch Flush: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 5 {
		t.Errorf("count after batch = %d, want 5", n)
	}

	// Conflict handling on a dao.T-declared conflict column, named explicitly.
	b2 := s.DAO().Batch().OnConflictUpdate(xName)
	b2.AddRow(&xuser{Name: "b1", Order: 99})
	if err := b2.Flush(); err != nil {
		t.Fatalf("batch upsert: %v", err)
	}
	got, err := s.DAO().With(xName, "b1").Get()
	if err != nil || got.Order != 99 {
		t.Errorf("batch upsert did not update: %+v err=%v", got, err)
	}
	if n, _ := s.DAO().Count(); n != 5 {
		t.Errorf("batch upsert inserted a duplicate: count = %d", n)
	}

	// The no-argument form must resolve to the schema's declared Conflict(xName)
	// and upsert just the same — against a real unique index, which is the only
	// place a missing conflict target actually shows up.
	b3 := s.DAO().Batch().OnConflictUpdate()
	b3.AddRow(&xuser{Name: "b2", Order: 123})
	if err := b3.Flush(); err != nil {
		t.Fatalf("no-arg OnConflictUpdate against the declared target: %v", err)
	}
	if got, err = s.DAO().With(xName, "b2").Get(); err != nil || got.Order != 123 {
		t.Errorf("no-arg upsert did not update: %+v err=%v", got, err)
	}
	if n, _ := s.DAO().Count(); n != 5 {
		t.Errorf("no-arg upsert inserted a duplicate: count = %d", n)
	}
}

// TestExprPG_LiteralsAndEscapeHatch covers Str/Int/SQL against the server.
func TestExprPG_LiteralsAndEscapeHatch(t *testing.T) {
	conn := exprSetup(t)

	type lrow struct {
		ID  int
		Str string
		Num int
		Now string
	}
	type lfield string
	const (
		lID  lfield = "id"
		lStr lfield = "s"
		lNum lfield = "n"
		lNow lfield = "now"
	)
	type lsort string

	s := dao.New[*lrow, lfield, lsort, int](conn,
		dao.Table[*lrow, lfield, lsort, int](xTblRsvd),
		dao.ID[*lrow, lfield, lsort, int](lID),
		dao.Fields[*lrow, lfield, lsort, int](map[lfield]dao.Field[*lrow]{
			lID:  {Expr: dao.T(xTblRsvd, "id"), Scan: func(r *lrow) any { return &r.ID }},
			lStr: {Expr: dao.Coalesce(dao.T(xTblRsvd, "MyCol"), "fallback"), ReadOnly: true, Scan: func(r *lrow) any { return &r.Str }},
			lNum: {Expr: dao.Coalesce(dao.T(xTblRsvd, "order"), 0), ReadOnly: true, Scan: func(r *lrow) any { return &r.Num }},
			lNow: {Expr: dao.SQL("to_char(now(), 'YYYY')"), ReadOnly: true, Scan: func(r *lrow) any { return &r.Now }},
		}),
		dao.Default[*lrow, lfield, lsort, int](lID, lStr, lNum, lNow),
	)

	mustExecTP(t, conn, `INSERT INTO "user" (name, "order", "MyCol") VALUES ('lit', 5, NULL);`)
	rows, err := s.DAO().Select()
	if err != nil {
		t.Fatalf("Select with literals: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].Str != "fallback" {
		t.Errorf("Str fallback = %q, want fallback", rows[0].Str)
	}
	if rows[0].Num != 5 {
		t.Errorf("Int coalesce over a non-null = %d, want 5", rows[0].Num)
	}
	if len(rows[0].Now) != 4 {
		t.Errorf("SQL escape hatch returned %q, want a 4-digit year", rows[0].Now)
	}
}

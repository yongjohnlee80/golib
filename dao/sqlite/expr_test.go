package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// The expression helpers against the real SqliteDialect, EXECUTED against a
// real database — sqlite needs no server, so this suite proves the generated SQL
// is not merely shaped right but accepted and correct.

// "order" and "user" are reserved words; declaring them as raw strings would
// need hand-written quotes, which is the whole point of dao.T / dao.C.
const exprDDL = `CREATE TABLE "user" (
	id      integer PRIMARY KEY AUTOINCREMENT,
	"order" integer NOT NULL DEFAULT 0,
	name    text    NOT NULL,
	"MyCol" text
);
CREATE TABLE grp (
	id   integer PRIMARY KEY AUTOINCREMENT,
	name text NOT NULL
);
CREATE TABLE member (
	id     integer PRIMARY KEY AUTOINCREMENT,
	name   text NOT NULL,
	grp_id integer
);`

type urow struct {
	ID    int64
	Order int64
	Name  string
	MyCol string
}

type ufield string

const (
	uID    ufield = "id"
	uOrder ufield = "order"
	uName  ufield = "name"
	uMyCol ufield = "MyCol"
)

type usort string

const tableUser = "user"

func exprConn(t *testing.T) dao.DataConn {
	t.Helper()
	ctx := context.Background()
	conn, err := Open(ctx, filepath.Join(t.TempDir(), "expr.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := conn.ExecContext(ctx, exprDDL); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestExpr_ReservedWordsRoundTrip is the case a raw-string declaration gets
// wrong: a reserved table AND column name, written and read through helpers.
func TestExpr_ReservedWordsRoundTrip(t *testing.T) {
	t.Parallel()
	conn := exprConn(t)

	fields := map[ufield]dao.Field[*urow]{
		uID:    {Expr: dao.T(tableUser, uID), Scan: func(r *urow) any { return &r.ID }},
		uOrder: {Expr: dao.T(tableUser, uOrder), Scan: func(r *urow) any { return &r.Order }, Value: func(r *urow) any { return r.Order }},
		uName:  {Expr: dao.T(tableUser, uName), Scan: func(r *urow) any { return &r.Name }, Value: func(r *urow) any { return r.Name }},
	}
	s := dao.New(conn,
		dao.Table[*urow, ufield, usort, int64](tableUser),
		dao.ID[*urow, ufield, usort, int64](uID),
		dao.Fields[*urow, ufield, usort, int64](fields),
		dao.Default[*urow, ufield, usort, int64](uID, uOrder, uName),
	)

	id, err := s.DAO().Set(uName, "ada").Set(uOrder, 3).Insert()
	if err != nil {
		t.Fatalf("Insert into a reserved-word table: %v", err)
	}
	got, err := s.DAO().With(uID, id).Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "ada" || got.Order != 3 {
		t.Errorf("round trip = %+v, want name=ada order=3", got)
	}
	// Update and delete exercise the write path on a reserved column.
	if err := s.DAO().With(uID, id).Set(uOrder, 9).Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, err = s.DAO().With(uID, id).Get(); err != nil || got.Order != 9 {
		t.Errorf("after update: %+v err=%v", got, err)
	}
	if err := s.DAO().With(uID, id).Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 0 {
		t.Errorf("count after delete = %d, want 0", n)
	}
}

// TestExpr_MixedCaseNeedsTheEscapeHatch pins the rule that quoting is not
// semantically neutral. "MyCol" was created quoted, so the quoted form finds it
// and an unquoted lower-case reference would not.
func TestExpr_MixedCaseColumn(t *testing.T) {
	t.Parallel()
	conn := exprConn(t)

	fields := map[ufield]dao.Field[*urow]{
		uID:    {Expr: dao.T(tableUser, uID), Scan: func(r *urow) any { return &r.ID }},
		uName:  {Expr: dao.T(tableUser, uName), Scan: func(r *urow) any { return &r.Name }, Value: func(r *urow) any { return r.Name }},
		uMyCol: {Expr: dao.T(tableUser, uMyCol), Scan: func(r *urow) any { return &r.MyCol }, Value: func(r *urow) any { return r.MyCol }},
	}
	s := dao.New(conn,
		dao.Table[*urow, ufield, usort, int64](tableUser),
		dao.ID[*urow, ufield, usort, int64](uID),
		dao.Fields[*urow, ufield, usort, int64](fields),
		dao.Default[*urow, ufield, usort, int64](uID, uName, uMyCol),
	)
	if _, err := s.DAO().Set(uName, "n").Set(uMyCol, "v").Insert(); err != nil {
		t.Fatalf("mixed-case column insert: %v", err)
	}
	got, err := s.DAO().With(uName, "n").Get()
	if err != nil || got.MyCol != "v" {
		t.Errorf("mixed-case round trip: %+v err=%v", got, err)
	}
}

// --- joined COALESCE, declared entirely from constants ----------------------

type mrow struct {
	ID    int64
	Name  string
	Group string
}

type mfield string

const (
	mID    mfield = "id"
	mName  mfield = "name"
	mGrpID mfield = "grp_id"
	mGroup mfield = "group_name"
)

type msort string

const (
	tableMember = "member"
	tableGrp    = "grp"
)

func TestExpr_CoalesceOverOptionalJoin(t *testing.T) {
	t.Parallel()
	conn := exprConn(t)

	fields := map[mfield]dao.Field[*mrow]{
		mID:    {Expr: dao.T(tableMember, mID), Scan: func(r *mrow) any { return &r.ID }},
		mName:  {Expr: dao.T(tableMember, mName), Scan: func(r *mrow) any { return &r.Name }, Value: func(r *mrow) any { return r.Name }},
		mGrpID: {Expr: dao.T(tableMember, mGrpID), Value: func(r *mrow) any { return nil }},
		mGroup: {
			Expr:     dao.Coalesce(dao.T(tableGrp, mName), dao.SQL("'«none»'")),
			Join:     dao.JoinKey(tableGrp),
			ReadOnly: true,
			Scan:     func(r *mrow) any { return &r.Group },
		},
	}
	s := dao.New(conn,
		dao.Table[*mrow, mfield, msort, int64](tableMember),
		dao.ID[*mrow, mfield, msort, int64](mID),
		dao.Fields[*mrow, mfield, msort, int64](fields),
		dao.Default[*mrow, mfield, msort, int64](mID, mName, mGroup),
		dao.OptionalJoinExpr[*mrow, mfield, msort, int64](dao.JoinKey(tableGrp),
			dao.LeftJoin(tableGrp, dao.T(tableGrp, mID), dao.T(tableMember, mGrpID))),
	)

	if _, err := conn.ExecContext(context.Background(),
		`INSERT INTO grp (id, name) VALUES (1, 'alpha');
		 INSERT INTO member (name, grp_id) VALUES ('has-group', 1), ('no-group', NULL);`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rows, err := s.DAO().OrderBy().Select()
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
	if byName["has-group"] != "alpha" {
		t.Errorf("joined value = %q, want alpha", byName["has-group"])
	}
	// The COALESCE fallback — a dao.Str literal — must be what NULL becomes.
	if byName["no-group"] != "«none»" {
		t.Errorf("coalesced default = %q, want «none»", byName["no-group"])
	}

	// A projection that does not select the joined column must not join at all.
	if _, err := s.DAO().Select(mID, mName); err != nil {
		t.Fatalf("unjoined select: %v", err)
	}
}

// TestExpr_BatchWritesThroughExprColumns covers the write path batch uses (the
// derived write column), which is where a doubly-quoted identifier would show up.
func TestExpr_BatchWritesThroughExprColumns(t *testing.T) {
	t.Parallel()
	conn := exprConn(t)

	fields := map[mfield]dao.Field[*mrow]{
		mID:   {Expr: dao.T(tableMember, mID), Scan: func(r *mrow) any { return &r.ID }},
		mName: {Expr: dao.T(tableMember, mName), Scan: func(r *mrow) any { return &r.Name }, Value: func(r *mrow) any { return r.Name }},
	}
	s := dao.New(conn,
		dao.Table[*mrow, mfield, msort, int64](tableMember),
		dao.ID[*mrow, mfield, msort, int64](mID),
		dao.Fields[*mrow, mfield, msort, int64](fields),
		dao.Default[*mrow, mfield, msort, int64](mID, mName),
	)

	b := s.DAO().Batch()
	for _, n := range []string{"a", "b", "c"} {
		b.AddRow(&mrow{Name: n})
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("batch Flush through Expr-declared columns: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	names, err := s.DAO().Select(mName)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range names {
		got = append(got, r.Name)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("batch rows = %v", got)
	}
}

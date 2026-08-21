package mysql

import (
	"context"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// ADR-0016 expression helpers against the REAL MysqlDialect. MySQL is where a
// hand-quoted declaration goes wrong — ANSI double quotes are string literals
// here, not identifiers — so these assertions are the reason Expr exists.
//
// No server needed: a capturing DataConn records the emitted SQL. The executed
// half is TestExprMy_* below, gated on TEST_MYSQL_DSN like the rest of this
// package.

type capConn struct{ q, e string }

func (c *capConn) QueryContext(_ context.Context, q string, _ ...any) (dao.Rows, error) {
	c.q = q
	return &oneRow{}, nil
}

func (c *capConn) ExecContext(_ context.Context, q string, _ ...any) (dao.Result, error) {
	c.e = q
	return lastIDResult{}, nil
}

func (c *capConn) Dialect() dao.Dialect                      { return MysqlDialect{} }
func (c *capConn) Begin(context.Context) (dao.TxConn, error) { return nil, dao.ErrUnsupported }
func (c *capConn) Name() string                              { return "mysql-cap" }
func (c *capConn) Close() error                              { return nil }

type oneRow struct{ done bool }

func (r *oneRow) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *oneRow) Scan(dest ...any) error {
	for _, d := range dest {
		switch v := d.(type) {
		case *string:
			*v = "1"
		case *int64:
			*v = 1
		case *int:
			*v = 1
		}
	}
	return nil
}

func (r *oneRow) Close() error { return nil }
func (r *oneRow) Err() error   { return nil }

type lastIDResult struct{}

func (lastIDResult) RowsAffected() (int64, error) { return 1, nil }
func (lastIDResult) LastInsertId() (int64, error) { return 7, nil }

type mrow struct {
	ID    int64
	Order int64
	Name  string
	Group string
}

type mfield string

const (
	fID    mfield = "id"
	fOrder mfield = "order" // reserved in MySQL too
	fName  mfield = "name"
	fGrpID mfield = "grp_id"
	fGroup mfield = "group_name"
)

type msort string

const (
	tblUser = "user" // reserved
	tblGrp  = "grp"
)

func exprSchema(t *testing.T, conn dao.DataConn) *dao.Schema[*mrow, mfield, msort, int64] {
	t.Helper()
	return dao.New(conn,
		dao.Table[*mrow, mfield, msort, int64](tblUser),
		dao.ID[*mrow, mfield, msort, int64](fID),
		dao.Fields[*mrow, mfield, msort, int64](map[mfield]dao.Field[*mrow]{
			fID:    {Expr: dao.T(tblUser, fID), Scan: func(r *mrow) any { return &r.ID }},
			fOrder: {Expr: dao.T(tblUser, fOrder), Scan: func(r *mrow) any { return &r.Order }, Value: func(r *mrow) any { return r.Order }},
			fName:  {Expr: dao.T(tblUser, fName), Scan: func(r *mrow) any { return &r.Name }, Value: func(r *mrow) any { return r.Name }},
			fGrpID: {Expr: dao.T(tblUser, fGrpID), Value: func(r *mrow) any { return nil }},
			fGroup: {
				Expr:     dao.Coalesce(dao.T(tblGrp, fName), ""),
				Join:     dao.JoinKey(tblGrp),
				ReadOnly: true,
				Scan:     func(r *mrow) any { return &r.Group },
			},
		}),
		dao.Default[*mrow, mfield, msort, int64](fID, fOrder, fName),
		dao.Conflict[*mrow, mfield, msort, int64](fName),
		dao.OptionalJoinExpr[*mrow, mfield, msort, int64](dao.JoinKey(tblGrp),
			dao.LeftJoin(tblGrp, dao.T(tblGrp, fID), dao.T(tblUser, fGrpID))),
	)
}

func TestExpr_MysqlEmittedSQL(t *testing.T) {
	t.Parallel()

	t.Run("backtick-quoted projection over reserved names", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c)
		if _, err := s.DAO().With(fOrder, 1).Select(); err != nil {
			t.Fatal(err)
		}
		want := "SELECT `user`.`id`, `user`.`order`, `user`.`name` FROM `user` WHERE `user`.`order` = ?"
		if c.q != want {
			t.Errorf("\n got %s\nwant %s", c.q, want)
		}
		// The ANSI form a hand-written declaration would carry is a STRING
		// literal in MySQL, never an identifier: it must not appear.
		if strings.Contains(c.q, `"`) {
			t.Errorf("ANSI double quotes leaked into MySQL SQL: %s", c.q)
		}
	})

	t.Run("write columns are raw, backticked once", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c)
		if _, err := s.DAO().Set(fName, "x").Set(fOrder, 2).Insert(); err != nil {
			t.Fatal(err)
		}
		// MySQL has no RETURNING: the insert goes through Exec + LastInsertId.
		want := "INSERT INTO `user` (`name`, `order`) VALUES (?, ?)"
		if c.e != want {
			t.Errorf("\n got %s\nwant %s", c.e, want)
		}
		if strings.Contains(c.e, "``") {
			t.Errorf("doubly-quoted identifier in DML: %s", c.e)
		}
	})

	t.Run("update and upsert", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c)
		if err := s.DAO().With(fID, 1).Set(fOrder, 3).Update(); err != nil {
			t.Fatal(err)
		}
		if want := "UPDATE `user` SET `order` = ? WHERE `user`.`id` = ?"; c.e != want {
			t.Errorf("\n got %s\nwant %s", c.e, want)
		}
		if err := s.DAO().Set(fName, "x").Upsert(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(c.e, "ON DUPLICATE KEY UPDATE") {
			t.Errorf("MySQL upsert suffix missing: %s", c.e)
		}
	})

	t.Run("coalesce + join clause", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c)
		if _, err := s.DAO().Select(fID, fGroup); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"COALESCE(`grp`.`name`, '')",
			"LEFT JOIN `grp` ON `grp`.`id` = `user`.`grp_id`",
		} {
			if !strings.Contains(c.q, want) {
				t.Errorf("missing %s in %s", want, c.q)
			}
		}
	})

	t.Run("schema-qualified table splits per part", func(t *testing.T) {
		c := &capConn{}
		s := dao.New(c,
			dao.Table[*mrow, mfield, msort, int64]("app.user"),
			dao.ID[*mrow, mfield, msort, int64](fID),
			dao.Fields[*mrow, mfield, msort, int64](map[mfield]dao.Field[*mrow]{
				fID:   {Expr: dao.T("app.user", fID), Scan: func(r *mrow) any { return &r.ID }},
				fName: {Expr: dao.T("app.user", fName), Scan: func(r *mrow) any { return &r.Name }, Value: func(r *mrow) any { return r.Name }},
			}),
			dao.Default[*mrow, mfield, msort, int64](fID, fName),
		)
		if _, err := s.DAO().Select(); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"`app`.`user`.`id`", "FROM `app`.`user`"} {
			if !strings.Contains(c.q, want) {
				t.Errorf("missing %s in %s", want, c.q)
			}
		}
	})
}

// --- executed, gated on TEST_MYSQL_DSN -------------------------------------

func TestExprMy_ReservedWordRoundTrip(t *testing.T) {
	conn := testConn(t) // skips when TEST_MYSQL_DSN is unset
	ctx := context.Background()

	for _, stmt := range []string{
		"DROP TABLE IF EXISTS `user`",
		"CREATE TABLE `user` (id bigint AUTO_INCREMENT PRIMARY KEY, `order` int NOT NULL DEFAULT 0, name varchar(64) NOT NULL UNIQUE)",
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("ddl %q: %v", stmt, err)
		}
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(ctx, "DROP TABLE IF EXISTS `user`") })

	s := dao.New(conn,
		dao.Table[*mrow, mfield, msort, int64](tblUser),
		dao.ID[*mrow, mfield, msort, int64](fID),
		dao.Fields[*mrow, mfield, msort, int64](map[mfield]dao.Field[*mrow]{
			fID:    {Expr: dao.T(tblUser, fID), Scan: func(r *mrow) any { return &r.ID }},
			fOrder: {Expr: dao.T(tblUser, fOrder), Scan: func(r *mrow) any { return &r.Order }, Value: func(r *mrow) any { return r.Order }},
			fName:  {Expr: dao.T(tblUser, fName), Scan: func(r *mrow) any { return &r.Name }, Value: func(r *mrow) any { return r.Name }},
		}),
		dao.Default[*mrow, mfield, msort, int64](fID, fOrder, fName),
		dao.Conflict[*mrow, mfield, msort, int64](fName),
	)

	id, err := s.DAO().Set(fName, "ada").Set(fOrder, 3).Insert()
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := s.DAO().With(fID, id).Get()
	if err != nil || got.Name != "ada" || got.Order != 3 {
		t.Fatalf("round trip: %+v err=%v", got, err)
	}
	if err := s.DAO().With(fID, id).Set(fOrder, 9).Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := s.DAO().Set(fName, "ada").Set(fOrder, 42).Upsert(); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got, err = s.DAO().With(fName, "ada").Get(); err != nil || got.Order != 42 {
		t.Errorf("after upsert: %+v err=%v", got, err)
	}
	if n, _ := s.DAO().Count(); n != 1 {
		t.Errorf("upsert duplicated the row: count = %d", n)
	}
}

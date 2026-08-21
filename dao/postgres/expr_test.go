package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// ADR-0016 expression helpers against the REAL PostgresDialect, with no server:
// a capturing DataConn records the SQL the engine emits, so a change in this
// driver's quoting shows up here rather than in a consumer's logs.
// The executed half lives in expr_integration_test.go.

// --- a capturing DataConn (no server, real dialect) -------------------------

type capConn struct {
	q, e string
}

func (c *capConn) QueryContext(_ context.Context, q string, _ ...any) (dao.Rows, error) {
	c.q = q
	// One row, so a RETURNING scan completes and the verb returns cleanly:
	// this suite asserts the SQL, not the driver.
	return &oneRow{}, nil
}

func (c *capConn) ExecContext(_ context.Context, q string, _ ...any) (dao.Result, error) {
	c.e = q
	return zeroResult{}, nil
}

func (c *capConn) Dialect() dao.Dialect                      { return PostgresDialect{} }
func (c *capConn) Begin(context.Context) (dao.TxConn, error) { return nil, dao.ErrUnsupported }
func (c *capConn) Name() string                              { return "pg-cap" }
func (c *capConn) Close() error                              { return nil }

// oneRow yields exactly one row and scans a placeholder into each destination.
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

type zeroResult struct{}

func (zeroResult) RowsAffected() (int64, error) { return 0, nil }
func (zeroResult) LastInsertId() (int64, error) { return 0, nil }

// --- the entity, declared entirely from constants ---------------------------

type exprRow struct {
	ID, Name, Group string
}

type exprField string

const (
	eID    exprField = "id"
	eName  exprField = "name"
	eGrpID exprField = "grp_id"
	eGroup exprField = "group_name"
)

type exprSort string

const (
	tblArtist = "artist"
	tblGrp    = "label_group"
	tblApp    = "app.artist" // schema-qualified
)

func exprSchema(t *testing.T, conn dao.DataConn, table string) *dao.Schema[*exprRow, exprField, exprSort, string] {
	t.Helper()
	fields := map[exprField]dao.Field[*exprRow]{
		eID:    {Expr: dao.T(table, eID), Scan: func(r *exprRow) any { return &r.ID }},
		eName:  {Expr: dao.T(table, eName), Scan: func(r *exprRow) any { return &r.Name }, Value: func(r *exprRow) any { return r.Name }},
		eGrpID: {Expr: dao.T(table, eGrpID), Value: func(r *exprRow) any { return nil }},
		eGroup: {
			Expr:     dao.Coalesce(dao.T(tblGrp, eName), ""),
			Join:     dao.JoinKey(tblGrp),
			ReadOnly: true,
			Scan:     func(r *exprRow) any { return &r.Group },
		},
	}
	return dao.New(conn,
		dao.Table[*exprRow, exprField, exprSort, string](table),
		dao.ID[*exprRow, exprField, exprSort, string](eID),
		dao.Fields[*exprRow, exprField, exprSort, string](fields),
		dao.Default[*exprRow, exprField, exprSort, string](eID, eName),
		dao.Conflict[*exprRow, exprField, exprSort, string](eName),
		dao.OptionalJoinExpr[*exprRow, exprField, exprSort, string](dao.JoinKey(tblGrp),
			dao.LeftJoin(tblGrp, dao.T(tblGrp, eID), dao.T(table, eGrpID))),
	)
}

func TestExpr_PostgresEmittedSQL(t *testing.T) {
	t.Parallel()

	t.Run("projection quotes every part", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c, tblArtist)
		if _, err := s.DAO().With(eID, "1").Select(); err != nil {
			t.Fatal(err)
		}
		want := `SELECT "artist"."id", "artist"."name" FROM "artist" WHERE "artist"."id" = $1`
		if c.q != want {
			t.Errorf("\n got %s\nwant %s", c.q, want)
		}
	})

	t.Run("write columns are raw, quoted once", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c, tblArtist)
		if _, err := s.DAO().Set(eName, "x").Insert(); err != nil {
			t.Fatal(err)
		}
		want := `INSERT INTO "artist" ("name") VALUES ($1) RETURNING "id"`
		if c.q != want {
			t.Errorf("\n got %s\nwant %s", c.q, want)
		}
		if strings.Contains(c.q, `""`) {
			t.Errorf("doubly-quoted identifier in DML: %s", c.q)
		}
	})

	t.Run("update", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c, tblArtist)
		if err := s.DAO().With(eID, "1").Set(eName, "x").Update(); err != nil {
			t.Fatal(err)
		}
		want := `UPDATE "artist" SET "name" = $1 WHERE "artist"."id" = $2`
		if c.e != want {
			t.Errorf("\n got %s\nwant %s", c.e, want)
		}
	})

	t.Run("upsert on an Expr-declared conflict column", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c, tblArtist)
		if err := s.DAO().Set(eName, "x").Upsert(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(c.e, `ON CONFLICT ("name")`) {
			t.Errorf("conflict target should be the raw column: %s", c.e)
		}
	})

	t.Run("coalesce + join clause", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c, tblArtist)
		if _, err := s.DAO().Select(eID, eGroup); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`COALESCE("label_group"."name", '')`,
			`LEFT JOIN "label_group" ON "label_group"."id" = "artist"."grp_id"`,
		} {
			if !strings.Contains(c.q, want) {
				t.Errorf("missing %s in %s", want, c.q)
			}
		}
	})

	t.Run("schema-qualified table splits per part", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c, tblApp)
		if _, err := s.DAO().Select(eID, eName); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"app"."artist"."id"`, `FROM "app"."artist"`} {
			if !strings.Contains(c.q, want) {
				t.Errorf("missing %s in %s", want, c.q)
			}
		}
	})

	t.Run("no join when nothing triggers it", func(t *testing.T) {
		c := &capConn{}
		s := exprSchema(t, c, tblArtist)
		if _, err := s.DAO().Select(eID); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(c.q, "JOIN") {
			t.Errorf("join applied without a trigger: %s", c.q)
		}
	})
}

// Reserved words are the case a raw string literal gets wrong; Postgres folds
// unquoted identifiers, so "user" MUST be quoted to be addressable at all.
func TestExpr_PostgresReservedWords(t *testing.T) {
	t.Parallel()

	type rrow struct{ ID, Order string }
	type rfield string
	const (
		rID    rfield = "id"
		rOrder rfield = "order"
	)
	type rsort string

	c := &capConn{}
	s := dao.New[*rrow, rfield, rsort, string](c,
		dao.Table[*rrow, rfield, rsort, string]("user"),
		dao.ID[*rrow, rfield, rsort, string](rID),
		dao.Fields[*rrow, rfield, rsort, string](map[rfield]dao.Field[*rrow]{
			rID:    {Expr: dao.T("user", rID), Scan: func(r *rrow) any { return &r.ID }},
			rOrder: {Expr: dao.T("user", rOrder), Scan: func(r *rrow) any { return &r.Order }, Value: func(r *rrow) any { return r.Order }},
		}),
		dao.Default[*rrow, rfield, rsort, string](rID, rOrder),
	)
	if _, err := s.DAO().With(rOrder, "1").Select(); err != nil {
		t.Fatal(err)
	}
	want := `SELECT "user"."id", "user"."order" FROM "user" WHERE "user"."order" = $1`
	if c.q != want {
		t.Errorf("\n got %s\nwant %s", c.q, want)
	}
	if err := s.DAO().With(rID, "1").Set(rOrder, "2").Update(); err != nil {
		t.Fatal(err)
	}
	if want := `UPDATE "user" SET "order" = $1 WHERE "user"."id" = $2`; c.e != want {
		t.Errorf("\n got %s\nwant %s", c.e, want)
	}
}

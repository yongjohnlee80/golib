package dao

import (
	"errors"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// wrapped is a DAO from outside this package: it has DAO's methods and none
// of the optional capabilities, as a caller's decorator would.
type wrapped struct {
	DAO[*artist, artistField, string]
}

func TestCmp_ComparesTwoColumnsAndBindsNothing(t *testing.T) {
	t.Parallel()
	conn := newConn()
	_, _ = buildSchema(conn).DAO().
		With(aName, "x").
		WithPredicate(Cmp(T("artist", "plays"), OpLte, T("label_group", "cap"))).
		WithPredicate(Gt("artist.plays", 3)).
		Select(aID)
	want := `SELECT artist.id FROM "artist" WHERE artist.name = $1 AND "artist"."plays" <= "label_group"."cap" AND artist.plays > $2`
	if conn.lastQuery != want {
		t.Errorf("sql = %q\nwant  %q", conn.lastQuery, want)
	}
	if !reflect.DeepEqual(conn.lastArgs, []any{"x", 3}) {
		t.Errorf("args = %v, want [x 3]: Cmp binds nothing and the numbering runs on", conn.lastArgs)
	}
}

func TestCmp_EveryOperatorAndItsRefusals(t *testing.T) {
	t.Parallel()
	for op, want := range map[CmpOp]string{OpEq: "=", OpNe: "<>", OpLt: "<", OpLte: "<=", OpGt: ">", OpGte: ">="} {
		n := 0
		if got, args := Cmp(C("a"), op, Int(1)).ToSQL(returningDialect{}, &n); got != `"a" `+want+` 1` || args != nil || n != 0 {
			t.Errorf("Cmp(%q) = %q, %v, counter %d", op, got, args, n)
		}
	}
	for name, f := range map[string]func(){
		"an unknown operator": func() { Cmp(C("a"), CmpOp("= 1 OR 1 ="), C("b")) },
		"a zero left side":    func() { Cmp(Expr{}, OpEq, C("b")) },
		"a zero right side":   func() { Cmp(C("a"), OpEq, Expr{}) },
	} {
		func() {
			defer func() {
				var fatal errs.Fatal
				r := recover()
				if err, _ := r.(error); !errors.As(err, &fatal) {
					t.Errorf("%s: recovered %v, want an errs.Fatal", name, r)
				}
			}()
			f()
		}()
	}
}

func TestIncr_AddsToTheColumnInAnUpdate(t *testing.T) {
	t.Parallel()
	conn := newConn()
	if err := buildSchema(conn).DAO().With(aID, "1").Set(aName, "x").Set(aPublic, Incr(2)).Update(); err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "artist" SET "name" = $1, "public" = "public" + $2 WHERE artist.id = $3`
	if conn.lastExec != want {
		t.Errorf("sql = %q\nwant  %q", conn.lastExec, want)
	}
	if !reflect.DeepEqual(conn.lastEArgs, []any{"x", 2, "1"}) {
		t.Errorf("args = %v: the increment is bound, not rendered", conn.lastEArgs)
	}
	// through a DAO from outside the package too: it is a value, staged by Set
	conn = newConn()
	var d DAO[*artist, artistField, string] = wrapped{buildSchema(conn).DAO()}
	if err := d.With(aID, "1").Set(aPublic, Incr(1)).Update(); err != nil || conn.lastExec != `UPDATE "artist" SET "public" = "public" + $1 WHERE artist.id = $2` {
		t.Errorf("through a wrapper: %v, %q", err, conn.lastExec)
	}
}

func TestIncr_ANewRowIsRefused(t *testing.T) {
	t.Parallel()
	conn := newConn()
	if _, err := buildSchema(conn).DAO().Set(aName, "x").Set(aPublic, Incr(1)).Insert(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Insert: %v, want ErrInvalidArgument", err)
	}
	if err := buildSchema(conn).DAO().Set(aURI, "u").Set(aPublic, Incr(1)).Upsert(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Upsert: %v, want ErrInvalidArgument", err)
	}
	if err := buildSchema(conn).DAO().Batch().Add(map[artistField]any{aURI: "u", aPublic: Incr(1)}).Flush(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("a batch: %v, want ErrInvalidArgument", err)
	}
	if conn.lastExec != "" || conn.lastQuery != "" {
		t.Errorf("a refused write reached the database: %q %q", conn.lastExec, conn.lastQuery)
	}
}

func TestUpsertOnly_UpdatesOnlyTheNamedFields(t *testing.T) {
	t.Parallel()
	conn := newConn()
	d := buildSchema(conn).DAO().Set(aURI, "u").Set(aName, "n").Set(aPublic, true)
	if err := UpsertOnly(d, aName); err != nil {
		t.Fatal(err)
	}
	want := `INSERT INTO "artist" ("name", "public", "uri") VALUES ($1, $2, $3) ON CONFLICT ("uri") DO UPDATE SET "name" = EXCLUDED."name"`
	if conn.lastExec != want {
		t.Errorf("sql = %q\nwant  %q", conn.lastExec, want)
	}
	conn = newConn()
	if err := UpsertOnly(buildSchema(conn).DAO().Set(aURI, "u").Set(aName, "n")); err != nil {
		t.Fatal(err)
	}
	if want := `INSERT INTO "artist" ("name", "uri") VALUES ($1, $2) ON CONFLICT ("uri") DO NOTHING`; conn.lastExec != want {
		t.Errorf("no fields: sql = %q, want the row kept: %q", conn.lastExec, want)
	}
	conn = newConn()
	if err := buildSchema(conn).DAO().Set(aURI, "u").Set(aName, "n").Set(aPublic, true).Upsert(); err != nil ||
		conn.lastExec != `INSERT INTO "artist" ("name", "public", "uri") VALUES ($1, $2, $3) ON CONFLICT ("uri") DO UPDATE SET "name" = EXCLUDED."name", "public" = EXCLUDED."public"` {
		t.Errorf("a plain Upsert changed: %v, %q", err, conn.lastExec)
	}
}

func TestUpsertOnly_Refusals(t *testing.T) {
	t.Parallel()
	conn := newConn()
	if err := UpsertOnly(buildSchema(conn).DAO().Set(aURI, "u").Set(aName, "n"), aPublic); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("an unstaged field: %v, want ErrInvalidArgument", err)
	}
	if err := UpsertOnly(buildSchema(conn).DAO().Set(aURI, "u"), artistField("nope")); !errors.Is(err, ErrUnknownField) {
		t.Errorf("an unknown field: %v, want ErrUnknownField", err)
	}
	if err := UpsertOnly(buildSchema(conn).DAO().Set(aURI, "u"), aLabelGroup); !errors.Is(err, ErrReadOnlyField) {
		t.Errorf("a read-only field: %v, want ErrReadOnlyField", err)
	}
	if conn.lastExec != "" {
		t.Errorf("a refused upsert reached the database: %q", conn.lastExec)
	}
	if err := UpsertOnly[*artist, artistField, string](wrapped{buildSchema(conn).DAO()}, aName); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a DAO without the capability: %v, want ErrUnsupported", err)
	}
}

func TestSelectDistinct(t *testing.T) {
	t.Parallel()
	conn := newConn()
	if _, err := SelectDistinct(buildSchema(conn).DAO().With(aPublic, true).OrderBy(Asc(aSortName)), aName); err != nil {
		t.Fatal(err)
	}
	if want := `SELECT DISTINCT artist.name FROM "artist" WHERE artist.public = $1 ORDER BY artist.name ASC`; conn.lastQuery != want {
		t.Errorf("sql = %q\nwant  %q", conn.lastQuery, want)
	}
	conn = newConn()
	if _, err := buildSchema(conn).DAO().Select(aName); err != nil || conn.lastQuery != `SELECT artist.name FROM "artist"` {
		t.Errorf("a plain Select changed: %v, %q", err, conn.lastQuery)
	}
	if _, err := SelectDistinct[*artist, artistField, string](wrapped{buildSchema(conn).DAO()}, aName); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a DAO without the capability: %v, want ErrUnsupported", err)
	}
}

func TestCountDistinct(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{uint64(4)}}}
	n, err := CountDistinct(buildSchema(conn).DAO().With(aPublic, true).Limit(1), aName)
	if err != nil || n != 4 {
		t.Fatalf("CountDistinct = %d, %v", n, err)
	}
	if want := `SELECT COUNT(DISTINCT artist.name) FROM "artist" WHERE artist.public = $1`; conn.lastQuery != want {
		t.Errorf("sql = %q\nwant  %q (Limit ignored)", conn.lastQuery, want)
	}
	conn = newConn()
	conn.rows = &fakeRows{data: [][]any{{uint64(1)}}}
	if _, err := CountDistinct(buildSchema(conn).DAO(), aLabelGroup); err != nil ||
		conn.lastQuery != `SELECT COUNT(DISTINCT COALESCE(label_group.name,'')) FROM "artist" LEFT JOIN label_group ON label_group.id = artist.label_group_id` {
		t.Errorf("a joined field brings its join: %v, %q", err, conn.lastQuery)
	}
	if _, err := CountDistinct(buildSchema(conn).DAO(), artistField("nope")); !errors.Is(err, ErrUnknownField) {
		t.Errorf("an unknown field: %v, want ErrUnknownField", err)
	}
	if _, err := CountDistinct[*artist, artistField, string](wrapped{buildSchema(conn).DAO()}, aName); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a DAO without the capability: %v, want ErrUnsupported", err)
	}
}

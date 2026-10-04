package dao

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// questionDialect numbers nothing: every placeholder is "?", as SQLite's and
// MySQL's are, so the arguments' order alone carries the binding.
type questionDialect struct{ returningDialect }

func (questionDialect) Placeholder(int) string { return "?" }

// distance is a one-value bound expression that renders a marker, so a test
// reads where its placeholder landed.
func distance(name string) ParamExpr {
	return ParamExpr{arity: 1, render: func(_ Dialect, ph func(any) string, args []any) string {
		return name + "(" + ph(args[0]) + ")"
	}}
}

// needing is distance with a need: the engine is refused with err.
func needing(err error) ParamExpr {
	pe := distance("d")
	pe.needs = func(Dialect) error { return err }
	return pe
}

func sortParam(key artistSort, e ParamExpr) Option[*artist, artistField, artistSort, string] {
	return SortParam[*artist, artistField, artistSort, string](key, e)
}

func TestBoundSort_NumbersAfterWhereAndBeforeLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		d    Dialect
		want string
	}{
		{"numbered", returningDialect{}, `SELECT artist.id FROM "artist" WHERE artist.name = $1 AND artist.public = $2 ORDER BY near($3) ASC, artist.name DESC LIMIT $4 OFFSET $5`},
		{"question marks", questionDialect{}, `SELECT artist.id FROM "artist" WHERE artist.name = ? AND artist.public = ? ORDER BY near(?) ASC, artist.name DESC LIMIT ? OFFSET ?`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &fakeConn{d: tc.d}
			_, err := buildSchema(conn, sortParam("near", distance("near"))).DAO().
				With(aName, "x").With(aPublic, true).
				OrderBy(AscBy("near", "vec"), Desc(aSortName)).Limit(10).Offset(20).Select(aID)
			if err != nil {
				t.Fatal(err)
			}
			if conn.lastQuery != tc.want {
				t.Errorf("sql\n got: %s\nwant: %s", conn.lastQuery, tc.want)
			}
			if want := []any{"x", true, "vec", int64(10), int64(20)}; !reflect.DeepEqual(conn.lastArgs, want) {
				t.Errorf("args = %v, want %v", conn.lastArgs, want)
			}
		})
	}
}

func TestBoundSort_TwoTermsBindInOrder(t *testing.T) {
	t.Parallel()
	conn := newConn()
	_, err := buildSchema(conn, sortParam("a", distance("a")), sortParam("b", distance("b"))).DAO().
		OrderBy(DescBy("b", 2), AscBy("a", 1)).Select(aID)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id FROM "artist" ORDER BY b($1) DESC, a($2) ASC`; conn.lastQuery != want {
		t.Errorf("sql = %s, want %s", conn.lastQuery, want)
	}
	if !reflect.DeepEqual(conn.lastArgs, []any{2, 1}) {
		t.Errorf("args = %v, want [2 1]", conn.lastArgs)
	}
}

func TestBoundSort_EveryReadPathBinds(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.rows = &fakeRows{data: [][]any{{"1"}}}
	s := buildSchema(conn, sortParam("near", distance("near")))
	if _, err := s.DAO().With(aName, "x").OrderBy(AscBy("near", "v")).Get(aID); err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id FROM "artist" WHERE artist.name = $1 ORDER BY near($2) ASC LIMIT $3`; conn.lastQuery != want {
		t.Errorf("Get: %s", conn.lastQuery)
	}
	it, err := s.DAO().With(aName, "x").OrderBy(AscBy("near", "v")).Iterate(aID)
	if err != nil {
		t.Fatal(err)
	}
	it.Close()
	if want := `SELECT artist.id FROM "artist" WHERE artist.name = $1 ORDER BY near($2) ASC`; conn.lastQuery != want {
		t.Errorf("Iterate: %s", conn.lastQuery)
	}
	if !reflect.DeepEqual(conn.lastArgs, []any{"x", "v"}) {
		t.Errorf("Iterate args = %v", conn.lastArgs)
	}
}

func TestBoundSort_WrongValueCountIsInvalid(t *testing.T) {
	t.Parallel()
	for name, srt := range map[string]Sort{
		"bound key with Asc":      Asc("near"),
		"bound key, two values":   AscBy("near", 1, 2),
		"plain key with AscBy":    AscBy(aSortName, 1),
		"plain key with DescBy":   DescBy(aSortName, "x"),
		"bound key, no AscBy arg": AscBy("near"),
	} {
		conn := newConn()
		_, err := buildSchema(conn, sortParam("near", distance("near"))).DAO().OrderBy(srt).Select(aID)
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%s: %v, want ErrInvalidArgument", name, err)
		}
		if conn.lastQuery != "" {
			t.Errorf("%s: the query reached the database: %s", name, conn.lastQuery)
		}
	}
}

func TestBoundSort_SortStaysComparable(t *testing.T) {
	t.Parallel()
	if Asc("k") != (Sort{Key: "k"}) || Desc("k") != (Sort{Key: "k", Desc: true}) {
		t.Error("Asc/Desc no longer equal their literal")
	}
	if got := ParseSorts("-a", "b"); got[0] != (Sort{Key: "a", Desc: true}) || got[1] != (Sort{Key: "b"}) {
		t.Errorf("ParseSorts = %v", got)
	}
	if AscBy("k", 1) == Asc("k") {
		t.Error("a bound Sort compares equal to a plain one")
	}
}

func TestBoundSort_AnEngineWithoutItFailsTheQueryNotNew(t *testing.T) {
	t.Parallel()
	conn := newConn()
	s := buildSchema(conn, sortParam("near", needing(fmt.Errorf("%w: vectors on test", ErrUnsupported))))
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panicked: %v", r)
			}
		}()
		if _, err := s.DAO().OrderBy(AscBy("near", "v")).Select(aID); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%v, want ErrUnsupported", err)
		}
	}()
	if conn.lastQuery != "" {
		t.Errorf("the query reached the database: %s", conn.lastQuery)
	}
	// Other keys of the same schema still work.
	if _, err := s.DAO().OrderBy(Asc(aSortName)).Select(aID); err != nil {
		t.Errorf("a plain key on the same schema: %v", err)
	}
}

func TestBoundSort_AnInvalidDeclarationPanicsAtNew(t *testing.T) {
	t.Parallel()
	mustFatal(t, "an invalid declaration", func() {
		buildSchema(newConn(), sortParam("near", needing(errs.Wrap(errs.ErrInvalidArgument, "bad config"))))
	})
	mustFatal(t, "a zero ParamExpr", func() { sortParam("near", ParamExpr{}) })
}

func TestBoundSort_WinsOverAPlainKeyAndTriggersItsJoin(t *testing.T) {
	t.Parallel()
	conn := newConn()
	_, err := buildSchema(conn,
		sortParam(aSortName, distance("near")),
		JoinForSort[*artist, artistField, artistSort, string](aSortName, "label_group"),
	).DAO().OrderBy(AscBy(aSortName, "v")).Select(aID)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id FROM "artist" LEFT JOIN label_group ON label_group.id = artist.label_group_id ORDER BY near($1) ASC`; conn.lastQuery != want {
		t.Errorf("sql = %s", conn.lastQuery)
	}
}

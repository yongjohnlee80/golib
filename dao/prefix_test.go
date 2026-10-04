package dao

import (
	"errors"
	"reflect"
	"testing"
)

// engineDialect is the test dialect under an engine's name, for predicates that
// are supported per engine.
type engineDialect struct {
	returningDialect
	name string
}

func (d engineDialect) Name() string { return d.name }

func TestHasPrefix_RendersAnEscapedPattern(t *testing.T) {
	t.Parallel()
	for _, name := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		conn := &fakeConn{d: engineDialect{name: name}}
		if _, err := buildSchema(conn).DAO().WithPredicate(HasPrefix("path", `a!b%c_d\e/`)).With(aName, "x").Select(aID); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := `SELECT artist.id FROM "artist" WHERE path LIKE $1 ESCAPE '!' AND artist.name = $2`; conn.lastQuery != want {
			t.Errorf("%s: %s", name, conn.lastQuery)
		}
		if want := []any{`a!!b!%c!_d\e/%`, "x"}; !reflect.DeepEqual(conn.lastArgs, want) {
			t.Errorf("%s: args %q, want %q", name, conn.lastArgs, want)
		}
	}
}

func TestHasPrefix_OtherEnginesAreUnsupported(t *testing.T) {
	t.Parallel()
	for _, name := range []string{DialectBigQuery, DialectGeneric, "oracle"} {
		conn := &fakeConn{d: engineDialect{name: name}}
		if _, err := buildSchema(conn).DAO().WithPredicate(HasPrefix("path", "a/")).Select(aID); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
		if conn.lastQuery != "" {
			t.Errorf("%s: sent %s", name, conn.lastQuery)
		}
	}
}

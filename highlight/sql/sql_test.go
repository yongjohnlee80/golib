package sql_test

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/highlight/sql"
	psql "github.com/yongjohnlee80/golib/parse/sql"
)

// The forwarding package keeps its first shape: definitions that carry a Highlighter, under the
// names, patterns, aliases and priorities parse/sql gives them.
func TestTheDeprecatedDefinitionsKeepTheirShape(t *testing.T) {
	old, now := sql.Definitions(), psql.Definitions()
	if len(old) != len(now) {
		t.Fatalf("%d definitions, parse/sql has %d", len(old), len(now))
	}
	for i := range old {
		if old[i].Highlighter == nil || old[i].SourceFactory != nil {
			t.Errorf("%s: want a Highlighter and no SourceFactory, as before", old[i].Name)
		}
		o, n := old[i], now[i]
		if o.Name != n.Name || !reflect.DeepEqual(o.Extensions, n.Extensions) ||
			!reflect.DeepEqual(o.Aliases, n.Aliases) || o.Priority != n.Priority {
			t.Errorf("definition %d: %+v, parse/sql has %+v", i, o, n)
		}
	}
	if sql.PostgreSQL != psql.PostgreSQLDialect || sql.SQLite != psql.SQLiteDialect || sql.MySQL != psql.MySQLDialect {
		t.Error("the dialect constants differ from parse/sql's")
	}
}

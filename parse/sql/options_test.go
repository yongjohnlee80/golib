package sql_test

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/parse/sql"
)

// Each option sets exactly the field it names, so New and the legacy struct literal build the same
// splitter and the two read a script identically.
func TestNew_MatchesTheLegacyFields(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		opts   []sql.Option
		legacy sql.SQL
	}{
		"none":     {nil, sql.SQL{}},
		"mysql":    {[]sql.Option{sql.Backticks()}, sql.SQL{Backticks: true}},
		"postgres": {[]sql.Option{sql.DollarQuotes(), sql.NestedBlockComments(), sql.EStringEscapes()}, sql.SQL{DollarQuotes: true, NestedBlockComments: true, EStringEscapes: true}},
		"sqlite":   {[]sql.Option{sql.TriggerBodies(), nil}, sql.SQL{TriggerBodies: true}},
	} {
		got := sql.New(tc.opts...)
		if got != tc.legacy {
			t.Errorf("%s: New = %+v, want %+v", name, got, tc.legacy)
		}
		src := []byte("SELECT $$a;b$$; CREATE TRIGGER t BEGIN SELECT 1; END; SELECT `x;y`;")
		a, errA := got.Parse(src)
		b, errB := tc.legacy.Parse(src)
		if !reflect.DeepEqual(a, b) || (errA == nil) != (errB == nil) {
			t.Errorf("%s: New and the struct literal parse differently", name)
		}
	}
}
